package validations

import (
	"strings"
	"testing"
)

func TestLinkedIssueInStatus_Pass(t *testing.T) {
	cfg := Config{Lookup: func(slug string) *IssueView {
		if slug == "workflow/parent" {
			return &IssueView{Slug: slug, Status: "done"}
		}
		return nil
	}}
	a := Action{Rule: "linked_issue_in_status", RefKey: "parent", LinkedStatus: "done"}
	if err := LinkedIssueInStatus(a, sampleIssue(), cfg); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestLinkedIssueInStatus_WrongStatus(t *testing.T) {
	cfg := Config{Lookup: func(slug string) *IssueView {
		return &IssueView{Slug: slug, Status: "in progress"}
	}}
	a := Action{Rule: "linked_issue_in_status", RefKey: "parent", LinkedStatus: "done"}
	err := LinkedIssueInStatus(a, sampleIssue(), cfg)
	if err == nil || !strings.Contains(err.Error(), "expected \"done\"") {
		t.Fatalf("expected wrong-status error, got %v", err)
	}
}

func TestLinkedIssueInStatus_MissingRef(t *testing.T) {
	cfg := Config{Lookup: func(slug string) *IssueView { return nil }}
	a := Action{Rule: "linked_issue_in_status", RefKey: "ghost", LinkedStatus: "done"}
	err := LinkedIssueInStatus(a, sampleIssue(), cfg)
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty-ref error, got %v", err)
	}
}

func TestLinkedIssueInStatus_NotFound(t *testing.T) {
	cfg := Config{Lookup: func(slug string) *IssueView { return nil }}
	a := Action{Rule: "linked_issue_in_status", RefKey: "parent", LinkedStatus: "done"}
	err := LinkedIssueInStatus(a, sampleIssue(), cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestLinkedIssueInStatus_NoLookup(t *testing.T) {
	a := Action{Rule: "linked_issue_in_status", RefKey: "parent", LinkedStatus: "done"}
	err := LinkedIssueInStatus(a, sampleIssue(), Config{})
	if err == nil || !strings.Contains(err.Error(), "lookup") {
		t.Fatalf("expected lookup-missing error, got %v", err)
	}
}
