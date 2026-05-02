package validations

import (
	"strings"
	"testing"
)

func TestSectionMaxLength_Pass(t *testing.T) {
	if err := SectionMaxLength(Action{Rule: "section_max_length", Section: "Design", Max: 1000}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestSectionMaxLength_TooLong(t *testing.T) {
	err := SectionMaxLength(Action{Rule: "section_max_length", Section: "Design", Max: 5}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "max 5") {
		t.Fatalf("expected too-long error, got %v", err)
	}
}

func TestSectionMaxLength_MissingSectionPasses(t *testing.T) {
	if err := SectionMaxLength(Action{Rule: "section_max_length", Section: "Ghost", Max: 5}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("missing section should pass max-length, got %v", err)
	}
}
