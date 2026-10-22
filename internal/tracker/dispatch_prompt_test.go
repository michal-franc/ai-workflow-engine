package tracker

import (
	"path/filepath"
	"regexp"
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

	dp := BuildDispatchPrompt(proj, issue, wf, "/w", nil)
	if dp.Worktree != filepath.Join("/w", ".worktrees", "api/fix") || dp.Branch != "work/api/fix" {
		t.Fatalf("worktree = %q branch = %q", dp.Worktree, dp.Branch)
	}
	if dp.Session != "work:agent-api-fix" || dp.Slug != "api/fix" || dp.Status != "in progress" {
		t.Fatalf("got %+v", dp)
	}
	if !strings.HasPrefix(dp.Prompt, BuildAgentPrompt(proj, issue, wf, dp.Worktree, dp.Branch)) {
		t.Fatal("Prompt does not start with BuildAgentPrompt for the resolved worktree")
	}
	if dp.Lead != "lead-proj" || !strings.Contains(dp.Prompt, "## Your lead\n\nYou report to the project lead lead-proj (not running).") {
		t.Fatalf("lead = %q, prompt missing the lead section: %q", dp.Lead, dp.Prompt)
	}
	if !strings.Contains(dp.Prompt, "message lead-proj, or another agent on this project, with ListAgents / SendMessage") || !strings.Contains(dp.Prompt, "yours is agent-api-fix") {
		t.Fatalf("prompt missing the SendMessage line: %q", dp.Prompt)
	}
	if !strings.Contains(dp.Prompt, "issue-cli --project proj ") || !strings.Contains(dp.Prompt, "## Worktree") {
		t.Fatalf("prompt missing --project injection or worktree section")
	}

	noWT := BuildDispatchPrompt(nil, issue, nil, "/w", nil)
	if noWT.Worktree != "" || noWT.Branch != "" || noWT.Session != "agent-api-fix" || strings.Contains(noWT.Prompt, "## Worktree") {
		t.Fatalf("without worktree: %+v", noWT)
	}
}

// The Workflow review block's retrospective command must name the issue's
// slug: `retrospective <System>` fuzzy-matches some other issue of that
// system and files the retro there.
func TestBuildAgentPrompt_RetrospectiveNamesSlugNotSystem(t *testing.T) {
	issue := &Issue{Slug: "campaign/fix-loot", Title: "Fix loot", Status: "idea", System: "Campaign", BodyRaw: "Body."}
	prompt := BuildAgentPrompt(nil, issue, nil, "", "")

	if !strings.Contains(prompt, `issue-cli retrospective campaign/fix-loot --body "Base workflow: ...\nSubsystem workflow for Campaign: ...`) {
		t.Fatalf("workflow review block does not name the slug:\n\n%s", prompt)
	}
	if strings.Contains(prompt, "retrospective Campaign") {
		t.Fatalf("retrospective command names the system:\n\n%s", prompt)
	}
	if !strings.Contains(prompt, "system-specific instructions that should be added for Campaign\n") {
		t.Fatalf("missing-instructions line does not name the system:\n\n%s", prompt)
	}
}

// Every {{name}} in the template has a field and every field is used, so a
// renamed or misspelt placeholder can't reach an agent unfilled.
func TestAgentDispatchPromptTemplate_EveryPlaceholderFilled(t *testing.T) {
	issue := &Issue{Slug: "the-slug", System: "the-system", Title: "the-title", Priority: "the-priority", BodyRaw: "the-body"}
	fields := agentPromptFields(issue, "the-guidance", "the-reminder", "the-status")

	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{\{(\w+)\}\}`).FindAllStringSubmatch(AgentDispatchPromptTemplate, -1) {
		used[m[1]] = true
		if _, ok := fields[m[1]]; !ok {
			t.Errorf("template placeholder {{%s}} has no field", m[1])
		}
	}
	for name, value := range fields {
		if !used[name] {
			t.Errorf("field %q is not used by the template", name)
		}
		if !strings.Contains(renderPromptTemplate(AgentDispatchPromptTemplate, fields), value) {
			t.Errorf("rendered prompt lacks %q (field %q)", value, name)
		}
	}
	if got := renderPromptTemplate(AgentDispatchPromptTemplate, fields); strings.Contains(got, "{{") {
		t.Fatalf("rendered prompt still has a placeholder:\n\n%s", got)
	}
}

// A value is not re-scanned: an issue body quoting {{slug}} stays literal.
func TestRenderPromptTemplate_LeavesValuesAlone(t *testing.T) {
	got := renderPromptTemplate("{{slug}} / {{body}}", map[string]string{"slug": "a/b", "body": "see {{slug}}"})
	if got != "a/b / see {{slug}}" {
		t.Fatalf("got %q", got)
	}
}
