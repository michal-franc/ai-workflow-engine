package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

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
	dispatchAgentSession = func(proj *tracker.Project, session string, prompt string, issueSlug string, agentType string, viewerURL string) DispatchResponse {
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

	prompt := buildAgentPrompt(nil, issue, wf)

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
	// nil project (bootstrap mode) must NOT inject --project — there's no
	// slug to inject and bots running in a single-project repo don't need it.
	if strings.Contains(prompt, "--project") {
		t.Fatalf("expected nil-project prompt to omit --project flag\n\n%s", prompt)
	}
}

func TestBuildAgentPrompt_InjectsProjectFlagForMultiProjectDispatch(t *testing.T) {
	// In a multi-project setup the dispatcher knows the project; the bot
	// shouldn't have to discover it. Every issue-cli invocation in the
	// prompt must already be scoped with --project <slug> so a
	// dispatched session running from any cwd hits the right project.
	proj := &tracker.Project{Name: "CLI", Slug: "cli"}
	issue := &tracker.Issue{
		Slug:    "cli/some-bug",
		Title:   "Some bug",
		Status:  "in progress",
		System:  "CLI",
		BodyRaw: "body",
	}
	prompt := buildAgentPrompt(proj, issue, &tracker.WorkflowConfig{})

	for _, want := range []string{
		"issue-cli --project cli process workflow",
		"issue-cli --project cli show cli/some-bug",
		"issue-cli --project cli check cli/some-bug",
		"issue-cli --project cli transition cli/some-bug",
		"issue-cli --project cli start cli/some-bug",
		"issue-cli --project cli retrospective cli/some-bug",
		"issue-cli --project cli report-bug",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\n\n%s", want, prompt)
		}
	}
	// Defense in depth: no bare "issue-cli " (no flag) should remain after
	// the rewrite — a plain `issue-cli foo` in a multi-project bot prompt is
	// exactly the bug this code fixes.
	for _, line := range strings.Split(prompt, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "issue-cli ") && !strings.HasPrefix(trimmed, "issue-cli --project ") {
			t.Fatalf("unrewritten issue-cli call leaked into prompt: %q", trimmed)
		}
	}
}
