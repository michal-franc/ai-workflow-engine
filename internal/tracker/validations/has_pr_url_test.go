package validations

import (
	"strings"
	"testing"
)

func TestHasPRURL_Pass(t *testing.T) {
	if err := HasPRURL(Action{Rule: "has_pr_url"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestHasPRURL_Missing(t *testing.T) {
	iss := sampleIssue()
	delete(iss.Frontmatter, "pr")
	err := HasPRURL(Action{Rule: "has_pr_url"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "PR url") {
		t.Fatalf("expected pr-url error, got %v", err)
	}
}

func TestHasPRURL_BadFormat(t *testing.T) {
	iss := sampleIssue()
	iss.Frontmatter["pr"] = "https://example.com/something"
	err := HasPRURL(Action{Rule: "has_pr_url"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "PR url") {
		t.Fatalf("expected format error, got %v", err)
	}
}
