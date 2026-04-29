package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleDetail_ReturnsIssueDetail(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHandleDetail_404ForUnknownSlug(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/nonexistent-issue")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleDetail_BackURLFromBoard(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login?from=board")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHandleDetail_ShowsActiveSessionDetails(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"},
		{Name: "agent-unrelated-work", StartTime: "2026-04-02 21:58:59"},
	})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{"1 active bot", "Active Agent", "agent-bug-in-login", "2026-04-02 21:57:59"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected detail view to contain %q\n%s", want, html)
		}
	}
}

func TestHandleTransitionPreview_ExposesFields(t *testing.T) {
	proj, _ := setupTestProject(t)
	// Provide a workflow with a declarative field on an adjacent transition.
	workflowPath := filepath.Join(t.TempDir(), "workflow.yaml")
	wfSrc := `
statuses:
  - name: "in progress"
  - name: "testing"
transitions:
  - from: "in progress"
    to: "testing"
    fields:
      - name: deferred_to
        prompt: "Who is covering?"
        target: frontmatter
        required: true
`
	if err := os.WriteFile(workflowPath, []byte(wfSrc), 0644); err != nil {
		t.Fatal(err)
	}
	proj.WorkflowFile = workflowPath

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login/transition?to=testing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var preview tracker.TransitionPreview
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Fields) != 1 || preview.Fields[0].Name != "deferred_to" {
		t.Fatalf("unexpected preview fields: %+v", preview.Fields)
	}
}

func TestHandleDetail_ShowsEditInNvimAction(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/bug-in-login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Edit in nvim") {
		t.Fatalf("detail page missing Edit in nvim action: %s", body)
	}
}

func TestHandleDetail_OptionalApprovalHiddenBehindCTA(t *testing.T) {
	tmpDir := t.TempDir()
	issueDir := filepath.Join(tmpDir, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}

	workflow := `statuses:
  - name: "in-review"
    description: "Awaiting review"
  - name: "waiting-for-team-input"
    optional: true
    description: "Parked — blocked on another team"
  - name: "approve-comment-created"
    description: "Approval comment posted"
  - name: "done"
    description: "Completed"

transitions:
  - from: "in-review"
    to: "approve-comment-created"
    actions:
      - type: "require_human_approval"
        status: "approve-comment-created"
  - from: "in-review"
    to: "waiting-for-team-input"
    actions:
      - type: "require_human_approval"
        status: "waiting-for-team-input"
`
	wfPath := filepath.Join(tmpDir, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	issue := `---
title: "Review PR"
status: "in-review"
---

Body
`
	if err := os.WriteFile(filepath.Join(issueDir, "review-pr.md"), []byte(issue), 0644); err != nil {
		t.Fatal(err)
	}

	proj := tracker.Project{
		Name:         "Test",
		Slug:         "test",
		IssueDir:     issueDir,
		WorkflowFile: wfPath,
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test/issue/review-pr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	// Required-path approval renders as today.
	if !strings.Contains(html, "Human-approved for approve-comment-created") {
		t.Fatalf("detail view should show required approval widget:\n%s", html)
	}

	// Optional-path approval is hidden behind a CTA button.
	if !strings.Contains(html, "Divert to waiting-for-team-input") {
		t.Fatalf("detail view should show CTA for optional approval:\n%s", html)
	}
	if !strings.Contains(html, "Parked — blocked on another team") {
		t.Fatalf("CTA should include the optional status description:\n%s", html)
	}

	// The optional approval widget exists but starts hidden.
	ctaIdx := strings.Index(html, "optional-approval-widget hidden")
	if ctaIdx == -1 {
		t.Fatalf("optional approval widget should be hidden by default:\n%s", html)
	}
	if !strings.Contains(html, "Human-approved for waiting-for-team-input") {
		t.Fatalf("optional approval checkbox should still be present in DOM:\n%s", html)
	}
}

func TestHandleDetail_OptionalApprovalUsesConfiguredCTALabel(t *testing.T) {
	tmpDir := t.TempDir()
	issueDir := filepath.Join(tmpDir, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}

	workflow := `statuses:
  - name: "in-review"
  - name: "waiting-for-team-input"
    optional: true
    description: "Parked"
  - name: "done"

transitions:
  - from: "in-review"
    to: "waiting-for-team-input"
    cta_label: "Block on Platform team"
    actions:
      - type: "require_human_approval"
        status: "waiting-for-team-input"
`
	wfPath := filepath.Join(tmpDir, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	issue := `---
title: "Review PR"
status: "in-review"
---

Body
`
	if err := os.WriteFile(filepath.Join(issueDir, "review-pr.md"), []byte(issue), 0644); err != nil {
		t.Fatal(err)
	}

	proj := tracker.Project{
		Name:         "Test",
		Slug:         "test",
		IssueDir:     issueDir,
		WorkflowFile: wfPath,
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test/issue/review-pr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	if !strings.Contains(html, "Block on Platform team") {
		t.Fatalf("CTA should use the configured cta_label:\n%s", html)
	}
	if strings.Contains(html, "Divert to waiting-for-team-input") {
		t.Fatalf("CTA should not fall back to default when cta_label is set:\n%s", html)
	}
}

func TestHandleDetail_OptionalApprovalRevealedWhenSet(t *testing.T) {
	tmpDir := t.TempDir()
	issueDir := filepath.Join(tmpDir, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}

	workflow := `statuses:
  - name: "in-review"
  - name: "waiting-for-team-input"
    optional: true
    description: "Parked"
  - name: "done"

transitions:
  - from: "in-review"
    to: "waiting-for-team-input"
    actions:
      - type: "require_human_approval"
        status: "waiting-for-team-input"
`
	wfPath := filepath.Join(tmpDir, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	issue := `---
title: "Review PR"
status: "in-review"
human_approval: "waiting-for-team-input"
---

Body
`
	if err := os.WriteFile(filepath.Join(issueDir, "review-pr.md"), []byte(issue), 0644); err != nil {
		t.Fatal(err)
	}

	proj := tracker.Project{
		Name:         "Test",
		Slug:         "test",
		IssueDir:     issueDir,
		WorkflowFile: wfPath,
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test/issue/review-pr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	// When approval is already set on the optional path, the widget is revealed
	// (not hidden) and the CTA is hidden so the checked state is visible.
	if !strings.Contains(html, "optional-approval-cta hidden") {
		t.Fatalf("CTA should be hidden once the optional approval is set:\n%s", html)
	}
	if strings.Contains(html, "optional-approval-widget hidden") {
		t.Fatalf("approval widget should be visible once the optional approval is set:\n%s", html)
	}
}

func TestHandleDetail_RendersDataTableAtMarker(t *testing.T) {
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "with-marker.md")
	content := `---
title: "Has marker"
status: "in progress"
---

intro line

<!-- data statuses=open,resolved,wontfix -->

trailing text
`
	if err := os.WriteFile(issuePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.AddEntry(issuePath, "first row", "open"); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issue/has-marker")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, `class="data-table"`) {
		t.Errorf("expected rendered data-table in response")
	}
	if !strings.Contains(html, "first row") {
		t.Errorf("expected entry description in response")
	}
	if !strings.Contains(html, `value="wontfix"`) {
		t.Errorf("expected declared status 'wontfix' in dropdown")
	}
	// Marker comment itself should have been removed.
	if strings.Contains(html, "<!-- data statuses=open,resolved,wontfix -->") {
		t.Errorf("data marker should be replaced, not preserved literally")
	}
}
