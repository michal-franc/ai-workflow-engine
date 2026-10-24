package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func overviewSnap(t *testing.T, root string, at time.Time, rows map[string]float64) {
	t.Helper()
	s := &health.Snapshot{Run: health.RunInfo{Date: at.Local().Format("2006-01-02T15:04"), Trigger: health.TriggerDaily}, Families: map[string]health.FamilyResult{"workflow": {}}}
	for id, v := range rows {
		metric, labels := id, map[string]string(nil)
		if i := strings.Index(id, "{"); i > 0 {
			metric = id[:i]
			k, val, _ := strings.Cut(strings.Trim(id[i:], "{}"), "=")
			labels = map[string]string{k: val}
		}
		s.Metrics = append(s.Metrics, health.Row{ID: health.RowID(metric, labels), Metric: metric, Labels: labels, Value: v})
	}
	if _, err := health.Write(root, s, at); err != nil {
		t.Fatal(err)
	}
}

// AC14: the Stats page renders the Disk use card from a storage snapshot.
func TestStatsPageDiskUseCard(t *testing.T) {
	withStubTelemetryReport(t, func(*tracker.Project) ([]byte, error) { return []byte(`{"v":1,"enabled":true}`), nil })
	proj := fixItProject(t)
	at := time.Now().Add(-time.Hour)
	s := &health.Snapshot{Run: health.RunInfo{Date: at.Local().Format("2006-01-02T15:04"), Trigger: health.TriggerDaily}, Families: map[string]health.FamilyResult{"storage": {}}}
	for id, v := range map[string]float64{"storage.total_mb": 3430, "storage.worktrees_mb": 1880, "storage.playtest_mb": 1336, "storage.git_mb": 37, "storage.worktrees": 96} {
		s.Metrics = append(s.Metrics, health.Row{ID: id, Metric: id, Value: v})
	}
	if _, err := health.Write(proj.WorkDir, s, at); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/p/" + proj.Slug + "/stats?range=7d")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)
	for _, want := range []string{`data-chart="storage"`, `data-testid="stats.card.storage"`, `<div class="stats-big">3.35 GB</div>`, "playtest shots", "1,336 MB", "issue-cli playtest prune --all"} {
		if !strings.Contains(page, want) {
			t.Errorf("stats page lacks %q", want)
		}
	}
}

// AC2: /stats compares the projects that have snapshots, one row each,
// sorted by the chosen key; the others are named at the bottom.
func TestStatsOverview(t *testing.T) {
	now := time.Now()
	mk := func(name, slug string, rows map[string]float64) tracker.Project {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, "issues"), 0o755)
		if rows != nil {
			overviewSnap(t, root, now.AddDate(0, 0, -2), rows)
			overviewSnap(t, root, now.Add(-time.Hour), rows)
		}
		return tracker.Project{Name: name, Slug: slug, IssueDir: filepath.Join(root, "issues"), WorkDir: root}
	}
	projects := []tracker.Project{
		mk("Small", "small", map[string]float64{"workflow.issues{type=all}": 3, "workflow.time.share{kind=waiting}": 0.1, "workflow.cycle.p50{type=all}": 3600, "codebase.source_lines": 5000, "codebase.test_ratio": 0.5}),
		mk("Big", "big", map[string]float64{"workflow.issues{type=all}": 60, "workflow.time.share{kind=waiting}": 0.95, "workflow.cycle.p50{type=all}": 28800, "codebase.source_lines": 237000, "codebase.big_files": 38}),
		mk("Quiet", "quiet", map[string]float64{"workflow.issues{type=all}": 0, "codebase.source_lines": 17000}),
		mk("Fresh", "fresh", nil),
	}
	d := buildOverview(projects, url.Values{}, now)
	if len(d.Rows) != 3 || d.Rows[0].Slug != "big" || d.Rows[2].Slug != "quiet" {
		t.Fatalf("rows = %+v", d.Rows)
	}
	big := d.Rows[0]
	if big.Waiting.Value != "95%" || !big.WaitingOver || big.Cycle.Value != "8h" || big.LOC.Value != "237k" || big.BigFiles != 38 || !strings.Contains(string(big.LOC.Spark), "<polyline") {
		t.Errorf("big row = %+v", big)
	}
	if q := d.Rows[2]; q.Waiting.Has || q.Done != 0 || q.LOC.Value != "17k" {
		t.Errorf("quiet row = %+v", q)
	}
	if len(d.None) != 1 || d.None[0] != "Fresh" {
		t.Errorf("none = %v", d.None)
	}
	if d = buildOverview(projects, url.Values{"sort": {"loc"}, "range": {"7d"}}, now); d.Rows[0].Slug != "big" || d.Rows[1].Slug != "quiet" || d.Days != 7 {
		t.Errorf("sort by loc: %s %s, days %d", d.Rows[0].Slug, d.Rows[1].Slug, d.Days)
	}

	ts := newTestServer(t, projects)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{`data-testid="stats.overview"`, `data-project="big"`, `href="/p/big/stats"`, "over 25%", `data-testid="stats.overview.none"`, "Fresh"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("overview lacks %q", want)
		}
	}
	if strings.Contains(string(body), "<script") && !strings.Contains(string(body), `src="/static/theme.js"`) {
		t.Error("overview carries an inline script")
	}
}

// The project page: every section renders, and the dashboard carries no
// inline script or handler (the strict-CSP plan).
func TestStatsPageDashboard(t *testing.T) {
	withStubTelemetryReport(t, func(*tracker.Project) ([]byte, error) { return []byte(`{"v":1,"enabled":true}`), nil })
	proj := fixItProject(t)
	overviewSnap(t, proj.WorkDir, time.Now().Add(-time.Hour), map[string]float64{"workflow.issues{type=all}": 1, "codebase.big_files": 3})
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/p/" + proj.Slug + "/stats?range=7d&group=system&drill=status:human-testing")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)
	for _, want := range []string{
		`data-testid="stats.dashboard"`, `data-testid="stats.range"`, `data-testid="stats.group"`, `<option value="system" selected>`,
		`data-testid="stats.gauge"`, `data-chart="cycle"`, `data-chart="time"`, `data-chart="group"`, `data-chart="weekly"`,
		`data-testid="stats.backfill_hint"`, `data-testid="stats.pending"`, `data-testid="stats.drilldown"`, `/issue/fix-it?tab=timeline`,
		`data-testid="stats.table.time"`, `data-testid="stats.table_btn.time"`, `data-testid="stats.tables"`, `data-testid="stats.time"`,
		`role="img"`, `aria-current="true" data-range="7d" data-testid="stats.range.7d"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("stats page lacks %q", want)
		}
	}
	// The dashboard itself (the shared sidebar is not this page's).
	dash := page[strings.Index(page, `data-testid="stats.dashboard"`):strings.Index(page, `data-testid="stats.tables"`)]
	for _, line := range strings.Split(dash, "\n") {
		if strings.Contains(line, "<script") && !strings.Contains(line, `src="/static/`) {
			t.Errorf("script that is not a viewer file: %s", strings.TrimSpace(line))
		}
		if strings.Contains(line, " onchange=") || strings.Contains(line, " onclick=") {
			t.Errorf("inline handler: %s", strings.TrimSpace(line))
		}
	}
}

// bigProject is a project of n done issues over the last 120 days, each
// with a stats sidecar and a short clilog: RogueTech's size.
func bigProject(t testing.TB, n int) tracker.Project {
	root := t.TempDir()
	dir := filepath.Join(root, "issues", "Core")
	os.MkdirAll(dir, 0o755)
	types := []string{"feature", "bugfix", "tweak", ""}
	now := time.Now()
	exit0 := 0
	dur := int64(100)
	for i := 0; i < n; i++ {
		slug := fmt.Sprintf("issue-%04d", i)
		typ := types[i%len(types)]
		front := fmt.Sprintf("---\ntitle: \"Issue %d\"\nstatus: \"done\"\nsystem: \"S%d\"\n", i, i%7)
		if typ != "" {
			front += "type: \"" + typ + "\"\n"
		}
		path := filepath.Join(dir, slug+".md")
		if err := os.WriteFile(path, []byte(front+"---\n\nBody.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		done := now.Add(-time.Duration(i%120) * 24 * time.Hour).Add(-time.Hour)
		t0 := done.Add(-3 * time.Hour)
		stats, _ := json.Marshal(tracker.StatsStore{Transitions: []tracker.TransitionStat{
			{From: "idea", To: "in progress", TS: t0.Add(10 * time.Minute)},
			{From: "in progress", To: "human-testing", TS: t0.Add(90 * time.Minute)},
			{From: "human-testing", To: "done", TS: done},
		}})
		os.WriteFile(tracker.StatsSidecarPath(path), stats, 0o644)
		log := clilog.AgentPath(root, tracker.AgentSessionName("Core/"+slug))
		for k := 0; k < 8; k++ {
			clilog.Append(log, clilog.Record{TS: t0.Add(time.Duration(k*10) * time.Minute).Format(time.RFC3339), Args: []string{"show", "Core/" + slug}, Exit: &exit0, DurMS: &dur})
		}
	}
	return tracker.Project{Name: "Big", Slug: "big", IssueDir: filepath.Join(root, "issues"), WorkDir: root}
}

// AC3: on a project RogueTech's size, the page renders in under 300 ms
// once its aggregate is cached (the server warms it at start).
func TestStatsPageFastOnABigProject(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an 800-issue project")
	}
	withStubTelemetryReport(t, func(*tracker.Project) ([]byte, error) { return []byte(`{"v":1,"enabled":true}`), nil })
	proj := bigProject(t, 800)
	for d := 0; d < 90; d++ {
		overviewSnap(t, proj.WorkDir, time.Now().AddDate(0, 0, -d).Add(-time.Minute), map[string]float64{"workflow.issues{type=all}": 50, "workflow.cycle.p50{type=all}": 3600, "codebase.source_lines": float64(200000 + d)})
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	get := func(q string) time.Duration {
		start := time.Now()
		resp, err := http.Get(ts.URL + "/p/big/stats?" + q)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
		return time.Since(start)
	}
	cold := get("range=90d")
	best := time.Hour
	for _, q := range []string{"range=30d", "range=7d&group=system", "range=90d&group=type&drill=status:human-testing", "range=30d&scale=share"} {
		for i := 0; i < 2; i++ {
			if d := get(q); d < best {
				best = d
			}
			if d := get(q); d > 300*time.Millisecond {
				t.Errorf("%s: %v warm, want under 300 ms", q, d)
			}
		}
	}
	t.Logf("800 issues: cold %v, warm best %v", cold, best)
}
