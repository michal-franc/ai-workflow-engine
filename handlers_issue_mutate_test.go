package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleApproveIssue_NotifiesActiveSession(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	withMockTmuxSessions(t, []AgentSession{{Name: "agent-add-dark-mode"}})

	var gotTarget string
	var gotLines []string
	withMockTmuxSendKeys(t, func(target string, lines []string) error {
		gotTarget = target
		gotLines = append([]string(nil), lines...)
		return nil
	})

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/p/test-project/issue/add-dark-mode/approve", bytes.NewBufferString(`{"status":"in progress"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload approvalResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}

	if payload.HumanApproval != "in progress" {
		t.Fatalf("human approval = %q, want %q", payload.HumanApproval, "in progress")
	}
	if payload.NotifiedSession != "agent-add-dark-mode" {
		t.Fatalf("notified session = %q, want %q", payload.NotifiedSession, "agent-add-dark-mode")
	}
	if gotTarget != "agent-add-dark-mode" {
		t.Fatalf("tmux target = %q, want %q", gotTarget, "agent-add-dark-mode")
	}
	if len(gotLines) != 1 {
		t.Fatalf("expected 1 tmux line, got %d: %#v", len(gotLines), gotLines)
	}
	if !strings.Contains(gotLines[0], "in progress") {
		t.Fatalf("approval message %q does not mention status %q", gotLines[0], "in progress")
	}
}

func TestHandleApproveIssue_ReportsMissingSessionButPersistsApproval(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	withMockTmuxSessions(t, nil)
	withMockTmuxSendKeys(t, func(target string, lines []string) error {
		t.Fatalf("tmux send-keys should not run without a matching session")
		return nil
	})

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/p/test-project/issue/add-dark-mode/approve", bytes.NewBufferString(`{"status":"in progress"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload approvalResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.HumanApproval != "in progress" {
		t.Fatalf("human approval = %q, want %q", payload.HumanApproval, "in progress")
	}
	if payload.NotificationError == "" {
		t.Fatal("expected notification error for missing session")
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	var found *tracker.Issue
	for _, issue := range issues {
		if issue.Slug == "add-dark-mode" {
			found = issue
			break
		}
	}
	if found == nil {
		t.Fatal("expected issue to exist")
	}
	if found.HumanApproval != "in progress" {
		t.Fatalf("persisted human approval = %q, want %q", found.HumanApproval, "in progress")
	}
}

func TestHandleUpdateIssue_BlocksIllegalStatusJump(t *testing.T) {
	// Status-only updates route through the workflow engine; jumping
	// "in progress" → "done" skips required intermediate statuses and is
	// blocked with 409, same as `issue-cli transition`.
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"status":"done"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["error"] == "" {
		t.Fatalf("expected error message, got %v", result)
	}

	// File must be untouched on a blocked transition.
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Slug == "bug-in-login" && issue.Status != "in progress" {
			t.Fatalf("issue mutated despite block: status=%s", issue.Status)
		}
	}
}

func TestHandleUpdateIssue_AdjacentStatusTransition(t *testing.T) {
	// Legal adjacent transition ("in progress" → "testing") passes through the
	// engine and applies workflow actions (e.g. appends ## Testing).
	proj, _ := setupTestProject(t)
	// Prepare the issue with the checkboxes ## Implementation + test plan so
	// validation passes.
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	content := `---
title: "Bug in login"
status: "in progress"
assignee: "alice"
---

## Implementation
- [x] done

## Test Plan
### Automated
- [ ] a

### Manual
- [ ] m
`
	if err := os.WriteFile(issuePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"status":"testing"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if issue.Status != "testing" {
				t.Fatalf("status = %s, want testing", issue.Status)
			}
			if !strings.Contains(issue.BodyRaw, "## Testing") {
				t.Fatalf("expected ## Testing section appended, body:\n%s", issue.BodyRaw)
			}
			return
		}
	}
	t.Fatal("issue not found")
}

func TestHandleUpdateIssue_BodyOnlyBypassesEngine(t *testing.T) {
	// Body edits from the detail view do not touch status and must not trigger
	// the workflow engine.
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"body":"Updated body text"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if issue.Status != "in progress" {
				t.Fatalf("status mutated unexpectedly: %s", issue.Status)
			}
			if !strings.Contains(issue.BodyRaw, "Updated body text") {
				t.Fatalf("body not updated:\n%s", issue.BodyRaw)
			}
			return
		}
	}
	t.Fatal("issue not found")
}

func TestHandleUpdateIssue_UpdatesPriority(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"priority":"critical"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if issue.Priority != "critical" {
				t.Fatalf("expected priority 'critical', got '%s'", issue.Priority)
			}
			return
		}
	}
	t.Fatal("issue not found after update")
}

func TestHandleUpdateIssue_UpdatesAssignee(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	assignee := "charlie"
	body := `{"assignee":"charlie"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if issue.Assignee != assignee {
				t.Fatalf("expected assignee '%s', got '%s'", assignee, issue.Assignee)
			}
			return
		}
	}
	t.Fatal("issue not found after update")
}

func TestHandleUpdateIssue_404ForUnknownSlug(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"status":"done"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/nonexistent", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleEditIssueInNvim_UsesLauncherAndPreservesComments(t *testing.T) {
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	if err := tracker.AddComment(issuePath, 0, "Keep me", "app"); err != nil {
		t.Fatal(err)
	}

	origLauncher := launchIssueBodyEditor
	t.Cleanup(func() { launchIssueBodyEditor = origLauncher })
	launchIssueBodyEditor = func(proj *tracker.Project, issue *tracker.Issue) (BodyEditResponse, error) {
		updated := "Edited in nvim"
		if err := tracker.UpdateIssueFrontmatter(issue.FilePath, tracker.IssueUpdate{Body: &updated}); err != nil {
			return BodyEditResponse{}, err
		}
		return BodyEditResponse{Status: "launched", Session: "agent-bug-in-login-edit", Message: "Opened in nvim"}, nil
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login/edit-in-nvim", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result BodyEditResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "launched" {
		t.Fatalf("expected launched status, got %#v", result)
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if issue.BodyRaw != "Edited in nvim" {
				t.Fatalf("expected updated body, got %q", issue.BodyRaw)
			}
			comments, err := tracker.LoadComments(issue.FilePath)
			if err != nil {
				t.Fatal(err)
			}
			if len(comments) != 1 || comments[0].Text != "Keep me" {
				t.Fatalf("expected preserved comments, got %#v", comments)
			}
			return
		}
	}
	t.Fatal("issue not found after nvim edit")
}

func TestHandleEditIssueInNvim_404ForUnknownSlug(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/nonexistent/edit-in-nvim", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleUpdateIssue_BadJSON(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login", "application/json", strings.NewReader("{bad"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleDeleteIssue_DeletesIssueFile(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/fix-typo/delete", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify file is gone
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Slug == "fix-typo" {
			t.Fatal("issue should have been deleted")
		}
	}
}

func TestHandleDeleteIssue_404ForUnknownSlug(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/nonexistent/delete", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleCreateIssue_CreatesNewIssue(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"title":"New feature request","status":"idea","system":"UI"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issues/create", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", result)
	}
	if result["slug"] == "" {
		t.Fatal("expected slug in response")
	}

	// Verify the file exists
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	found := false
	for _, issue := range issues {
		if issue.Title == "New feature request" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("created issue not found")
	}
}

func TestHandleCreateIssue_DefaultsStatusToIdea(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"title":"Minimal issue"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issues/create", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	issues, _ := tracker.LoadIssues(proj.IssueDir)
	for _, issue := range issues {
		if issue.Title == "Minimal issue" {
			if issue.Status != "idea" {
				t.Fatalf("expected default status 'idea', got '%s'", issue.Status)
			}
			return
		}
	}
	t.Fatal("created issue not found")
}

func TestHandleCreateIssue_RequiresTitle(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"title":"","status":"idea"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issues/create", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleCreateIssue_BadJSON(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issues/create", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
