package main

import (
	"html/template"
	"math"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/michal-franc/issue-viewer/internal/chart"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// /stats without a project: one row per project from its health snapshots
// only (never the live aggregate, so ten projects stay fast), each value
// with a sparkline over the range.

// OverviewData is the page.
type OverviewData struct {
	Rows   []OverviewRow
	None   []string
	Ranges []statsOption
	Sorts  []statsOption
	Range  string
	Days   int
}

// OverviewCell is one value and its sparkline.
type OverviewCell struct {
	Value string
	Spark template.HTML
	Has   bool
}

// OverviewRow is one project.
type OverviewRow struct {
	Name, Slug, Href string
	Latest           string

	Waiting      OverviewCell
	WaitingShare float64
	WaitingOver  bool
	Limit        float64 // 0: none
	Cycle        OverviewCell
	Rework       OverviewCell
	Done         int
	LOC          OverviewCell
	TestRatio    float64
	HasRatio     bool
	BigFiles     int
	HasBig       bool

	cycleSecs float64
	locLines  float64
}

var overviewSorts = []struct{ Key, Label string }{{"done", "Issues done"}, {"waiting", "Waiting on you"}, {"cycle", "Cycle p50"}, {"loc", "Lines of code"}}

func (s *Server) handleStatsOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/stats" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	data := buildOverview(s.projects, q, time.Now())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "stats_overview.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buildOverview(projects []tracker.Project, q url.Values, now time.Time) OverviewData {
	d := OverviewData{Range: "30d", Days: 30}
	for _, r := range statsRanges[:3] {
		if q.Get("range") == r.Key {
			d.Range, d.Days = r.Key, r.Days
		}
	}
	sortKey := "done"
	for _, o := range overviewSorts {
		if q.Get("sort") == o.Key {
			sortKey = o.Key
		}
	}
	for _, r := range statsRanges[:3] {
		d.Ranges = append(d.Ranges, statsOption{Key: r.Key, Label: r.Label, On: r.Key == d.Range, Href: "/stats?" + url.Values{"range": {r.Key}, "sort": {sortKey}}.Encode()})
	}
	for _, o := range overviewSorts {
		d.Sorts = append(d.Sorts, statsOption{Key: o.Key, Label: o.Label, On: o.Key == sortKey, Href: "/stats?" + url.Values{"range": {d.Range}, "sort": {o.Key}}.Encode()})
	}

	from := now.AddDate(0, 0, -d.Days)
	seen := map[string]bool{}
	for i := range projects {
		proj := &projects[i]
		wf := proj.LoadWorkflow()
		root := ciRootFor(proj)
		if !wf.Health.Enabled() || seen[root] {
			d.None = append(d.None, proj.Name)
			continue
		}
		snaps := health.Range(root, time.Time{}, now)
		if len(snaps) == 0 {
			d.None = append(d.None, proj.Name)
			continue
		}
		seen[root] = true
		d.Rows = append(d.Rows, overviewRow(proj, wf, lastPerDay(snaps, from, now), snaps[len(snaps)-1]))
	}
	less := map[string]func(a, b OverviewRow) bool{
		"done":    func(a, b OverviewRow) bool { return a.Done > b.Done },
		"waiting": func(a, b OverviewRow) bool { return a.WaitingShare > b.WaitingShare },
		"cycle":   func(a, b OverviewRow) bool { return a.cycleSecs > b.cycleSecs },
		"loc":     func(a, b OverviewRow) bool { return a.locLines > b.locLines },
	}[sortKey]
	sort.SliceStable(d.Rows, func(i, j int) bool { return less(d.Rows[i], d.Rows[j]) })
	return d
}

func overviewRow(proj *tracker.Project, wf *tracker.WorkflowConfig, daily []*health.Snapshot, latest *health.Snapshot) OverviewRow {
	row := OverviewRow{Name: proj.Name, Slug: proj.Slug, Href: "/p/" + proj.Slug + "/stats", Latest: latest.Time().Format("2 Jan 15:04")}
	all := map[string]string{"type": timeline.All}
	issues := func(s *health.Snapshot) float64 { v, _ := s.Value("workflow.issues", all); return v }
	series := func(metric string, labels map[string]string, needIssues bool) []float64 {
		var out []float64
		for _, s := range daily {
			v, ok := s.Value(metric, labels)
			if !ok || (needIssues && issues(s) == 0) {
				out = append(out, math.NaN())
				continue
			}
			out = append(out, v)
		}
		return out
	}
	cell := func(label, metric string, labels map[string]string, needIssues bool, u chart.Unit) (OverviewCell, float64) {
		v, ok := latest.Value(metric, labels)
		if !ok || (needIssues && issues(latest) == 0) {
			return OverviewCell{}, 0
		}
		return OverviewCell{Value: chart.Format(u, v), Has: true, Spark: chart.Spark{Label: label + ", " + plural(len(daily), "day", "days"), Values: series(metric, labels, needIssues), Class: "s-all"}.SVG()}, v
	}

	n := issues(latest)
	row.Done = int(n)
	row.Waiting, row.WaitingShare = cell("Waiting on you", "workflow.time.share", map[string]string{"kind": "waiting"}, true, chart.Share)
	if limit, ok := wf.Health.Limit("waiting_share"); ok {
		row.Limit = limit
		row.WaitingOver = row.Waiting.Has && row.WaitingShare > limit
	}
	row.Cycle, row.cycleSecs = cell("Cycle p50", "workflow.cycle.p50", all, true, chart.Seconds)
	row.Rework, _ = cell("Rework share", "workflow.rework.share", all, true, chart.Share)
	row.LOC, row.locLines = cell("Lines of code", "codebase.source_lines", nil, false, chart.Count)
	if v, ok := latest.Value("codebase.test_ratio", nil); ok {
		row.TestRatio, row.HasRatio = v, true
	}
	if v, ok := latest.Value("codebase.big_files", nil); ok {
		row.BigFiles, row.HasBig = int(v), true
	}
	return row
}

// Pct is a share as a whole percentage, capped at 100, for meter widths.
func (r OverviewRow) Pct(v float64) int { return int(math.Round(math.Min(1, math.Max(0, v)) * 100)) }
