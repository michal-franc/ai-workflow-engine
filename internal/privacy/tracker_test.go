package privacy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// gitEnv gives every git call in these tests an identity and no signing.
func gitEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@test")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@test")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// codeRepo is a checkout at <tmp>/proj with one code commit and an
// untracked issues/ folder; the tracker git folder goes to <tmp>/.trackers.
func codeRepo(t *testing.T) string {
	t.Helper()
	gitEnv(t)
	root := filepath.Join(t.TempDir(), "proj")
	write(t, filepath.Join(root, "main.go"), "package main\n")
	run(t, root, "git", "init", "-q", "-b", "main")
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-q", "-m", "code")
	write(t, filepath.Join(root, "issues", "UI", "a.md"), "---\ntitle: A\n---\n")
	write(t, filepath.Join(root, "retros", "r.md"), "retro\n")
	return root
}

func bare(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tracker.git")
	run(t, t.TempDir(), "git", "init", "-q", "--bare", "-b", "main", dir)
	return dir
}

func newTracker(root string) Tracker {
	return Tracker{Root: root, GitDir: DefaultGitDir(root, "proj"), Paths: tracker.DefaultWorkPaths}
}

func TestDefaultGitDirSitsBesideTheCheckout(t *testing.T) {
	if got := DefaultGitDir("/home/u/Work/issue-viewer", "issue-viewer"); got != "/home/u/Work/.trackers/issue-viewer.git" {
		t.Errorf("DefaultGitDir = %s", got)
	}
}

func TestInitCreatesTheGitFolderOutsideTheCheckoutAndIgnoresTheWorkPaths(t *testing.T) {
	root := codeRepo(t)
	remote := bare(t)
	tr := newTracker(root)
	res, err := tr.Init(remote)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Adopted || !res.RemoteSet {
		t.Errorf("result = %+v", res)
	}
	if strings.HasPrefix(tr.GitDir, root) {
		t.Errorf("git folder %s is inside the checkout", tr.GitDir)
	}
	cfg := func(key string) string { return run(t, root, "git", "--git-dir="+tr.GitDir, "config", key) }
	if cfg("core.worktree") != root || cfg("status.showUntrackedFiles") != "no" || cfg("core.bare") != "false" {
		t.Errorf("config: worktree=%s untracked=%s bare=%s", cfg("core.worktree"), cfg("status.showUntrackedFiles"), cfg("core.bare"))
	}
	ignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	for _, p := range tracker.DefaultWorkPaths {
		if !strings.Contains(string(ignore), "/"+p+"/\n") {
			t.Errorf(".gitignore lacks /%s/:\n%s", p, ignore)
		}
	}
	if out := run(t, root, "git", "status", "--porcelain", "--", "issues", "retros"); out != "" {
		t.Errorf("code repo still sees work files: %q", out)
	}

	// Run again: adopts, adds nothing.
	res, err = tr.Init(remote)
	if err != nil || !res.Adopted || len(res.Ignored) != 0 || res.RemoteSet {
		t.Errorf("second init = %+v, %v", res, err)
	}
}

func TestInitReportsWorkPathsTheCodeRepoTracksWithoutUntrackingThem(t *testing.T) {
	root := codeRepo(t)
	run(t, root, "git", "add", "issues")
	run(t, root, "git", "commit", "-q", "-m", "issues in code")
	res, err := newTracker(root).Init("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Tracked, ",") != "issues" {
		t.Errorf("Tracked = %v", res.Tracked)
	}
	if ls := run(t, root, "git", "ls-files", "issues"); ls == "" {
		t.Error("Init untracked issues/ in the code repo; it must only report it")
	}
}

func TestInitRefusesAGitFolderOfAnotherCheckout(t *testing.T) {
	root := codeRepo(t)
	tr := newTracker(root)
	if _, err := tr.Init(""); err != nil {
		t.Fatal(err)
	}
	other := codeRepo(t)
	_, err := Tracker{Root: other, GitDir: tr.GitDir, Paths: tr.Paths}.Init("")
	if err == nil || !strings.Contains(err.Error(), "belongs to") {
		t.Errorf("err = %v", err)
	}
}

func TestSyncCommitsOnlyWhenAWorkPathChangedAndPushes(t *testing.T) {
	root := codeRepo(t)
	remote := bare(t)
	tr := newTracker(root)
	if _, err := tr.Init(remote); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	res, err := tr.Sync(now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed != 2 || !res.Pushed {
		t.Errorf("first sync = %+v", res)
	}
	if msg := run(t, root, "git", "--git-dir="+remote, "log", "-1", "--format=%s", "main"); msg != "tracker: 2 files" {
		t.Errorf("remote head = %q", msg)
	}
	if files := run(t, root, "git", "--git-dir="+remote, "ls-tree", "-r", "--name-only", "main"); strings.Contains(files, "main.go") {
		t.Errorf("tracker took a code file:\n%s", files)
	}

	res, err = tr.Sync(now)
	if err != nil || res.Committed != 0 || res.Pushed {
		t.Errorf("idle sync = %+v, %v", res, err)
	}

	write(t, filepath.Join(root, "issues", "UI", "a.md"), "---\ntitle: A2\n---\n")
	os.Remove(filepath.Join(root, "retros", "r.md"))
	res, err = tr.Sync(now)
	if err != nil || res.Committed != 2 || !res.Pushed {
		t.Errorf("change sync = %+v, %v", res, err)
	}
	st := tr.ReadSync()
	if st == nil || !st.OK || !st.LastOK.Equal(now) {
		t.Errorf("sync record = %+v", st)
	}
}

func TestSyncPullsAnotherMachinesChangesFirst(t *testing.T) {
	root := codeRepo(t)
	remote := bare(t)
	tr := newTracker(root)
	tr.Init(remote)
	if _, err := tr.Sync(time.Now()); err != nil {
		t.Fatal(err)
	}
	// Another machine: its own checkout and tracker on the same remote.
	other := codeRepo(t)
	os.RemoveAll(filepath.Join(other, "issues"))
	os.RemoveAll(filepath.Join(other, "retros"))
	ot := Tracker{Root: other, GitDir: filepath.Join(filepath.Dir(other), "other.git"), Paths: tr.Paths}
	ot.Init(remote)
	if _, err := ot.Sync(time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(other, "sessions", "s.md"), "handoff\n")
	if _, err := ot.Sync(time.Now()); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(root, "issues", "UI", "b.md"), "---\ntitle: B\n---\n")
	res, err := tr.Sync(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pulled || !res.Pushed {
		t.Errorf("sync = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "s.md")); err != nil {
		t.Errorf("the other machine's session note did not arrive: %v", err)
	}
}

func TestSyncConflictKeepsTheLocalCommitAndRecordsTheError(t *testing.T) {
	root := codeRepo(t)
	remote := bare(t)
	tr := newTracker(root)
	tr.Init(remote)
	tr.Sync(time.Now())
	other := codeRepo(t)
	os.RemoveAll(filepath.Join(other, "issues"))
	os.RemoveAll(filepath.Join(other, "retros"))
	ot := Tracker{Root: other, GitDir: filepath.Join(filepath.Dir(other), "other.git"), Paths: tr.Paths}
	ot.Init(remote)
	ot.Sync(time.Now())
	write(t, filepath.Join(other, "issues", "UI", "a.md"), "theirs\n")
	if _, err := ot.Sync(time.Now()); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(root, "issues", "UI", "a.md"), "ours\n")
	_, err := tr.Sync(time.Now())
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "issues", "UI", "a.md"))
	if string(data) != "ours\n" {
		t.Errorf("checkout left half-merged: %q", data)
	}
	if msg := run(t, root, "git", "--git-dir="+tr.GitDir, "log", "-1", "--format=%s"); !strings.HasPrefix(msg, "tracker: ") {
		t.Errorf("local commit lost, head = %q", msg)
	}
	if st := tr.ReadSync(); st == nil || st.OK || st.Error == "" {
		t.Errorf("sync record = %+v", st)
	}
}

func TestWorkBadgeStates(t *testing.T) {
	now := time.Now()
	t.Run("red when the code repo tracks work files, even without the feature", func(t *testing.T) {
		root := codeRepo(t)
		run(t, root, "git", "add", "issues", "retros")
		run(t, root, "git", "commit", "-q", "-m", "work in code")
		b := WorkBadge(Input{Name: "Proj", Root: root, Slug: "proj", Now: now})
		if b.Level != Red || b.Label != "Work in code repo" || !b.NotSetUp {
			t.Errorf("badge = %+v", b)
		}
		if !strings.Contains(b.Sentence, "issues/ (1) and retros/ (1) are tracked by Proj") || b.Where != "issues 1 · retros 1" {
			t.Errorf("sentence = %q, where = %q", b.Sentence, b.Where)
		}
	})
	t.Run("amber when ignored and backed up nowhere", func(t *testing.T) {
		root := codeRepo(t)
		b := WorkBadge(Input{Root: root, Slug: "proj", Now: now})
		if b.Level != Amber || b.Label != "No backup" || !strings.Contains(b.Sentence, "issues/ and retros/ are kept out of git") {
			t.Errorf("badge = %+v", b)
		}
	})
	t.Run("grey when public by choice", func(t *testing.T) {
		root := codeRepo(t)
		run(t, root, "git", "add", "issues")
		run(t, root, "git", "commit", "-q", "-m", "work in code")
		cfg := &tracker.PrivacyConfig{WorkFiles: "in_repo", Decided: "2026-10-24, owner: \"keep them\""}
		b := WorkBadge(Input{Root: root, Slug: "proj", Cfg: cfg, Now: now})
		if b.Level != Grey || b.Label != "Work public" || !strings.Contains(b.Sentence, "keep them") {
			t.Errorf("badge = %+v", b)
		}
	})
	t.Run("green with a tracker, amber when its sync fails", func(t *testing.T) {
		root := codeRepo(t)
		remote := bare(t)
		cfg := &tracker.PrivacyConfig{WorkFiles: "tracker", Decided: "x"}
		in := Input{Root: root, Slug: "proj", Cfg: cfg, Now: time.Now()}
		tr := in.TrackerFor()
		tr.Init(remote)
		if _, err := tr.Sync(time.Now()); err != nil {
			t.Fatal(err)
		}
		b := WorkBadge(in)
		if b.Level != Green || b.Label != "Work private" || !strings.Contains(b.Sentence, "tracker, last backup just now") {
			t.Errorf("badge = %+v", b)
		}
		os.RemoveAll(remote)
		write(t, filepath.Join(root, "issues", "x.md"), "x\n")
		if _, err := tr.Sync(time.Now()); err == nil {
			t.Fatal("sync to a missing remote succeeded")
		}
		b = WorkBadge(in)
		if b.Level != Amber || b.Label != "Backup failing" || b.Fix != "issue-cli tracker sync" {
			t.Errorf("badge = %+v", b)
		}
	})
	t.Run("grey not set up with no work files at all", func(t *testing.T) {
		gitEnv(t)
		root := t.TempDir()
		run(t, root, "git", "init", "-q")
		b := WorkBadge(Input{Root: root, Slug: "x", Now: now})
		if b.Level != Grey || b.Label != "Not set up" || !b.NotSetUp {
			t.Errorf("badge = %+v", b)
		}
	})
}

func TestRepoNameFromURL(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:michal-franc/ai-workflow-engine-tracker.git": "ai-workflow-engine-tracker",
		"https://github.com/o/r":                                     "r",
		"/tmp/x/tracker.git":                                         "tracker",
	} {
		if got := RepoNameFromURL(url); got != want {
			t.Errorf("RepoNameFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}
