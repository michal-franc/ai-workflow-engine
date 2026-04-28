package tracker

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultWorkflow(t *testing.T) {
	wf := DefaultWorkflow()

	if len(wf.Statuses) == 0 {
		t.Fatal("default workflow has no statuses")
	}

	order := wf.GetStatusOrder()
	if order[0] != "idea" {
		t.Errorf("first status = %q, want %q", order[0], "idea")
	}
	if order[len(order)-1] != "done" {
		t.Errorf("last status = %q, want %q", order[len(order)-1], "done")
	}

	descs := wf.GetStatusDescriptions()
	if descs["idea"] != "Raw idea, needs exploration" {
		t.Errorf("idea description = %q", descs["idea"])
	}
}

func TestGetStatusOrder(t *testing.T) {
	wf := DefaultWorkflow()
	order := wf.GetStatusOrder()

	expected := []string{"idea", "in design", "backlog", "in progress", "testing", "human-testing", "documentation", "shipping", "done"}
	if len(order) != len(expected) {
		t.Fatalf("got %d statuses, want %d", len(order), len(expected))
	}
	for i, s := range expected {
		if order[i] != s {
			t.Errorf("order[%d] = %q, want %q", i, order[i], s)
		}
	}
}

func TestGetStatusIndex(t *testing.T) {
	wf := DefaultWorkflow()

	tests := []struct {
		status string
		want   int
	}{
		{"idea", 0},
		{"in design", 1},
		{"human-testing", 5},
		{"shipping", 7},
		{"done", 8},
		{"none", -1},
		{"unknown", -1},
	}

	for _, tt := range tests {
		got := wf.GetStatusIndex(tt.status)
		if got != tt.want {
			t.Errorf("GetStatusIndex(%q) = %d, want %d", tt.status, got, tt.want)
		}
	}
}

func TestIsValidTransition(t *testing.T) {
	wf := DefaultWorkflow()

	tests := []struct {
		from string
		to   string
		want bool
	}{
		{"idea", "in design", true},
		{"in progress", "testing", true},
		{"idea", "done", false},    // skip
		{"done", "idea", false},    // backwards
		{"unknown", "idea", false}, // unknown from
		{"idea", "unknown", false}, // unknown to
		{"idea", "idea", false},    // same
		{"none", "idea", false},    // none no longer exists
		{"testing", "human-testing", true},
		{"human-testing", "documentation", true},
		{"documentation", "shipping", true},
		{"shipping", "done", true},
		{"documentation", "done", false}, // shipping must come first
	}

	for _, tt := range tests {
		got := wf.IsValidTransition(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("IsValidTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestIsValidTransition_HonorsYAMLBackwardEdge(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "in-review"},
			{Name: "waiting-for-team-input"},
			{Name: "approved"},
		},
		Transitions: []WorkflowTransition{
			{From: "in-review", To: "waiting-for-team-input"},
			{From: "waiting-for-team-input", To: "in-review"},
			{From: "in-review", To: "approved"},
		},
	}

	if !wf.IsValidTransition("waiting-for-team-input", "in-review") {
		t.Errorf("backward edge declared in YAML should be valid")
	}
	if !wf.IsValidTransition("in-review", "waiting-for-team-input") {
		t.Errorf("forward edge declared in YAML should be valid")
	}
	if !wf.IsValidTransition("in-review", "approved") {
		t.Errorf("index-skipping forward edge declared in YAML should be valid")
	}
	if wf.IsValidTransition("approved", "in-review") {
		t.Errorf("undeclared backward edge should remain invalid")
	}
}

func TestIsValidTransition_SkipsOptionalStatuses(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "in-review"},
			{Name: "waiting-for-team-input", Optional: true},
			{Name: "approve-comment-created"},
			{Name: "approved"},
		},
	}

	if !wf.IsValidTransition("in-review", "approve-comment-created") {
		t.Errorf("forward jump across an optional status should be valid")
	}
	if !wf.IsValidTransition("in-review", "waiting-for-team-input") {
		t.Errorf("forward step into an optional status should still be valid")
	}
	if !wf.IsValidTransition("waiting-for-team-input", "approve-comment-created") {
		t.Errorf("stepping out of an optional status should be valid")
	}
	if wf.IsValidTransition("in-review", "approved") {
		t.Errorf("skipping a required status must not be allowed")
	}
}

func TestIsValidTransition_MultipleOptionalInARow(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "a"},
			{Name: "b", Optional: true},
			{Name: "c", Optional: true},
			{Name: "d"},
		},
	}
	if !wf.IsValidTransition("a", "d") {
		t.Errorf("jumping across two consecutive optional statuses should be valid")
	}
}

func TestNextRequiredStatus(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "a"},
			{Name: "b", Optional: true},
			{Name: "c", Optional: true},
			{Name: "d"},
		},
	}

	if got := wf.NextRequiredStatus("a"); got != "d" {
		t.Errorf("NextRequiredStatus(a) = %q, want d", got)
	}
	if got := wf.NextRequiredStatus("b"); got != "d" {
		t.Errorf("NextRequiredStatus(b) = %q, want d", got)
	}
	if got := wf.NextRequiredStatus("d"); got != "" {
		t.Errorf("NextRequiredStatus(d) = %q, want empty", got)
	}
	if got := wf.NextRequiredStatus("unknown"); got != "" {
		t.Errorf("NextRequiredStatus(unknown) = %q, want empty", got)
	}
}

func TestDefaultNextStatus(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "a"},
			{Name: "b", Optional: true},
			{Name: "c", Optional: true},
			{Name: "d"},
			{Name: "e", Optional: true},
		},
	}

	required, optionals := wf.DefaultNextStatus("a")
	if required != "d" {
		t.Errorf("DefaultNextStatus(a) required = %q, want d", required)
	}
	if len(optionals) != 2 || optionals[0] != "b" || optionals[1] != "c" {
		t.Errorf("DefaultNextStatus(a) optionals = %v, want [b c]", optionals)
	}

	required, optionals = wf.DefaultNextStatus("b")
	if required != "d" {
		t.Errorf("DefaultNextStatus(b) required = %q, want d", required)
	}
	if len(optionals) != 1 || optionals[0] != "c" {
		t.Errorf("DefaultNextStatus(b) optionals = %v, want [c]", optionals)
	}

	// When only optional statuses remain, required is empty and all are returned so
	// callers can render them as alternatives rather than silently pick one.
	required, optionals = wf.DefaultNextStatus("d")
	if required != "" {
		t.Errorf("DefaultNextStatus(d) required = %q, want empty", required)
	}
	if len(optionals) != 1 || optionals[0] != "e" {
		t.Errorf("DefaultNextStatus(d) optionals = %v, want [e]", optionals)
	}

	required, optionals = wf.DefaultNextStatus("e")
	if required != "" || len(optionals) != 0 {
		t.Errorf("DefaultNextStatus(e) = (%q, %v), want (\"\", nil)", required, optionals)
	}

	required, optionals = wf.DefaultNextStatus("unknown")
	if required != "" || len(optionals) != 0 {
		t.Errorf("DefaultNextStatus(unknown) = (%q, %v), want (\"\", nil)", required, optionals)
	}
}

func TestApplyTransitionToFile_ErrorPointsAtRequiredNext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	body := "---\ntitle: \"Test\"\nstatus: \"a\"\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "a"},
			{Name: "b", Optional: true},
			{Name: "c"},
			{Name: "d"},
		},
	}

	// Try to jump from a directly to d — c is required and cannot be skipped.
	_, _, err := wf.ApplyTransitionToFile(path, "d")
	if err == nil {
		t.Fatal("expected error when skipping a required status")
	}
	if !strings.Contains(err.Error(), "\"c\"") {
		t.Errorf("error should mention required next status %q, got: %v", "c", err)
	}
}

func TestMerge_PropagatesOptional(t *testing.T) {
	base := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "a"},
			{Name: "b"},
		},
	}
	overlay := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "b", Optional: true},
		},
	}
	base.Merge(overlay)
	if got := base.GetStatus("b"); got == nil || !got.Optional {
		t.Errorf("merged status b should be Optional=true, got %+v", got)
	}
}

func TestIsValidTransition_FallsBackToLinearWhenNoExplicitEdge(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "a"},
			{Name: "b"},
			{Name: "c"},
		},
	}

	if !wf.IsValidTransition("a", "b") {
		t.Errorf("adjacent transition should be valid via linear fallback")
	}
	if wf.IsValidTransition("a", "c") {
		t.Errorf("skip transition should be invalid without explicit edge")
	}
	if wf.IsValidTransition("b", "a") {
		t.Errorf("backward transition should be invalid without explicit edge")
	}
}

func TestGetStatusDescriptions(t *testing.T) {
	wf := DefaultWorkflow()
	descs := wf.GetStatusDescriptions()

	if _, ok := descs["none"]; ok {
		t.Errorf("none should not be in descriptions")
	}
	if descs["backlog"] != "Ready to work on" {
		t.Errorf("backlog description = %q", descs["backlog"])
	}
	if descs["human-testing"] != "Manual verification by humans" {
		t.Errorf("human-testing description = %q", descs["human-testing"])
	}
}

func TestDefaultWorkflowPromptsAndApprovals(t *testing.T) {
	wf := DefaultWorkflow()

	if got := wf.StatusPrompt("backlog"); !strings.Contains(got, "Do not run `issue-cli start`") {
		t.Fatalf("backlog prompt = %q, want start approval guidance", got)
	}

	if got := wf.RequiredHumanApproval("backlog", "in progress"); got != "in progress" {
		t.Fatalf("RequiredHumanApproval(backlog, in progress) = %q, want in progress", got)
	}

	if got := wf.RequiredHumanApproval("in progress", "testing"); got != "" {
		t.Fatalf("RequiredHumanApproval(in progress, testing) = %q, want empty", got)
	}

	apiOverlay, ok := wf.Systems["API"]
	if !ok {
		t.Fatal("default workflow missing API overlay")
	}

	merged := DefaultWorkflow()
	merged.Merge(&WorkflowConfig{
		Statuses:    apiOverlay.Statuses,
		Transitions: apiOverlay.Transitions,
	})
	if got := merged.StatusPrompt("in design"); !strings.Contains(got, "/hash") {
		t.Fatalf("merged API in-design prompt = %q, want /hash guidance", got)
	}
}

func TestTemplateForStatus(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "idea", Template: "## Ideas\n- Item 1\n"},
			{Name: "done", Template: ""},
		},
	}

	t.Run("existing with template", func(t *testing.T) {
		tmpl := wf.TemplateForStatus("idea")
		if tmpl != "## Ideas\n- Item 1" {
			t.Errorf("template = %q", tmpl)
		}
	})

	t.Run("existing without template", func(t *testing.T) {
		tmpl := wf.TemplateForStatus("done")
		if tmpl != "" {
			t.Errorf("template = %q, want empty", tmpl)
		}
	})

	t.Run("missing status", func(t *testing.T) {
		tmpl := wf.TemplateForStatus("unknown")
		if tmpl != "" {
			t.Errorf("template = %q, want empty", tmpl)
		}
	})
}

func TestAppendTemplate(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "testing", Template: "## Test Plan\n- [ ] Test 1\n"},
			{Name: "idea", Template: ""},
		},
	}

	t.Run("append to body", func(t *testing.T) {
		body, appended := wf.AppendTemplate("Existing content", "testing")
		if !appended {
			t.Fatal("expected template to be appended")
		}
		if body != "Existing content\n\n## Test Plan\n- [ ] Test 1\n" {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("append to empty body", func(t *testing.T) {
		body, appended := wf.AppendTemplate("", "testing")
		if !appended {
			t.Fatal("expected template to be appended")
		}
		if body != "## Test Plan\n- [ ] Test 1\n" {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("duplicate guard", func(t *testing.T) {
		body, appended := wf.AppendTemplate("## Test Plan\nAlready here", "testing")
		if appended {
			t.Fatal("should not append duplicate")
		}
		if body != "## Test Plan\nAlready here" {
			t.Errorf("body changed: %q", body)
		}
	})

	t.Run("no template for status", func(t *testing.T) {
		body, appended := wf.AppendTemplate("content", "idea")
		if appended {
			t.Fatal("should not append empty template")
		}
		if body != "content" {
			t.Errorf("body = %q", body)
		}
	})
}

func TestAppendToSection(t *testing.T) {
	t.Run("creates section when missing", func(t *testing.T) {
		body, changed := appendToSection("## Existing\n- item", "Testing", "- [ ] add test")
		if !changed {
			t.Fatal("expected section append")
		}
		want := "## Existing\n- item\n\n## Testing\n- [ ] add test\n"
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("reuses existing section", func(t *testing.T) {
		body, changed := appendToSection("## Testing\n- [ ] existing", "Testing", "- [ ] new item")
		if changed {
			t.Fatal("expected no change when section already exists")
		}
		want := "## Testing\n- [ ] existing"
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("does not duplicate identical content", func(t *testing.T) {
		body, changed := appendToSection("## Testing\n- [ ] same", "Testing", "- [ ] same")
		if changed {
			t.Fatal("expected no change for duplicate content")
		}
		if body != "## Testing\n- [ ] same" {
			t.Errorf("body changed: %q", body)
		}
	})

	t.Run("does not append into existing section with different content", func(t *testing.T) {
		body, changed := appendToSection("## Design\n- [ ] existing item", "Design", "- [ ] another item")
		if changed {
			t.Fatal("expected existing section to be left unchanged")
		}
		if body != "## Design\n- [ ] existing item" {
			t.Errorf("body changed: %q", body)
		}
	})

	t.Run("matches normalized headings", func(t *testing.T) {
		body, changed := appendToSection("###   Design  \nexisting detail", "## design", "new detail")
		if changed {
			t.Fatal("expected normalized heading match to be treated as existing section")
		}
		want := "###   Design  \nexisting detail"
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})
}

func TestValidate(t *testing.T) {
	wf := DefaultWorkflow()

	t.Run("body_not_empty passes", func(t *testing.T) {
		issue := &Issue{BodyRaw: "Some content"}
		err := wf.Validate(issue, "in design", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("body_not_empty fails", func(t *testing.T) {
		issue := &Issue{BodyRaw: ""}
		err := wf.Validate(issue, "in design", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("design and acceptance criteria gates pass", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Design\n- [x] Approach documented\n- [x] Dependencies and risks identified\n- [x] Human approval requested for backlog\n\n## Acceptance Criteria\n- [ ] task 1", HumanApproval: "backlog"}
		err := wf.Validate(issue, "backlog", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("design section blocks backlog when unchecked", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Design\n- [x] Approach documented\n- [ ] Dependencies and risks identified\n- [x] Human approval requested for backlog\n\n## Acceptance Criteria\n- [ ] task 1", HumanApproval: "backlog"}
		err := wf.Validate(issue, "backlog", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("acceptance criteria section required for backlog", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Design\n- [x] Approach documented\n- [x] Dependencies and risks identified\n- [x] Human approval requested for backlog", HumanApproval: "backlog"}
		err := wf.Validate(issue, "backlog", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("human_approval blocks backlog", func(t *testing.T) {
		issue := &Issue{BodyRaw: "- [ ] task 1"}
		err := wf.Validate(issue, "backlog", nil)
		if err == nil {
			t.Fatal("expected error for missing approval")
		}
	})

	t.Run("human_approval wrong status", func(t *testing.T) {
		issue := &Issue{BodyRaw: "- [ ] task 1", HumanApproval: "testing"}
		err := wf.Validate(issue, "backlog", nil)
		if err == nil {
			t.Fatal("expected error for wrong approval status")
		}
	})

	t.Run("has_assignee passes", func(t *testing.T) {
		issue := &Issue{BodyRaw: "content", Assignee: "alice", HumanApproval: "in progress"}
		err := wf.Validate(issue, "in progress", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("has_assignee fails", func(t *testing.T) {
		issue := &Issue{BodyRaw: "content", Assignee: ""}
		err := wf.Validate(issue, "in progress", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("implementation section gate ignores unrelated unchecked items", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Acceptance Criteria\n- [ ] still open by design\n\n## Implementation\n- [x] done 1\n- [x] done 2\n\n## Test Plan\n### Automated\nTests\n### Manual\nSteps"}
		err := wf.Validate(issue, "testing", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("implementation section gate fails", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Implementation\n- [x] done\n- [ ] not done\n\n## Test Plan\n### Automated\nTests\n### Manual\nSteps"}
		err := wf.Validate(issue, "testing", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("testing requires test plan before entering testing", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Implementation\n- [x] done"}
		err := wf.Validate(issue, "testing", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("section_checkboxes_checked passes", func(t *testing.T) {
		sectionWf := &WorkflowConfig{
			Statuses: []WorkflowStatus{
				{Name: "testing", Validation: []string{"section_checkboxes_checked: Implementation"}},
			},
		}
		issue := &Issue{
			Slug:    "test",
			BodyRaw: "## Implementation\n- [x] Code done\n- [x] Tests written\n\n## Testing\n- [ ] Not done yet",
		}
		err := sectionWf.Validate(issue, "testing", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("section_checkboxes_checked fails with unchecked", func(t *testing.T) {
		sectionWf := &WorkflowConfig{
			Statuses: []WorkflowStatus{
				{Name: "testing", Validation: []string{"section_checkboxes_checked: Implementation"}},
			},
		}
		issue := &Issue{
			Slug:    "test",
			BodyRaw: "## Implementation\n- [x] Code done\n- [ ] Tests written\n\n## Testing\n- [ ] Not done yet",
		}
		err := sectionWf.Validate(issue, "testing", nil)
		if err == nil {
			t.Fatal("expected error for unchecked section checkbox")
		}
	})

	t.Run("section_checkboxes_checked skips missing section", func(t *testing.T) {
		sectionWf := &WorkflowConfig{
			Statuses: []WorkflowStatus{
				{Name: "testing", Validation: []string{"section_checkboxes_checked: Implementation"}},
			},
		}
		issue := &Issue{
			Slug:    "test",
			BodyRaw: "## Other\n- [x] Done",
		}
		err := sectionWf.Validate(issue, "testing", nil)
		if err != nil {
			t.Errorf("expected no error for missing section, got: %v", err)
		}
	})

	t.Run("section_has_checkboxes passes", func(t *testing.T) {
		sectionWf := &WorkflowConfig{
			Statuses: []WorkflowStatus{
				{Name: "backlog", Validation: []string{"section_has_checkboxes: Acceptance Criteria"}},
			},
		}
		issue := &Issue{
			BodyRaw: "## Acceptance Criteria\n- [ ] observable behavior",
		}
		err := sectionWf.Validate(issue, "backlog", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("section_has_checkboxes fails", func(t *testing.T) {
		sectionWf := &WorkflowConfig{
			Statuses: []WorkflowStatus{
				{Name: "backlog", Validation: []string{"section_has_checkboxes: Acceptance Criteria"}},
			},
		}
		issue := &Issue{
			BodyRaw: "## Acceptance Criteria\nNo checklist",
		}
		err := sectionWf.Validate(issue, "backlog", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("has_test_plan passes for human-testing", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Test Plan\n### Automated\nTests\n### Manual\nSteps\n\n## Testing\n- [x] Relevant tests for changed code passing\n- [x] Known unrelated failures documented if full suite is red\n- [x] Test results logged as comment"}
		comments := []Comment{{Text: "tests: all pass"}}
		err := wf.Validate(issue, "human-testing", comments)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("has_test_plan fails for human-testing", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Testing\n- [x] done"}
		comments := []Comment{{Text: "tests: all pass"}}
		err := wf.Validate(issue, "human-testing", comments)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("has_comment_prefix tests: fails for human-testing", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Test Plan\n### Automated\nTests\n### Manual\nSteps\n\n## Testing\n- [x] Relevant tests for changed code passing\n- [x] Known unrelated failures documented if full suite is red\n- [x] Test results logged as comment"}
		comments := []Comment{{Text: "some other comment"}}
		err := wf.Validate(issue, "human-testing", comments)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("testing section must be complete for human-testing", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Test Plan\n### Automated\nTests\n### Manual\nSteps\n\n## Testing\n- [x] Relevant tests for changed code passing\n- [ ] Known unrelated failures documented if full suite is red"}
		comments := []Comment{{Text: "tests: all pass"}}
		err := wf.Validate(issue, "human-testing", comments)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("documentation requires approval", func(t *testing.T) {
		issue := &Issue{BodyRaw: "content"}
		err := wf.Validate(issue, "documentation", nil)
		if err == nil {
			t.Fatal("expected error for missing approval")
		}
	})

	t.Run("documentation passes with approval", func(t *testing.T) {
		issue := &Issue{BodyRaw: "content", HumanApproval: "documentation"}
		err := wf.Validate(issue, "documentation", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("has_comment_prefix docs: passes for shipping", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Documentation\n- [x] docs updated"}
		comments := []Comment{{Text: "docs: updated docs"}}
		err := wf.Validate(issue, "shipping", comments)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("has_comment_prefix docs: fails for shipping", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Documentation\n- [x] docs updated"}
		comments := []Comment{{Text: "some other comment"}}
		err := wf.Validate(issue, "shipping", comments)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("done requires approval", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Shipping\n- [x] committed\n- [x] pushed\n- [x] pr"}
		err := wf.Validate(issue, "done", nil)
		if err == nil {
			t.Fatal("expected error for missing approval")
		}
	})

	t.Run("done passes with approval and checked shipping", func(t *testing.T) {
		issue := &Issue{BodyRaw: "## Shipping\n- [x] committed\n- [x] pushed\n- [x] pr", HumanApproval: "done"}
		err := wf.Validate(issue, "done", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unknown status", func(t *testing.T) {
		issue := &Issue{BodyRaw: "content"}
		err := wf.Validate(issue, "nonexistent", nil)
		if err == nil {
			t.Fatal("expected error for unknown status")
		}
	})
}

func TestValidateTransition_WithActions(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "backlog"},
			{Name: "in progress"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "backlog",
				To:   "in progress",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "has_assignee"},
					{Type: "require_human_approval", Status: "in progress"},
				},
			},
		},
	}

	err := wf.ValidateTransition(&Issue{Slug: "x", Assignee: "alice"}, "backlog", "in progress", nil)
	if err == nil {
		t.Fatal("expected missing approval to fail")
	}

	err = wf.ValidateTransition(&Issue{Slug: "x", Assignee: "alice", HumanApproval: "in progress"}, "backlog", "in progress", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTransition(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "backlog"},
			{Name: "in progress"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "backlog",
				To:   "in progress",
				Actions: []WorkflowAction{
					{Type: "append_section", Title: "Implementation", Body: "- [ ] Code complete"},
					{Type: "inject_prompt", Prompt: "Implement carefully"},
					{Type: "set_fields", Field: "assignee", Value: ""},
				},
			},
		},
	}

	issue := &Issue{BodyRaw: "Existing", HumanApproval: "in progress"}
	result := wf.ApplyTransition(issue, "backlog", "in progress")

	if result.Update.Status == nil || *result.Update.Status != "in progress" {
		t.Fatalf("status update = %#v", result.Update.Status)
	}
	if result.Update.Assignee == nil || *result.Update.Assignee != "" {
		t.Fatalf("assignee update = %#v", result.Update.Assignee)
	}
	if result.Update.HumanApproval == nil || *result.Update.HumanApproval != "" {
		t.Fatalf("human_approval update = %#v", result.Update.HumanApproval)
	}
	if result.Update.Body == nil || !strings.Contains(*result.Update.Body, "## Implementation") {
		t.Fatalf("body update missing implementation section: %#v", result.Update.Body)
	}
	if len(result.InjectedPrompts) != 1 || result.InjectedPrompts[0] != "Implement carefully" {
		t.Fatalf("unexpected prompts: %#v", result.InjectedPrompts)
	}
}

func TestApplyTransition_DoesNotDuplicateExistingSection(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "idea"},
			{Name: "in design"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "idea",
				To:   "in design",
				Actions: []WorkflowAction{
					{Type: "append_section", Title: "Idea", Body: "- [ ] Problem described clearly"},
					{Type: "append_section", Title: "Design", Body: "- [ ] Acceptance criteria defined as checkboxes"},
				},
			},
		},
	}

	body := "## Idea\n- [ ] Problem described clearly\n\n## Design\n- [ ] Existing design checklist"
	issue := &Issue{BodyRaw: body}
	result := wf.ApplyTransition(issue, "idea", "in design")

	if result.Update.Body != nil {
		t.Fatalf("expected no body update, got %#v", *result.Update.Body)
	}
	if result.BodyChanged {
		t.Fatal("expected body to remain unchanged")
	}
	if issue.BodyRaw != body {
		t.Fatalf("issue body changed:\n%s", issue.BodyRaw)
	}
}

func TestApplyTransition_SecondRunIsIdempotent(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "idea"},
			{Name: "in design"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "idea",
				To:   "in design",
				Actions: []WorkflowAction{
					{Type: "append_section", Title: "Idea", Body: "- [ ] Problem described clearly"},
					{Type: "append_section", Title: "Design", Body: "- [ ] Acceptance criteria defined as checkboxes"},
				},
			},
		},
	}

	issue := &Issue{BodyRaw: "Problem statement"}

	first := wf.ApplyTransition(issue, "idea", "in design")
	if !first.BodyChanged || first.Update.Body == nil {
		t.Fatalf("expected first transition to scaffold body, got %#v", first)
	}

	bodyAfterFirst := issue.BodyRaw
	second := wf.ApplyTransition(issue, "idea", "in design")

	if second.Update.Body != nil {
		t.Fatalf("expected second transition to skip body update, got %#v", *second.Update.Body)
	}
	if second.BodyChanged {
		t.Fatalf("expected second transition to leave body unchanged, got %#v", second)
	}
	if issue.BodyRaw != bodyAfterFirst {
		t.Fatalf("issue body changed on second run:\n%s", issue.BodyRaw)
	}
}

func TestFindHeadingMatches_IgnoresFencedCodeBlocks(t *testing.T) {
	body := "intro\n" +
		"\n" +
		"```markdown\n" +
		"## Documentation\n" +
		"- [ ] inside fence\n" +
		"```\n" +
		"\n" +
		"## Implementation\n" +
		"- [x] real heading\n"

	if got := findHeadingMatches(body, "Documentation"); len(got) != 0 {
		t.Fatalf("findHeadingMatches matched fenced heading: %#v", got)
	}
	if got := findHeadingMatches(body, "Implementation"); len(got) != 1 {
		t.Fatalf("findHeadingMatches missed real heading: %#v", got)
	}

	// Tilde fences and indentation should also be respected.
	tildeBody := "  ~~~\n" +
		"## Documentation\n" +
		"  ~~~\n"
	if got := findHeadingMatches(tildeBody, "Documentation"); len(got) != 0 {
		t.Fatalf("findHeadingMatches matched heading inside tilde fence: %#v", got)
	}
}

func TestApplyTransitionToFile_AppendsDocumentationWhenBodyHasFencedExample(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "fenced.md")
	content := "---\n" +
		"title: \"fenced example\"\n" +
		"status: \"human-testing\"\n" +
		"human_approval: \"documentation\"\n" +
		"assignee: \"agent-fenced\"\n" +
		"---\n" +
		"\n" +
		"The body shows what a Documentation section looks like:\n" +
		"\n" +
		"```markdown\n" +
		"## Documentation\n" +
		"- [ ] User-facing docs updated if behavior changed\n" +
		"```\n" +
		"\n" +
		"## Human Testing\n" +
		"- [x] Human verification complete if required\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf, err := LoadWorkflow(filepath.Join("..", "..", "workflow.yaml"))
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	_, result, err := wf.ApplyTransitionToFile(fp, "documentation")
	if err != nil {
		t.Fatalf("ApplyTransitionToFile: %v", err)
	}
	if !result.BodyAppended {
		t.Fatal("BodyAppended = false; the fenced example should not block the real append")
	}

	got, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	body := string(got)

	// The real `## Documentation` section must follow `## Human Testing`
	// (the last existing section), and both new checkboxes must be present.
	humanIdx := strings.Index(body, "## Human Testing")
	docsIdx := strings.LastIndex(body, "## Documentation")
	if humanIdx < 0 || docsIdx < humanIdx {
		t.Fatalf("real `## Documentation` heading not appended after `## Human Testing`:\n%s", body)
	}
	if !strings.Contains(body[docsIdx:], "- [ ] User-facing docs updated if behavior changed") {
		t.Fatalf("appended Documentation section missing first checkbox:\n%s", body)
	}
	if !strings.Contains(body[docsIdx:], "- [ ] Docs comment added") {
		t.Fatalf("appended Documentation section missing second checkbox:\n%s", body)
	}
}

func TestApplyTransitionToFile_HumanTestingToDocumentation_PersistsAppendedSection(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "14.md")
	content := `---
title: "sample"
status: "human-testing"
human_approval: "documentation"
assignee: "agent-sample"
---

Some prose.

## Implementation
- [x] Code changes complete

## Test Plan
### Automated
- [x] Automated verification recorded

### Manual
- [x] Manual verification listed if needed

## Testing
- [x] Relevant tests for changed code passing
- [x] Known unrelated failures documented if full suite is red
- [x] Test results logged as comment

## Human Testing
- [x] Human verification complete if required


<!-- issue-viewer-comments
{"id":1,"block":0,"date":"2026-04-28","text":"tests: ok","status":"open","source":"cli"}
-->`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	// Use the project workflow.yaml so the test exercises the same definitions
	// agents hit at runtime, not just the hard-coded DefaultWorkflow.
	wf, err := LoadWorkflow(filepath.Join("..", "..", "workflow.yaml"))
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	from, result, err := wf.ApplyTransitionToFile(fp, "documentation")
	if err != nil {
		t.Fatalf("ApplyTransitionToFile: %v", err)
	}
	if from != "human-testing" {
		t.Fatalf("from = %q, want human-testing", from)
	}
	if !result.BodyAppended {
		t.Fatalf("BodyAppended = false, want true (Documentation should be appended)")
	}

	got, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	body := string(got)

	if !strings.Contains(body, "## Documentation") {
		t.Fatalf("on-disk body missing `## Documentation` heading after transition:\n%s", body)
	}
	if !strings.Contains(body, "- [ ] User-facing docs updated if behavior changed") {
		t.Fatalf("on-disk body missing first Documentation checkbox:\n%s", body)
	}
	if !strings.Contains(body, "- [ ] Docs comment added") {
		t.Fatalf("on-disk body missing second Documentation checkbox:\n%s", body)
	}

	// Comment block must survive the rewrite.
	if !strings.Contains(body, "<!-- issue-viewer-comments") {
		t.Fatalf("on-disk body lost the comments block:\n%s", body)
	}
}

func TestPreviewTransition(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "backlog"},
			{Name: "in progress"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "backlog",
				To:   "in progress",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "has_assignee"},
					{Type: "append_section", Title: "Implementation", Body: "- [ ] Code complete"},
					{Type: "inject_prompt", Prompt: "Implement carefully"},
					{Type: "set_fields", Field: "assignee", Value: ""},
				},
			},
		},
	}

	preview := wf.PreviewTransition(&Issue{Slug: "x", BodyRaw: "Existing", Assignee: "alice"}, "backlog", "in progress", "", nil)

	if !preview.Allowed {
		t.Fatalf("preview unexpectedly blocked: %#v", preview)
	}
	if len(preview.Steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(preview.Steps))
	}
	if preview.Steps[0].Outcome != "passed" {
		t.Fatalf("first step outcome = %q, want passed", preview.Steps[0].Outcome)
	}
	if preview.Steps[1].Outcome != "changed" {
		t.Fatalf("append step outcome = %q, want changed", preview.Steps[1].Outcome)
	}
	if preview.Result.Update.Status == nil || *preview.Result.Update.Status != "in progress" {
		t.Fatalf("status update = %#v", preview.Result.Update.Status)
	}
}

func TestStartIssueOnce_SecondRunIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "backlog"
human_approval: "in progress"
---

Body.
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	first, err := wf.StartIssueOnce(fp, "sample", "alice")
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	if first.Issue.StartedAt == "" {
		t.Fatal("started_at should be recorded")
	}
	if !first.Transitioned {
		t.Fatal("first start should record transition backlog → in progress")
	}
	if first.Issue.Status != "in progress" {
		t.Fatalf("first status = %q, want in progress", first.Issue.Status)
	}

	second, err := wf.StartIssueOnce(fp, "sample", "alice")
	if err != nil {
		t.Fatalf("second start unexpectedly failed: %v", err)
	}
	if second.Transitioned {
		t.Fatal("second start should not transition")
	}
	if second.Claimed {
		t.Fatal("second start should not re-claim")
	}
	if second.Issue.Status != "in progress" {
		t.Fatalf("second status = %q, want in progress", second.Issue.Status)
	}
	if second.Issue.StartedAt != first.Issue.StartedAt {
		t.Fatalf("started_at changed on second run: %q → %q", first.Issue.StartedAt, second.Issue.StartedAt)
	}
}

func TestStartIssueOnce_ClaimsWorkStatusWithoutTransition(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "in progress"
---

## Implementation
- [ ] do the thing
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	got, err := wf.StartIssueOnce(fp, "sample", "alice")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if got.Transitioned {
		t.Fatal("expected no transition when starting at a work status")
	}
	if !got.Claimed {
		t.Fatal("expected claim on an unassigned work status")
	}
	if got.Issue.Assignee != "alice" {
		t.Fatalf("assignee = %q, want alice", got.Issue.Assignee)
	}
	if got.Issue.Status != "in progress" {
		t.Fatalf("status = %q, want in progress", got.Issue.Status)
	}
	if got.Issue.StartedAt == "" {
		t.Fatal("started_at should be recorded on first claim")
	}
}

func TestStartIssueOnce_ClaimsIdeaWithoutTransition(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "idea"
---

Rough thought.
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	got, err := wf.StartIssueOnce(fp, "sample", "alice")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if got.Transitioned {
		t.Fatal("idea is a work status — no transition expected")
	}
	if !got.Claimed {
		t.Fatal("expected claim")
	}
	if got.Issue.Status != "idea" {
		t.Fatalf("status = %q, want idea", got.Issue.Status)
	}
}

func TestStartIssueOnce_RejectsDone(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "done"
---

Body.
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	if _, err := wf.StartIssueOnce(fp, "sample", "alice"); err == nil {
		t.Fatal("expected start to reject done issues")
	} else if !strings.Contains(err.Error(), "already done") {
		t.Fatalf("error = %q, want 'already done' message", err)
	}
}

func TestStartIssueOnce_HumanTestingRequiresApproval(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "human-testing"
assignee: "alice"
---

Body.
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	if _, err := wf.StartIssueOnce(fp, "sample", "alice"); err == nil {
		t.Fatal("expected start at human-testing to require approval")
	} else {
		if !strings.Contains(err.Error(), `human approval for "documentation" is missing`) {
			t.Fatalf("error = %q, want documentation approval failure", err)
		}
		if !strings.Contains(err.Error(), "no changes were made") {
			t.Fatalf("error = %q, want no-mutation message", err)
		}
	}
}

func TestMarkIssueDoneOnce_RejectsSecondRun(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "shipping"
assignee: "alice"
human_approval: "done"
---

## Shipping
- [x] Changes committed with a message referencing the issue
- [x] Commit pushed to the remote
- [x] PR opened if applicable
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	issue, err := wf.MarkIssueDoneOnce(fp, "sample")
	if err != nil {
		t.Fatalf("first done failed: %v", err)
	}
	if issue.DoneAt == "" {
		t.Fatal("done_at should be recorded")
	}
	if issue.Status != "done" {
		t.Fatalf("status = %q, want done", issue.Status)
	}

	if _, err := wf.MarkIssueDoneOnce(fp, "sample"); err == nil {
		t.Fatal("second done should fail")
	}
}

func TestStartIssueOnceMissingApprovalDoesNotClaimIssue(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "issue.md")
	content := `---
title: "Sample"
status: "backlog"
---

## Acceptance Criteria
- [ ] ship it
`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	if _, err := wf.StartIssueOnce(fp, "sample", "alice"); err == nil {
		t.Fatal("expected missing approval to fail")
	} else {
		if !strings.Contains(err.Error(), `human approval for "in progress" is missing`) {
			t.Fatalf("error = %q, want approval-specific failure", err)
		}
		if !strings.Contains(err.Error(), "no changes were made") {
			t.Fatalf("error = %q, want explicit no-mutation guidance", err)
		}
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read issue: %v", err)
	}
	issue, err := ParseIssue(filepath.Base(fp), data)
	if err != nil {
		t.Fatalf("parse issue: %v", err)
	}
	if issue.Assignee != "" {
		t.Fatalf("issue assignee = %q, want empty", issue.Assignee)
	}
	if issue.StartedAt != "" {
		t.Fatalf("issue started_at = %q, want empty", issue.StartedAt)
	}
	if issue.Status != "backlog" {
		t.Fatalf("issue status = %q, want backlog", issue.Status)
	}
}

func TestPreviewTransition_SkipsExistingSection(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "idea"},
			{Name: "in design"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "idea",
				To:   "in design",
				Actions: []WorkflowAction{
					{Type: "append_section", Title: "Idea", Body: "- [ ] Problem described clearly"},
				},
			},
		},
	}

	body := "## Idea\n- [ ] Existing content"
	preview := wf.PreviewTransition(&Issue{Slug: "x", BodyRaw: body}, "idea", "in design", "", nil)

	if !preview.Allowed {
		t.Fatalf("preview unexpectedly blocked: %#v", preview)
	}
	if len(preview.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(preview.Steps))
	}
	if preview.Steps[0].Outcome != "skipped" {
		t.Fatalf("step outcome = %q, want skipped", preview.Steps[0].Outcome)
	}
	if preview.Result.Update.Body != nil {
		t.Fatalf("expected no body update, got %#v", *preview.Result.Update.Body)
	}
	if preview.Result.BodyChanged {
		t.Fatalf("expected preview result to leave body unchanged, got %#v", preview.Result)
	}
}

func TestPreviewTransition_Failure(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "backlog"},
			{Name: "in progress"},
		},
		Transitions: []WorkflowTransition{
			{
				From: "backlog",
				To:   "in progress",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "has_assignee"},
					{Type: "require_human_approval", Status: "in progress"},
				},
			},
		},
	}

	preview := wf.PreviewTransition(&Issue{Slug: "x"}, "backlog", "in progress", "", nil)

	if preview.Allowed {
		t.Fatal("expected preview to be blocked")
	}
	if preview.ValidationError == "" {
		t.Fatal("expected validation error")
	}
	if len(preview.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(preview.Steps))
	}
	if preview.Steps[0].Outcome != "failed" {
		t.Fatalf("step outcome = %q, want failed", preview.Steps[0].Outcome)
	}
}

func TestNextStatus(t *testing.T) {
	wf := DefaultWorkflow()

	tests := []struct {
		current string
		want    string
	}{
		{"idea", "in design"},
		{"testing", "human-testing"},
		{"human-testing", "documentation"},
		{"documentation", "shipping"},
		{"shipping", "done"},
		{"done", ""},
		{"none", ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		got := wf.NextStatus(tt.current)
		if got != tt.want {
			t.Errorf("NextStatus(%q) = %q, want %q", tt.current, got, tt.want)
		}
	}
}

func TestLoadWorkflow(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "workflow.yaml")

	content := `statuses:
  - name: "todo"
    description: "To do"
    prompt: "Clarify the work before starting"
  - name: "doing"
    description: "In progress"
  - name: "done"
    description: "Complete"
transitions:
  - from: "todo"
    to: "doing"
    actions:
      - type: validate
        rule: body_not_empty
systems:
  Combat:
    transitions:
      - from: "doing"
        to: "done"
        actions:
          - type: inject_prompt
            prompt: "Combat-specific guidance"
`
	os.WriteFile(fp, []byte(content), 0644)

	wf, err := LoadWorkflow(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(wf.Statuses) != 3 {
		t.Fatalf("got %d statuses, want 3", len(wf.Statuses))
	}

	if wf.Statuses[0].Name != "todo" {
		t.Errorf("first status = %q, want %q", wf.Statuses[0].Name, "todo")
	}
	if wf.Statuses[0].Prompt != "Clarify the work before starting" {
		t.Errorf("prompt = %q, want %q", wf.Statuses[0].Prompt, "Clarify the work before starting")
	}

	if len(wf.Transitions) != 1 {
		t.Fatalf("got %d transitions, want 1", len(wf.Transitions))
	}
	if wf.Transitions[0].Actions[0].Rule != "body_not_empty" {
		t.Errorf("rule = %q, want %q", wf.Transitions[0].Actions[0].Rule, "body_not_empty")
	}
	if _, ok := wf.Systems["Combat"]; !ok {
		t.Fatal("expected Combat system overlay")
	}
}

func TestEntryPrompts(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "backlog"},
			{Name: "in progress", Prompt: "Implement according to the accepted design."},
		},
		Transitions: []WorkflowTransition{
			{
				From: "backlog",
				To:   "in progress",
				Actions: []WorkflowAction{
					{Type: "inject_prompt", Prompt: "Update the Implementation section while coding."},
				},
			},
		},
	}

	prompts := wf.EntryPrompts("backlog", "in progress")
	if len(prompts) != 2 {
		t.Fatalf("got %d prompts, want 2", len(prompts))
	}
	if prompts[0] != "Implement according to the accepted design." {
		t.Fatalf("status prompt = %q", prompts[0])
	}
	if prompts[1] != "Update the Implementation section while coding." {
		t.Fatalf("transition prompt = %q", prompts[1])
	}
}

func TestGetBoardColumns_ConfiguredReturnsInOrder(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "idea"},
			{Name: "backlog"},
			{Name: "in progress"},
			{Name: "done"},
		},
		Board: WorkflowBoardConfig{
			Columns: []string{"in progress", "backlog", "done"},
		},
	}
	got := wf.GetBoardColumns()
	want := []string{"in progress", "backlog", "done"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGetBoardColumns_FallsBackToAllStatuses(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{Name: "idea"},
			{Name: "backlog"},
			{Name: "done"},
		},
	}
	got := wf.GetBoardColumns()
	want := []string{"idea", "backlog", "done"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGetBoardCardFields_ConfiguredReturnsInOrder(t *testing.T) {
	wf := &WorkflowConfig{
		Board: WorkflowBoardConfig{
			CardFields: []string{"priority", "system", "labels"},
		},
	}
	got := wf.GetBoardCardFields()
	want := []string{"priority", "system", "labels"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGetBoardCardFields_FallsBackToDefault(t *testing.T) {
	wf := &WorkflowConfig{}
	got := wf.GetBoardCardFields()
	want := defaultBoardCardFields
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadWorkflow_InvalidFile(t *testing.T) {
	_, err := LoadWorkflow("/nonexistent/path/workflow.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestWorkflowSchemaSections_EveryYAMLFieldHasDescTag(t *testing.T) {
	// Drift guard: every yaml-tagged field on any workflow struct must also
	// carry a `desc:"..."` tag. WorkflowSchemaSections() is what
	// `issue-cli process schema` prints, so a missing desc tag = a field
	// that silently ships with no agent-visible documentation.
	types := []reflect.Type{
		reflect.TypeOf(WorkflowConfig{}),
		reflect.TypeOf(WorkflowStatus{}),
		reflect.TypeOf(WorkflowTransition{}),
		reflect.TypeOf(WorkflowAction{}),
		reflect.TypeOf(WorkflowBoardConfig{}),
		reflect.TypeOf(WorkflowOverlay{}),
	}
	for _, tp := range types {
		for i := 0; i < tp.NumField(); i++ {
			f := tp.Field(i)
			yamlTag := f.Tag.Get("yaml")
			if yamlTag == "" || yamlTag == "-" {
				continue
			}
			if f.Tag.Get("desc") == "" {
				t.Errorf("%s.%s has yaml tag %q but no desc tag", tp.Name(), f.Name, yamlTag)
			}
		}
	}
}

func TestWorkflowActionTypes_CoverAllHandledTypes(t *testing.T) {
	// Every action.Type the preview switch handles must appear in
	// WorkflowActionTypes so the schema command can document it.
	handled := []string{
		"validate",
		"require_human_approval",
		"append_section",
		"inject_prompt",
		"set_fields",
	}
	names := map[string]bool{}
	for _, a := range WorkflowActionTypes {
		names[a.Name] = true
	}
	for _, want := range handled {
		if !names[want] {
			t.Errorf("WorkflowActionTypes missing entry for %q", want)
		}
	}
}

func TestWorkflowValidationRules_CoverAllHandledRules(t *testing.T) {
	// Every rule name checkRule recognizes must be documented. The entries
	// include argument syntax (e.g. "approved_for: <status>"), so we match
	// on the bare rule prefix.
	handled := []string{
		"body_not_empty",
		"has_checkboxes",
		"section_has_checkboxes",
		"has_assignee",
		"all_checkboxes_checked",
		"section_checkboxes_checked",
		"has_test_plan",
		"has_comment_prefix",
		"approved_for",
		"human_approval",
	}
	for _, want := range handled {
		found := false
		for _, r := range WorkflowValidationRules {
			if r.Name == want || strings.HasPrefix(r.Name, want+":") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("WorkflowValidationRules missing entry for %q", want)
		}
	}
}
