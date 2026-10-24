package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const privacyTrackerBlock = "privacy:\n  work_files: tracker\n  decided: \"2026-10-24, owner: \\\"private please\\\"\"\n"

// makePrivacyFixture is a project at <tmp>/proj (so its default tracker git
// folder is <tmp>/.trackers/test.git) with untracked issues/ and the given
// workflow tail, plus a bare repo to use as the tracker's remote.
func makePrivacyFixture(t *testing.T, workflowTail string) (proj *tracker.Project, root, remote string) {
	t.Helper()
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@test", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@test"} {
		t.Setenv(k, v)
	}
	base := t.TempDir()
	root = filepath.Join(base, "proj")
	remote = filepath.Join(base, "tracker-remote.git")
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
	write("workflow.yaml", "statuses:\n  - name: \"idea\"\n  - name: \"done\"\ntransitions:\n  - from: \"idea\"\n    to: \"done\"\n"+workflowTail)
	write("main.go", "package main\n")
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "config", "commit.gpgsign", "false")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "code")
	write("issues/UI/a.md", "---\ntitle: \"a\"\nstatus: \"idea\"\nsystem: \"UI\"\n---\n\nBody.\n")
	gitIn(t, base, "init", "-q", "--bare", "-b", "main", remote)
	proj = &tracker.Project{Name: "Test", Slug: "test", IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root}
	return proj, root, remote
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestTrackerRefusesWithoutTheOwnersChoice(t *testing.T) {
	proj, _, remote := makePrivacyFixture(t, "")
	ctx, _, _ := newTestContext(proj, false)
	err := runTracker(ctx, []string{"init", "--remote", remote})
	if err == nil || !strings.Contains(err.Error(), "the owner has not chosen a private tracker") || !strings.Contains(err.Error(), "work_files: tracker") {
		t.Errorf("err = %v", err)
	}
}

func TestTrackerInitSyncAndTheGuard(t *testing.T) {
	proj, root, remote := makePrivacyFixture(t, privacyTrackerBlock)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTracker(ctx, []string{"init", "--remote", remote}); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(filepath.Dir(root), ".trackers", "test.git")
	out := stdout.String()
	for _, want := range []string{"✓ Created the tracker git folder " + gitDir, "✓ Remote origin: " + remote, ".gitignore now ignores issues, sessions, retros, bugs, .health/snapshots", "✓ Pre-push guard installed", "✓ Committed tracker: 1 file", "✓ Pushed main to origin"} {
		assertContains(t, out, want)
	}
	if got := gitOut(t, root, "--git-dir="+remote, "log", "-1", "--format=%s", "main"); got != "tracker: 1 file" {
		t.Errorf("remote head = %q", got)
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runTracker(ctx, []string{"sync"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ Nothing changed under the work paths")
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runTracker(ctx, []string{"sync", "--quiet"}); err != nil || stdout.Len() != 0 {
		t.Errorf("quiet sync printed %q, err %v", stdout.String(), err)
	}

	// A commit that adds a work file is refused by the guard.
	gitIn(t, root, "add", "-f", "issues/UI/a.md")
	gitIn(t, root, "commit", "-q", "-m", "oops")
	sha := gitOut(t, root, "rev-parse", "HEAD")
	ctx, _, _ = newTestContext(proj, false)
	ctx.Stdin = strings.NewReader("refs/heads/main " + sha + " refs/heads/main 0000000000000000000000000000000000000000\n")
	err := runPrivacy(ctx, []string{"pre-push", "origin", "git@example:o/r.git"})
	var code *exitCodeError
	if !errors.As(err, &code) || code.Code != 1 || !strings.Contains(err.Error(), "adds work files to the code repo (issues/UI/a.md)") {
		t.Errorf("pre-push = %v", err)
	}
	// A clean commit goes out.
	gitIn(t, root, "reset", "-q", "--hard", "HEAD~1")
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "add", "x.go")
	gitIn(t, root, "commit", "-q", "-m", "code only")
	sha = gitOut(t, root, "rev-parse", "HEAD")
	ctx, _, _ = newTestContext(proj, false)
	ctx.Stdin = strings.NewReader("refs/heads/main " + sha + " refs/heads/main 0000000000000000000000000000000000000000\n")
	if err := runPrivacy(ctx, []string{"pre-push", "origin", "git@example:o/r.git"}); err != nil {
		t.Errorf("clean push refused: %v", err)
	}
}

func TestPrivacyHookKeepsAnExistingHook(t *testing.T) {
	proj, root, _ := makePrivacyFixture(t, privacyTrackerBlock)
	hooks := filepath.Join(root, ".git", "hooks")
	os.MkdirAll(hooks, 0o755)
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj.ConfigPath = "/tmp/my projects.yaml"
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runPrivacy(ctx, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "kept as pre-push.local")
	hook, _ := os.ReadFile(filepath.Join(hooks, "pre-push"))
	assertContains(t, string(hook), "issue-cli --config '/tmp/my projects.yaml' --project test privacy pre-push")
	if local, _ := os.ReadFile(filepath.Join(hooks, "pre-push.local")); string(local) != "#!/bin/sh\nexit 0\n" {
		t.Errorf("pre-push.local = %q", local)
	}
	// Installing again doesn't swallow our own hook as .local.
	ctx, _, _ = newTestContext(proj, false)
	if err := runPrivacy(ctx, []string{"hook"}); err != nil {
		t.Fatal(err)
	}
}

func TestPrivacyCheckLinesAndExitCode(t *testing.T) {
	proj, root, remote := makePrivacyFixture(t, privacyTrackerBlock)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runPrivacy(ctx, []string{"check"}); err != nil {
		t.Fatalf("amber should not fail the check: %v", err)
	}
	assertContains(t, stdout.String(), "work   amber  No backup: privacy.work_files is tracker, but there is no tracker git folder")
	assertContains(t, stdout.String(), "fix: issue-cli tracker init --remote <private repo url>")
	assertContains(t, stdout.String(), "guard  amber  Guard off")

	ctx, _, _ = newTestContext(proj, false)
	if err := runTracker(ctx, []string{"init", "--remote", remote}); err != nil {
		t.Fatal(err)
	}
	ctx, stdout, _ = newTestContext(proj, true)
	if err := runPrivacy(ctx, []string{"check"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), `"label": "Work private"`)
	assertContains(t, stdout.String(), `"label": "Guard on"`)

	// Red: the code repo tracks a work path.
	gitIn(t, root, "add", "-f", "issues")
	gitIn(t, root, "commit", "-q", "-m", "work in code")
	ctx, stdout, _ = newTestContext(proj, false)
	err := runPrivacy(ctx, []string{"check"})
	var code *exitCodeError
	if !errors.As(err, &code) || code.Code != 1 {
		t.Errorf("red check err = %v", err)
	}
	assertContains(t, stdout.String(), "work   red    Work in code repo: Work files are in the code repo: issues/ (1) is tracked by Test")
	assertContains(t, stdout.String(), "fix: git rm -r --cached -- issues and commit")
}

func TestTrackerInitNeedsTheOwnersRepoURL(t *testing.T) {
	proj, _, remote := makePrivacyFixture(t, privacyTrackerBlock)
	ctx, _, _ := newTestContext(proj, false)
	err := runTracker(ctx, []string{"init"})
	var ue *usageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "needs --remote <url>: the private repo the owner created") {
		t.Fatalf("init without --remote = %v", err)
	}
	// Once the tracker has an origin, a re-run may leave it out.
	ctx, _, _ = newTestContext(proj, false)
	if err := runTracker(ctx, []string{"init", "--remote", remote}); err != nil {
		t.Fatal(err)
	}
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTracker(ctx, []string{"init"}); err != nil {
		t.Fatalf("re-run without --remote: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Adopted the tracker git folder")
}
