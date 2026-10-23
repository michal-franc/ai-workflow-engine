package ci

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// newRepo makes a git repo on branch main with one commit and returns its
// root. Real git, throwaway directory.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "git", "init", "-q", "-b", "main")
	run(t, root, "git", "config", "user.email", "ci@test")
	run(t, root, "git", "config", "user.name", "ci")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(root, ".gitignore"), "build.out\n.worktrees/\n.ci/\n")
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-q", "-m", "initial")
	return root
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, root, msg string) string {
	t.Helper()
	run(t, root, "git", "commit", "-q", "--allow-empty", "-m", msg)
	return run(t, root, "git", "rev-parse", "HEAD")
}

func cfg(cmds ...tracker.CICommand) *tracker.CIConfig {
	zero := 0
	return &tracker.CIConfig{Run: cmds, Nice: &zero}
}

func cmd(s string) tracker.CICommand { return tracker.CICommand{Cmd: s} }

func mustState(t *testing.T, root string) *State {
	t.Helper()
	st, err := ReadState(root)
	if err != nil || st == nil {
		t.Fatalf("state: %v, %v", st, err)
	}
	return st
}

func TestExecuteGreenRecordsRunAndState(t *testing.T) {
	root := newRepo(t)
	head := run(t, root, "git", "rev-parse", "HEAD")
	var out strings.Builder
	runs, _, err := Execute(Options{Root: root, Config: cfg(cmd("echo hello")), Out: &out})
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
	r := runs[0]
	if r.State != Green || r.Sha != head || r.Branch != "main" || len(r.Commands) != 1 || r.Commands[0].Exit != 0 {
		t.Fatalf("run = %+v", r)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("live output missing command output: %q", out.String())
	}
	st := mustState(t, root)
	if st.State != Green || st.GreenSha != head || st.LastSha != head {
		t.Errorf("state = %+v", st)
	}
	for _, p := range []string{filepath.Join(RunsDir(root), head+".json"), filepath.Join(root, r.Log), filepath.Join(Dir(root), ".gitignore")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if !isWorktree(WorktreePath(root)) {
		t.Error("CI worktree not created")
	}
	if _, err := os.Stat(LockPath(root)); !os.IsNotExist(err) {
		t.Error("lock not released")
	}
	if got := RecentRuns(root, 5); len(got) != 1 || got[0].Sha != head {
		t.Errorf("RecentRuns = %+v", got)
	}
}

func TestExecuteRedStopsAtFirstFailureAndPullsTestNames(t *testing.T) {
	root := newRepo(t)
	c := tracker.CICommand{Cmd: "echo '--- FAIL: TestA'; echo '--- FAIL: TestB'; exit 3", Failures: `--- FAIL: (\S+)`}
	runs, _, err := Execute(Options{Root: root, Config: cfg(cmd("true"), c, cmd("echo never"))})
	if err != nil {
		t.Fatal(err)
	}
	r := runs[0]
	if r.State != Red || r.Failed != c.Cmd || len(r.Commands) != 2 || r.Commands[1].Exit != 3 {
		t.Fatalf("run = %+v", r)
	}
	if strings.Join(r.Failures, ",") != "TestA,TestB" {
		t.Errorf("failures = %v", r.Failures)
	}
	st := mustState(t, root)
	if st.State != Red || st.RedSince != r.Sha || st.Failed != c.Cmd || len(st.LogTail) == 0 {
		t.Errorf("state = %+v", st)
	}
	if line := OneLine(st, nil, false, "", time.Now()); !strings.Contains(line, "main red since "+Short(r.Sha)) || !strings.Contains(line, "TestA, TestB") {
		t.Errorf("OneLine = %q", line)
	}
}

func TestExecuteTimeoutKillsTheCommand(t *testing.T) {
	root := newRepo(t)
	c := cfg(cmd("sleep 30"))
	c.Timeout = "1s"
	start := time.Now()
	runs, _, err := Execute(Options{Root: root, Config: c})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("timeout took %s", time.Since(start))
	}
	r := runs[0]
	if r.State != Red || !r.Commands[0].TimedOut || !strings.Contains(r.Failed, "timeout after 1s") {
		t.Errorf("run = %+v", r)
	}
}

func TestExecuteCoalescesCommitsThatLandDuringARun(t *testing.T) {
	root := newRepo(t)
	marker := filepath.Join(t.TempDir(), "landed")
	t.Setenv("CI_TEST_ROOT", root)
	t.Setenv("CI_TEST_MARKER", marker)
	// The first run lands two merges on main while it runs; the next run
	// must test only the newest head.
	land := `if [ ! -f "$CI_TEST_MARKER" ]; then touch "$CI_TEST_MARKER"; git -C "$CI_TEST_ROOT" commit -q --allow-empty -m one; git -C "$CI_TEST_ROOT" commit -q --allow-empty -m two; fi`
	runs, _, err := Execute(Options{Root: root, Config: cfg(cmd(land))})
	if err != nil {
		t.Fatal(err)
	}
	head := run(t, root, "git", "rev-parse", "HEAD")
	if len(runs) != 2 || runs[1].Sha != head || runs[1].Subject != "two" {
		t.Fatalf("runs = %d, last = %+v", len(runs), runs[len(runs)-1])
	}
	if st := mustState(t, root); st.LastSha != head {
		t.Errorf("LastSha = %s, want %s", st.LastSha, head)
	}
}

func TestExecuteOnlyUntestedSkipsTestedHead(t *testing.T) {
	root := newRepo(t)
	if _, _, err := Execute(Options{Root: root, Config: cfg(cmd("true"))}); err != nil {
		t.Fatal(err)
	}
	runs, _, err := Execute(Options{Root: root, Config: cfg(cmd("true")), OnlyUntested: true})
	if err != nil || len(runs) != 0 {
		t.Errorf("watcher run re-tested the head: %d runs, %v", len(runs), err)
	}
	runs, _, err = Execute(Options{Root: root, Config: cfg(cmd("true"))})
	if err != nil || len(runs) != 1 {
		t.Errorf("manual run should re-test the head: %d runs, %v", len(runs), err)
	}
}

func TestExecuteLock(t *testing.T) {
	root := newRepo(t)
	if err := ensureDir(root); err != nil {
		t.Fatal(err)
	}
	writeFile(t, LockPath(root), `{"pid": 4242, "sha": "abcdef1234", "tier": "default"}`)

	orig := pidAlive
	defer func() { pidAlive = orig }()
	pidAlive = func(pid int) bool { return pid == 4242 }
	_, held, err := Execute(Options{Root: root, Config: cfg(cmd("true"))})
	if !errors.Is(err, ErrBusy) || held == nil || held.PID != 4242 {
		t.Fatalf("live lock: held=%+v err=%v", held, err)
	}
	if err := ResetWorktree(root); !errors.Is(err, ErrBusy) {
		t.Errorf("reset while locked: %v", err)
	}

	pidAlive = func(int) bool { return false }
	runs, _, err := Execute(Options{Root: root, Config: cfg(cmd("true"))})
	if err != nil || len(runs) != 1 {
		t.Errorf("stale lock should be replaced: %d runs, %v", len(runs), err)
	}
}

func TestWorktreePersistsIgnoredOutputAndCleansTheRest(t *testing.T) {
	root := newRepo(t)
	build := cfg(cmd("test -f build.out || echo built > build.out; touch junk.txt"))
	if _, _, err := Execute(Options{Root: root, Config: build}); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "next")
	check := cfg(cmd("test -f build.out && test ! -f junk.txt"))
	runs, _, err := Execute(Options{Root: root, Config: check})
	if err != nil || runs[0].State != Green {
		t.Fatalf("ignored output should survive and untracked files go: %+v %v", runs, err)
	}
	if err := ResetWorktree(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(WorktreePath(root)); !os.IsNotExist(err) {
		t.Error("reset left the worktree")
	}
	runs, _, err = Execute(Options{Root: root, Config: cfg(cmd("test ! -f build.out"))})
	if err != nil || runs[0].State != Green {
		t.Errorf("after reset the worktree starts clean: %+v %v", runs, err)
	}
}

func TestPlainDirectoryIsAnErrorNotTheMainCheckout(t *testing.T) {
	root := newRepo(t)
	if err := os.MkdirAll(WorktreePath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "precious.txt"), "untracked work")
	runs, _, err := Execute(Options{Root: root, Config: cfg(cmd("true"))})
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].State != Error || !strings.Contains(runs[0].Error, "ci reset") {
		t.Errorf("run = %+v", runs[0])
	}
	if _, err := os.Stat(filepath.Join(root, "precious.txt")); err != nil {
		t.Error("the main checkout was cleaned")
	}
	st := mustState(t, root)
	if st.State != Error || len(st.Blamed) != 0 {
		t.Errorf("state = %+v", st)
	}
	if line := OneLine(st, nil, false, "", time.Now()); !strings.Contains(line, "CI can't run") {
		t.Errorf("OneLine = %q", line)
	}
}

func TestExecuteTierRunsDefaultPlusTierOnce(t *testing.T) {
	root := newRepo(t)
	c := cfg(cmd("echo fast"), tracker.CICommand{Cmd: "echo slow; exit 1", Tier: "slow"})
	runs, _, err := Execute(Options{Root: root, Config: c})
	if err != nil || len(runs[0].Commands) != 1 || runs[0].State != Green {
		t.Fatalf("default tier must skip slow commands: %+v %v", runs, err)
	}
	runs, _, err = Execute(Options{Root: root, Config: c, Tier: "slow"})
	if err != nil || len(runs) != 1 || len(runs[0].Commands) != 2 || runs[0].State != Red {
		t.Fatalf("slow tier: %+v %v", runs, err)
	}
	st := mustState(t, root)
	if st.State != Green || st.Tiers["slow"].State != Red {
		t.Errorf("a slow-tier red must not turn the branch red: %+v", st)
	}
	if _, _, err := Execute(Options{Root: root, Config: c, Tier: "nope"}); err == nil {
		t.Error("unknown tier accepted")
	}
}

func TestBlame(t *testing.T) {
	root := newRepo(t)
	green := run(t, root, "git", "rev-parse", "HEAD")

	// work/ui/a: fast-forwarded into main.
	run(t, root, "git", "checkout", "-q", "-b", "work/ui/a")
	a := commit(t, root, "a work")
	run(t, root, "git", "checkout", "-q", "main")
	run(t, root, "git", "merge", "-q", "--ff-only", "work/ui/a")
	// feature-y: merged --no-ff, then deleted (a pulled cloud PR).
	run(t, root, "git", "checkout", "-q", "-b", "feature-y")
	commit(t, root, "y work")
	run(t, root, "git", "checkout", "-q", "main")
	run(t, root, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'feature-y'", "feature-y")
	run(t, root, "git", "branch", "-q", "-D", "feature-y")
	// A commit straight on main.
	direct := commit(t, root, "hotfix on main")
	head := run(t, root, "git", "rev-parse", "HEAD")

	issues := []*tracker.Issue{{Slug: "ui/a"}}
	got := blame(root, "main", green, head, issues)
	var names []string
	for _, m := range got {
		names = append(names, m.Branch+"|"+m.Slug+"|"+Short(m.Sha))
	}
	want := []string{"work/ui/a|ui/a|" + Short(a), "feature-y||", "||" + Short(direct)}
	if len(got) != 3 || names[0] != want[0] || !strings.HasPrefix(names[1], "feature-y||") || names[2] != want[2] {
		t.Fatalf("blame = %v, want like %v", names, want)
	}
	if s := Slugs(got); len(s) != 1 || s[0] != "ui/a" {
		t.Errorf("Slugs = %v", s)
	}
	if got := blame(root, "main", "", head, issues); got != nil {
		t.Errorf("no green yet must blame nothing: %v", got)
	}
}

func TestBlameRewoundBranchCountsFromMergeBase(t *testing.T) {
	root := newRepo(t)
	base := run(t, root, "git", "rev-parse", "HEAD")
	oldGreen := commit(t, root, "later rewound")
	run(t, root, "git", "reset", "-q", "--hard", base)
	run(t, root, "git", "checkout", "-q", "-b", "work/b")
	commit(t, root, "b work")
	run(t, root, "git", "checkout", "-q", "main")
	run(t, root, "git", "merge", "-q", "--ff-only", "work/b")
	head := run(t, root, "git", "rev-parse", "HEAD")
	got := blame(root, "main", oldGreen, head, []*tracker.Issue{{Slug: "b"}})
	if len(got) != 1 || got[0].Slug != "b" {
		t.Errorf("blame = %+v", got)
	}
}

func TestApplyRunStreaks(t *testing.T) {
	t0 := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	green := &Run{Sha: "g1", Branch: "main", Tier: defaultTier, State: Green, Finished: t0}
	st := applyRun(nil, green)
	red1 := &Run{Sha: "r1", Branch: "main", Tier: defaultTier, State: Red, Failed: "make validate", Merged: []Merged{{Branch: "work/x", Slug: "x", Sha: "r1"}}}
	st = applyRun(st, red1)
	if st.RedSince != "r1" || st.GreenSha != "g1" || len(st.Blamed) != 1 {
		t.Fatalf("first red: %+v", st)
	}
	st.RedIssue = "ci/fix"
	red2 := &Run{Sha: "r2", Branch: "main", Tier: defaultTier, State: Red, Failed: "make validate", Merged: []Merged{{Slug: "x"}, {Slug: "y"}}}
	st = applyRun(st, red2)
	if st.RedSince != "r1" || st.LastSha != "r2" || len(st.Blamed) != 2 || st.RedIssue != "ci/fix" {
		t.Fatalf("still red: %+v", st)
	}
	st = applyRun(st, &Run{Sha: "g2", Branch: "main", Tier: defaultTier, State: Green, Finished: t0.Add(time.Hour)})
	if st.State != Green || st.RedSince != "" || st.Blamed != nil || st.RedIssue != "" || st.GreenSha != "g2" {
		t.Fatalf("green again: %+v", st)
	}
}

func TestWatcher(t *testing.T) {
	root := newRepo(t)
	c := cfg(cmd("true"))
	var spawns int
	w := &Watcher{Spawn: func(string) error { spawns++; return nil }}

	if _, spawned, _ := w.Check(root, nil); spawned {
		t.Error("no ci: block must never spawn")
	}
	if _, spawned, err := w.Check(root, c); !spawned || err != nil {
		t.Fatalf("untested head must spawn: %v", err)
	}
	if _, spawned, _ := w.Check(root, c); spawned {
		t.Error("same head spawned twice within respawnAfter")
	}
	if _, _, err := Execute(Options{Root: root, Config: c}); err != nil {
		t.Fatal(err)
	}
	w.spawned = nil
	if _, spawned, _ := w.Check(root, c); spawned {
		t.Error("tested head must not spawn")
	}
	commit(t, root, "merge lands")
	orig := pidAlive
	defer func() { pidAlive = orig }()
	writeFile(t, LockPath(root), `{"pid": 4242}`)
	pidAlive = func(int) bool { return true }
	if _, spawned, _ := w.Check(root, c); spawned {
		t.Error("must not spawn while a run is live")
	}
	pidAlive = func(int) bool { return false }
	if _, spawned, _ := w.Check(root, c); !spawned || spawns != 2 {
		t.Errorf("new head with a stale lock must spawn (spawns=%d)", spawns)
	}
}

func TestOneLine(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if got := OneLine(nil, nil, false, "main", now); got != "main: no CI run yet" {
		t.Errorf("nil: %q", got)
	}
	st := &State{Branch: "main", State: Green, GreenSha: "abcdef123456", GreenAt: now.Add(-12 * time.Minute)}
	if got := OneLine(st, nil, false, "", now); got != "main green at abcdef1 (12m ago)" {
		t.Errorf("green: %q", got)
	}
	lock := &Lock{Sha: "123456789", Started: now.Add(-3 * time.Minute)}
	if got := OneLine(st, lock, true, "", now); !strings.HasSuffix(got, "· running at 1234567 (3m)") {
		t.Errorf("running: %q", got)
	}
	red := &State{Branch: "master", State: Red, RedSince: "def5678aa", Failed: "make check", Failures: []string{"A", "B", "C", "D", "E"}}
	if got := OneLine(red, nil, false, "", now); got != "master red since def5678: make check: A, B, C (+2)" {
		t.Errorf("red: %q", got)
	}
}

func TestCommandEnv(t *testing.T) {
	env := commandEnv([]string{"PATH=/bin", "ISSUE_VIEWER_ISSUE_SLUG=x", "TMUX=/tmp/s", "AGENT_NAME=a", "CI=false", "HOME=/h"})
	got := strings.Join(env, " ")
	if got != "PATH=/bin HOME=/h CI=true" {
		t.Errorf("env = %q", got)
	}
}
