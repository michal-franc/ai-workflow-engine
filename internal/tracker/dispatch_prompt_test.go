package tracker

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptDeliveryMarker(t *testing.T) {
	cases := []struct{ name, prompt, want string }{
		{"last non-empty line", "first\nsecond\n\n  last one  \n\n", "last one"},
		{"single line", "only", "only"},
		{"blank", " \n\n", ""},
		{"truncated to 40 runes", "x\n" + strings.Repeat("é", 50), strings.Repeat("é", 40)},
	}
	for _, c := range cases {
		if got := PromptDeliveryMarker(c.prompt); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildDispatchPrompt(t *testing.T) {
	enabled := true
	wf := &WorkflowConfig{Worktree: &enabled}
	proj := &Project{Slug: "proj", TmuxSession: "work"}
	issue := &Issue{Slug: "api/fix", Title: "Fix", Status: "in progress", BodyRaw: "Body text."}

	dp := BuildDispatchPrompt(proj, issue, wf, "/w")
	if dp.Worktree != filepath.Join("/w", ".worktrees", "api/fix") || dp.Branch != "work/api/fix" {
		t.Fatalf("worktree = %q branch = %q", dp.Worktree, dp.Branch)
	}
	if dp.Session != "work:agent-api-fix" || dp.Slug != "api/fix" || dp.Status != "in progress" {
		t.Fatalf("got %+v", dp)
	}
	if dp.Prompt != BuildAgentPrompt(proj, issue, wf, dp.Worktree, dp.Branch) {
		t.Fatal("Prompt differs from BuildAgentPrompt with the resolved worktree")
	}
	if !strings.Contains(dp.Prompt, "issue-cli --project proj ") || !strings.Contains(dp.Prompt, "## Worktree") {
		t.Fatalf("prompt missing --project injection or worktree section")
	}

	noWT := BuildDispatchPrompt(nil, issue, nil, "/w")
	if noWT.Worktree != "" || noWT.Branch != "" || noWT.Session != "agent-api-fix" || strings.Contains(noWT.Prompt, "## Worktree") {
		t.Fatalf("without worktree: %+v", noWT)
	}
}
