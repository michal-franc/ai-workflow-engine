package validations

import (
	"strings"
	"testing"
)

func TestHasSection_Pass(t *testing.T) {
	if err := HasSection(Action{Rule: "has_section", Section: "Design"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestHasSection_CaseInsensitive(t *testing.T) {
	if err := HasSection(Action{Rule: "has_section", Section: "design"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected case-insensitive pass, got %v", err)
	}
}

func TestHasSection_Missing(t *testing.T) {
	err := HasSection(Action{Rule: "has_section", Section: "Nonexistent"}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("expected missing-section error, got %v", err)
	}
}
