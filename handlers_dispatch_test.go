package main

import (
	"os"
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
	got, step, ok := ensureWorktree(dir, "fix-foo", &tracker.WorkflowConfig{Worktree: boolPtr(false)})
	if !ok {
		t.Fatalf("expected ok=true when disabled")
	}
	if got != dir {
		t.Fatalf("expected workdir unchanged, got %s", got)
	}
	if step != nil {
		t.Fatalf("expected no dispatch step when disabled, got %+v", step)
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
	got, step, ok := ensureWorktree(tmp, "fix-foo", wf)
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
	if step == nil || step.Status != "ok" {
		t.Fatalf("expected ok step, got %+v", step)
	}
}

func TestEnsureWorktree_GitFailureSurfacesErrorAndAborts(t *testing.T) {
	tmp := t.TempDir()
	orig := runGitWorktreeAdd
	runGitWorktreeAdd = func(workdir, branch, wtPath string) ([]byte, error) {
		return []byte("fatal: dirty working tree\n"), errStub{}
	}
	t.Cleanup(func() { runGitWorktreeAdd = orig })

	got, step, ok := ensureWorktree(tmp, "fix-foo", &tracker.WorkflowConfig{Worktree: boolPtr(true)})
	if ok {
		t.Fatalf("expected ok=false on git failure")
	}
	if got != tmp {
		t.Fatalf("expected workdir to fall back to %s, got %s", tmp, got)
	}
	if step == nil || step.Status != "error" {
		t.Fatalf("expected error step, got %+v", step)
	}
	if !strings.Contains(step.Detail, "dirty working tree") {
		t.Fatalf("expected git stderr in step.Detail, got %q", step.Detail)
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

	got, step, ok := ensureWorktree(tmp, "fix-foo", &tracker.WorkflowConfig{Worktree: boolPtr(true)})
	if !ok {
		t.Fatalf("expected ok=true on re-dispatch")
	}
	if called {
		t.Fatalf("expected git not to run when worktree dir already exists")
	}
	if got != wt {
		t.Fatalf("expected workdir %s, got %s", wt, got)
	}
	if step == nil || !strings.Contains(step.Name, "Reuse worktree") {
		t.Fatalf("expected reuse step, got %+v", step)
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
