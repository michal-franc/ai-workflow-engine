package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestApprovalHint_DefaultURL(t *testing.T) {
	t.Setenv("ISSUE_VIEWER_URL", "")
	proj := &tracker.Project{Slug: "demo"}

	got := approvalHint(proj, "fix-bug", "in progress")

	want := "http://localhost:8080/p/demo/issue/fix-bug#approve-in-progress"
	if !strings.Contains(got, want) {
		t.Errorf("hint missing default deep link\n  got:  %q\n  want substring: %q", got, want)
	}
	if !strings.Contains(got, "set ISSUE_VIEWER_URL") {
		t.Errorf("hint should suggest ISSUE_VIEWER_URL when env is unset; got: %q", got)
	}
}

func TestApprovalHint_HonoursEnvVar(t *testing.T) {
	t.Setenv("ISSUE_VIEWER_URL", "https://issues.example.com:9000/")
	proj := &tracker.Project{Slug: "demo"}

	got := approvalHint(proj, "fix-bug", "in progress")

	want := "https://issues.example.com:9000/p/demo/issue/fix-bug#approve-in-progress"
	if !strings.Contains(got, want) {
		t.Errorf("hint did not honor ISSUE_VIEWER_URL\n  got:  %q\n  want substring: %q", got, want)
	}
	// When the env var is set we trust the user's setup; the
	// "set ISSUE_VIEWER_URL if..." nudge would be noise.
	if strings.Contains(got, "set ISSUE_VIEWER_URL") {
		t.Errorf("hint should not suggest setting env var when already set; got: %q", got)
	}
}

func TestApprovalHint_NoProject(t *testing.T) {
	t.Setenv("ISSUE_VIEWER_URL", "")
	got := approvalHint(nil, "fix-bug", "in progress")
	if !strings.Contains(got, "/issue/fix-bug#approve-in-progress") {
		t.Errorf("hint should still emit issue path without project; got: %q", got)
	}
	if strings.Contains(got, "/p/") {
		t.Errorf("hint should not include /p/ segment when project is nil; got: %q", got)
	}
}

// TestDecorateApprovalError exercises the top-level wrap that adds the
// deep-link hint to errors flowing out of run(). The wrap must preserve
// errors.Is/As so callers (and tests) can still match the underlying error.
func TestDecorateApprovalError(t *testing.T) {
	t.Setenv("ISSUE_VIEWER_URL", "")
	proj := &tracker.Project{Slug: "demo"}
	ctx := &Context{Project: proj}

	src := &tracker.ApprovalMissingError{
		Slug:     "fix-bug",
		Required: "in progress",
		Verb:     "validate",
	}

	out := decorateApprovalError(src, ctx)
	if out == nil {
		t.Fatal("decorateApprovalError returned nil for an approval error")
	}
	if !errors.Is(out, tracker.ErrApprovalMissing) {
		t.Errorf("wrapped error lost errors.Is(ErrApprovalMissing): %v", out)
	}
	var got *tracker.ApprovalMissingError
	if !errors.As(out, &got) {
		t.Errorf("wrapped error lost errors.As(*ApprovalMissingError): %v", out)
	}
	msg := out.Error()
	if !strings.Contains(msg, "http://localhost:8080/p/demo/issue/fix-bug#approve-in-progress") {
		t.Errorf("wrapped message missing deep link; got: %q", msg)
	}
	if !strings.Contains(msg, `not human-approved for "in progress"`) {
		t.Errorf("wrapped message lost original phrasing; got: %q", msg)
	}
}

func TestDecorateApprovalError_PassThrough(t *testing.T) {
	plain := errors.New("something else broke")
	if got := decorateApprovalError(plain, &Context{}); got != plain {
		t.Errorf("non-approval error should pass through unchanged; got %v want %v", got, plain)
	}
	if got := decorateApprovalError(nil, &Context{}); got != nil {
		t.Errorf("nil error should pass through unchanged; got %v", got)
	}
}
