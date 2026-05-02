package validations

import (
	"strings"
	"testing"
)

func TestFieldMatches_Pass(t *testing.T) {
	a := Action{Rule: "field_matches", Field: "pr", Pattern: `^https://github\.com/.*/pull/\d+$`}
	if err := FieldMatches(a, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestFieldMatches_Mismatch(t *testing.T) {
	iss := sampleIssue()
	iss.Frontmatter["pr"] = "not-a-url"
	a := Action{Rule: "field_matches", Field: "pr", Pattern: `^https://github\.com/.*/pull/\d+$`}
	err := FieldMatches(a, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch, got %v", err)
	}
}

func TestFieldMatches_BadRegex(t *testing.T) {
	a := Action{Rule: "field_matches", Field: "pr", Pattern: `[unterminated`}
	err := FieldMatches(a, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "invalid regex") {
		t.Fatalf("expected invalid-regex error, got %v", err)
	}
}
