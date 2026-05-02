package validations

import (
	"strings"
	"testing"
)

func TestHasAnyLabel_Pass(t *testing.T) {
	if err := HasAnyLabel(Action{Rule: "has_any_label"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestHasAnyLabel_Empty(t *testing.T) {
	iss := sampleIssue()
	iss.Labels = nil
	err := HasAnyLabel(Action{Rule: "has_any_label"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "no labels") {
		t.Fatalf("expected no-labels error, got %v", err)
	}
}
