package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func groupsWorkflow() *tracker.WorkflowConfig {
	var wf tracker.WorkflowConfig
	for _, s := range []string{"idea", "in design", "backlog", "in progress", "done"} {
		wf.Statuses = append(wf.Statuses, tracker.WorkflowStatus{Name: s})
	}
	wf.Statuses = append(wf.Statuses, tracker.WorkflowStatus{Name: "parked", Optional: true})
	return &wf
}

func iss(slug, status, system, version, parent string) *tracker.Issue {
	i := &tracker.Issue{Slug: slug, Title: slug, Status: status, System: system, Version: version}
	if parent != "" {
		i.ExtraFields = []tracker.ExtraField{{Key: "parent", Value: parent}}
	}
	return i
}

func TestAreaOf(t *testing.T) {
	for _, c := range []struct {
		issue *tracker.Issue
		want  string
	}{
		{&tracker.Issue{Slug: "x/a", System: "UI"}, "ui"},
		{&tracker.Issue{Slug: "combat/a"}, "combat"},
		{&tracker.Issue{Slug: "loose"}, "other"},
	} {
		if got := areaOf(c.issue); got != c.want {
			t.Errorf("areaOf(%s) = %q, want %q", c.issue.Slug, got, c.want)
		}
	}
}

// TestBuildMilestoneView: done first, then open statuses furthest along
// first; the workflow's first status is the track (legend only, last).
func TestBuildMilestoneView(t *testing.T) {
	wf := groupsWorkflow()
	cs := newCanvasStatuses(wf)
	items := []*tracker.Issue{
		iss("ui/a", "done", "UI", "0.1", ""),
		iss("ui/b", "idea", "UI", "0.1", ""),
		iss("ui/c", "idea", "UI", "0.1", ""),
		iss("data/d", "in progress", "Data", "0.1", ""),
		iss("map/e", "in design", "", "0.1", ""),
		iss("map/f", "parked", "", "0.1", ""),
	}
	mv := buildMilestoneView(wf, cs, "0.1", progressOf("0.1", cs, items), items)
	if mv.Cut != "v0.1.0" || mv.Open != 4 || mv.Progress.Done != 1 || mv.Progress.Total != 5 {
		t.Fatalf("milestone = %+v", mv)
	}
	var seg, legend []string
	for _, s := range mv.Segments {
		seg = append(seg, fmt.Sprintf("%s %d %s", s.Status, s.Count, s.Pct))
	}
	for _, s := range mv.Legend {
		legend = append(legend, s.Status)
	}
	if got := strings.Join(seg, ", "); got != "done 1 20.00, in progress 1 20.00, in design 1 20.00" {
		t.Errorf("segments = %s", got)
	}
	if got := strings.Join(legend, ", "); got != "done, in progress, in design, idea" {
		t.Errorf("legend = %s", got)
	}
	if got := areaCountsLine(mv.OpenAreas, 2); got != "ui 2, data 1, +1 area" {
		t.Errorf("open areas = %s", got)
	}
}

// TestBuildEpicsView: a panel per version with epic children (current
// first), then all; a version's panel counts only its children (D-3).
func TestBuildEpicsView(t *testing.T) {
	cs := newCanvasStatuses(groupsWorkflow())
	issues := []*tracker.Issue{
		{Slug: "data/e1", Title: "E1", Status: "idea", System: "Data", Type: "epic"},
		{Slug: "data/e2", Title: "E2", Status: "idea", System: "Data", Type: "epic"},
		{Slug: "ui/e3", Title: "E3", Status: "idea", System: "UI", Type: "epic"},
		{Slug: "e4", Title: "E4", Status: "idea", Type: "epic"},
		iss("data/a", "done", "Data", "0.1", "data/e1"),
		iss("data/b", "idea", "Data", "0.1", "data/e1"),
		iss("data/c", "idea", "Data", "0.2", "data/e1"),
		iss("data/d", "idea", "Data", "0.1", "data/e2"),
		iss("ui/e", "done", "UI", "0.2", "ui/e3"),
		iss("ui/f", "idea", "UI", "", "ui/e3"),
	}
	var epics []EpicProgress
	for _, i := range issues[:4] {
		epics = append(epics, EpicProgress{Issue: i})
	}
	ev := buildEpicsView(cs, issues, epics, "0.1")
	if ev.Epics != 4 || ev.Areas != 3 || ev.Current != "0.1" {
		t.Fatalf("view = %+v", ev)
	}
	var keys []string
	for _, p := range ev.Panels {
		keys = append(keys, p.Key+"="+p.Label)
	}
	if got := strings.Join(keys, ", "); got != "0.1=0.1 (current), 0.2=0.2, all=All" {
		t.Errorf("panels = %s", got)
	}

	p := ev.CurrentPanel()
	if p.Active != 1 || p.NotStarted != 1 || p.All() != 2 || len(p.Areas) != 1 {
		t.Fatalf("0.1 panel = %+v", p)
	}
	data := p.Areas[0]
	if data.Area != "data" || data.Progress.Done != 1 || data.Progress.Total != 3 || !data.Started() || !data.HasNotStarted() {
		t.Errorf("0.1 data = %+v", data)
	}
	// Furthest along first: e1 (1/2) before e2 (0/1).
	if data.Epics[0].Issue.Slug != "data/e1" || data.Epics[0].Progress.Total != 2 {
		t.Errorf("0.1 data epics = %+v", data.Epics)
	}

	all := ev.Panels[2]
	var areas []string
	for _, a := range all.Areas {
		areas = append(areas, fmt.Sprintf("%s %d/%d", a.Area, a.Progress.Done, a.Progress.Total))
	}
	// Started areas first by done, then the rest by size.
	if got := strings.Join(areas, ", "); got != "data 1/4, ui 1/2, other 0/0" {
		t.Errorf("all areas = %s", got)
	}
	if all.Active != 2 || all.NotStarted != 2 {
		t.Errorf("all counts = %d/%d", all.Active, all.NotStarted)
	}

	// No children in the milestone: the first panel is all.
	if ev := buildEpicsView(cs, issues, epics, "0.9"); ev.Current != epicsAll || ev.Panels[0].Key != "0.1" {
		t.Errorf("no milestone children: current %s, first %s", ev.Current, ev.Panels[0].Key)
	}
	if ev := buildEpicsView(cs, issues, nil, "0.1"); len(ev.Panels) != 0 || ev.CurrentPanel() != nil {
		t.Errorf("no epics: %+v", ev)
	}
}

// TestGroupShipped: largest area first, newest first inside; past 5 areas
// the rest merge into one group.
func TestGroupShipped(t *testing.T) {
	now := time.Now()
	var rows []DoneRow
	add := func(area string, n int) {
		for i := 0; i < n; i++ {
			rows = append(rows, DoneRow{Issue: &tracker.Issue{Slug: fmt.Sprintf("%s/%d", area, i)}, At: now.Add(-time.Duration(len(rows)) * time.Hour)})
		}
	}
	add("a", 6)
	add("b", 3)
	for _, a := range []string{"c", "d", "e", "f", "g"} {
		add(a, 1)
	}
	groups := groupShipped(rows)
	if len(groups) != 6 {
		t.Fatalf("groups = %d, want 5 and a merged one", len(groups))
	}
	if groups[0].Area != "a" || groups[0].Pct != 100 || groups[1].Area != "b" || groups[1].Pct != 50 {
		t.Errorf("first groups = %+v %+v", groups[0], groups[1])
	}
	last := groups[5]
	if !last.Merged || last.Area != "2 more areas" || len(last.Rows) != 2 || last.Rows[0].Issue.Slug != "f/0" {
		t.Errorf("merged = %+v", last)
	}
	// Exactly one area over the cap stays its own group.
	if g := groupShipped(rows[:len(rows)-1]); len(g) != 6 || g[5].Merged {
		t.Errorf("six areas should not merge: %+v", g[5])
	}
}

func TestMilestoneOpenSummary(t *testing.T) {
	ms := &MilestoneRow{Open: []*tracker.Issue{{Status: "in progress"}, {Status: "idea"}, {Status: "idea"}}}
	if got := ms.OpenSummary(); got != "1 in progress, 2 idea" {
		t.Errorf("summary = %q", got)
	}
}

func TestReleaseSummary(t *testing.T) {
	r := Release{Summary: "**Salvage:** pick parts, or `sell` the [lot](http://x).<script>x</script>"}
	if got := r.SummaryText(); got != "Salvage: pick parts, or sell the lot.<script>x</script>" {
		t.Errorf("text = %q", got)
	}
	html := string(r.SummaryHTML())
	for _, want := range []string{"<strong>Salvage:</strong>", "<code>sell</code>", `<a href="http://x">lot</a>`} {
		if !strings.Contains(html, want) {
			t.Errorf("html %q missing %q", html, want)
		}
	}
	if strings.Contains(html, "<script>") || strings.HasPrefix(html, "<p>") {
		t.Errorf("html = %q", html)
	}
	long := Release{Summary: "**" + strings.Repeat("a", 200) + "**"}
	if got := long.SummaryText(); strings.Contains(got, "*") || len([]rune(got)) != 140 {
		t.Errorf("long text = %q", got)
	}
}

func TestSliceAt(t *testing.T) {
	list := []int{1, 2, 3}
	if got := sliceAt(list, 2, true).([]int); len(got) != 2 {
		t.Errorf("head = %v", got)
	}
	if got := sliceAt(list, 2, false).([]int); len(got) != 1 || got[0] != 3 {
		t.Errorf("rest = %v", got)
	}
	if sliceAt(list, 3, false) != nil || sliceAt(list, 9, false) != nil || sliceAt("x", 1, true) != nil {
		t.Error("rest past the end, or of a non-slice, should be nil")
	}
	if got := sliceAt(list, 9, true).([]int); len(got) != 3 {
		t.Errorf("head past the end = %v", got)
	}
}

// TestStatusMarkdownCaps: a 700-issue project's status.md stays short, and
// every list ends in "+K more".
func TestStatusMarkdownCaps(t *testing.T) {
	now := time.Now()
	m := &StatusModel{Project: "Big", AsOf: now}
	for i := 0; i < 40; i++ {
		m.Waiting = append(m.Waiting, WaitingItem{Kind: "gate", Target: "backlog", Issue: &tracker.Issue{Slug: fmt.Sprintf("ui/g%d", i), Status: "in design"}})
		m.Active = append(m.Active, ActiveRow{Issue: &tracker.Issue{Slug: fmt.Sprintf("ui/a%d", i), Status: "in progress"}})
		m.Backlog = append(m.Backlog, &tracker.Issue{Slug: fmt.Sprintf("ui/b%d", i)})
		m.Parked = append(m.Parked, ParkedRow{Issue: &tracker.Issue{Slug: fmt.Sprintf("ui/p%d", i)}})
	}
	for i := 0; i < 120; i++ {
		m.DoneRecent = append(m.DoneRecent, DoneRow{Issue: &tracker.Issue{Slug: fmt.Sprintf("a%d/x%d", i%9, i), System: fmt.Sprintf("A%d", i%9)}, At: now})
	}
	m.Shipped = groupShipped(m.DoneRecent)
	for i := 0; i < 12; i++ {
		m.Versions = append(m.Versions, Progress{Label: fmt.Sprintf("0.%d", i), Total: 10})
	}
	md := statusMarkdown(m)
	if n := strings.Count(md, "\n"); n > 70 {
		t.Errorf("status.md is %d lines:\n%s", n, md)
	}
	for _, want := range []string{
		"## Waiting on you (40)\n",
		"- +30 more on the Status page\n",
		"Backlog (40):\n",
		"- +35 more: issue-cli list --status backlog\n",
		"- +35 more: issue-cli list --status parked\n",
		"## Recently shipped: 120 in 7 days\n",
		"(issue-cli list --status done --system A0)",
		"- 4 more areas 52: ",
		"· +4 more\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("status.md missing %q:\n%s", want, md)
		}
	}
	// Owner and running work come before progress and history.
	order := []string{"## Waiting on you", "## Working on", "## Where we are", "## Next", "## Recently shipped", "## Versions", "## Recently released"}
	last := -1
	for _, h := range order {
		i := strings.Index(md, h)
		if i < last {
			t.Errorf("%s is out of order", h)
		}
		last = i
	}
}

func TestTileSummaries(t *testing.T) {
	m := &StatusModel{}
	if m.NeedsSummary() != "Nothing waits on you" || m.WorkingSummary() != "Nothing in flight" || m.NextSummary() != "0 backlog · 0 parked" {
		t.Errorf("empty = %q / %q / %q", m.NeedsSummary(), m.WorkingSummary(), m.NextSummary())
	}
	m.Waiting = []WaitingItem{{Kind: "gate"}, {Kind: "gate"}, {Kind: "question"}, {Kind: "question"}, {Kind: "playtest"}}
	m.Active = []ActiveRow{{Agent: "working"}, {}, {Agent: "working"}, {Agent: "stopped"}}
	m.LeadName, m.LeadState = "lead-x", "live"
	m.Next.Found = true
	m.Backlog = []*tracker.Issue{{}}
	if got := m.NeedsSummary(); got != "2 to approve · 1 to playtest · 2 questions" {
		t.Errorf("needs = %q", got)
	}
	if got := m.WorkingSummary(); got != "2 agents working · 1 without an agent · 1 agent stopped · lead live" {
		t.Errorf("working = %q", got)
	}
	if got := m.NextSummary(); got != "the lead's ## Next · 1 backlog · 0 parked" {
		t.Errorf("next = %q", got)
	}
}
