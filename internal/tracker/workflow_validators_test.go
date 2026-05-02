package tracker

import (
	"strings"
	"testing"
)

// These tests exercise the tracker-side dispatch wiring: WorkflowConfig
// translates its native types to the validations package and back, and the
// LookupIssue adapter passes through to validations.
//
// Per-validator semantics are covered in
// internal/tracker/validations/*_test.go; this file only checks the
// integration seams.

func TestValidateTransition_DispatchesStructured(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Actions: []WorkflowAction{
				{Type: "validate", Rule: "field_in", Field: "priority", Values: []string{"high"}},
			},
		}},
	}
	pass := &Issue{Slug: "x", Priority: "high"}
	if err := wf.ValidateTransition(pass, "a", "b", nil); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	fail := &Issue{Slug: "x", Priority: "low"}
	if err := wf.ValidateTransition(fail, "a", "b", nil); err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("expected enum failure, got %v", err)
	}
}

func TestValidateTransition_LegacyRuleStillWorks(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Actions: []WorkflowAction{{Type: "validate", Rule: "body_not_empty"}},
		}},
	}
	if err := wf.ValidateTransition(&Issue{Slug: "x", BodyRaw: "real body"}, "a", "b", nil); err != nil {
		t.Fatalf("expected legacy pass, got %v", err)
	}
	if err := wf.ValidateTransition(&Issue{Slug: "x"}, "a", "b", nil); err == nil {
		t.Fatal("expected legacy body_not_empty to fail on empty body")
	}
}

func TestValidateTransition_LookupAdapter(t *testing.T) {
	wf := &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}},
		Transitions: []WorkflowTransition{{
			From: "a", To: "b",
			Actions: []WorkflowAction{
				{Type: "validate", Rule: "linked_issue_in_status", RefKey: "parent", LinkedStatus: "done"},
			},
		}},
		LookupIssue: func(slug string) *Issue {
			if slug == "workflow/parent" {
				return &Issue{Slug: slug, Status: "done"}
			}
			return nil
		},
	}
	iss := &Issue{
		Slug: "child", Status: "in progress",
		ExtraFields: []ExtraField{{Key: "parent", Value: "workflow/parent"}},
	}
	if err := wf.ValidateTransition(iss, "a", "b", nil); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}

	wf.LookupIssue = func(string) *Issue { return &Issue{Slug: "workflow/parent", Status: "in progress"} }
	if err := wf.ValidateTransition(iss, "a", "b", nil); err == nil || !strings.Contains(err.Error(), "expected \"done\"") {
		t.Fatalf("expected status mismatch, got %v", err)
	}
}

func TestToIssueView_FrontmatterFlattens(t *testing.T) {
	iss := &Issue{
		Slug: "s", Title: "T", Status: "idea", Priority: "low", Number: 7, Repo: "o/r",
		ExtraFields: []ExtraField{
			{Key: "pr", Value: "https://github.com/o/r/pull/7"},
			{Key: "tags", IsList: true, Values: []string{"a", "b"}},
		},
	}
	v := toIssueView(iss)
	if v.Frontmatter["pr"] != "https://github.com/o/r/pull/7" {
		t.Errorf("expected pr value, got %q", v.Frontmatter["pr"])
	}
	if !v.HasKey("tags") {
		t.Error("expected tags key to be present (list-typed)")
	}
	if v.Frontmatter["number"] != "7" {
		t.Errorf("expected number flattened to string, got %q", v.Frontmatter["number"])
	}
}

func TestDescribeAction_StructuredSummary(t *testing.T) {
	got := DescribeAction(WorkflowAction{
		Type: "validate", Rule: "field_in", Field: "priority", Values: []string{"low", "high"},
	}, "")
	if !strings.Contains(got, "frontmatter \"priority\"") || !strings.Contains(got, "low, high") {
		t.Fatalf("expected structured summary, got %q", got)
	}
}
