package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func init() {
	startDetachedCI = func(*Context, *tracker.Project, string, string, bool) (string, error) {
		panic("test started a real CI run; stub startDetachedCI")
	}
}

const ciFailCmd = `if [ -f fail.flag ]; then echo '--- FAIL: TestBroken'; exit 1; fi`

// makeCIFixture is a project whose root is a throwaway git repo on main, with
// issue ui/a (done) and a workflow whose ci: block fails while fail.flag is
// committed. ciBlock "" leaves CI off.
func makeCIFixture(t *testing.T, ciBlock string) (*tracker.Project, string) {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "ci@test")
	git("config", "user.name", "ci")
	git("config", "commit.gpgsign", "false")
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "issues/\n.worktrees/\n.agent-logs/\n")
	write("workflow.yaml", "statuses:\n  - name: \"idea\"\n  - name: \"in progress\"\n  - name: \"done\"\ntransitions:\n  - from: \"in progress\"\n    to: \"done\"\n"+ciBlock)
	write("issues/UI/a.md", "---\ntitle: \"a\"\nstatus: \"done\"\nsystem: \"UI\"\n---\n\nBody.\n")
	write("issues/UI/b.md", "---\ntitle: \"b\"\nstatus: \"in progress\"\nsystem: \"UI\"\n---\n\nBody.\n")
	git("add", ".")
	git("commit", "-q", "-m", "initial")
	proj := &tracker.Project{Name: "Test", Slug: "test", IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root}
	return proj, root
}

func gitIn(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// landBranch commits on work/ui/a (adding or removing fail.flag) and
// fast-forwards main to it, the way agents ship.
func landBranch(t *testing.T, root string, addFail bool) {
	t.Helper()
	gitIn(t, root, "checkout", "-q", "-B", "work/ui/a")
	if addFail {
		if err := os.WriteFile(filepath.Join(root, "fail.flag"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, root, "add", "fail.flag")
	} else {
		gitIn(t, root, "rm", "-q", "fail.flag")
	}
	gitIn(t, root, "commit", "-q", "-m", "ui/a work")
	gitIn(t, root, "checkout", "-q", "main")
	gitIn(t, root, "merge", "-q", "--ff-only", "work/ui/a")
}

func comments(t *testing.T, path string) []tracker.Comment {
	t.Helper()
	cs, err := tracker.LoadComments(path)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func ciBlockFor(onRed string) string {
	return "ci:\n  nice: 0\n  on_red: " + onRed + "\n  run:\n    - cmd: \"" + strings.ReplaceAll(ciFailCmd, `"`, `\"`) + "\"\n      failures: '--- FAIL: (\\S+)'\n    - cmd: \"exit 1\"\n      tier: slow\n"
}

func TestCIStatusNotConfigured(t *testing.T) {
	proj, _ := makeCIFixture(t, "")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "ci: not configured — add a ci: block to workflow.yaml")
	ctx, stdout, _ = newTestContext(proj, true)
	if err := runCI(ctx, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), `"configured": false`)
	ctx, _, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("run without ci: %v", err)
	}
}

func TestCIRunRedGreenReporting(t *testing.T) {
	proj, root := makeCIFixture(t, ciBlockFor("mark"))
	issuePath := filepath.Join(root, "issues", "UI", "a.md")
	var nudges []string
	origLive, origPaste := sessionLive, pasteIntoSession
	defer func() { sessionLive, pasteIntoSession = origLive, origPaste }()
	sessionLive = func(*tracker.Project) func(string) bool {
		return func(n string) bool { return n == tracker.LeadSessionName(proj) }
	}
	pasteIntoSession = func(_ *tracker.Project, session, text, _ string, _ string) agentrun.Response {
		nudges = append(nudges, session+": "+text)
		return agentrun.Response{Status: "sent"}
	}

	// First run: green, nobody hears about it.
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatalf("green run: %v\n%s", err, stdout.String())
	}
	assertContains(t, stdout.String(), "main green at ")
	if len(nudges) != 0 {
		t.Errorf("green first run nudged: %v", nudges)
	}

	// ui/a lands and breaks main.
	landBranch(t, root, true)
	ctx, stdout, _ = newTestContext(proj, false)
	err := runCI(ctx, []string{"run"})
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) || codeErr.Code != 1 {
		t.Fatalf("red run must exit 1: %v", err)
	}
	assertContains(t, stdout.String(), "main red since ")
	assertContains(t, stdout.String(), "TestBroken")
	cs := comments(t, issuePath)
	if len(cs) != 1 || !strings.HasPrefix(cs[0].Text, "ci: local CI red on main at ") || cs[0].Source != "ci" {
		t.Fatalf("comments = %+v", cs)
	}
	assertContains(t, cs[0].Text, "(TestBroken)")
	assertContains(t, cs[0].Text, "Landed since the last green run (")
	assertContains(t, cs[0].Text, "ui/a (work/ui/a)")
	assertContains(t, cs[0].Text, "issue-cli --project test ci status")
	if len(nudges) != 1 || !strings.Contains(nudges[0], "[from ci] local CI red on main") || !strings.Contains(nudges[0], "Merged since green: ui/a") {
		t.Fatalf("nudges = %v", nudges)
	}

	// status, text and JSON.
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "main red since ") {
		t.Errorf("status must lead with the one-liner:\n%s", out)
	}
	assertContains(t, out, "Landed since green")
	assertContains(t, out, "Recent runs:")
	ctx, stdout, _ = newTestContext(proj, true)
	if err := runCI(ctx, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	var js struct {
		Configured bool
		Line       string
		State      ci.State
		Runs       []ci.Run
	}
	if err := json.Unmarshal(stdout.Bytes(), &js); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if !js.Configured || js.State.State != ci.Red || len(js.State.Blamed) != 1 || len(js.Runs) != 2 {
		t.Errorf("json = %+v", js)
	}

	// Re-run, same failure: no new comment, no new nudge.
	ctx, _, _ = newTestContext(proj, false)
	_ = runCI(ctx, []string{"run"})
	if got := len(comments(t, issuePath)); got != 1 || len(nudges) != 1 {
		t.Errorf("still red re-commented (%d comments) or re-nudged (%v)", got, nudges)
	}

	// The fix lands: green again, the lead hears.
	landBranch(t, root, false)
	ctx, _, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatalf("fixed run: %v", err)
	}
	if len(nudges) != 2 || !strings.Contains(nudges[1], "local CI green again on main") {
		t.Errorf("nudges = %v", nudges)
	}
	if st, _ := ci.ReadState(root); st.State != ci.Green || len(st.Blamed) != 0 {
		t.Errorf("state = %+v", st)
	}

	// The slow tier runs on request and leaves main green.
	ctx, stdout, _ = newTestContext(proj, false)
	err = runCI(ctx, []string{"run", "--tier", "slow"})
	if !errors.As(err, &codeErr) {
		t.Errorf("red slow tier must exit 1: %v", err)
	}
	assertContains(t, stdout.String(), "main green at ")
	assertContains(t, stdout.String(), "tier slow: red")
	ctx, stdout, _ = newTestContext(proj, false)
	_ = runCI(ctx, []string{"status", "--tier", "slow"})
	assertContains(t, stdout.String(), "tier slow: red at ")
}

func TestCIRunFileIssueOnRed(t *testing.T) {
	proj, root := makeCIFixture(t, ciBlockFor("file_issue"))
	ctx, _, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatal(err)
	}
	landBranch(t, root, true)
	ctx, _, stderr := newTestContext(proj, false)
	_ = runCI(ctx, []string{"run"})
	st, _ := ci.ReadState(root)
	if st.RedIssue == "" || !strings.HasPrefix(st.RedIssue, "ui/fix-red-ci-on-main-at-") {
		t.Fatalf("red issue = %q (stderr %s)", st.RedIssue, stderr.String())
	}
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	filed := tracker.FindIssue(issues, st.RedIssue)
	if filed == nil {
		t.Fatal("filed issue not found")
	}
	cs := comments(t, filed.FilePath)
	if len(cs) != 1 || !strings.Contains(cs[0].Text, "ci: filed by local CI") || !strings.Contains(cs[0].Text, "ui/a (work/ui/a)") {
		t.Errorf("filed issue comments = %+v", cs)
	}
	blamed := comments(t, filepath.Join(root, "issues", "UI", "a.md"))
	if len(blamed) != 1 || !strings.Contains(blamed[0].Text, "tracked in "+st.RedIssue) {
		t.Errorf("blamed issue must link the bugfix issue: %+v", blamed)
	}

	// Still red: the same issue is updated, no second issue.
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "another merge")
	ctx, _, _ = newTestContext(proj, false)
	_ = runCI(ctx, []string{"run"})
	issues, _ = tracker.LoadIssues(proj.IssueDir)
	if n := len(issues); n != 3 {
		t.Errorf("a second red run filed another issue: %d issues", n)
	}
	if cs := comments(t, filed.FilePath); len(cs) != 2 || !strings.Contains(cs[1].Text, "still red") {
		t.Errorf("filed issue not updated: %+v", cs)
	}
}

func TestCIRunDetachAndBusy(t *testing.T) {
	proj, root := makeCIFixture(t, ciBlockFor("mark"))
	orig := startDetachedCI
	defer func() { startDetachedCI = orig }()
	var gotTier string
	var gotUntested bool
	startDetachedCI = func(_ *Context, _ *tracker.Project, r, tier string, untested bool) (string, error) {
		if r != root {
			t.Errorf("root = %s, want %s", r, root)
		}
		gotTier, gotUntested = tier, untested
		return "tmux window test:ci-test", nil
	}
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"run", "--detach", "--tier", "slow"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ CI run started in tmux window test:ci-test")
	if gotTier != "slow" || gotUntested {
		t.Errorf("tier=%q untested=%v", gotTier, gotUntested)
	}

	// A live lock: run says so and starts nothing.
	if err := os.MkdirAll(ci.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := `{"pid": ` + itoa(os.Getpid()) + `, "sha": "abcdef1234567", "tier": "default"}`
	if err := os.WriteFile(ci.LockPath(root), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "already running at abcdef1")
	ctx, stdout, _ = newTestContext(proj, false)
	_ = runCI(ctx, []string{"status"})
	assertContains(t, stdout.String(), "· running at abcdef1")
	ctx, _, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"reset"}); err == nil {
		t.Error("reset must refuse while a run is live")
	}
	os.Remove(ci.LockPath(root))
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"reset"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ Removed")
}

func TestCIWatchOnce(t *testing.T) {
	proj, root := makeCIFixture(t, ciBlockFor("mark"))
	orig := startDetachedCI
	defer func() { startDetachedCI = orig }()
	spawns := 0
	startDetachedCI = func(_ *Context, _ *tracker.Project, _ string, _ string, untested bool) (string, error) {
		if !untested {
			t.Error("watch must spawn with --if-untested")
		}
		spawns++
		return "tmux", nil
	}
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"watch", "--once"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "run started")
	if spawns != 1 {
		t.Errorf("spawns = %d", spawns)
	}
	// Once the head is tested, watch has nothing to do.
	ctx, _, _ = newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatal(err)
	}
	ctx, stdout, _ = newTestContext(proj, false)
	_ = runCI(ctx, []string{"watch", "--once"})
	assertContains(t, stdout.String(), "nothing to run")
	_ = root
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{"/usr/bin/issue-cli": "/usr/bin/issue-cli", "--if-untested": "--if-untested", "a b": "'a b'", "it's": `'it'\''s'`, "": "''"} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestCILineForAgents(t *testing.T) {
	proj, root := makeCIFixture(t, ciBlockFor("mark"))
	ctx, _, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatal(err)
	}
	landBranch(t, root, true)
	ctx, _, _ = newTestContext(proj, false)
	_ = runCI(ctx, []string{"run"})

	// start: the red line, marked as inherited.
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runStart(ctx, []string{"ui/b"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertContains(t, stdout.String(), "Local CI: main red since ")
	assertContains(t, stdout.String(), ": TestBroken — inherited from main, not caused by your branch")

	// The dispatch prompt carries the same line and the never-wait rule.
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runDispatchPrompt(ctx, []string{"ui/b"}); err != nil {
		t.Fatalf("dispatch-prompt: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "## Local CI\n\nLocal CI: main red since ")
	assertContains(t, out, "Never wait for it")
	assertContains(t, out, "issue-cli --project test ci status")

	// transition --to done: CI runs without the agent.
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runTransition(ctx, []string{"ui/b", "--to", "done"}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	assertContains(t, stdout.String(), "Local CI tests main at ")
	assertContains(t, stdout.String(), "don't wait for it, you are done")
}

func TestCILineAbsentWithoutCIBlock(t *testing.T) {
	proj, _ := makeCIFixture(t, "")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runStart(ctx, []string{"ui/b"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertNotContains(t, stdout.String(), "Local CI")
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runDispatchPrompt(ctx, []string{"ui/b"}); err != nil {
		t.Fatal(err)
	}
	assertNotContains(t, stdout.String(), "Local CI")
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runTransition(ctx, []string{"ui/b", "--to", "done"}); err != nil {
		t.Fatal(err)
	}
	assertNotContains(t, stdout.String(), "Local CI")
}
