package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowFields_UnmarshalFromYAML(t *testing.T) {
	yamlSrc := `
statuses:
  - name: decision
  - name: deferred
transitions:
  - from: decision
    to: deferred
    fields:
      - name: deferred_to
        prompt: "Deferred to whom?"
        target: frontmatter
        required: true
      - name: deferral_reason
        prompt: "Reason for deferral"
        target: "section:Deferred Record"
        type: multiline
    actions:
      - type: append_section
        title: "Deferred Record"
        body: "- Deferred"
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlSrc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	fields := cfg.TransitionFields("decision", "deferred")
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].Name != "deferred_to" || fields[0].Target != "frontmatter" || !fields[0].Required {
		t.Errorf("first field = %+v", fields[0])
	}
	if fields[1].Target != "section:Deferred Record" || fields[1].Type != "multiline" {
		t.Errorf("second field = %+v", fields[1])
	}
}

func TestApplyTransition_WritesFrontmatterField(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Fields: []WorkflowField{
				{Name: "deferred_to", Prompt: "To whom?", Target: "frontmatter", Required: true},
			},
		}},
	}

	result := wf.ApplyTransitionWithFields(&Issue{Status: "a"}, "a", "b", map[string]string{
		"deferred_to": "alice",
	})
	if got := result.Update.ExtraFields["deferred_to"]; got != "alice" {
		t.Fatalf("extra_fields.deferred_to = %q, want alice", got)
	}
}

func TestApplyTransition_AppendsToSection(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Fields: []WorkflowField{
				{Name: "reason", Prompt: "Why?", Target: "section:Record"},
			},
			Actions: []WorkflowAction{
				{Type: "append_section", Title: "Record", Body: "- Moved to b"},
			},
		}},
	}

	issue := &Issue{Status: "a", BodyRaw: "existing"}
	result := wf.ApplyTransitionWithFields(issue, "a", "b", map[string]string{"reason": "team rebalance"})
	if result.Update.Body == nil {
		t.Fatalf("expected body to change")
	}
	body := *result.Update.Body
	if !strings.Contains(body, "## Record") {
		t.Fatalf("body missing ## Record:\n%s", body)
	}
	if !strings.Contains(body, "**Why?:** team rebalance") {
		t.Fatalf("body missing answer line:\n%s", body)
	}
}

func TestValidateFieldAnswers_BlocksWhenRequiredMissing(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Fields: []WorkflowField{
				{Name: "deferred_to", Prompt: "Who?", Required: true},
			},
		}},
	}

	if err := wf.ValidateFieldAnswers("a", "b", nil); err == nil {
		t.Fatal("expected error for missing required field")
	}
	if err := wf.ValidateFieldAnswers("a", "b", map[string]string{"deferred_to": "  "}); err == nil {
		t.Fatal("whitespace should count as missing")
	}
	if err := wf.ValidateFieldAnswers("a", "b", map[string]string{"deferred_to": "alice"}); err != nil {
		t.Fatalf("unexpected error with value provided: %v", err)
	}
}

func TestApplyTransitionToFile_WritesArbitraryFrontmatterField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	src := "---\ntitle: \"T\"\nstatus: \"a\"\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Fields: []WorkflowField{
				{Name: "deferred_to", Prompt: "To whom?", Target: "frontmatter", Required: true},
			},
		}},
	}

	if _, _, err := wf.ApplyTransitionToFileWithFields(path, "b", map[string]string{"deferred_to": "bob"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, `deferred_to: "bob"`) {
		t.Fatalf("frontmatter missing deferred_to:\n%s", s)
	}
	if !strings.Contains(s, `status: "b"`) {
		t.Fatalf("status not updated:\n%s", s)
	}
}

func TestApplyTransitionToFile_BlocksWhenRequiredFieldMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	src := "---\ntitle: \"T\"\nstatus: \"a\"\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Fields: []WorkflowField{
				{Name: "deferred_to", Prompt: "Who?", Required: true},
			},
		}},
	}

	_, _, err := wf.ApplyTransitionToFileWithFields(path, "b", nil)
	if err == nil {
		t.Fatal("expected error when required field is missing")
	}
	if !strings.Contains(err.Error(), "deferred_to") {
		t.Errorf("error should mention field name, got: %v", err)
	}

	// File should be unchanged — status is still "a".
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `status: "a"`) {
		t.Fatalf("file mutated despite validation failure:\n%s", string(got))
	}
}

func TestResolveTransition_WildcardFallback(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}},
		Transitions: []WorkflowTransition{
			// Specific edge from b → d.
			{From: "b", To: "d", Actions: []WorkflowAction{{Type: "inject_prompt", Prompt: "from-b"}}},
			// Wildcard anything → d.
			{From: "*", To: "d", Fields: []WorkflowField{{Name: "why", Prompt: "Why?", Required: true}}},
		},
	}

	// Specific match wins.
	resolved := wf.ResolveTransition("b", "d")
	if resolved == nil || resolved.From != "b" {
		t.Fatalf("expected specific edge, got %+v", resolved)
	}
	// Wildcard fallback for other sources.
	resolved = wf.ResolveTransition("a", "d")
	if resolved == nil || resolved.From != "*" {
		t.Fatalf("expected wildcard, got %+v", resolved)
	}
	// Fields come from whichever edge resolved.
	if fs := wf.TransitionFields("a", "d"); len(fs) != 1 || fs[0].Name != "why" {
		t.Fatalf("wildcard fields not surfaced: %+v", fs)
	}
	// IsValidTransition accepts wildcard targets.
	if !wf.IsValidTransition("a", "d") {
		t.Errorf("IsValidTransition should honor wildcard")
	}
	// GetTransition stays exact-only (used by Merge etc.).
	if wf.GetTransition("a", "d") != nil {
		t.Errorf("GetTransition must not fall back to wildcard")
	}
}

func TestPreviewTransition_ExposesFields(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Fields: []WorkflowField{
				{Name: "deferred_to", Prompt: "Who?", Required: true},
			},
		}},
	}

	preview := wf.PreviewTransition(&Issue{Status: "a"}, "a", "b", "", nil)
	if len(preview.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(preview.Fields))
	}
	if preview.Fields[0].Name != "deferred_to" {
		t.Errorf("field name = %q", preview.Fields[0].Name)
	}
}

func TestUpdateIssueFrontmatter_ExtraFieldsIgnoresProtectedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	src := "---\ntitle: \"T\"\nstatus: \"a\"\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	update := IssueUpdate{
		ExtraFields: map[string]string{
			"deferred_to": "alice",
			"status":      "hacked", // protected — must be ignored
			"title":       "hacked", // protected — must be ignored
		},
	}
	if err := UpdateIssueFrontmatter(path, update); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, `deferred_to: "alice"`) {
		t.Fatalf("missing arbitrary field:\n%s", s)
	}
	if !strings.Contains(s, `status: "a"`) {
		t.Fatalf("protected status was overwritten:\n%s", s)
	}
	if !strings.Contains(s, `title: "T"`) {
		t.Fatalf("protected title was overwritten:\n%s", s)
	}
}
