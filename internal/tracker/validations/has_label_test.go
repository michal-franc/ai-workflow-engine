package validations

import (
	"strings"
	"testing"
)

func TestHasLabel_Pass(t *testing.T) {
	if err := HasLabel(Action{Rule: "has_label", Field: "bug"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestHasLabel_Missing(t *testing.T) {
	err := HasLabel(Action{Rule: "has_label", Field: "missing"}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "missing label") {
		t.Fatalf("expected missing-label, got %v", err)
	}
	if !strings.Contains(err.Error(), "issue-cli set-meta") {
		t.Fatalf("error should hint set-meta, got: %v", err)
	}
}
