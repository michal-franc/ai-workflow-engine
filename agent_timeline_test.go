package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestLoadDispatchPrompt(t *testing.T) {
	workDir := t.TempDir()
	assignee := "agent-test-dispatch"
	dir := filepath.Join(workDir, ".agent-logs", assignee)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if got := LoadDispatchPrompt(workDir, assignee); got != "" {
		t.Errorf("missing file should return empty, got %q", got)
	}

	want := "You have been assigned: Fix thing.\n\n## Goal\nDo the work."
	if err := os.WriteFile(filepath.Join(dir, "dispatch-prompt.txt"), []byte(want), 0644); err != nil {
		t.Fatal(err)
	}
	if got := LoadDispatchPrompt(workDir, assignee); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if got := LoadDispatchPrompt("", assignee); got != "" {
		t.Error("empty workDir should return empty")
	}
	if got := LoadDispatchPrompt(workDir, ""); got != "" {
		t.Error("empty assignee should return empty")
	}
}

func TestLoadAgentTimeline_MissingLogReturnsNil(t *testing.T) {
	if got := LoadAgentTimeline("", ""); got != nil {
		t.Errorf("empty inputs should return nil, got %d events", len(got))
	}
	if got := LoadAgentTimeline(t.TempDir(), "agent-nonexistent"); got != nil {
		t.Errorf("missing log should return nil, got %d events", len(got))
	}
}

func TestLoadAgentTimeline_ParsesClilog(t *testing.T) {
	workDir := t.TempDir()
	assignee := "agent-test-slug"
	dir := filepath.Join(workDir, ".agent-logs", assignee)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, assignee+".clilog")

	lines := []string{
		`{"args":["start","test-slug"],"ts":"2026-04-23T16:06:45Z"}`,
		`{"args":["show","test-slug"],"ts":"2026-04-23T16:06:48Z"}`,
		`{"args":["process","schema"],"ts":"2026-04-23T16:07:00Z"}`,
		`{"args":["append","test-slug","--body","## Decisions\n- score caching server-side"],"ts":"2026-04-23T16:10:29Z"}`,
		`{"args":["check","test-slug","Code changes complete"],"ts":"2026-04-23T16:16:51Z"}`,
		`{"args":["transition","test-slug","--to","testing"],"ts":"2026-04-23T17:17:58Z"}`,
		`{"args":["comment","test-slug","--text","tests: 301 passing across 3 packages"],"ts":"2026-04-23T17:18:12Z"}`,
		`{"args":["retrospective","test-slug","--body","## Base workflow\nSmooth overall."],"ts":"2026-04-23T17:23:57Z"}`,
		`not valid json`,
		`{"args":[],"ts":"2026-04-23T17:24:00Z"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	events := LoadAgentTimeline(workDir, assignee)
	if len(events) != 8 {
		t.Fatalf("expected 8 events (invalid + empty-args skipped), got %d", len(events))
	}

	checkEvent(t, events[0], "start", "start", "")
	checkEvent(t, events[1], "show", "show", "")
	checkEvent(t, events[2], "process", "process schema", "")
	if events[3].Kind != "append" || !strings.Contains(events[3].Summary, "Decisions") {
		t.Errorf("append event: got kind=%q summary=%q", events[3].Kind, events[3].Summary)
	}
	if events[3].Detail == "" {
		t.Error("append event should carry body as Detail")
	}
	checkEvent(t, events[4], "check", "check: Code changes complete", "")
	checkEvent(t, events[5], "transition", "transition → testing", "")

	if events[6].Kind != "comment-tests" {
		t.Errorf("comment-tests kind expected, got %q", events[6].Kind)
	}
	if !strings.HasPrefix(events[6].Summary, "tests:") {
		t.Errorf("comment summary should start with prefix: %q", events[6].Summary)
	}

	if events[7].Kind != "retrospective" || events[7].Detail == "" {
		t.Errorf("retrospective event missing Detail: %+v", events[7])
	}

	if events[0].TimeLabel != "2026-04-23 16:06:45" {
		t.Errorf("TimeLabel format wrong: %q", events[0].TimeLabel)
	}
}

func checkEvent(t *testing.T, ev TimelineEvent, wantKind, wantSummary, wantDetail string) {
	t.Helper()
	if ev.Kind != wantKind {
		t.Errorf("kind: got %q, want %q", ev.Kind, wantKind)
	}
	if ev.Summary != wantSummary {
		t.Errorf("summary: got %q, want %q", ev.Summary, wantSummary)
	}
	if ev.Detail != wantDetail {
		t.Errorf("detail: got %q, want %q", ev.Detail, wantDetail)
	}
}

func TestFirstLineTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := firstLine(long)
	if len(got) != 120 {
		t.Errorf("long single-line should be truncated to 120 (117 + ...), got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated string should end with ellipsis: %q", got)
	}

	multi := "first line\nsecond line"
	if got := firstLine(multi); got != "first line" {
		t.Errorf("firstLine of multi-line: got %q", got)
	}
}

func TestEnrichTimelineWithWorkflow(t *testing.T) {
	events := []TimelineEvent{
		{Kind: "start", Summary: "start"},
		{Kind: "transition", ToStatus: "testing"},
		{Kind: "comment-tests", Summary: "tests: 301 passing"},
		{Kind: "transition", ToStatus: "documentation"},
	}

	wf := &tracker.WorkflowConfig{
		Transitions: []tracker.WorkflowTransition{
			{From: "in progress", To: "testing", Actions: []tracker.WorkflowAction{
				{Type: "validate", Rule: "all_checkboxes_checked"},
				{Type: "append_section", Title: "Testing", Body: "- [ ] Relevant tests passing"},
			}},
			{From: "testing", To: "documentation", Actions: []tracker.WorkflowAction{
				{Type: "validate", Rule: "has_comment_prefix: tests:"},
				{Type: "inject_prompt", Prompt: "Update docs for the change."},
				{Type: "require_human_approval", Status: "documentation"},
			}},
		},
	}

	enriched := EnrichTimelineWithWorkflow(events, wf, "in progress")

	if len(enriched[1].Actions) != 2 {
		t.Fatalf("testing transition: want 2 actions, got %d", len(enriched[1].Actions))
	}
	if enriched[1].FromStatus != "in progress" {
		t.Errorf("testing transition: FromStatus %q want 'in progress'", enriched[1].FromStatus)
	}
	if enriched[1].Actions[0].Type != "validate" || !strings.Contains(enriched[1].Actions[0].Label, "all_checkboxes_checked") {
		t.Errorf("first action mismatch: %+v", enriched[1].Actions[0])
	}
	if enriched[1].Actions[1].Body != "- [ ] Relevant tests passing" {
		t.Errorf("append_section body not carried: %q", enriched[1].Actions[1].Body)
	}

	docTrans := enriched[3]
	if docTrans.FromStatus != "testing" {
		t.Errorf("doc transition FromStatus %q want 'testing' (chained from prev)", docTrans.FromStatus)
	}
	if len(docTrans.Actions) != 3 {
		t.Fatalf("doc transition: want 3 actions, got %d", len(docTrans.Actions))
	}
	foundPrompt := false
	for _, a := range docTrans.Actions {
		if a.Type == "inject_prompt" && a.Body == "Update docs for the change." {
			foundPrompt = true
		}
	}
	if !foundPrompt {
		t.Errorf("inject_prompt body not surfaced: %+v", docTrans.Actions)
	}
}

func TestEnrichTimeline_RuleDescriptionsAttached(t *testing.T) {
	events := []TimelineEvent{{Kind: "transition", ToStatus: "done"}}
	wf := &tracker.WorkflowConfig{
		Transitions: []tracker.WorkflowTransition{
			{From: "shipping", To: "done", Actions: []tracker.WorkflowAction{
				{Type: "validate", Rule: "body_not_empty"},
			}},
		},
	}
	got := EnrichTimelineWithWorkflow(events, wf, "")
	if len(got[0].Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(got[0].Actions))
	}
	if got[0].Actions[0].Body == "" {
		t.Error("expected rule description body to be populated from tracker.WorkflowValidationRules registry")
	}
}

func TestSplitRule(t *testing.T) {
	name, arg := splitRule("has_comment_prefix: tests:")
	if name != "has_comment_prefix" || arg != "tests:" {
		t.Errorf("splitRule: got (%q, %q), want (has_comment_prefix, tests:)", name, arg)
	}
	name, arg = splitRule("body_not_empty")
	if name != "body_not_empty" || arg != "" {
		t.Errorf("splitRule no colon: got (%q, %q)", name, arg)
	}
}

func TestExtractFlag(t *testing.T) {
	args := []string{"comment", "slug", "--text", "hello", "--extra", "world"}
	if got := extractFlag(args, "--text"); got != "hello" {
		t.Errorf("--text: got %q", got)
	}
	if got := extractFlag(args, "--extra"); got != "world" {
		t.Errorf("--extra: got %q", got)
	}
	if got := extractFlag(args, "--missing"); got != "" {
		t.Errorf("missing flag: got %q, want empty", got)
	}
	if got := extractFlag([]string{"comment", "slug", "--text"}, "--text"); got != "" {
		t.Errorf("flag at end without value: got %q, want empty", got)
	}
}
