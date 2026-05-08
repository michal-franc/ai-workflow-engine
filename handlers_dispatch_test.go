package main

import (
	"os/exec"
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

	resp := startAgentSession(proj, session, "do the work", "bug-in-login", "claude", "")

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

	resp := startAgentSession(proj, session, "prompt", "slug", "claude", "")
	if resp.Status == "reattached" {
		t.Fatalf("Status = reattached when session does not exist; steps=%+v", resp.Steps)
	}
	for _, step := range resp.Steps {
		if step.Name == "Existing session — attaching" {
			t.Fatalf("unexpected attaching step when session missing: %+v", step)
		}
	}
}
