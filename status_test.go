package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// setupStatusProject builds a project for the Status page:
//   - ui/epic (in progress) with children ui/ready (in design, checklist
//     done: a ready gate) and ui/open (in design, checklist open)
//   - ui/asker (in progress, newest comment is a question:)
//   - ui/tested (human-testing, a playtest session with no judge run;
//     moved there 3 days ago per its stats sidecar)
//   - ui/judged (human-testing, a session with a judge run)
//   - ui/shipped (done 2 days ago), ui/old (done 30 days ago)
//   - ui/parked, ui/low and ui/high (backlog)
//
// plus a changelog with 4 releases and a lead handoff note with ## Next.
func setupStatusProject(t *testing.T, now time.Time) tracker.Project {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) string {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	issue := func(title, status, extra, body string) string {
		return "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\n" + extra + "---\n\n" + body + "\n"
	}
	stats := func(issuePath, to string, at time.Time) {
		write(strings.TrimPrefix(tracker.StatsSidecarPath(issuePath), root+"/"),
			`{"transitions":[{"from":"x","to":"`+to+`","ts":"`+at.UTC().Format(time.RFC3339)+`","static_tokens":0,"dynamic_tokens":0,"actual_tokens":null}]}`)
	}

	write("issues/UI/epic.md", issue("Epic", "in progress", "type: \"epic\"\n", "Body."))
	write("issues/UI/ready.md", issue("Ready", "in design", "parent: \"ui/epic\"\n", "## Design\n- [x] Approach documented"))
	write("issues/UI/open.md", issue("Open", "in design", "parent: \"ui/epic\"\n", "## Design\n- [ ] Approach documented"))
	write("issues/UI/asker.md", issue("Asker", "in progress", "", "Body.")+
		"<!-- issue-viewer-comments\n{\"id\":1,\"block\":0,\"date\":\"2026-10-18\",\"text\":\"question: which API?\",\"status\":\"open\",\"source\":\"cli\"}\n-->\n")
	tested := write("issues/UI/tested.md", issue("Tested", "human-testing", "", "Body."))
	stats(tested, "human-testing", now.Add(-72*time.Hour))
	write("issues/UI/judged.md", issue("Judged", "human-testing", "", "Body."))
	shipped := write("issues/UI/shipped.md", issue("Shipped", "done", "", "Body."))
	stats(shipped, "done", now.Add(-48*time.Hour))
	old := write("issues/UI/old.md", issue("Old", "done", "", "Body."))
	stats(old, "done", now.Add(-30*24*time.Hour))
	write("issues/UI/parked.md", issue("Parked", "parked", "",
		"## Parked Record\n- **Why is it parked? (blocked on…):** Blocked on the sidebar."))
	write("issues/UI/low.md", issue("Low", "backlog", "priority: \"low\"\ncreated: \"2026-01-01\"\n", "Body."))
	write("issues/UI/high.md", issue("High", "backlog", "priority: \"high\"\ncreated: \"2026-02-01\"\n", "Body."))

	write("playtest/sessions/tested.yaml", "session: tested-basics\nissue: ui/tested\nsteps: []\n")
	write("playtest/sessions/judged.yaml", "session: judged-basics\nissue: ui/judged\nsteps: []\n")
	write(".playtest/runs/judged-basics/20261020-1200-judge/results.yaml", "session: judged-basics\n")
	write(".playtest/runs/tested-basics/20261020-1200-auto/results.yaml", "session: tested-basics\n")

	write("docs/CHANGELOG.md", `# Changelog

Entries are newest-first. Each entry has the form:

    ## <version> — <YYYY-MM-DD>

## v0.4.0 — 2026-10-21

- **Breaking:** ask is gone,
  questions stay in chat.
- Second bullet.

## v0.3.0 — 2026-10-20

- Three.

## v0.2.0 — 2026-10-19

- Two.

## v0.1.0 — 2026-10-18

- One.
`)
	write("sessions/2026-10-01-lead-session.md", "# Old note\n\n## Next\n1. Stale step.\n")
	write("sessions/2026-10-21-lead-status.md", "# Handoff\n\n## What is live\nStuff.\n\n## Next\n1. Approve `ui/ready`.\n2. Build it.\n\n## Resume\nRun things.\n")

	write("workflow.yaml", `workbench:
  changelog: docs/CHANGELOG.md
statuses:
  - name: "idea"
  - name: "in design"
  - name: "backlog"
  - name: "in progress"
  - name: "human-testing"
  - name: "shipping"
  - name: "done"
  - name: "parked"
    optional: true
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: "validate"
        rule: "section_checkboxes_checked: Design"
      - type: "require_human_approval"
        status: "backlog"
`)
	return tracker.Project{Name: "Status", Slug: "status", IssueDir: filepath.Join(root, "issues"), WorkDir: root, WorkflowFile: filepath.Join(root, "workflow.yaml")}
}

func TestBuildStatus(t *testing.T) {
	now := time.Now()
	proj := setupStatusProject(t, now)
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	agents := &AgentsModel{
		ProjectLead: AgentNode{Name: "lead-status", State: "live"},
		Workers: []AgentNode{
			{Name: "agent-ui-asker", State: "question", Issue: &AgentIssue{Slug: "ui/asker"}},
			{Name: "agent-ui-ready", State: "at-gate", Gate: "backlog", Issue: &AgentIssue{Slug: "ui/ready"}},
		},
		Unfinished: []AgentNode{{Name: "agent-ui-tested", State: "stopped", Issue: &AgentIssue{Slug: "ui/tested"}}},
	}
	m := buildStatus(&proj, issues, agents, now)

	// Parked is left out of the total; done counts both done issues.
	if m.All.Done != 2 || m.All.Total != 10 {
		t.Errorf("all = %+v, want 2/10", m.All)
	}
	if len(m.Versions) != 0 {
		t.Errorf("versions = %+v, want none", m.Versions)
	}
	if len(m.Epics) != 1 || m.Epics[0].Issue.Slug != "ui/epic" || m.Epics[0].Progress.Total != 2 || m.Epics[0].Running != 1 {
		t.Fatalf("epics = %+v", m.Epics)
	}
	if got := m.Epics[0].Note(); got != "1 child with agents" {
		t.Errorf("epic note = %q", got)
	}

	if m.LeadName != "lead-status" || m.LeadState != "live" {
		t.Errorf("lead = %s %s", m.LeadName, m.LeadState)
	}
	active := map[string]ActiveRow{}
	var order []string
	for _, r := range m.Active {
		active[r.Issue.Slug] = r
		order = append(order, r.Issue.Slug)
	}
	if len(m.Active) != 4 || order[0] != "ui/judged" && order[0] != "ui/tested" {
		t.Errorf("active = %v, want human-testing first, then in progress", order)
	}
	if r := active["ui/asker"]; r.Agent != "question" {
		t.Errorf("asker = %+v", r)
	}
	if r := active["ui/tested"]; r.Agent != "stopped" || r.Age.Approx || r.Age.Text != "3d" {
		t.Errorf("tested = %+v, want stopped, 3d from the sidecar", r)
	}
	if r := active["ui/epic"]; r.Agent != "" || !r.Age.Approx {
		t.Errorf("epic = %+v, want no agent and an mtime age", r)
	}
	if len(m.InDesign) != 1 || m.InDesign[0].Issue.Slug != "ui/ready" || m.InDesign[0].State != "at gate" {
		t.Errorf("in design = %+v", m.InDesign)
	}

	if !m.Next.Found || m.Next.File != "sessions/2026-10-21-lead-status.md" || m.Next.Text != "1. Approve `ui/ready`.\n2. Build it." {
		t.Errorf("next = %+v", m.Next)
	}
	if len(m.Backlog) != 2 || m.Backlog[0].Slug != "ui/high" {
		t.Errorf("backlog = %v, want high first", slugsOf(m.Backlog))
	}
	if len(m.Parked) != 1 || m.Parked[0].Reason != "Blocked on the sidebar." {
		t.Errorf("parked = %+v", m.Parked)
	}

	kinds := map[string]string{}
	for _, it := range m.Waiting {
		kinds[it.Issue.Slug] = it.Kind
	}
	if len(m.Waiting) != 3 || kinds["ui/ready"] != "gate" || kinds["ui/asker"] != "question" || kinds["ui/tested"] != "playtest" {
		t.Errorf("waiting = %v, want ready gate, asker question, tested playtest", kinds)
	}
	if pt := m.WaitingOf("playtest"); len(pt) != 1 || strings.Join(pt[0].Sessions, ",") != "tested-basics" {
		t.Errorf("playtests = %+v", pt)
	}
	if m.NotReady != 1 {
		t.Errorf("not ready = %d, want 1 (ui/open)", m.NotReady)
	}

	if m.ReleaseSource != "docs/CHANGELOG.md" || len(m.Releases) != 3 || m.Releases[0].Version != "v0.4.0" || m.Releases[2].Version != "v0.2.0" {
		t.Fatalf("releases = %+v from %s", m.Releases, m.ReleaseSource)
	}
	if got := m.Releases[0].Summary; got != "**Breaking:** ask is gone, questions stay in chat." {
		t.Errorf("summary = %q", got)
	}
	if len(m.DoneRecent) != 1 || m.DoneRecent[0].Issue.Slug != "ui/shipped" {
		t.Errorf("done recent = %+v", m.DoneRecent)
	}
}

func TestBuildStatusWithoutAgents(t *testing.T) {
	now := time.Now()
	proj := setupStatusProject(t, now)
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	m := buildStatus(&proj, issues, nil, now)
	if m.LeadName != "" || len(m.InDesign) != 0 {
		t.Errorf("no tmux should mean no agents: %+v %+v", m.LeadName, m.InDesign)
	}
	for _, r := range m.Active {
		if r.Agent != "" {
			t.Errorf("%s agent = %q, want none", r.Issue.Slug, r.Agent)
		}
	}
}

func TestStatusMarkdownEndpoint(t *testing.T) {
	proj := setupStatusProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	stubAgentsGit(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, body := getResp(t, ts.URL+"/p/status/status.md")
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("content type = %q", ct)
	}
	for _, want := range []string{
		"# Status status, ",
		"2/10 done · 1 epic in 1 area\n",
		"## Where we are\n- All issues: 2/10 done (no versions set)\n",
		"## Epics: 0 active, 1 not started, in 1 area\n- Not started: ui 0/2 (1)\n",
		"## Recently released\n- v0.4.0 (2026-10-21): Breaking: ask is gone, questions stay in chat.",
		"## Recently shipped: 1 in 7 days\n- ui 1: ui/shipped\n",
		"Lead: lead-status (not running)",
		"] ui/tested (agent stopped)",
		"From sessions/2026-10-21-lead-status.md:\n1. Approve `ui/ready`.",
		"Backlog (2):\n- ui/high (high)\n- ui/low (low)",
		"Parked (1):\n- ui/parked: Blocked on the sidebar.",
		"## Waiting on you (3)",
		"- Approve ui/ready: in design → backlog",
		"- Playtest ui/tested: session tested-basics, no judge run",
		"- Answer ui/asker: open question: comment",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status.md missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "v0.1.0") || strings.Contains(body, "ui/old") {
		t.Errorf("status.md shows more than 3 releases or old done issues:\n%s", body)
	}
}

func TestRecentReleasesFallsBackToTags(t *testing.T) {
	orig := gitTags
	gitTags = func(root string, n int) []Release {
		return []Release{{Version: "v1.2.0", Date: "2026-10-01"}}
	}
	t.Cleanup(func() { gitTags = orig })

	rels, src := recentReleases(t.TempDir(), "", 3)
	if src != "git tags" || len(rels) != 1 || rels[0].Version != "v1.2.0" {
		t.Errorf("got %+v from %q", rels, src)
	}

	gitTags = func(string, int) []Release { return nil }
	if rels, src := recentReleases(t.TempDir(), "", 3); rels != nil || src != "" {
		t.Errorf("no changelog and no tags: got %+v from %q", rels, src)
	}
}

func TestShortDurationAndAge(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second:            "now",
		25 * time.Minute:            "25m",
		90 * time.Minute:            "1h 30m",
		2 * time.Hour:               "2h",
		13*24*time.Hour + time.Hour: "13d",
		15 * 24 * time.Hour:         "15d",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
	now := time.Now()
	for days, want := range map[int]string{1: "", 7: "stale", 14: "very-stale"} {
		if got := ageOf(now.Add(-time.Duration(days)*24*time.Hour), false, now).Level; got != want {
			t.Errorf("%d days: level %q, want %q", days, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	if got := clip("abcdef", 4); got != "abc…" {
		t.Errorf("clip = %q", got)
	}
	if got := clip("abc", 4); got != "abc" {
		t.Errorf("clip short = %q", got)
	}
}

func slugsOf(issues []*tracker.Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Slug)
	}
	return out
}

func TestStatusPageRenders(t *testing.T) {
	proj := setupStatusProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	stubAgentsGit(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, body := getResp(t, ts.URL+"/p/status/status")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{
		`data-testid="nav.tab.status" aria-current="page"`,
		`data-testid="nav.tab.status.count">3</span>`,
		`data-testid="status.no_versions"`,
		`data-testid="status.epic.ui/epic"`,
		`data-testid="status.working.row.ui/tested"`,
		`>3d</span>`,
		`From <span class="mono">sessions/2026-10-21-lead-status.md</span>`,
		`<li>Approve <a href="/p/status/issue/ui/ready"><code>ui/ready</code></a>.</li>`,
		`data-testid="status.waiting.gate.ui/ready"`,
		`data-testid="status.waiting.playtest.ui/tested"`,
		`data-testid="status.waiting.question.ui/asker"`,
		`1 more issue is at a gate`,
		`data-testid="status.release.v0_4_0"`,
		`Blocked on the sidebar.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status page missing %q", want)
		}
	}
	if strings.Contains(body, "status.release.v0_1_0") {
		t.Error("only the newest 3 releases should show")
	}

	// The count follows the page onto every other view.
	_, list := getResp(t, ts.URL+"/p/status/")
	if !strings.Contains(list, `data-testid="nav.tab.status.count">3</span>`) {
		t.Error("list page sidebar is missing the Status count")
	}
}

func TestLinkCodeSlugs(t *testing.T) {
	issues := []*tracker.Issue{{Slug: "ui/ready"}}
	got := linkCodeSlugs(renderMarkdown("1. Ship `ui/ready`, then `make validate`.\n<script>x</script>"), "/p/s", issues)
	if !strings.Contains(got, `<a href="/p/s/issue/ui/ready"><code>ui/ready</code></a>`) || !strings.Contains(got, "<code>make validate</code>") {
		t.Errorf("links = %s", got)
	}
	if strings.Contains(got, "<script>") {
		t.Errorf("raw HTML must be dropped: %s", got)
	}
}

func TestStatusUnsentBugReports(t *testing.T) {
	proj := setupStatusProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	stubAgentsGit(t)
	prev := unsentBugReports
	unsentBugReports = func() int { return 2 }
	t.Cleanup(func() { unsentBugReports = prev })
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	_, body := getResp(t, ts.URL+"/p/status/status")
	for _, want := range []string{
		`data-testid="status.waiting.cli_bugs"`,
		`2 issue-cli bug reports not sent`,
		`issue-cli report-bug --send`,
		`data-testid="nav.tab.status.count">4</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status page missing %q", want)
		}
	}
	_, md := getResp(t, ts.URL+"/p/status/status.md")
	if !strings.Contains(md, "## Waiting on you (4)") || !strings.Contains(md, "- 2 issue-cli bug report(s) not sent (issue-cli report-bug --send)") {
		t.Errorf("status.md:\n%s", md)
	}
}
