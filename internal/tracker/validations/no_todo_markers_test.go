package validations

import (
	"strings"
	"testing"
)

func TestNoTodoMarkers_Pass(t *testing.T) {
	if err := NoTodoMarkers(Action{Rule: "no_todo_markers"}, sampleIssue(), Config{}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestNoTodoMarkers_TODO(t *testing.T) {
	iss := sampleIssue()
	iss.BodyRaw += "\nTODO: finish this\n"
	err := NoTodoMarkers(Action{Rule: "no_todo_markers"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "TODO/FIXME") {
		t.Fatalf("expected todo error, got %v", err)
	}
}

func TestNoTodoMarkers_FIXME(t *testing.T) {
	iss := sampleIssue()
	iss.BodyRaw = "FIXME me later"
	err := NoTodoMarkers(Action{Rule: "no_todo_markers"}, iss, Config{})
	if err == nil || !strings.Contains(err.Error(), "FIXME") {
		t.Fatalf("expected FIXME error, got %v", err)
	}
}

func TestNoTodoMarkers_LowercaseIgnored(t *testing.T) {
	iss := sampleIssue()
	iss.BodyRaw = "todo lowercase should not match"
	if err := NoTodoMarkers(Action{Rule: "no_todo_markers"}, iss, Config{}); err != nil {
		t.Fatalf("lowercase should not match, got %v", err)
	}
}

func TestNoTodoMarkers_WordBoundary(t *testing.T) {
	iss := sampleIssue()
	iss.BodyRaw = "FIXMEUP not a marker"
	if err := NoTodoMarkers(Action{Rule: "no_todo_markers"}, iss, Config{}); err != nil {
		t.Fatalf("word-bounded marker should not match, got %v", err)
	}
}
