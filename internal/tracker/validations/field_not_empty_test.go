package validations

import (
	"strings"
	"testing"
)

func TestFieldNotEmpty_Pass(t *testing.T) {
	if err := FieldNotEmpty(Action{Rule: "field_not_empty", Field: "priority"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestFieldNotEmpty_Blank(t *testing.T) {
	iss := sampleIssue()
	iss.Frontmatter["priority"] = "   "
	err := FieldNotEmpty(Action{Rule: "field_not_empty", Field: "priority"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "blank") {
		t.Fatalf("expected blank error, got %v", err)
	}
}

func TestFieldNotEmpty_Missing(t *testing.T) {
	iss := sampleIssue()
	delete(iss.Frontmatter, "priority")
	err := FieldNotEmpty(Action{Rule: "field_not_empty", Field: "priority"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "blank") {
		t.Fatalf("missing key should also be reported as blank, got %v", err)
	}
}
