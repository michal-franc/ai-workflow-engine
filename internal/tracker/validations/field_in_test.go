package validations

import (
	"strings"
	"testing"
)

func TestFieldIn_Pass(t *testing.T) {
	a := Action{Rule: "field_in", Field: "priority", Values: []string{"low", "medium", "high", "critical"}}
	if err := FieldIn(a, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestFieldIn_NotIn(t *testing.T) {
	iss := sampleIssue()
	iss.Frontmatter["priority"] = "weird"
	a := Action{Rule: "field_in", Field: "priority", Values: []string{"low", "medium", "high", "critical"}}
	err := FieldIn(a, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("expected enum error, got %v", err)
	}
}

func TestFieldIn_RequiresValues(t *testing.T) {
	if err := FieldIn(Action{Rule: "field_in", Field: "priority"}, sampleIssue(), Config{}); err == nil {
		t.Fatal("expected error for empty 'values'")
	}
}
