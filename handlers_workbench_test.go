package main

// Shared fixture for the workbench pages (decisions, balance) and the
// approve endpoint's queued follow-up approval.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const workbenchWorkflow = `statuses:
  - name: "in design"
  - name: "backlog"
  - name: "in progress"
  - name: "done"
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: validate
        rule: "section_checkboxes_checked: Design"
      - type: require_human_approval
        status: "backlog"
  - from: "backlog"
    to: "in progress"
    actions:
      - type: require_human_approval
        status: "in progress"
      - type: validate
        rule: has_assignee
  - from: "in progress"
    to: "done"
    actions:
      - type: validate
        rule: all_checkboxes_checked
      - type: require_human_approval
        status: "done"
workbench:
  systems:
    Economy:
      summary: "Gold and shops"
  tuning:
    - ../outside.cs
`

func writeWorkbenchIssue(t *testing.T, dir, name, status, extra, body string) string {
	t.Helper()
	fp := filepath.Join(dir, name+".md")
	content := "---\ntitle: \"" + name + "\"\nstatus: \"" + status + "\"\nsystem: \"Economy\"\n" + extra + "---\n\n" + body + "\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return fp
}

type workbenchFixture struct {
	proj  tracker.Project
	paths map[string]string
}

func setupWorkbenchProject(t *testing.T, now time.Time) workbenchFixture {
	t.Helper()
	root := t.TempDir()
	issueDir := filepath.Join(root, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}
	wfPath := filepath.Join(root, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(workbenchWorkflow), 0644); err != nil {
		t.Fatal(err)
	}
	designDone := "## Design\n- [x] Approach documented"
	f := workbenchFixture{
		proj:  tracker.Project{Name: "Workbench Project", Slug: "wb", IssueDir: issueDir, WorkflowFile: wfPath},
		paths: map[string]string{},
	}
	f.paths["ready"] = writeWorkbenchIssue(t, issueDir, "ready", "in design", "assignee: \"agent-ready\"\n", designDone)
	f.paths["unready"] = writeWorkbenchIssue(t, issueDir, "unready", "in design", "", "## Design\n- [ ] Approach documented")
	f.paths["approved"] = writeWorkbenchIssue(t, issueDir, "approved", "in design", "human_approval: \"backlog\"\n", designDone)
	f.paths["idle"] = writeWorkbenchIssue(t, issueDir, "idle", "backlog", "", "Waiting in backlog.")
	f.paths["blocked"] = writeWorkbenchIssue(t, issueDir, "blocked", "backlog", "", "An agent ran start --wait.")
	f.paths["asked"] = writeWorkbenchIssue(t, issueDir, "asked", "in progress", "assignee: \"agent-asked\"\n", "- [ ] still building")

	// ready entered "in design" 3h ago; blocked has an agent waiting for 5h;
	// asked has an open question.
	if err := tracker.AppendTransitionStat(f.paths["ready"], tracker.TransitionStat{From: "idea", To: "in design", TS: now.Add(-3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.BeginWait(f.paths["blocked"], "in progress", now.Add(-5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	q := tracker.NewQuestion{
		Text: "Does unspent gold carry over?",
		Options: []tracker.QuestionOption{
			{ID: "A", Label: "Reset", Tradeoff: "Simplest"},
			{ID: "B", Label: "Carry over, capped at 300", Recommended: true},
		},
		Links: []string{"docs/economy.md", "https://example.com/diff"},
	}
	if _, err := tracker.AddQuestion(f.paths["asked"], q, now.Add(-41*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.BeginQuestionWait(f.paths["asked"], "Q-1", now.Add(-40*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestHandleApprove_ThenQueuesSecondApproval(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-ready"}})
	var nudge string
	withMockTmuxSendKeys(t, func(target string, lines []string) error {
		nudge = strings.Join(lines, " ")
		return nil
	})
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	url := ts.URL + "/p/wb/issue/ready/approve"

	if resp := pagePost(t, url, `{"status":"backlog","then":"done"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid then status = %d, want 400", resp.StatusCode)
	}

	resp := pagePost(t, url, `{"status":"backlog","then":"in progress"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d", resp.StatusCode)
	}
	var payload approvalResponse
	json.NewDecoder(resp.Body).Decode(&payload)
	if payload.HumanApproval != "backlog" || payload.QueuedApproval != "in progress" {
		t.Fatalf("payload = %+v", payload)
	}
	if !strings.Contains(nudge, "Approved for backlog and in progress") || !strings.Contains(nudge, "issue-cli start ready") {
		t.Fatalf("nudge = %q", nudge)
	}

	wf := f.proj.LoadWorkflow()
	if _, _, err := wf.ApplyTransitionToFile(f.paths["ready"], "backlog"); err != nil {
		t.Fatalf("design → backlog: %v", err)
	}
	data, _ := os.ReadFile(f.paths["ready"])
	issue, _ := tracker.ParseIssue("ready.md", data)
	if issue.HumanApproval != "in progress" || issue.QueuedApproval != "" {
		t.Fatalf("after transition: human_approval=%q queued=%q", issue.HumanApproval, issue.QueuedApproval)
	}
}

func TestHandleApprove_UnapproveClearsQueued(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	url := ts.URL + "/p/wb/issue/ready/approve"
	pagePost(t, url, `{"status":"backlog","then":"in progress"}`)
	pagePost(t, url, `{"status":"backlog"}`) // toggle off
	data, _ := os.ReadFile(f.paths["ready"])
	if strings.Contains(string(data), "queued_approval") || strings.Contains(string(data), "human_approval") {
		t.Fatalf("un-approve left approvals behind:\n%s", data)
	}
}

func TestHandleApprove_ThenAddsFollowUpToExistingApproval(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	url := ts.URL + "/p/wb/issue/approved/approve"

	// approved already has human_approval: backlog; asking for the follow-up
	// adds it instead of toggling the approval off.
	var payload approvalResponse
	json.NewDecoder(pagePost(t, url, `{"status":"backlog","then":"in progress"}`).Body).Decode(&payload)
	if payload.HumanApproval != "backlog" || payload.QueuedApproval != "in progress" {
		t.Fatalf("payload = %+v", payload)
	}
	// The same request again toggles both off.
	var off approvalResponse
	json.NewDecoder(pagePost(t, url, `{"status":"backlog","then":"in progress"}`).Body).Decode(&off)
	if off.HumanApproval != "" || off.QueuedApproval != "" {
		t.Fatalf("second click payload = %+v", off)
	}
}

func TestDetail_ApproveBothButton(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/wb/issue/ready")
	for _, want := range []string{
		`id="approve-backlog"`,
		`id="approve-then-backlog" data-testid="detail.approve_then.backlog" class="approve-btn approve-then-btn "`,
		`data-then-status="backlog" data-then="in progress"`,
		`Approved for backlog and in progress`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	// in progress → done needs approval, but done is not a handoff status:
	// no combined button.
	if page := pageGet(t, ts.URL+"/p/wb/issue/asked"); strings.Contains(page, "approve-then-") {
		t.Error("combined approve button shown for a non-handoff approval")
	}

	pagePost(t, ts.URL+"/p/wb/issue/ready/approve", `{"status":"backlog","then":"in progress"}`)
	page = pageGet(t, ts.URL+"/p/wb/issue/ready")
	if !strings.Contains(page, `id="approve-then-backlog" data-testid="detail.approve_then.backlog" class="approve-btn approve-then-btn approved"`) {
		t.Error("combined approve button not shown as approved once queued")
	}
}

func TestInboxRoutesRemoved(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	for _, path := range []string{"/inbox", "/inbox.json"} {
		resp, err := http.Get(ts.URL + "/p/wb" + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s = 200, want the route gone", path)
		}
	}
	resp := pagePost(t, ts.URL+"/p/wb/issue/asked/questions/Q-1/answer", `{"option":"B"}`)
	if resp.StatusCode == http.StatusOK {
		t.Error("the inbox answer endpoint still answers")
	}
}

func pageGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	return string(body)
}

func pagePost(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
