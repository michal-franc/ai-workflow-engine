package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/chart"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// dashboardFor builds the dashboard the stats handler would for proj, with
// the given query and snapshots.
func dashboardFor(t *testing.T, proj *tracker.Project, query string, snaps []*health.Snapshot, now time.Time) *Dashboard {
	t.Helper()
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(query)
	opts := parseStatsOpts(q)
	wf := proj.LoadWorkflow()
	wide, _ := newStatsCache().aggregate(proj, wf, issues, opts.window(), now, nil)
	return buildDashboard(dashIn{Proj: proj, Base: "/p/" + proj.Slug + "/stats", Prefix: "/p/" + proj.Slug, WF: wf, Issues: issues, Opts: opts, Now: now, Wide: wide, Snaps: snaps})
}

// AC4: the dashboard's numbers for an issue are its Timeline tab's: the
// waiting gauge, a status bar's drill-down, a week's drill-down, and the
// workflow snapshot rows the trend lines draw all come out of the same
// split the tab shows.
func TestDashboardMatchesTimelineTab(t *testing.T) {
	proj := fixItProject(t)
	now := time.Now()
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	var fix *tracker.Issue
	for _, is := range issues {
		if is.Slug == "fix-it" {
			fix = is
		}
	}
	if fix == nil {
		t.Fatal("no fix-it issue")
	}
	wf := proj.LoadWorkflow()
	tab := buildTimeline(loadTimelineSources(&proj, fix, wf), timelineInput{Status: "done", Now: now, WF: wf})
	if !tab.Known || tab.Waiting == 0 || tab.Working == 0 {
		t.Fatalf("tab = known %v working %v waiting %v", tab.Known, tab.Working, tab.Waiting)
	}

	// The waiting gauge: the only issue in the range is fix-it.
	d := dashboardFor(t, &proj, "range=7d&group=type", nil, now)
	share := float64(tab.Waiting) / float64(tab.Working+tab.Waiting+tab.Idle)
	if g := d.Gauges[0]; g.Value != chart.Format(chart.Share, share) {
		t.Errorf("waiting gauge = %q, tab says %s", g.Value, chart.Format(chart.Share, share))
	}

	// A status bar's issues: human-testing as the tab's band shows it.
	var ht timelineBand
	for _, b := range tab.Bands {
		if b.Status == "human-testing" {
			ht.Working += b.Working
			ht.Waiting += b.Waiting
			ht.Idle += b.Idle
			ht.End = ht.End.Add(b.End.Sub(b.Start))
		}
	}
	d = dashboardFor(t, &proj, "range=7d&group=type&drill=status:human-testing", nil, now)
	if d.Drill == nil || len(d.Drill.Issues) != 1 {
		t.Fatalf("status drill = %+v", d.Drill)
	}
	want := "in status " + chart.Duration(ht.End.Sub(time.Time{})) + " · agent " + chart.Duration(ht.Working) + " · waiting " + chart.Duration(ht.Waiting) + " · idle " + chart.Duration(ht.Idle)
	if got := d.Drill.Issues[0].Detail; got != want {
		t.Errorf("status drill detail = %q, tab says %q", got, want)
	}

	// A week's issues: cycle and agent time as the tab totals them.
	key, _ := periodOf(tab.End, false) // the week fix-it was done in
	d = dashboardFor(t, &proj, "range=7d&group=type&drill=week:"+key+":bugfix", nil, now)
	if d.Drill == nil || len(d.Drill.Issues) != 1 {
		t.Fatalf("week drill = %+v", d.Drill)
	}
	if got, want := d.Drill.Issues[0].Detail, "cycle "+chart.Duration(tab.Total)+" · agent "+chart.Duration(tab.Working); got != want {
		t.Errorf("week drill detail = %q, tab says %q", got, want)
	}

	// The trend lines: the workflow family's rows, exact to the second.
	snap, _, err := health.Take(context.Background(), health.Env{Root: proj.WorkDir, Project: &proj, Workflow: wf, Now: now}, health.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	for metric, want := range map[string]time.Duration{
		"workflow.time.working": tab.Working,
		"workflow.time.waiting": tab.Waiting,
		"workflow.time.idle":    tab.Idle,
		"workflow.cycle.p50":    tab.Total,
	} {
		got, ok := snap.Value(metric, map[string]string{"type": "bugfix"})
		if !ok || got != want.Seconds() {
			t.Errorf("%s{type=bugfix} = %v (found %v), tab says %v", metric, got, ok, want.Seconds())
		}
	}
	if got, _ := snap.Value("workflow.time.share", map[string]string{"kind": "waiting"}); chart.Format(chart.Share, got) != chart.Format(chart.Share, share) {
		t.Errorf("workflow.time.share = %v, tab says %v", got, share)
	}
}

// The dashboard draws its trends from snapshots, one point per day, leaves
// out days without issues, and marks families that do not exist yet.
func TestDashboardTrendsFromSnapshots(t *testing.T) {
	proj := fixItProject(t)
	// Noon today: day(3).Add(-time.Hour) must stay on day(3)'s date, which
	// time.Now() breaks in the hour after midnight.
	y, m, dd := time.Now().Date()
	now := time.Date(y, m, dd, 12, 0, 0, 0, time.Local)
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	snap := func(at time.Time, issues, cycle, share float64) *health.Snapshot {
		date := at.Local().Format("2006-01-02T15:04")
		return &health.Snapshot{
			Run:      health.RunInfo{Date: date, Trigger: health.TriggerBackfill},
			Families: map[string]health.FamilyResult{"workflow": {}, "codebase": {}},
			Metrics: []health.Row{
				{ID: "workflow.issues{type=all}", Metric: "workflow.issues", Labels: map[string]string{"type": "all"}, Value: issues},
				{ID: "workflow.issues{type=bugfix}", Metric: "workflow.issues", Labels: map[string]string{"type": "bugfix"}, Value: issues},
				{ID: "workflow.cycle.p50{type=all}", Metric: "workflow.cycle.p50", Labels: map[string]string{"type": "all"}, Value: cycle},
				{ID: "workflow.cycle.p50{type=bugfix}", Metric: "workflow.cycle.p50", Labels: map[string]string{"type": "bugfix"}, Value: cycle},
				{ID: "workflow.time.share{kind=waiting}", Metric: "workflow.time.share", Labels: map[string]string{"kind": "waiting"}, Value: share},
				{ID: "codebase.source_lines", Metric: "codebase.source_lines", Value: 1000 + issues},
				{ID: "codebase.big_files", Metric: "codebase.big_files", Value: 12},
			},
		}
	}
	snaps := []*health.Snapshot{
		snap(day(5), 1, 3600, 0.4),
		snap(day(4), 0, 0, 0), // no issues done in its window: no workflow point
		snap(day(3).Add(-time.Hour), 2, 1800, 0.5),
		snap(day(3), 2, 2400, 0.5), // the day's last one wins
		snap(day(1), 3, 1200, 0.3),
	}
	d := dashboardFor(t, &proj, "range=7d", snaps, now)
	var cycle, loc ChartCard
	for _, c := range d.Trends {
		switch c.ID {
		case "cycle":
			cycle = c
		case "loc":
			loc = c
		}
	}
	if cycle.Points != 3 || !strings.Contains(string(cycle.SVG), "bugfix 20m") {
		t.Errorf("cycle chart: %d points, end label in %s", cycle.Points, cycle.SVG)
	}
	if loc.Points != 4 {
		t.Errorf("lines of code points = %d, want 4 (one per day)", loc.Points)
	}
	if len(cycle.Table.Rows) != 3 || cycle.Table.Rows[1][1] != "40m" {
		t.Errorf("cycle table = %+v", cycle.Table)
	}
	if d.Hint != "" {
		t.Errorf("backfill hint shown with %d snapshots", len(snaps))
	}
	if big := d.Gauges[2]; big.Value != "12" || !big.Over || big.Verdict != "Over limit" {
		t.Errorf("big files gauge = %+v", big)
	}
	if len(d.Pending) != 4 || d.Pending[2].Family != "context" || d.Pending[3].Family != "storage" {
		t.Errorf("pending = %+v", d.Pending)
	}
	// Hiding a series keeps its legend entry, struck, as a link back.
	d = dashboardFor(t, &proj, "range=7d&hide=bugfix", snaps, now)
	if d.Trends[0].Points != 0 || !d.Trends[0].Legend[0].Hidden || !strings.Contains(d.Trends[0].Legend[0].Href, "range=7d") || strings.Contains(d.Trends[0].Legend[0].Href, "hide=") {
		t.Errorf("hidden bugfix: points %d, legend %+v", d.Trends[0].Points, d.Trends[0].Legend)
	}
	// A point links to its issues; the drill lists them.
	d = dashboardFor(t, &proj, "range=7d&drill="+url.QueryEscape(pointKey("cycle", day(1), "bugfix")), snaps, now)
	if d.Drill == nil || d.Drill.Anchor != "cycle" || !strings.Contains(d.Drill.Title, "bugfix") {
		t.Fatalf("point drill = %+v", d.Drill)
	}
	// One snapshot: the hint names the backfill command.
	d = dashboardFor(t, &proj, "range=7d", snaps[:1], now)
	if d.Hint != "issue-cli --project test-project health backfill" {
		t.Errorf("hint = %q", d.Hint)
	}
}

// AC14: the Disk use card shows the newest storage snapshot's total and a
// bar per area, the total's line once there are two snapshots, and is
// pending until a snapshot has the family.
func TestDashboardDiskUseCard(t *testing.T) {
	proj := fixItProject(t)
	now := time.Now()
	snap := func(daysAgo int, total, worktrees, shots float64) *health.Snapshot {
		return &health.Snapshot{
			Run:      health.RunInfo{Date: now.AddDate(0, 0, -daysAgo).Local().Format("2006-01-02T15:04"), Trigger: health.TriggerDaily},
			Families: map[string]health.FamilyResult{"storage": {}},
			Metrics: []health.Row{
				{ID: "storage.total_mb", Metric: "storage.total_mb", Value: total},
				{ID: "storage.worktrees_mb", Metric: "storage.worktrees_mb", Value: worktrees},
				{ID: "storage.worktrees", Metric: "storage.worktrees", Value: 96},
				{ID: "storage.playtest_mb", Metric: "storage.playtest_mb", Value: shots},
				{ID: "storage.git_mb", Metric: "storage.git_mb", Value: 37},
				{ID: "storage.agent_logs_mb", Metric: "storage.agent_logs_mb", Value: 21},
				{ID: "storage.attachments_mb", Metric: "storage.attachments_mb", Value: 0.4},
			},
		}
	}
	card := func(d *Dashboard) (ChartCard, bool) {
		for _, c := range d.Trends {
			if c.ID == "storage" {
				return c, true
			}
		}
		return ChartCard{}, false
	}

	d := dashboardFor(t, &proj, "range=7d", nil, now)
	if _, ok := card(d); ok {
		t.Fatal("Disk use card without a storage snapshot")
	}

	d = dashboardFor(t, &proj, "range=7d", []*health.Snapshot{snap(1, 3430, 1880, 1336)}, now)
	c, ok := card(d)
	if !ok {
		t.Fatal("no Disk use card")
	}
	if c.Big != "3.35 GB" || !strings.Contains(c.Sub, "96 worktrees") || !strings.Contains(c.Empty, "second daily snapshot") {
		t.Errorf("one snapshot: big %q sub %q empty %q", c.Big, c.Sub, c.Empty)
	}
	want := []AreaBar{{"worktrees", "1,880 MB", 100}, {"playtest shots", "1,336 MB", 71}, {".git", "37 MB", 2}, {".agent-logs", "21 MB", 1}, {"attachments", "0.4 MB", 1}}
	if len(c.Bars) != len(want) {
		t.Fatalf("bars = %+v", c.Bars)
	}
	for i, w := range want {
		if c.Bars[i] != w {
			t.Errorf("bar %d = %+v, want %+v", i, c.Bars[i], w)
		}
	}
	for _, p := range d.Pending {
		if p.Family == "storage" {
			t.Error("Disk use still pending with a storage snapshot")
		}
	}

	d = dashboardFor(t, &proj, "range=7d", []*health.Snapshot{snap(3, 3000, 1800, 1100), snap(1, 3430, 1880, 1336)}, now)
	if c, _ = card(d); c.Points != 2 || c.Empty != "" || c.SVG == "" {
		t.Errorf("two snapshots: points %d, empty %q", c.Points, c.Empty)
	}
}

func TestFmtMB(t *testing.T) {
	for v, want := range map[float64]string{0: "0.0 MB", 9.94: "9.9 MB", 37: "37 MB", 1336: "1,336 MB", 3430: "3,430 MB", 1023.6: "1,024 MB"} {
		if got := fmtMB(v); got != want {
			t.Errorf("fmtMB(%v) = %q, want %q", v, got, want)
		}
	}
	if got := fmtSize(3430); got != "3.35 GB" {
		t.Errorf("fmtSize(3430) = %q", got)
	}
	if got := fmtSize(1000); got != "1,000 MB" {
		t.Errorf("fmtSize(1000) = %q", got)
	}
}
