package main

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

const inboxWorkflow = `statuses:
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

func writeInboxIssue(t *testing.T, dir, name, status, extra, body string) string {
	t.Helper()
	fp := filepath.Join(dir, name+".md")
	content := "---\ntitle: \"" + name + "\"\nstatus: \"" + status + "\"\nsystem: \"Economy\"\n" + extra + "---\n\n" + body + "\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return fp
}

type inboxFixture struct {
	proj  tracker.Project
	paths map[string]string
}

func setupInboxProject(t *testing.T, now time.Time) inboxFixture {
	t.Helper()
	root := t.TempDir()
	issueDir := filepath.Join(root, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}
	wfPath := filepath.Join(root, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(inboxWorkflow), 0644); err != nil {
		t.Fatal(err)
	}
	designDone := "## Design\n- [x] Approach documented"
	f := inboxFixture{
		proj:  tracker.Project{Name: "Inbox Project", Slug: "inbox", IssueDir: issueDir, WorkflowFile: wfPath},
		paths: map[string]string{},
	}
	f.paths["ready"] = writeInboxIssue(t, issueDir, "ready", "in design", "assignee: \"agent-ready\"\n", designDone)
	f.paths["unready"] = writeInboxIssue(t, issueDir, "unready", "in design", "", "## Design\n- [ ] Approach documented")
	f.paths["approved"] = writeInboxIssue(t, issueDir, "approved", "in design", "human_approval: \"backlog\"\n", designDone)
	f.paths["idle"] = writeInboxIssue(t, issueDir, "idle", "backlog", "", "Waiting in backlog.")
	f.paths["blocked"] = writeInboxIssue(t, issueDir, "blocked", "backlog", "", "An agent ran start --wait.")
	f.paths["asked"] = writeInboxIssue(t, issueDir, "asked", "in progress", "assignee: \"agent-asked\"\n", "- [ ] still building")

	// ready entered "in design" 3h ago; blocked has an agent waiting for 5h.
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

func TestBuildInbox_ListsOnlyItemsWaitingOnAHuman(t *testing.T) {
	now := time.Now()
	f := setupInboxProject(t, now)
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-asked"}})

	issues, err := tracker.LoadIssues(f.proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	sessionMap, _ := sessionsByIssueSlug(issues)
	items := buildInbox(&f.proj, issues, sessionMap, now)

	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	want := []string{"blocked|in progress", "ready|backlog", "asked|Q-1"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("inbox keys = %v, want %v (oldest first; unready, approved, idle excluded)", keys, want)
	}

	blocked, ready, asked := items[0], items[1], items[2]
	if !blocked.AgentBlocked || blocked.WaitLabel() != "5 h 0 m" || !blocked.IsOld() {
		t.Errorf("blocked item = blocked:%v wait:%q", blocked.AgentBlocked, blocked.WaitLabel())
	}
	if ready.AgentBlocked || ready.WaitLabel() != "3 h 0 m" || ready.ThenStatus != "in progress" || ready.From != "in design" {
		t.Errorf("ready item = %+v (wait %s)", ready, ready.WaitLabel())
	}
	if blocked.ThenStatus != "" {
		t.Errorf("in progress approval should not offer approve-and-start, got %q", blocked.ThenStatus)
	}
	if asked.Kind != inboxKindQuestion || !asked.AgentBlocked || !asked.AgentRunning || asked.Session != "agent-asked" || asked.WaitLabel() != "41 m" {
		t.Errorf("question item = %+v (wait %s)", asked, asked.WaitLabel())
	}
}

func TestHandleInbox_RendersApprovalAndQuestion(t *testing.T) {
	f := setupInboxProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := getBody(t, ts.URL+"/p/inbox/inbox?item=ready%7Cbacklog")
	for _, want := range []string{
		"Waiting on you",
		"Approve and start work",
		"Approve backlog only",
		`href="/p/inbox/issue/ready"`,
		"agent not running",
		"Questions 1",
		"Approvals 2",
		"workbench.tuning[0]",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("inbox page missing %q", want)
		}
	}

	page = getBody(t, ts.URL+"/p/inbox/inbox?issue=asked")
	for _, want := range []string{"Does unspent gold carry over?", "Agent recommends", "Carry over, capped at 300", `<code class="inbox-link-path">docs/economy.md</code>`, `href="https://example.com/diff"`, "agent waiting"} {
		if !strings.Contains(page, want) {
			t.Errorf("question pane missing %q", want)
		}
	}

	page = getBody(t, ts.URL+"/p/inbox/inbox?kind=question")
	if strings.Contains(page, "Approve and start work") {
		t.Error("question filter still shows approvals")
	}
}

func TestHandleInbox_EmptyState(t *testing.T) {
	proj, _ := setupTestProject(t)
	proj.WorkflowFile = filepath.Join(t.TempDir(), "missing.yaml")
	withWorkingDir(t, t.TempDir()) // no local workflow.yaml: default workflow
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	page := getBody(t, ts.URL+"/p/test-project/inbox")
	if !strings.Contains(page, "Nothing is waiting on you") {
		t.Fatal("empty inbox state not rendered")
	}
	if strings.Contains(page, "Workbench config") {
		t.Fatal("workbench panel shown without a workbench block")
	}
}

func TestHandleInboxJSON(t *testing.T) {
	f := setupInboxProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	var sum inboxSummary
	if err := json.Unmarshal([]byte(getBody(t, ts.URL+"/p/inbox/inbox.json")), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.Count != 3 || sum.Approvals != 2 || sum.Questions != 1 || sum.Blocked != 2 || sum.Oldest != "5 h 0 m" || sum.Sig == "" {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestHandleAnswerQuestion(t *testing.T) {
	f := setupInboxProject(t, time.Now())
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-asked"}})
	var nudged []string
	withMockTmuxSendKeys(t, func(target string, lines []string) error {
		nudged = append(nudged, target+": "+strings.Join(lines, " "))
		return nil
	})
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	url := ts.URL + "/p/inbox/issue/asked/questions/Q-1/answer"

	resp := postJSON(t, url, `{"option":"Z"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown option status = %d, want 400", resp.StatusCode)
	}

	resp = postJSON(t, url, `{"option":"B","note":"revisit after the economy pass"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status = %d", resp.StatusCode)
	}
	var got answerResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.DecisionID != "D-1" || got.NotifiedSession != "agent-asked" {
		t.Fatalf("answer response = %+v", got)
	}
	if len(nudged) != 1 || !strings.Contains(nudged[0], "Answered Q-1 in the inbox, recorded as D-1 under ## Decisions: Does unspent gold carry over? → **B.") {
		t.Fatalf("nudges = %v", nudged)
	}
	content, _ := os.ReadFile(f.paths["asked"])
	if !strings.Contains(string(content), `→ **B. Carry over, capped at 300**, "revisit after the economy pass"`) {
		t.Fatalf("decision not written:\n%s", content)
	}

	resp = postJSON(t, url, `{"option":"A"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second answer status = %d, want 409", resp.StatusCode)
	}

	var sum inboxSummary
	json.Unmarshal([]byte(getBody(t, ts.URL+"/p/inbox/inbox.json")), &sum)
	if sum.Questions != 0 {
		t.Fatalf("answered question still in inbox: %+v", sum)
	}
}

func TestHandleApprove_ThenQueuesSecondApproval(t *testing.T) {
	f := setupInboxProject(t, time.Now())
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-ready"}})
	var nudge string
	withMockTmuxSendKeys(t, func(target string, lines []string) error {
		nudge = strings.Join(lines, " ")
		return nil
	})
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	url := ts.URL + "/p/inbox/issue/ready/approve"

	if resp := postJSON(t, url, `{"status":"backlog","then":"done"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid then status = %d, want 400", resp.StatusCode)
	}

	resp := postJSON(t, url, `{"status":"backlog","then":"in progress"}`)
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
	f := setupInboxProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	url := ts.URL + "/p/inbox/issue/ready/approve"
	postJSON(t, url, `{"status":"backlog","then":"in progress"}`)
	postJSON(t, url, `{"status":"backlog"}`) // toggle off
	data, _ := os.ReadFile(f.paths["ready"])
	if strings.Contains(string(data), "queued_approval") || strings.Contains(string(data), "human_approval") {
		t.Fatalf("un-approve left approvals behind:\n%s", data)
	}
}

func TestNavLinksToInbox(t *testing.T) {
	f := setupInboxProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	for _, path := range []string{"/", "/board", "/graph", "/docs", "/stats"} {
		page := getBody(t, ts.URL+"/p/inbox"+path)
		if !strings.Contains(page, `href="/p/inbox/inbox"`) || !strings.Contains(page, "/static/inbox-badge.js") {
			t.Errorf("%s: missing Inbox nav link or badge script", path)
		}
	}
}

func getBody(t *testing.T, url string) string {
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

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
