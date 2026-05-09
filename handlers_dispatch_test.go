package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestAgentLaunchCommand_CodexUsesPromptFile(t *testing.T) {
	got := agentLaunchCommand("codex", "/tmp/agent-prompt-123.txt")
	if !strings.Contains(got, `codex "$(cat `) {
		t.Fatalf("agentLaunchCommand(codex) = %q, want codex to read from a temp prompt file", got)
	}
	if !strings.Contains(got, `/tmp/agent-prompt-123.txt`) {
		t.Fatalf("agentLaunchCommand(codex) = %q, missing prompt path", got)
	}
}

func TestAgentLaunchCommand_ClaudeRemainsInteractive(t *testing.T) {
	got := agentLaunchCommand("claude", "/tmp/agent-prompt-123.txt")
	if got != "claude" {
		t.Fatalf("agentLaunchCommand(claude) = %q, want %q", got, "claude")
	}
}

func TestStartAgentSession_ReattachWhenSessionExists(t *testing.T) {
	// Headless terminal so openTerminalStep doesn't shell out.
	proj := &tracker.Project{Slug: "test", Terminal: "none"}
	session := tmuxSessionName("bug-in-login")

	withMockTmuxHasSession(t, func(name string) bool {
		if name != session {
			t.Errorf("tmuxHasSession called with %q, want %q", name, session)
		}
		return true
	})

	resp := startAgentSession(proj, session, "do the work", "bug-in-login", "claude", "", nil)

	if resp.Status != "reattached" {
		t.Fatalf("Status = %q, want reattached", resp.Status)
	}
	if resp.Session != session {
		t.Fatalf("Session = %q, want %q", resp.Session, session)
	}
	if resp.AttachCmd == "" || !strings.Contains(resp.AttachCmd, session) {
		t.Fatalf("AttachCmd = %q, want it to mention %q", resp.AttachCmd, session)
	}

	var sawAttaching, sawHeadless bool
	for _, step := range resp.Steps {
		if step.Name == "Existing session — attaching" {
			sawAttaching = true
			if step.Status != "reattached" {
				t.Errorf("attaching step status = %q, want reattached", step.Status)
			}
		}
		if strings.HasPrefix(step.Name, "Terminal: headless") {
			sawHeadless = true
		}
		if strings.HasPrefix(step.Name, "Create tmux session") {
			t.Errorf("unexpected Create tmux session step on reattach: %+v", step)
		}
		if strings.HasPrefix(step.Name, "Log to ") || strings.HasPrefix(step.Name, "CLI log to ") {
			t.Errorf("unexpected logging setup step on reattach: %+v", step)
		}
	}
	if !sawAttaching {
		t.Errorf("missing 'Existing session — attaching' step; got %+v", resp.Steps)
	}
	if !sawHeadless {
		t.Errorf("missing headless terminal step; got %+v", resp.Steps)
	}
}

func TestStartAgentSession_DoesNotReattachWhenSessionMissing(t *testing.T) {
	// Regression guard: when tmuxHasSession returns false, the reattach branch
	// must NOT fire — the response status must not be "reattached" regardless
	// of whether the downstream new-session call succeeds in the test env.
	//
	// startAgentSession's downstream new-session step actually shells out to
	// real tmux, so the test must clean up any session it accidentally creates.
	proj := &tracker.Project{Slug: "test", Terminal: "none"}
	session := "issue-viewer-test-missing-session-xyz"
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	withMockTmuxHasSession(t, func(string) bool { return false })

	resp := startAgentSession(proj, session, "prompt", "slug", "claude", "", nil)
	if resp.Status == "reattached" {
		t.Fatalf("Status = reattached when session does not exist; steps=%+v", resp.Steps)
	}
	for _, step := range resp.Steps {
		if step.Name == "Existing session — attaching" {
			t.Fatalf("unexpected attaching step when session missing: %+v", step)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func TestResolveWorktree_OptInEnablesCreation(t *testing.T) {
	wf := &tracker.WorkflowConfig{Worktree: boolPtr(true)}
	path, branch, enabled := resolveWorktree("/work/proj", "fix-foo", wf)
	if !enabled {
		t.Fatalf("expected enabled=true when worktree:true")
	}
	if path != filepath.Join("/work/proj", ".worktrees", "fix-foo") {
		t.Fatalf("unexpected worktree path: %s", path)
	}
	if branch != "work/fix-foo" {
		t.Fatalf("unexpected branch: %s", branch)
	}
}

func TestResolveWorktree_DefaultsDisabled(t *testing.T) {
	// Worktree field absent → opt-in default is off so projects that haven't
	// reviewed the feature keep their existing dispatch behaviour.
	wf := &tracker.WorkflowConfig{}
	_, _, enabled := resolveWorktree("/work/proj", "fix-foo", wf)
	if enabled {
		t.Fatalf("expected enabled=false by default")
	}
}

func TestResolveWorktree_ExplicitDisableSkipsCreation(t *testing.T) {
	wf := &tracker.WorkflowConfig{Worktree: boolPtr(false)}
	_, _, enabled := resolveWorktree("/work/proj", "fix-foo", wf)
	if enabled {
		t.Fatalf("expected enabled=false when worktree:false")
	}
}

func TestResolveWorktree_NoSlugSkipsCreation(t *testing.T) {
	// Retros review and other dispatches without an issue slug must not
	// trigger worktree creation — there's no per-issue branch to make.
	wf := &tracker.WorkflowConfig{Worktree: boolPtr(true)}
	_, _, enabled := resolveWorktree("/work/proj", "", wf)
	if enabled {
		t.Fatalf("expected enabled=false when slug is empty")
	}
}

func TestResolveWorktree_NilWorkflowSkipsCreation(t *testing.T) {
	// nil wf is the bootstrap fallback path; without a workflow we never
	// create a worktree.
	_, _, enabled := resolveWorktree("/work/proj", "fix-foo", nil)
	if enabled {
		t.Fatalf("expected enabled=false when wf is nil")
	}
}

func TestEnsureWorktree_DisabledReturnsOriginalDir(t *testing.T) {
	dir := "/work/proj"
	got, steps, ok := ensureWorktree(dir, "fix-foo", &tracker.WorkflowConfig{Worktree: boolPtr(false)})
	if !ok {
		t.Fatalf("expected ok=true when disabled")
	}
	if got != dir {
		t.Fatalf("expected workdir unchanged, got %s", got)
	}
	if len(steps) != 0 {
		t.Fatalf("expected no dispatch steps when disabled, got %+v", steps)
	}
}

func TestEnsureWorktree_EnabledStubsGitAndReportsSuccess(t *testing.T) {
	tmp := t.TempDir()
	var gotWorkdir, gotBranch, gotPath string
	orig := runGitWorktreeAdd
	runGitWorktreeAdd = func(workdir, branch, wtPath string) ([]byte, error) {
		gotWorkdir, gotBranch, gotPath = workdir, branch, wtPath
		return nil, nil
	}
	t.Cleanup(func() { runGitWorktreeAdd = orig })

	wf := &tracker.WorkflowConfig{Worktree: boolPtr(true)}
	got, steps, ok := ensureWorktree(tmp, "fix-foo", wf)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	want := filepath.Join(tmp, ".worktrees", "fix-foo")
	if got != want {
		t.Fatalf("expected workdir %s, got %s", want, got)
	}
	if gotWorkdir != tmp || gotBranch != "work/fix-foo" || gotPath != want {
		t.Fatalf("unexpected git args: workdir=%s branch=%s path=%s", gotWorkdir, gotBranch, gotPath)
	}
	if len(steps) != 1 || steps[0].Status != "ok" {
		t.Fatalf("expected single ok step, got %+v", steps)
	}
}

func TestEnsureWorktree_GitFailureSurfacesErrorAndAborts(t *testing.T) {
	tmp := t.TempDir()
	orig := runGitWorktreeAdd
	runGitWorktreeAdd = func(workdir, branch, wtPath string) ([]byte, error) {
		return []byte("fatal: dirty working tree\n"), errStub{}
	}
	t.Cleanup(func() { runGitWorktreeAdd = orig })

	got, steps, ok := ensureWorktree(tmp, "fix-foo", &tracker.WorkflowConfig{Worktree: boolPtr(true)})
	if ok {
		t.Fatalf("expected ok=false on git failure")
	}
	if got != tmp {
		t.Fatalf("expected workdir to fall back to %s, got %s", tmp, got)
	}
	if len(steps) != 1 || steps[0].Status != "error" {
		t.Fatalf("expected single error step, got %+v", steps)
	}
	if !strings.Contains(steps[0].Detail, "dirty working tree") {
		t.Fatalf("expected git stderr in step detail, got %q", steps[0].Detail)
	}
}

func TestEnsureWorktree_ReusesExistingDir(t *testing.T) {
	tmp := t.TempDir()
	// Pre-create the worktree directory so ensureWorktree treats this as a
	// re-dispatch and skips git instead of erroring.
	wt := filepath.Join(tmp, ".worktrees", "fix-foo")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}

	called := false
	orig := runGitWorktreeAdd
	runGitWorktreeAdd = func(string, string, string) ([]byte, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runGitWorktreeAdd = orig })

	got, steps, ok := ensureWorktree(tmp, "fix-foo", &tracker.WorkflowConfig{Worktree: boolPtr(true)})
	if !ok {
		t.Fatalf("expected ok=true on re-dispatch")
	}
	if called {
		t.Fatalf("expected git not to run when worktree dir already exists")
	}
	if got != wt {
		t.Fatalf("expected workdir %s, got %s", wt, got)
	}
	if len(steps) != 1 || !strings.Contains(steps[0].Name, "Reuse worktree") {
		t.Fatalf("expected single reuse step, got %+v", steps)
	}
}

func TestBuildAgentPrompt_AppendsWorktreeBlockWhenSet(t *testing.T) {
	issue := &tracker.Issue{Slug: "fix-foo", Title: "T", Status: "in progress", BodyRaw: "body"}
	prompt := buildAgentPrompt(nil, issue, &tracker.WorkflowConfig{}, "/work/proj/.worktrees/fix-foo", "work/fix-foo")
	for _, want := range []string{
		"## Worktree",
		"/work/proj/.worktrees/fix-foo",
		"work/fix-foo",
		"human handles cleanup",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\n%s", want, prompt)
		}
	}
}

func TestBuildAgentPrompt_OmitsWorktreeBlockWhenEmpty(t *testing.T) {
	issue := &tracker.Issue{Slug: "fix-foo", Title: "T", Status: "in progress", BodyRaw: "body"}
	prompt := buildAgentPrompt(nil, issue, &tracker.WorkflowConfig{}, "", "")
	if strings.Contains(prompt, "## Worktree") {
		t.Fatalf("expected no worktree block when path is empty")
	}
}

type errStub struct{}

func (errStub) Error() string { return "exit status 1" }

func TestEnsureWorktree_RunsSetupAfterCreate(t *testing.T) {
	tmp := t.TempDir()
	origGit := runGitWorktreeAdd
	runGitWorktreeAdd = func(string, string, string) ([]byte, error) { return nil, nil }
	t.Cleanup(func() { runGitWorktreeAdd = origGit })

	var gotPath, gotCmd string
	origSetup := runWorktreeSetup
	runWorktreeSetup = func(wtPath, cmd string) ([]byte, error) {
		gotPath, gotCmd = wtPath, cmd
		return []byte("built\n"), nil
	}
	t.Cleanup(func() { runWorktreeSetup = origSetup })

	wf := &tracker.WorkflowConfig{Worktree: boolPtr(true), WorktreeSetup: "make"}
	got, steps, ok := ensureWorktree(tmp, "fix-foo", wf)
	if !ok {
		t.Fatalf("expected ok=true on successful setup")
	}
	wantWT := filepath.Join(tmp, ".worktrees", "fix-foo")
	if got != wantWT {
		t.Fatalf("expected workdir %s, got %s", wantWT, got)
	}
	if gotPath != wantWT || gotCmd != "make" {
		t.Fatalf("setup invoked with wtPath=%s cmd=%s, want %s + make", gotPath, gotCmd, wantWT)
	}
	if len(steps) != 2 {
		t.Fatalf("expected create + setup steps, got %+v", steps)
	}
	if !strings.Contains(steps[1].Name, "Worktree setup: make") || steps[1].Status != "ok" {
		t.Fatalf("expected ok setup step, got %+v", steps[1])
	}
}

func TestEnsureWorktree_SetupFailureAborts(t *testing.T) {
	tmp := t.TempDir()
	origGit := runGitWorktreeAdd
	runGitWorktreeAdd = func(string, string, string) ([]byte, error) { return nil, nil }
	t.Cleanup(func() { runGitWorktreeAdd = origGit })

	origSetup := runWorktreeSetup
	runWorktreeSetup = func(string, string) ([]byte, error) {
		return []byte("missing target\n"), errStub{}
	}
	t.Cleanup(func() { runWorktreeSetup = origSetup })

	wf := &tracker.WorkflowConfig{Worktree: boolPtr(true), WorktreeSetup: "make"}
	got, steps, ok := ensureWorktree(tmp, "fix-foo", wf)
	if ok {
		t.Fatalf("expected ok=false when setup fails")
	}
	if got != tmp {
		t.Fatalf("expected workdir to fall back to %s on setup failure, got %s", tmp, got)
	}
	if len(steps) != 2 {
		t.Fatalf("expected create + setup steps, got %+v", steps)
	}
	if steps[1].Status != "error" || !strings.Contains(steps[1].Detail, "missing target") {
		t.Fatalf("expected error setup step with stderr, got %+v", steps[1])
	}
}

func TestEnsureWorktree_SetupSkippedOnReuse(t *testing.T) {
	tmp := t.TempDir()
	wt := filepath.Join(tmp, ".worktrees", "fix-foo")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}

	called := false
	origSetup := runWorktreeSetup
	runWorktreeSetup = func(string, string) ([]byte, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runWorktreeSetup = origSetup })

	wf := &tracker.WorkflowConfig{Worktree: boolPtr(true), WorktreeSetup: "make"}
	_, steps, ok := ensureWorktree(tmp, "fix-foo", wf)
	if !ok {
		t.Fatalf("expected ok=true on reuse")
	}
	if called {
		t.Fatalf("expected setup not to run on reuse")
	}
	if len(steps) != 1 || !strings.Contains(steps[0].Name, "Reuse worktree") {
		t.Fatalf("expected single reuse step, got %+v", steps)
	}
}
