package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/viewerstate"
)

func TestApprovalHint_DefaultURL(t *testing.T) {
	proj := &tracker.Project{Slug: "demo"}

	got := approvalHint(proj, "fix-bug", "in progress")

	want := "http://localhost:8080/p/demo/issue/fix-bug#approve-in-progress"
	if !strings.Contains(got, want) {
		t.Errorf("hint missing default deep link\n  got:  %q\n  want substring: %q", got, want)
	}
	if !strings.Contains(got, "set viewer_url in projects.yaml") {
		t.Errorf("hint should suggest viewer_url when it is unset; got: %q", got)
	}
}

func TestApprovalHint_HonoursViewerURL(t *testing.T) {
	// The environment no longer moves the link.
	t.Setenv("ISSUE_VIEWER_URL", "http://ignored.example:1")
	proj := &tracker.Project{Slug: "demo", ViewerURL: "https://issues.example.com:9000"}

	got := approvalHint(proj, "fix-bug", "in progress")

	want := "https://issues.example.com:9000/p/demo/issue/fix-bug#approve-in-progress"
	if !strings.Contains(got, want) {
		t.Errorf("hint did not honor viewer_url\n  got:  %q\n  want substring: %q", got, want)
	}
	// With viewer_url set we trust the user's setup; the nudge would be noise.
	if strings.Contains(got, "set viewer_url") {
		t.Errorf("hint should not suggest viewer_url when it is set; got: %q", got)
	}
}

// Without viewer_url the links follow the running viewer that serves this
// config (viewer.json), and ignore one serving another config.
func TestViewerBaseURL_FollowsTheRunningViewer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	cfg := filepath.Join(dir, "projects.yaml")
	mustWrite(t, cfg, "projects: []\n")
	if err := viewerstate.Write(viewerstate.State{Config: cfg, URL: "http://127.0.0.1:9123"}); err != nil {
		t.Fatal(err)
	}
	if got := issueURL(&tracker.Project{Slug: "demo", ConfigPath: cfg}, "a/b"); got != "http://127.0.0.1:9123/p/demo/issue/a/b" {
		t.Errorf("issueURL = %q", got)
	}
	if got := viewerBaseURL(&tracker.Project{Slug: "demo", ConfigPath: filepath.Join(dir, "other.yaml")}); got != "http://localhost:8080" {
		t.Errorf("another config's viewer: %q", got)
	}
}

func TestApprovalHint_NoProject(t *testing.T) {
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
