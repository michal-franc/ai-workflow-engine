package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// setupAgentsProject builds a project with an epic (ui/epic, with a sub-lead
// recorded), children ui/child (parent:, at a gate), ui/asker (umbrella:,
// asked a question), ui/idle (in progress, nobody on it) and a loner.
func setupAgentsProject(t *testing.T) tracker.Project {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(title, status, extra string) string {
		return "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\n" + extra + "---\n\nBody.\n"
	}
	write("issues/UI/epic.md", issue("Epic", "in progress", "base: \"base/ui/epic\"\nlead: \"lead-ui-epic\"\n"))
	write("issues/UI/child.md", issue("Child", "in progress", "parent: \"ui/epic\"\n"))
	write("issues/UI/asker.md", issue("Asker", "in progress", "umbrella: \"ui/epic\"\n")+
		"<!-- issue-viewer-comments\n{\"id\":1,\"block\":0,\"date\":\"2026-10-18\",\"text\":\"question: which API?\",\"status\":\"open\",\"source\":\"cli\"}\n-->\n")
	write("issues/UI/idle.md", issue("Idle", "in progress", "parent: \"ui/epic\"\n"))
	write("issues/loner.md", issue("Loner", "backlog", ""))
	write("retros/20261018-idle.md", "Issue: ui/idle\nTitle: Idle\n\nIt stopped.\n")
	write("workflow.yaml", `worktree: true
statuses:
  - name: "backlog"
  - name: "in progress"
  - name: "testing"
  - name: "done"
transitions:
  - from: "in progress"
    to: "testing"
    actions:
      - type: require_human_approval
`)
	return tracker.Project{Name: "Agents", Slug: "agents", IssueDir: filepath.Join(root, "issues"), WorkDir: root, WorkflowFile: filepath.Join(root, "workflow.yaml")}
}

func stubAgentsGit(t *testing.T) {
	t.Helper()
	origAhead, origMain, origDiff := mainAhead, mainBranch, branchDiffStat
	mainAhead = func(_, base string) (int, string, bool) { return 2, "main", true }
	mainBranch = func(string) string { return "main" }
	branchDiffStat = func(_, against, branch string) string { return against + "..." + branch + " | 3 +++" }
	t.Cleanup(func() { mainAhead, mainBranch, branchDiffStat = origAhead, origMain, origDiff })
}

func TestAgentsModel(t *testing.T) {
	proj := setupAgentsProject(t)
	stubAgentsGit(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "lead-ui-epic"},
		{Name: "agent-ui-child"},
		{Name: "work:agent-ui-asker"},
		{Name: "agent-agents-ux-review"},
		{Name: "zsh"},
	})
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	_, body := getResp(t, ts.URL+"/p/agents/agents.json")
	var m AgentsModel
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	if m.ProjectLead.Name != "lead-agents" || m.ProjectLead.Live || m.ProjectLead.State != "not-running" {
		t.Errorf("project lead = %+v", m.ProjectLead)
	}
	if len(m.Epics) != 1 {
		t.Fatalf("epics = %+v", m.Epics)
	}
	g := m.Epics[0]
	if g.Issue.Slug != "ui/epic" || !g.HasSubLead || !g.SubLead.Live || g.Base != "base/ui/epic" || g.MainAhead != 2 ||
		g.ChildrenRunning != 2 || g.ChildrenTotal != 3 {
		t.Errorf("epic group = %+v", g)
	}

	states := map[string]AgentNode{}
	for _, n := range m.Workers {
		states[n.Name] = n
	}
	if n := states["agent-ui-child"]; n.State != "at-gate" || n.Gate != "testing" || n.Epic != "ui/epic" || n.Issue == nil || n.Issue.Color == "" {
		t.Errorf("child = %+v", n)
	}
	if n := states["agent-ui-asker"]; n.State != "question" || n.Epic != "ui/epic" {
		t.Errorf("asker (shared-session window, umbrella: link) = %+v", n)
	}
	if len(m.Unfinished) != 2 {
		t.Fatalf("unfinished = %+v", m.Unfinished)
	}
	for _, n := range m.Unfinished {
		switch n.Name {
		case "agent-ui-idle":
			if n.State != "stopped" || !n.RetroNotSwept || n.Epic != "ui/epic" {
				t.Errorf("idle = %+v", n)
			}
		case "agent-ui-epic":
			if n.Epic != "ui/epic" {
				t.Errorf("the epic's own job should sit in its box: %+v", n)
			}
		default:
			t.Errorf("unexpected unfinished %+v (backlog loner must not show)", n)
		}
	}
	if len(m.Others) != 1 || m.Others[0].Name != "agent-agents-ux-review" {
		t.Errorf("others = %+v", m.Others)
	}
}

func TestAgentsPageRenders(t *testing.T) {
	proj := setupAgentsProject(t)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, html := getResp(t, ts.URL+"/p/agents/agents")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, html)
	}
	for _, want := range []string{`class="view-tab active">Agents`, `data-layout="radial"`, `id="opt-stopped"`, `Spawn lead`, `"/p/agents"`} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q", want)
		}
	}
	_, board := getResp(t, ts.URL+"/p/agents/board")
	if !strings.Contains(board, `href="/p/agents/agents"`) || !strings.Contains(board, "spawnProjectLead(") {
		t.Errorf("board lacks the Agents tab or the Spawn lead button")
	}
}

func TestLeadSpawnEndpoint(t *testing.T) {
	proj := setupAgentsProject(t)
	var got agentrun.Spec
	orig := startLead
	startLead = func(_ *tracker.Project, spec agentrun.Spec) agentrun.Response {
		got = spec
		return agentrun.Response{Status: "dispatched", Session: spec.Session}
	}
	t.Cleanup(func() { startLead = orig })
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	code, out := postJSON(t, ts.URL+"/p/agents/lead/spawn", `{}`)
	if code != 200 || out["session"] != "lead-agents" || got.LeadScope != "project" || got.AgentType != "claude" || got.Headless {
		t.Fatalf("project lead: %d %v %+v", code, out, got)
	}

	code, out = postJSON(t, ts.URL+"/p/agents/lead/spawn", `{"epic":"ui/epic"}`)
	if code != 200 || got.Session != "lead-ui-epic" || got.Worktree.Branch != "base/ui/epic" {
		t.Fatalf("sub-lead: %d %v %+v", code, out, got)
	}

	code, out = postJSON(t, ts.URL+"/p/agents/lead/spawn", `{"epic":"loner"}`)
	if code != 422 || !strings.Contains(out["steps"].([]interface{})[0].(map[string]interface{})["detail"].(string), "not an epic") {
		t.Fatalf("non-epic: %d %v", code, out)
	}
}

func TestAgentMessageAndDetail(t *testing.T) {
	proj := setupAgentsProject(t)
	stubAgentsGit(t)
	var gotSession, gotText string
	origPaste := agentPaste
	agentPaste = func(_ *tracker.Project, session, text, _, _ string) agentrun.Response {
		gotSession, gotText = session, text
		if session == "agent-gone" {
			return agentrun.Response{Status: "no-session"}
		}
		return agentrun.Response{Status: "sent", Session: session}
	}
	origCapture := agentrun.CapturePane
	agentrun.CapturePane = func(string) (string, error) { return strings.Repeat("line\n", 30) + "last line\n", nil }
	t.Cleanup(func() { agentPaste, agentrun.CapturePane = origPaste, origCapture })
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	code, _ := postJSON(t, ts.URL+"/p/agents/agents/message", `{"session":"agent-ui-child","text":" hi "}`)
	if code != 200 || gotSession != "agent-ui-child" || gotText != "[from human] hi" {
		t.Fatalf("message: %d %q %q", code, gotSession, gotText)
	}
	if code, _ := postJSON(t, ts.URL+"/p/agents/agents/message", `{"session":"agent-gone","text":"hi"}`); code != 409 {
		t.Fatalf("no session: %d", code)
	}
	if code, _ := postJSON(t, ts.URL+"/p/agents/agents/message", `{"session":"zsh; rm","text":"hi"}`); code != 400 {
		t.Fatalf("bad session name: %d", code)
	}

	_, body := getResp(t, ts.URL+"/p/agents/agents/detail?session=agent-ui-asker&issue=ui/asker")
	var d AgentDetail
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(d.Pane, "\n"); len(lines) != 20 || lines[19] != "last line" {
		t.Errorf("pane tail = %q", d.Pane)
	}
	if len(d.Comments) != 1 || d.Branch != "work/ui/asker" || d.Against != "base/ui/epic" || d.DiffStat != "base/ui/epic...work/ui/asker | 3 +++" {
		t.Errorf("detail = %+v", d)
	}

	_, body = getResp(t, ts.URL+"/p/agents/agents/detail?session=lead-ui-epic&issue=ui/epic")
	json.Unmarshal([]byte(body), &d)
	if d.Branch != "base/ui/epic" || d.Against != "main" {
		t.Errorf("sub-lead detail = %+v", d)
	}
}

func TestDetailShowsLeadSection(t *testing.T) {
	proj := setupAgentsProject(t)
	withMockTmuxHasSession(t, func(string) bool { return false })
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	_, html := getResp(t, ts.URL+"/p/agents/issue/ui/epic")
	if !strings.Contains(html, "lead-ui-epic") || !strings.Contains(html, "spawnSubLead(") {
		t.Errorf("epic page lacks its sub-lead and the Spawn sub-lead button")
	}
	_, html = getResp(t, ts.URL+"/p/agents/issue/ui/child")
	if !strings.Contains(html, `<div class="active-session-name">lead-agents</div>`) || strings.Contains(html, "spawnSubLead('") {
		t.Errorf("child page should name the project lead (sub-lead not live) and offer no spawn")
	}
}
