package main

import (
	"strings"
	"testing"
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
