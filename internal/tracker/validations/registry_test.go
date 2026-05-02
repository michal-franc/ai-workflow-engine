package validations

import (
	"strings"
	"testing"
)

func TestRegistry_HasAllValidators(t *testing.T) {
	expected := []string{
		"field_present", "field_not_empty", "field_in", "field_matches",
		"has_label", "has_any_label",
		"has_pr_url", "linked_issue_in_status",
		"has_section", "section_min_length", "section_max_length", "no_todo_markers",
		"command_succeeds",
	}
	for _, name := range expected {
		if !Has(name) {
			t.Errorf("registry missing %q", name)
		}
	}
}

func TestCheck_UnknownRule(t *testing.T) {
	err := Check(Action{Rule: "made_up_rule"}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "unknown structured rule") {
		t.Fatalf("expected unknown-rule error, got %v", err)
	}
}

func TestCheck_DispatchesToValidator(t *testing.T) {
	err := Check(Action{Rule: "field_in", Field: "priority", Values: []string{"only-this"}}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("expected dispatch to field_in, got %v", err)
	}
}
