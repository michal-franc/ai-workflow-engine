package validations

import (
	"strings"
	"testing"
)

func TestSectionMinLength_Pass(t *testing.T) {
	if err := SectionMinLength(Action{Rule: "section_min_length", Section: "Design", Min: 10}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestSectionMinLength_TooShort(t *testing.T) {
	err := SectionMinLength(Action{Rule: "section_min_length", Section: "Design", Min: 10000}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "need ≥10000") {
		t.Fatalf("expected too-short error, got %v", err)
	}
}

func TestSectionMinLength_MissingSection(t *testing.T) {
	err := SectionMinLength(Action{Rule: "section_min_length", Section: "Ghost", Min: 10}, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("expected missing-section error, got %v", err)
	}
}

func TestSectionMinLength_RequiresMin(t *testing.T) {
	if err := SectionMinLength(Action{Rule: "section_min_length", Section: "Design"}, sampleIssue(), Config{}); err == nil {
		t.Fatal("expected error for missing/zero min")
	}
}
