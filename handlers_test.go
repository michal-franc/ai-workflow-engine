package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// helper: create a temp directory with test issue files and return a Project
func setupTestProject(t *testing.T) (tracker.Project, string) {
	t.Helper()
	tmpDir := t.TempDir()
	issueDir := filepath.Join(tmpDir, "issues")
	docsDir := filepath.Join(tmpDir, "docs")
	os.MkdirAll(issueDir, 0755)
	os.MkdirAll(docsDir, 0755)

	// Create test issues
	issues := map[string]string{
		"bug-in-login.md": `---
title: "Bug in login"
status: "in progress"
system: "Auth"
version: "1.0"
labels:
  - bug
  - urgent
priority: "high"
assignee: "alice"
created: "2025-01-15"
---

Login page crashes on submit.
`,
		"add-dark-mode.md": `---
title: "Add dark mode"
status: "backlog"
system: "UI"
version: "2.0"
labels:
  - enhancement
priority: "medium"
assignee: "bob"
created: "2025-01-10"
---

We need dark mode support.
`,
		"fix-typo.md": `---
title: "Fix typo"
status: "done"
system: "Docs"
priority: "low"
created: "2025-01-05"
---

Fix typo in readme.
`,
	}

	for name, content := range issues {
		if err := os.WriteFile(filepath.Join(issueDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create test doc
	doc := `---
title: "Getting Started"
order: 1
---

Welcome to the project.
`
	if err := os.WriteFile(filepath.Join(docsDir, "getting-started.md"), []byte(doc), 0644); err != nil {
		t.Fatal(err)
	}

	proj := tracker.Project{
		Name:     "Test Project",
		Slug:     "test-project",
		IssueDir: issueDir,
		DocsDir:  docsDir,
	}
	return proj, tmpDir
}

func newTestServer(t *testing.T, projects []tracker.Project) *httptest.Server {
	t.Helper()
	srv, err := NewServer(projects)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Routes())
}

func withMockTmuxSessions(t *testing.T, sessions []AgentSession) {
	t.Helper()
	original := listTmuxSessions
	listTmuxSessions = func() []AgentSession { return sessions }
	t.Cleanup(func() {
		listTmuxSessions = original
	})
}

func withMockTmuxSendKeys(t *testing.T, fn func(target string, lines []string) error) {
	t.Helper()
	original := tmuxSendKeys
	tmuxSendKeys = fn
	t.Cleanup(func() {
		tmuxSendKeys = original
	})
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatal(err)
		}
	})
}

// --- handleProjectList ---

func TestHandleProjectList_RedirectsSingleProject(t *testing.T) {
	proj, _ := setupTestProject(t)
	srv, err := NewServer([]tracker.Project{proj})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/p/test-project/" {
		t.Fatalf("expected redirect to /p/test-project/, got %s", loc)
	}
}

func TestHandleProjectList_ListsMultipleProjects(t *testing.T) {
	proj1, _ := setupTestProject(t)
	proj2 := tracker.Project{
		Name:     "Second Project",
		Slug:     "second-project",
		IssueDir: proj1.IssueDir, // reuse dir
		DocsDir:  proj1.DocsDir,
	}

	ts := newTestServer(t, []tracker.Project{proj1, proj2})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleProjectList_404ForNonRootPath(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj, proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// --- handleList ---

func TestHandleList_ReturnsIssueList(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content-type, got %s", ct)
	}
}

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

func TestHandleList_IncludesRetrosTab(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), `href="/p/test-project/retros"`) {
		t.Fatalf("expected list page to link to retros tab\n%s", string(body))
	}
}

func TestHandleList_FiltersWork(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	tests := []struct {
		name  string
		query string
	}{
		{"filter by status", "?status=done"},
		{"filter by system", "?system=Auth"},
		{"filter by priority", "?priority=high"},
		{"filter by label", "?label=bug"},
		{"filter by assignee", "?assignee=alice"},
		{"filter by search", "?search=login"},
		{"combined filters", "?status=in+progress&system=Auth&priority=high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/p/test-project/" + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestHandleRetros_ShowsProjectRetrosAndRelatedToolBugs(t *testing.T) {
	proj, tmpDir := setupTestProject(t)
	withWorkingDir(t, tmpDir)

	retroDir := filepath.Join(tmpDir, "retros")
	if err := os.MkdirAll(retroDir, 0755); err != nil {
		t.Fatal(err)
	}
	retro := `# Workflow Retrospective

Issue: bug-in-login
Title: Bug in login
Status: in progress
System: Auth
Date: 2026-04-03T10:00:00Z
ReviewStatus: open

The workflow prompt was clear.

- Good handoff
- Missing reproduction notes
`
	if err := os.WriteFile(filepath.Join(retroDir, "20260403-100000-bug-in-login.md"), []byte(retro), 0644); err != nil {
		t.Fatal(err)
	}

	bugDir := filepath.Join(tmpDir, "bugs")
	if err := os.MkdirAll(bugDir, 0755); err != nil {
		t.Fatal(err)
	}
	relatedBug := `{"description":"append duplicated a workflow section","issue_slug":"bug-in-login","tool":"issue-cli","ts":"2026-04-03T11:00:00Z"}`
	unrelatedBug := `{"description":"not part of this project","issue_slug":"some-other-issue","tool":"issue-cli","ts":"2026-04-03T11:30:00Z"}`
	if err := os.WriteFile(filepath.Join(bugDir, "related.json"), []byte(relatedBug), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bugDir, "unrelated.json"), []byte(unrelatedBug), 0644); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/retros")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	for _, want := range []string{
		"Project-scoped workflow feedback",
		"Bug in login",
		"Good handoff",
		`href="/p/test-project/issue/bug-in-login"`,
		"append duplicated a workflow section",
		"related.json",
		"Review Retros And Bugs",
		"Open retros",
		"Open related bugs",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected retros page to contain %q\n%s", want, html)
		}
	}
	if strings.Contains(html, "not part of this project") {
		t.Fatalf("expected retros page to exclude unrelated global bug reports\n%s", html)
	}
}

func TestHandleRetros_FilterAndStatusUpdates(t *testing.T) {
	proj, tmpDir := setupTestProject(t)
	withWorkingDir(t, tmpDir)

	retroDir := filepath.Join(tmpDir, "retros")
	if err := os.MkdirAll(retroDir, 0755); err != nil {
		t.Fatal(err)
	}
	retro := `# Workflow Retrospective

Issue: bug-in-login
Title: Bug in login
Status: in progress
System: Auth
Date: 2026-04-03T10:00:00Z
ReviewStatus: open

Needs triage.
`
	retroPath := filepath.Join(retroDir, "retro.md")
	if err := os.WriteFile(retroPath, []byte(retro), 0644); err != nil {
		t.Fatal(err)
	}

	bugDir := filepath.Join(tmpDir, "bugs")
	if err := os.MkdirAll(bugDir, 0755); err != nil {
		t.Fatal(err)
	}
	bugPath := filepath.Join(bugDir, "bug.json")
	bug := `{"description":"append duplicated a workflow section","issue_slug":"bug-in-login","tool":"issue-cli","ts":"2026-04-03T11:00:00Z","status":"open"}`
	if err := os.WriteFile(bugPath, []byte(bug), 0644); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	reqBody := strings.NewReader(`{"status":"processed"}`)
	resp, err := http.Post(ts.URL+"/p/test-project/retros/retro/retro.md/status", "application/json", reqBody)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	updatedRetro, err := os.ReadFile(retroPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedRetro), "ReviewStatus: processed") {
		t.Fatalf("expected retro file to be marked processed\n%s", string(updatedRetro))
	}

	reqBody = strings.NewReader(`{"status":"fixed"}`)
	resp, err = http.Post(ts.URL+"/p/test-project/retros/bug/bug.json/status", "application/json", reqBody)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	updatedBug, err := os.ReadFile(bugPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedBug), `"status":"fixed"`) {
		t.Fatalf("expected bug file to be marked fixed\n%s", string(updatedBug))
	}

	resp, err = http.Get(ts.URL + "/p/test-project/retros?retro_status=processed&bug_status=fixed")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{"Review processed", "status-fixed"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected filtered retros page to contain %q\n%s", want, html)
		}
	}
}

func TestHandleRetrosReviewDispatch_UsesReviewPrompt(t *testing.T) {
	proj, tmpDir := setupTestProject(t)
	withWorkingDir(t, tmpDir)

	retroDir := filepath.Join(tmpDir, "retros")
	if err := os.MkdirAll(retroDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retroDir, "retro.md"), []byte(`# Workflow Retrospective

Issue: bug-in-login
Title: Bug in login
Status: in progress
System: Auth
Date: 2026-04-03T10:00:00Z
ReviewStatus: open

Needs triage.
`), 0644); err != nil {
		t.Fatal(err)
	}

	var gotPrompt string
	var gotSession string
	var gotIssueSlug string
	var gotAgent string
	origDispatch := dispatchAgentSession
	dispatchAgentSession = func(proj *tracker.Project, session string, prompt string, issueSlug string, agentType string) DispatchResponse {
		gotPrompt = prompt
		gotSession = session
		gotIssueSlug = issueSlug
		gotAgent = agentType
		return DispatchResponse{Status: "dispatched", Prompt: prompt, Session: session}
	}
	t.Cleanup(func() {
		dispatchAgentSession = origDispatch
	})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/retros/review", "application/json", strings.NewReader(`{"agent":"codex"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	for _, want := range []string{
		"scan the project retrospectives under retros/",
		"scan related bug reports under bugs/",
		"mark that file with ReviewStatus: processed",
		"update its JSON status to fixed or wontfix",
		"Do not mention issue-cli in your writeup.",
	} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("expected review prompt to contain %q\n%s", want, gotPrompt)
		}
	}
	if gotIssueSlug != "" {
		t.Fatalf("expected no issue slug for project review dispatch, got %q", gotIssueSlug)
	}
	if gotAgent != "codex" {
		t.Fatalf("expected codex agent, got %q", gotAgent)
	}
	if !strings.Contains(gotSession, "test-project-retros-review") {
		t.Fatalf("unexpected session name %q", gotSession)
	}
}

func TestAgentLaunchCommand_CodexUsesPromptFile(t *testing.T) {
	got := agentLaunchCommand("codex", "/tmp/agent-prompt-123.txt")
	if !strings.Contains(got, `codex "$(cat `) {
		t.Fatalf("agentLaunchCommand(codex) = %q, want codex to read from a temp prompt file", got)
	}
	if !strings.Contains(got, `/tmp/agent-prompt-123.txt`) {
		t.Fatalf("agentLaunchCommand(codex) = %q, missing prompt path", got)
	}
}

func TestAgentLaunchCommand_ClaudeRemainsInteractive(t *testing.T) {
	got := agentLaunchCommand("claude", "/tmp/agent-prompt-123.txt")
	if got != "claude" {
		t.Fatalf("agentLaunchCommand(claude) = %q, want %q", got, "claude")
	}
}

func TestHandleList_ShowsActiveBotSummaryAndIssueChip(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"},
		{Name: "agent-unrelated-work", StartTime: "2026-04-02 21:58:59"},
	})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{"1 active bot", "1 agent active"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected list view to contain %q\n%s", want, html)
		}
	}
}

// --- handleBoard ---

func TestHandleBoard_ReturnsBoardView(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/board")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleBoard_Filters(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	tests := []struct {
		name  string
		query string
	}{
		{"version filter", "?version=1.0"},
		{"system filter", "?system=Auth"},
		{"assignee filter", "?assignee=alice"},
		{"claimed filter", "?assignee=_claimed"},
		{"unclaimed filter", "?assignee=_unclaimed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/p/test-project/board" + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestHandleBoard_ShowsActiveBotSummaryAndIssueChip(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"},
		{Name: "agent-unrelated-work", StartTime: "2026-04-02 21:58:59"},
	})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/board")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{"1 active bot", "board-card-agent-active"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected board view to contain %q\n%s", want, html)
		}
	}
}

// --- handleGraph ---

func TestHandleGraph_Returns200(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleGraph_ShowsWorkflowStatuses(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, status := range []string{"backlog", "in progress", "testing"} {
		if !strings.Contains(html, status) {
			t.Fatalf("expected graph to contain status %q", status)
		}
	}
}

func TestHandleGraph_HidesDoneByDefault(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if strings.Contains(html, "Fix typo") {
		t.Fatal("expected done issue to be hidden by default")
	}
}

func TestHandleGraph_ShowDoneFilter(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph?done=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Fix typo") {
		t.Fatal("expected done issue to appear with ?done=1")
	}
}

func TestHandleGraph_SystemFilter(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph?system=Auth")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Bug in login") {
		t.Fatal("expected Auth issue to appear")
	}
	if strings.Contains(html, "Add dark mode") {
		t.Fatal("expected non-Auth issue to be filtered out")
	}
}

func TestHandleGraph_ShowsGraphNavTab(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	for _, path := range []string{"/p/test-project/", "/p/test-project/board", "/p/test-project/graph"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "/graph") {
			t.Fatalf("expected %q page to contain graph nav link", path)
		}
	}
}

func TestBuildAgentPrompt_IncludesCurrentStatusGuidanceAndRetrospectiveTrigger(t *testing.T) {
	issue := &tracker.Issue{
		Slug:     "combat/fix-heat",
		Title:    "Fix heat",
		Status:   "testing",
		System:   "Combat",
		Priority: "high",
		BodyRaw:  "Body text",
	}
	wf := &tracker.WorkflowConfig{
		Statuses: []tracker.WorkflowStatus{
			{Name: "testing", Prompt: "Build relevant automated coverage before handoff."},
		},
	}

	prompt := buildAgentPrompt(issue, wf)

	for _, want := range []string{
		"## Current status guidance",
		"Build relevant automated coverage before handoff.",
		"issue-cli retrospective combat/fix-heat --body",
		"retros/ in the project",
		"Subsystem workflow for Combat:",
		"Missing system-specific instructions:",
		"issue-cli report-bug",
		"bugs/ in the server root",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\n\n%s", want, prompt)
		}
	}
}

// --- handleDetail ---

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

func TestHandleIssuesJSON_IncludesActiveSessions(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"}})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issues.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var issues []issueJSON
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		t.Fatal(err)
	}

	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if !issue.HasActiveAgent {
				t.Fatal("expected bug-in-login to be marked active")
			}
			if len(issue.ActiveSessions) != 1 || issue.ActiveSessions[0].Name != "agent-bug-in-login" {
				t.Fatalf("unexpected active sessions: %+v", issue.ActiveSessions)
			}
			return
		}
	}
	t.Fatal("bug-in-login missing from issues.json")
}

func TestHandleHash_ChangesWhenActiveSessionsChange(t *testing.T) {
	proj, _ := setupTestProject(t)

	fetchHash := func(sessions []AgentSession) string {
		withMockTmuxSessions(t, sessions)
		ts := newTestServer(t, []tracker.Project{proj})
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/p/test-project/hash")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var payload map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return payload["hash"]
	}

	hashWithout := fetchHash(nil)
	hashWith := fetchHash([]AgentSession{{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"}})
	if hashWithout == hashWith {
		t.Fatal("expected hash to change when active sessions change")
	}
}

func TestSessionMatchesIssue(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		slug        string
		want        bool
	}{
		{name: "normalized dispatch session", sessionName: "agent-api-integrate-with-claude-session-names-to-show-active-agent-work", slug: "api/integrate-with-claude-session-names-to-show-active-agent-work", want: true},
		{name: "plain slug fragment", sessionName: "claude-watch-bug-in-login", slug: "bug-in-login", want: true},
		{name: "different issue", sessionName: "agent-something-else", slug: "bug-in-login", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionMatchesIssue(tt.sessionName, tt.slug); got != tt.want {
				t.Fatalf("sessionMatchesIssue(%q, %q) = %v, want %v", tt.sessionName, tt.slug, got, tt.want)
			}
		})
	}
}

// --- handleUpdateIssue ---

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

// --- handleDeleteIssue ---

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

// --- handleCreateIssue ---

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

// --- handleAddComment ---

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

// --- handleGetComments ---

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

// --- handleProjectRoutes: unknown project ---

func TestHandleProjectRoutes_404ForUnknownProject(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/unknown-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// --- filterIssues unit tests ---

func TestFilterIssues(t *testing.T) {
	issues := []*tracker.Issue{
		{Title: "Bug A", Status: "in progress", System: "Auth", Priority: "high", Labels: []string{"bug"}, Assignee: "alice", BodyRaw: "auth login bug"},
		{Title: "Feature B", Status: "backlog", System: "UI", Priority: "medium", Labels: []string{"enhancement"}, Assignee: "bob", BodyRaw: "dark mode"},
		{Title: "Bug C", Status: "done", System: "Auth", Priority: "low", Labels: []string{"bug", "docs"}, Assignee: "", BodyRaw: "typo fix"},
		{Title: "Feature D", Status: "idea", System: "API", Priority: "critical", Labels: []string{"enhancement", "api"}, Assignee: "alice", BodyRaw: "new endpoint"},
	}

	tests := []struct {
		name     string
		filter   FilterParams
		expected int
	}{
		{
			name:     "no filter returns all",
			filter:   FilterParams{},
			expected: 4,
		},
		{
			name:     "filter by status",
			filter:   FilterParams{Status: "in progress"},
			expected: 1,
		},
		{
			name:     "filter by system (case insensitive)",
			filter:   FilterParams{System: "auth"},
			expected: 2,
		},
		{
			name:     "filter by priority",
			filter:   FilterParams{Priority: "high"},
			expected: 1,
		},
		{
			name:     "filter by label",
			filter:   FilterParams{Label: "bug"},
			expected: 2,
		},
		{
			name:     "filter by label case insensitive",
			filter:   FilterParams{Label: "BUG"},
			expected: 2,
		},
		{
			name:     "filter by assignee",
			filter:   FilterParams{Assignee: "alice"},
			expected: 2,
		},
		{
			name:     "filter by assignee _claimed",
			filter:   FilterParams{Assignee: "_claimed"},
			expected: 3,
		},
		{
			name:     "filter by assignee _unclaimed",
			filter:   FilterParams{Assignee: "_unclaimed"},
			expected: 1,
		},
		{
			name:     "search in title",
			filter:   FilterParams{Search: "Bug"},
			expected: 2,
		},
		{
			name:     "search in body",
			filter:   FilterParams{Search: "dark mode"},
			expected: 1,
		},
		{
			name:     "search case insensitive",
			filter:   FilterParams{Search: "AUTH"},
			expected: 1,
		},
		{
			name:     "combined status and system",
			filter:   FilterParams{Status: "in progress", System: "Auth"},
			expected: 1,
		},
		{
			name:     "combined filters no match",
			filter:   FilterParams{Status: "done", System: "UI"},
			expected: 0,
		},
		{
			name:     "filter by nonexistent label",
			filter:   FilterParams{Label: "nonexistent"},
			expected: 0,
		},
		{
			name:     "combined assignee and priority",
			filter:   FilterParams{Assignee: "alice", Priority: "critical"},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterIssues(issues, tt.filter)
			if len(result) != tt.expected {
				t.Errorf("expected %d issues, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestBoardCardFields_RendersExtraFields(t *testing.T) {
	fn, ok := funcMap["boardCardFields"].(func([]string, *IssueView) []BoardCardField)
	if !ok {
		t.Fatal("boardCardFields funcMap entry has unexpected signature")
	}

	issue := &IssueView{
		Issue: &tracker.Issue{
			System: "Repayments",
			ExtraFields: []tracker.ExtraField{
				{Key: "waiting", Value: "team-input"},
				{Key: "team", Value: "payments"},
				{Key: "participants", IsList: true, Values: []string{"alice", "bob"}},
				{Key: "empty", Value: ""},
			},
		},
	}

	fields := []string{"system", "waiting", "team", "participants", "empty", "unknown"}
	got := fn(fields, issue)

	byName := map[string]BoardCardField{}
	for _, f := range got {
		byName[f.Name] = f
	}

	if byName["system"].Value != "Repayments" {
		t.Errorf("system value = %q, want %q", byName["system"].Value, "Repayments")
	}
	if byName["waiting"].Value != "team-input" {
		t.Errorf("waiting value = %q, want %q", byName["waiting"].Value, "team-input")
	}
	if byName["team"].Value != "payments" {
		t.Errorf("team value = %q, want %q", byName["team"].Value, "payments")
	}
	p, ok := byName["participants"]
	if !ok {
		t.Fatal("participants field missing from result")
	}
	if !p.IsList || len(p.Values) != 2 || p.Values[0] != "alice" || p.Values[1] != "bob" {
		t.Errorf("participants field = %+v, want list [alice bob]", p)
	}
	if _, ok := byName["empty"]; ok {
		t.Errorf("empty extra field should be omitted, got %+v", byName["empty"])
	}
	if _, ok := byName["unknown"]; ok {
		t.Errorf("unknown field with no extra match should be omitted")
	}

	if len(got) < 3 {
		t.Fatalf("result length = %d, want at least 3", len(got))
	}
	if got[0].Name != "system" || got[1].Name != "waiting" || got[2].Name != "team" {
		t.Errorf("ordering not preserved: got %q, %q, %q", got[0].Name, got[1].Name, got[2].Name)
	}
}
