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
	proj.TmuxSession = "work"
	stubAgentsGit(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "work:lead-ui-epic"},
		{Name: "work:agent-ui-child"},
		{Name: "work:agent-ui-asker"},
		{Name: "work:agent-agents-ux-review"},
		{Name: "zsh"},
		// Another project's agents on the same tmux server, one with a
		// colliding name: none of them may show up here.
		{Name: "roguetech:agent-ui-idle"},
		{Name: "roguetech:agent-combat-remove-the-2d-view"},
		{Name: "agent-standalone-elsewhere", Path: "/elsewhere"},
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

// Clicking a live agent opens its tmux session in a terminal (D-12); the page
// never shows the pane and has no message box.
func TestAgentOpenAndDetail(t *testing.T) {
	proj := setupAgentsProject(t)
	stubAgentsGit(t)
	var opened []string
	origLive, origAttach := agentSessionLive, attachAgent
	agentSessionLive = func(_ *tracker.Project, name string) bool { return name != "agent-gone" }
	attachAgent = func(_ *tracker.Project, name string, steps *[]agentrun.Step) bool {
		opened = append(opened, name)
		*steps = append(*steps, agentrun.Step{Name: "Open terminal", Status: "ok"})
		return true
	}
	attached := map[string]bool{}
	var focused []string
	focusOK := true
	origAttached, origFocus := agentrun.SessionAttached, agentrun.FocusTerminal
	agentrun.SessionAttached = func(name string) bool { return attached[name] }
	agentrun.FocusTerminal = func(name string) bool {
		focused = append(focused, name)
		return focusOK
	}
	t.Cleanup(func() {
		agentSessionLive, attachAgent = origLive, origAttach
		agentrun.SessionAttached, agentrun.FocusTerminal = origAttached, origFocus
	})
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	code, resp := postJSON(t, ts.URL+"/p/agents/agents/open", `{"session":"lead-ui-epic"}`)
	if code != 200 || resp["status"] != "opened" || len(opened) != 1 || opened[0] != "lead-ui-epic" || resp["attach_cmd"] == "" {
		t.Fatalf("open: %d %v opened=%v", code, resp, opened)
	}
	// A session that already has a terminal never gets a second window: its
	// window is brought to the front, or the answer says it is open.
	attached["lead-ui-epic"] = true
	if code, resp := postJSON(t, ts.URL+"/p/agents/agents/open", `{"session":"lead-ui-epic"}`); code != 200 || resp["status"] != "focused" || len(opened) != 1 || len(focused) != 1 || focused[0] != "lead-ui-epic" {
		t.Fatalf("focus: %d %v opened=%v focused=%v", code, resp, opened, focused)
	}
	focusOK = false
	if code, resp := postJSON(t, ts.URL+"/p/agents/agents/open", `{"session":"lead-ui-epic"}`); code != 200 || resp["status"] != "already-open" || len(opened) != 1 {
		t.Fatalf("already open, no window to focus: %d %v opened=%v", code, resp, opened)
	}
	if code, _ := postJSON(t, ts.URL+"/p/agents/agents/open", `{"session":"agent-gone"}`); code != 409 || len(opened) != 1 {
		t.Fatalf("not running: %d opened=%v", code, opened)
	}
	if code, _ := postJSON(t, ts.URL+"/p/agents/agents/open", `{"session":"zsh; rm"}`); code != 400 {
		t.Fatalf("bad session name: %d", code)
	}
	if code, _ := postJSON(t, ts.URL+"/p/agents/agents/message", `{"session":"lead-ui-epic","text":"hi"}`); code == 200 {
		t.Fatalf("the canvas message endpoint is gone (D-12), got %d", code)
	}

	_, body := getResp(t, ts.URL+"/p/agents/agents/detail?session=agent-ui-asker&issue=ui/asker")
	var d AgentDetail
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatal(err)
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
	if !strings.Contains(html, "lead-ui-epic") {
		t.Errorf("epic page lacks its sub-lead")
	}
	// One lead per project for now (workbench umbrella D-13): no sub-lead button.
	if strings.Contains(html, "Spawn sub-lead") || strings.Contains(html, "spawnSubLead(") {
		t.Errorf("epic page still offers Spawn sub-lead")
	}
	_, html = getResp(t, ts.URL+"/p/agents/issue/ui/child")
	if !strings.Contains(html, `<div class="active-session-name" data-testid="detail.lead.name">lead-agents</div>`) || strings.Contains(html, "spawnSubLead('") {
		t.Errorf("child page should name the project lead (sub-lead not live) and offer no spawn")
	}
}
