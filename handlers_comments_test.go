package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleAddComment_AddsComment(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"block":0,"text":"This is a test comment"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login/comments", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	// Verify comment was saved
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	comments, err := tracker.LoadComments(issuePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Text != "This is a test comment" {
		t.Fatalf("expected comment text 'This is a test comment', got '%s'", comments[0].Text)
	}
}

func TestHandleAddComment_RequiresText(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"block":0,"text":""}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login/comments", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleAddComment_404ForUnknownIssue(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"block":0,"text":"comment"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/nonexistent/comments", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleGetComments_ReturnsEmptyArray(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login/comments")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var comments []tracker.Comment
	json.NewDecoder(resp.Body).Decode(&comments)
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(comments))
	}
}

func TestHandleGetComments_ReturnsExistingComments(t *testing.T) {
	proj, _ := setupTestProject(t)

	// Add a comment directly to the file
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	tracker.AddComment(issuePath, 0, "Existing comment", "test")

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login/comments")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var comments []tracker.Comment
	json.NewDecoder(resp.Body).Decode(&comments)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Text != "Existing comment" {
		t.Fatalf("expected 'Existing comment', got '%s'", comments[0].Text)
	}
}

func TestHandleGetComments_404ForUnknownIssue(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/nonexistent/comments")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
