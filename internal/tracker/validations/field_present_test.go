package validations

import (
	"strings"
	"testing"
)

func TestFieldPresent_Pass(t *testing.T) {
	if err := FieldPresent(Action{Rule: "field_present", Field: "priority"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestFieldPresent_Missing(t *testing.T) {
	err := FieldPresent(Action{Rule: "field_present", Field: "ghost"}, sampleIssue(), Config{})
	if err == nil {
		t.Fatal("expected missing error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing") || !strings.Contains(msg, "issue-cli set-meta") {
		t.Fatalf("error should describe missing + remediation, got: %s", msg)
	}
}

func TestFieldPresent_RequiresField(t *testing.T) {
	if err := FieldPresent(Action{Rule: "field_present"}, sampleIssue(), Config{}); err == nil {
		t.Fatal("expected configuration error for missing 'field'")
	}
}
