package main

import (
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/chart"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The stats page's dashboard (docs/workflow-stats.md, "Stats page"): gauges
// for now, trend lines from the health snapshots, and where the time goes,
// from the same timeline.Aggregate the issue Timeline tab uses. Options are
// query parameters, so every view is a link; static/stats.js remembers the
// last one per project.

// statsRange is one range option.
type statsRange struct {
	Key, Label string
	Days       int // 0: all
}

var statsRanges = []statsRange{{"7d", "7 days", 7}, {"30d", "30 days", 30}, {"90d", "90 days", 90}, {"all", "All", 0}}

// statsGroups are the group-by options.
var statsGroups = []struct{ Key, Label string }{{"type", "Type"}, {"system", "System"}, {"epic", "Epic"}, {"version", "Version"}}

// statsOpts is the page's options, from the query.
type statsOpts struct {
	Range string
	Group string
	P     string // cycle chart percentile: 50 or 90
	Scale string // by-status bars: hours or share
	Hide  []string
	Drill string
	// Days is the old ?days=N: a custom range that also opens the time
	// tables, as /stats?days=N did before the dashboard.
	Days int
}

func parseStatsOpts(q url.Values) statsOpts {
	o := statsOpts{Range: "30d", Group: "type", P: "50", Scale: "hours"}
	for _, r := range statsRanges {
		if q.Get("range") == r.Key {
			o.Range = r.Key
		}
	}
	for _, g := range statsGroups {
		if q.Get("group") == g.Key {
			o.Group = g.Key
		}
	}
	if q.Get("p") == "90" {
		o.P = "90"
	}
	if q.Get("scale") == "share" {
		o.Scale = "share"
	}
	for _, h := range strings.Split(q.Get("hide"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			o.Hide = append(o.Hide, h)
		}
	}
	o.Drill = q.Get("drill")
	if n, err := strconv.Atoi(q.Get("days")); err == nil && n >= 1 && n <= 3650 {
		o.Days = n
	}
	return o
}

// days is the range in days; 0 is all.
func (o statsOpts) days() int {
	if o.Days > 0 {
		return o.Days
	}
	for _, r := range statsRanges {
		if r.Key == o.Range {
			return r.Days
		}
	}
	return 30
}

func (o statsOpts) hidden(name string) bool {
	for _, h := range o.Hide {
		if h == name {
			return true
		}
	}
	return false
}

// query is the options as a query string; drill only when keepDrill.
func (o statsOpts) query(keepDrill bool) string {
	q := url.Values{}
	if o.Days > 0 {
		q.Set("days", strconv.Itoa(o.Days))
	} else {
		q.Set("range", o.Range)
	}
	q.Set("group", o.Group)
	if o.P != "50" {
		q.Set("p", o.P)
	}
	if o.Scale != "hours" {
		q.Set("scale", o.Scale)
	}
	if len(o.Hide) > 0 {
		q.Set("hide", strings.Join(o.Hide, ","))
	}
	if keepDrill && o.Drill != "" {
		q.Set("drill", o.Drill)
	}
	return q.Encode()
}

// statsCardData is a chart card and its dashboard, for the statsChart
// template.
type statsCardData struct {
	Dash *Dashboard
	Card ChartCard
}

// statsRangeLabel names the range: "30 days", "all time", "45 days".
func statsRangeLabel(o statsOpts) string {
	if o.Days > 0 {
		return plural(o.Days, "day", "days")
	}
	if o.Range == "all" {
		return "all time"
	}
	for _, r := range statsRanges {
		if r.Key == o.Range {
			return r.Label
		}
	}
	return o.Range
}

// Dashboard is the top of the stats page.
type Dashboard struct {
	Opts     statsOpts
	Base     string // the page path, /p/<slug>/stats
	From, To time.Time

	Ranges []statsOption
	Groups []statsOption

	Snapshots  int
	Backfilled int
	Latest     string // "23 Oct 13:17 (daily)"; "" without a snapshot
	AggMS      int64
	Project    string

	Gauges  []GaugeCard
	Trends  []ChartCard
	Pending []PendingCard
	Bars    []ChartCard
	Weekly  ChartCard
	Drill   *Drill
	// Hint is the backfill hint shown when trends have under two points
	// of history.
	Hint string
	// Done and Split count the issues done in the range, and those of
	// them with CLI calls.
	Done, Split int
}

// statsOption is a range or group choice as a link.
type statsOption struct {
	Key, Label, Href string
	On               bool
}

// GaugeCard is one gauge.
type GaugeCard struct {
	Title   string
	SVG     template.HTML
	Value   string
	Context string
	Verdict string // "Over limit", "Within limit", ""
	Over    bool
	Chip    string
	Href    string
	// Pending: no data yet; the issue that brings it.
	Pending string
	Empty   string
}

// ChartCard is one chart with its table.
type ChartCard struct {
	ID      string
	Title   string
	Sub     string
	SubSVG  template.HTML // a sparkline in the subtitle
	SVG     template.HTML
	Table   chart.Table
	Legend  []LegendItem
	Toggles []statsOption
	Empty   string
	Error   string
	Points  int
	// Big is a headline value over the chart (Disk use: "3.35 GB"); Bars
	// list its parts, each with a bar scaled to the largest.
	Big  string
	Bars []AreaBar
}

// LegendItem is one series in a legend; Href toggles it when set.
type LegendItem struct {
	Name, Class, Href string
	Hidden            bool
}

// PendingCard is a chart whose metric family does not exist yet.
type PendingCard struct {
	Title, Family, Issue string
}

// Drill lists the issues behind a point or a bar.
type Drill struct {
	Title  string
	Issues []DrillIssue
	More   int
	Close  string
	Anchor string
}

// DrillIssue is one issue in a drill-down.
type DrillIssue struct {
	Slug, Href, Date, Detail string
}

// The issues that bring the families this page waits for.
const (
	checkSpeedIssue  = "workflow/health-check-speed-ux-test-timing-tree-and-ci-durations"
	storageIssue     = "ui/evidence-tab-proof-shots-and-criteria"
	contextCostIssue = "workflow/health-context-cost-what-every-agent-pays-before-working"
)

// drillLimit is how many issues a drill-down lists before "N more".
const drillLimit = 12

// pointGap breaks a trend line across a missing day.
const pointGap = 36 * time.Hour

type dashIn struct {
	Proj     *tracker.Project
	Base     string // /p/<slug>/stats
	Prefix   string // /p/<slug>
	WF       *tracker.WorkflowConfig
	Issues   []*tracker.Issue
	Opts     statsOpts
	Now      time.Time
	Wide     timeline.Report // covers [From - timeline.Window, Now]
	AggMS    int64
	Snaps    []*health.Snapshot // every snapshot, oldest first
	Released string
}

// statsWindow is the aggregate window a request needs: its range plus the
// 30 days a trend point looks back. all is ten years.
func (o statsOpts) window() time.Duration {
	d := o.days()
	if d == 0 {
		return 3650 * 24 * time.Hour
	}
	return time.Duration(d)*24*time.Hour + timeline.Window
}

func buildDashboard(in dashIn) *Dashboard {
	d := &Dashboard{Opts: in.Opts, Base: in.Base, To: in.Now, AggMS: in.AggMS, Project: in.Proj.Slug}
	if days := in.Opts.days(); days > 0 {
		d.From = in.Now.AddDate(0, 0, -days)
	} else {
		d.From = in.Now.AddDate(0, 0, -30)
		if len(in.Snaps) > 0 && in.Snaps[0].Time().Before(d.From) {
			d.From = in.Snaps[0].Time()
		}
		for _, s := range in.Wide.Issues {
			if s.DoneAt.Before(d.From) {
				d.From = s.DoneAt
			}
		}
	}
	for _, r := range statsRanges {
		o := in.Opts
		o.Range, o.Days, o.Drill = r.Key, 0, ""
		d.Ranges = append(d.Ranges, statsOption{Key: r.Key, Label: r.Label, Href: in.Base + "?" + o.query(false), On: in.Opts.Days == 0 && in.Opts.Range == r.Key})
	}
	for _, g := range statsGroups {
		o := in.Opts
		o.Group, o.Drill = g.Key, ""
		d.Groups = append(d.Groups, statsOption{Key: g.Key, Label: g.Label, Href: in.Base + "?" + o.query(false), On: in.Opts.Group == g.Key})
	}

	d.Snapshots = len(in.Snaps)
	for _, s := range in.Snaps {
		if s.Run.Trigger == health.TriggerBackfill {
			d.Backfilled++
		}
	}
	var latest *health.Snapshot
	if n := len(in.Snaps); n > 0 {
		latest = in.Snaps[n-1]
		d.Latest = latest.Time().Format("2 Jan 15:04") + " (" + latest.Run.Trigger + ")"
	}

	rep := in.Wide.Filter(d.From, d.To)
	d.Done = len(rep.Issues)
	for _, s := range rep.Issues {
		if s.Known {
			d.Split++
		}
	}
	d.Gauges = gauges(in, rep, latest)

	daily := lastPerDay(in.Snaps, d.From, d.To)
	if len(daily) < 2 {
		d.Hint = "issue-cli --project " + in.Proj.Slug + " health backfill"
	}
	d.Trends = trends(in, d, daily, latest)
	d.Pending = pendingCards(in.Snaps)
	d.Bars = timeBars(in, d, rep)
	d.Weekly = weeklyColumns(in, d, rep)
	d.Drill = drill(in, d, rep)
	return d
}

// href is the page with the options, one changed by change.
func (d *Dashboard) href(change func(*statsOpts)) string {
	o := d.Opts
	o.Drill = ""
	if change != nil {
		change(&o)
	}
	return d.Base + "?" + o.query(true)
}

// drillHref opens the issues behind key, anchored at the chart.
func (d *Dashboard) drillHref(key, anchor string) string {
	return d.href(func(o *statsOpts) { o.Drill = key }) + "#" + anchor
}

func gauges(in dashIn, rep timeline.Report, latest *health.Snapshot) []GaugeCard {
	var cfg *tracker.HealthConfig
	if in.WF != nil {
		cfg = in.WF.Health
	}
	var out []GaugeCard

	// Waiting on you, live over the range.
	g := GaugeCard{Title: "Waiting on you", Href: "#time"}
	var all timeline.TypeRow
	for _, t := range rep.Types {
		if t.Type == timeline.All {
			all = t
		}
	}
	if all.Working+all.Waiting+all.Idle == 0 {
		g.Empty = "No issues with CLI calls done in this range"
	} else {
		share := all.WaitingShare()
		limit, hasLimit := cfg.Limit("waiting_share")
		gg := chart.Gauge{Label: "Waiting on you: " + chart.Format(chart.Share, share) + " of issue time", Value: share, Max: 1, Class: chart.ClassWaiting}
		g.Value, g.Context = chart.Format(chart.Share, share), "of issue time"
		if hasLimit {
			gg.Limit = limit
			gg.Label += ", limit " + chart.Format(chart.Share, limit)
			g.Context += " · limit " + chart.Format(chart.Share, limit)
			g.Over = share > limit
			g.Verdict = verdict(g.Over)
		}
		g.SVG = gg.SVG()
	}
	out = append(out, g)

	// The current milestone, live.
	m := GaugeCard{Title: "Current milestone", Href: in.Prefix + "/status"}
	if v, done, total, items := milestone.Pick(milestone.ByVersion(in.Issues), milestone.StatusesOf(in.WF), in.Released); v != "" {
		m.Title = "Milestone " + v
		m.Value = fmt.Sprintf("%d / %d", done, total)
		m.Context = fmt.Sprintf("done · %d open", total-done)
		m.SVG = chart.Gauge{Label: fmt.Sprintf("Milestone %s: %d of %d issues done", v, done, total), Value: float64(done), Max: float64(total), Class: chart.ClassAgent}.SVG()
		m.Chip = openByStatus(in.WF, items)
	} else {
		m.Empty = "No version with open issues"
	}
	out = append(out, m)

	// Big files, from the latest snapshot.
	b := GaugeCard{Title: "Big files"}
	if v, ok := snapValue(latest, "codebase.big_files", nil); ok {
		limit, hasLimit := cfg.Limit("big_files")
		max := v
		if hasLimit && limit*2 > max {
			max = limit * 2
		}
		gg := chart.Gauge{Label: fmt.Sprintf("Big files: %d over %d lines", int(v), cfg.BigFileLimit()), Value: v, Max: max, Class: chart.ClassAgent}
		b.Value, b.Context = strconv.Itoa(int(v)), fmt.Sprintf("over %d lines", cfg.BigFileLimit())
		if hasLimit {
			gg.Limit = limit
			gg.Label += fmt.Sprintf(", limit %d", int(limit))
			b.Context += fmt.Sprintf(" · limit %d", int(limit))
			b.Over = v > limit
			b.Verdict = verdict(b.Over)
		}
		b.SVG = gg.SVG()
	} else {
		b.Empty = "No codebase snapshot yet"
	}
	out = append(out, b)

	out = append(out,
		GaugeCard{Title: "CI green rate, 14 days", Pending: checkSpeedIssue},
		GaugeCard{Title: "Static context tokens", Pending: contextCostIssue},
	)
	return out
}

func verdict(over bool) string {
	if over {
		return "Over limit"
	}
	return "Within limit"
}

// openByStatus is "1 shipping, 1 in design, 12 idea" for the milestone's
// open issues, furthest along first.
func openByStatus(wf *tracker.WorkflowConfig, items []*tracker.Issue) string {
	st := milestone.StatusesOf(wf)
	n := map[string]int{}
	for _, is := range items {
		k := strings.ToLower(is.Status)
		if k == st.Final || st.Side[k] {
			continue
		}
		n[is.Status]++
	}
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := statusIndex(wf, keys[i]), statusIndex(wf, keys[j])
		if a != b {
			return a > b
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%d %s", n[k], k)
	}
	return strings.Join(parts, ", ")
}

func statusIndex(wf *tracker.WorkflowConfig, status string) int {
	if wf == nil {
		return -1
	}
	return wf.GetStatusIndex(status)
}

func snapValue(s *health.Snapshot, metric string, labels map[string]string) (float64, bool) {
	if s == nil {
		return 0, false
	}
	return s.Value(metric, labels)
}

// lastPerDay keeps each day's last snapshot in [from, to].
func lastPerDay(snaps []*health.Snapshot, from, to time.Time) []*health.Snapshot {
	var out []*health.Snapshot
	for _, s := range snaps {
		at := s.Time()
		if at.Before(from) || at.After(to) {
			continue
		}
		if n := len(out); n > 0 && sameDate(out[n-1].Time(), at) {
			out[n-1] = s
			continue
		}
		out = append(out, s)
	}
	return out
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// typeOrder puts the known types first in a fixed order, others by name,
// none (no type) last.
func typeOrder(types []string) []string {
	rank := map[string]int{"feature": 0, "bugfix": 1, "tweak": 2, "epic": 3}
	sort.SliceStable(types, func(i, j int) bool {
		ri, iok := rank[types[i]]
		rj, jok := rank[types[j]]
		switch {
		case types[i] == timeline.NoType:
			return false
		case types[j] == timeline.NoType:
			return true
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return types[i] < types[j]
	})
	return types
}

func pointKey(chartID string, at time.Time, series string) string {
	return "point:" + chartID + ":" + at.Format("2006-01-02") + ":" + series
}

func trends(in dashIn, d *Dashboard, daily []*health.Snapshot, latest *health.Snapshot) []ChartCard {
	var cfg *tracker.HealthConfig
	if in.WF != nil {
		cfg = in.WF.Health
	}
	issuesAt := func(s *health.Snapshot, typ string) float64 {
		v, _ := s.Value("workflow.issues", map[string]string{"type": typ})
		return v
	}
	base := func(id, title, sub string) (ChartCard, chart.Line) {
		return ChartCard{ID: id, Title: title, Sub: sub}, chart.Line{Label: title, From: d.From, To: d.To, Gap: pointGap}
	}
	finish := func(c ChartCard, l chart.Line, family string) ChartCard {
		if latest != nil {
			if r, ok := latest.Families[family]; ok && r.Error != "" {
				c.Error = "The " + family + " family failed in the latest snapshot: " + r.Error
			}
		}
		c.Points = l.Points()
		if l.Empty() {
			c.Empty = "No points in this range"
			return c
		}
		// Mark where the data starts when it starts inside the range.
		first := time.Time{}
		for _, s := range l.Series {
			for _, p := range s.Points {
				if first.IsZero() || p.At.Before(first) {
					first = p.At
				}
			}
		}
		if first.Sub(d.From) > 2*24*time.Hour {
			l.MarkerAt, l.MarkerText = first, "data starts"
		}
		c.SVG, c.Table = l.SVG(), l.Table()
		return c
	}
	selAt, selSeries, selChart := parseDrillPoint(in.Opts.Drill)
	selected := func(id string, l *chart.Line) {
		if selChart == id {
			l.SelectedAt, l.SelectedSeries = selAt, selSeries
		}
	}

	var out []ChartCard

	// Cycle time by type.
	metric := "workflow.cycle.p" + in.Opts.P
	c, l := base("cycle", "Cycle time p"+in.Opts.P+", by type", "Dispatch to done. Click a point for its issues.")
	l.Unit = chart.Seconds
	seen := map[string]bool{}
	var types []string
	for _, s := range daily {
		for _, r := range s.Rows(metric) {
			if t := r.Labels["type"]; t != timeline.All && !seen[t] {
				seen[t] = true
				types = append(types, t)
			}
		}
	}
	types = typeOrder(types)
	for _, typ := range types {
		name := typeName(typ)
		other := append([]string(nil), d.Opts.Hide...)
		item := LegendItem{Name: name, Class: chart.TypeClass(typ, types)}
		if d.Opts.hidden(typ) {
			item.Hidden = true
			item.Href = d.href(func(o *statsOpts) { o.Hide = without(other, typ) }) + "#cycle"
			c.Legend = append(c.Legend, item)
			continue
		}
		item.Href = d.href(func(o *statsOpts) { o.Hide = append(other, typ) }) + "#cycle"
		c.Legend = append(c.Legend, item)
		ser := chart.Series{Name: name, Class: item.Class}
		for _, s := range daily {
			n := issuesAt(s, typ)
			v, ok := s.Value(metric, map[string]string{"type": typ})
			if !ok || n == 0 {
				continue
			}
			ser.Points = append(ser.Points, chart.Point{At: s.Time(), Value: v, Note: plural(int(n), "issue", "issues"), Href: d.drillHref(pointKey("cycle", s.Time(), typ), "cycle")})
		}
		l.Series = append(l.Series, ser)
	}
	for _, p := range []string{"50", "90"} {
		p := p
		c.Toggles = append(c.Toggles, statsOption{Key: p, Label: "p" + p, On: in.Opts.P == p, Href: d.href(func(o *statsOpts) { o.P = p }) + "#cycle"})
	}
	selected("cycle", &l)
	out = append(out, finish(c, l, "workflow"))

	// Waiting on you.
	c, l = base("waiting", "Waiting on you", "Share of issue time in statuses only you can move on, and up to each approval")
	l.Unit = chart.Share
	if limit, ok := cfg.Limit("waiting_share"); ok {
		l.Limit, l.LimitLabel = limit, "limit "+chart.Format(chart.Share, limit)+" (health.limits.waiting_share)"
	}
	ser := chart.Series{Name: "waiting", Class: chart.ClassWaiting}
	for _, s := range daily {
		v, ok := s.Value("workflow.time.share", map[string]string{"kind": "waiting"})
		if !ok || issuesAt(s, timeline.All) == 0 {
			continue
		}
		ser.Points = append(ser.Points, chart.Point{At: s.Time(), Value: v, Href: d.drillHref(pointKey("waiting", s.Time(), timeline.All), "waiting")})
	}
	l.Series = []chart.Series{ser}
	selected("waiting", &l)
	out = append(out, finish(c, l, "workflow"))

	// Agent working time per issue.
	c, l = base("working", "Agent working time per issue", "p50 and p90 across the issues done in the 30 days before each point")
	l.Unit = chart.Seconds
	for _, p := range []string{"p50", "p90"} {
		ser := chart.Series{Name: p, Class: chart.ClassAgent, Faint: p == "p90"}
		for _, s := range daily {
			v, ok := s.Value("workflow.working."+p, map[string]string{"type": timeline.All})
			if !ok || issuesAt(s, timeline.All) == 0 {
				continue
			}
			ser.Points = append(ser.Points, chart.Point{At: s.Time(), Value: v, Href: d.drillHref(pointKey("working", s.Time(), timeline.All), "working")})
		}
		l.Series = append(l.Series, ser)
		c.Legend = append(c.Legend, LegendItem{Name: p, Class: chart.ClassAgent, Hidden: false})
	}
	selected("working", &l)
	out = append(out, finish(c, l, "workflow"))

	// Rework and gate failures.
	c, l = base("rework", "Rework and gate failures", "Rework loops, and refused transition calls that were real attempts (dry runs are in each point's tooltip)")
	l.Unit = chart.Count
	loops := chart.Series{Name: "rework loops", Class: "s-feature"}
	fails := chart.Series{Name: "gate failures", Class: "s-bugfix", Faint: true}
	for _, s := range daily {
		if issuesAt(s, timeline.All) == 0 {
			continue
		}
		href := d.drillHref(pointKey("rework", s.Time(), timeline.All), "rework")
		if v, ok := s.Value("workflow.rework.loops", map[string]string{"type": timeline.All}); ok {
			loops.Points = append(loops.Points, chart.Point{At: s.Time(), Value: v, Href: href})
		}
		real, dry := 0.0, 0.0
		for _, r := range s.Rows("workflow.gate_failures") {
			if r.Labels["dry_run"] == "true" {
				dry += r.Value
			} else {
				real += r.Value
			}
		}
		fails.Points = append(fails.Points, chart.Point{At: s.Time(), Value: real, Note: plural(int(dry), "dry run", "dry runs"), Href: href})
	}
	l.Series = []chart.Series{loops, fails}
	c.Legend = []LegendItem{{Name: loops.Name, Class: loops.Class}, {Name: fails.Name, Class: fails.Class}}
	selected("rework", &l)
	out = append(out, finish(c, l, "workflow"))

	// Lines of code, with the test ratio in the subtitle.
	c, l = base("loc", "Lines of code", "Code and test lines at the branch's commit")
	l.Unit = chart.Count
	ser = chart.Series{Name: "lines", Class: "s-all"}
	var ratio []float64
	for _, s := range daily {
		if v, ok := s.Value("codebase.source_lines", nil); ok {
			ser.Points = append(ser.Points, chart.Point{At: s.Time(), Value: v})
		}
		if v, ok := s.Value("codebase.test_ratio", nil); ok {
			ratio = append(ratio, v)
		}
	}
	l.Series = []chart.Series{ser}
	if n := len(ratio); n > 0 {
		c.Sub += fmt.Sprintf(" · test ratio %.2f", ratio[n-1])
		c.SubSVG = chart.Spark{Label: "Test ratio trend", Values: ratio, Class: "s-tweak"}.SVG()
	}
	out = append(out, finish(c, l, "codebase"))

	// Disk use, once a snapshot has the storage family.
	if c, ok := storageCard(d, daily, latest); ok {
		out = append(out, c)
	}
	return out
}

func without(list []string, x string) []string {
	var out []string
	for _, v := range list {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

// typeName is how a type shows: "none" is "no type".
func typeName(t string) string {
	if t == timeline.NoType {
		return "no type"
	}
	return t
}

// pendingCards are the trend charts whose family no snapshot has yet.
func pendingCards(snaps []*health.Snapshot) []PendingCard {
	has := map[string]bool{}
	for _, s := range snaps {
		for f := range s.Families {
			has[f] = true
		}
	}
	var out []PendingCard
	for _, p := range []PendingCard{
		{Title: "CI and make validate duration", Family: "checks", Issue: checkSpeedIssue},
		{Title: "ux-test duration and flaky steps", Family: "checks", Issue: checkSpeedIssue},
		{Title: "Tokens per issue", Family: "context", Issue: contextCostIssue},
		{Title: "Disk use", Family: "storage", Issue: storageIssue},
	} {
		if !has[p.Family] {
			out = append(out, p)
		}
	}
	return out
}

// issueMeta indexes issues by slug.
func issueMeta(issues []*tracker.Issue) map[string]*tracker.Issue {
	m := make(map[string]*tracker.Issue, len(issues))
	for _, is := range issues {
		m[is.Slug] = is
	}
	return m
}

// groupOf is an issue's group for the group-by option.
func groupOf(group string, s timeline.IssueStat, meta map[string]*tracker.Issue) string {
	is := meta[s.Slug]
	switch group {
	case "system":
		if is != nil && is.System != "" {
			return is.System
		}
		return "(no system)"
	case "epic":
		if is != nil {
			if p := tracker.ParentSlug(is); p != "" {
				return p
			}
		}
		return "(no epic)"
	case "version":
		if is != nil && is.Version != "" {
			return is.Version
		}
		return "(no version)"
	}
	return typeName(s.Type)
}

// statusNames is the workflow's statuses in order, then any other status
// the issues spent time in.
func statusNames(wf *tracker.WorkflowConfig, issues []timeline.IssueStat) []string {
	var out []string
	seen := map[string]bool{}
	if wf != nil {
		for _, s := range wf.Statuses {
			if !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s.Name)
			}
		}
	}
	var extra []string
	for _, is := range issues {
		for st := range is.Statuses {
			if !seen[st] {
				seen[st] = true
				extra = append(extra, st)
			}
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

func timeSegments(working, waiting, idle time.Duration) []chart.Segment {
	return []chart.Segment{
		{Name: "agent working", Class: chart.ClassAgent, Value: working.Seconds()},
		{Name: "waiting on you", Class: chart.ClassWaiting, Value: waiting.Seconds()},
		{Name: "idle", Class: chart.ClassIdle, Value: idle.Seconds()},
	}
}

// maxGroups is how many group rows the by-group chart draws; the rest are
// summed into one row.
const maxGroups = 12

func timeBars(in dashIn, d *Dashboard, rep timeline.Report) []ChartCard {
	share := in.Opts.Scale == "share"
	unit := chart.Hours
	if share {
		unit = chart.Share
	}
	drillKind, drillVal, _ := strings.Cut(in.Opts.Drill, ":")

	// By status.
	type sums struct{ working, waiting, idle time.Duration }
	byStatus := map[string]*sums{}
	var total sums
	for _, s := range rep.Issues {
		if !s.Known {
			continue
		}
		for st, t := range s.Statuses {
			b := byStatus[st]
			if b == nil {
				b = &sums{}
				byStatus[st] = b
			}
			b.working += t.Working
			b.waiting += t.Waiting
			b.idle += t.Idle
			total.working += t.Working
			total.waiting += t.Waiting
			total.idle += t.Idle
		}
	}
	status := ChartCard{ID: "time", Title: "By status", Sub: fmt.Sprintf("Summed over the issues: agent working %s of %s", chart.Format(chart.Hours, total.working.Seconds()), chart.Format(chart.Hours, (total.working+total.waiting+total.idle).Seconds()))}
	for _, sc := range []struct{ key, label string }{{"hours", "Hours"}, {"share", "Share"}} {
		key := sc.key
		status.Toggles = append(status.Toggles, statsOption{Key: key, Label: sc.label, On: in.Opts.Scale == key, Href: d.href(func(o *statsOpts) { o.Scale = key }) + "#time"})
	}
	h := chart.HBar{Label: "Time by status: agent working, waiting on you and idle", Unit: unit}
	for _, st := range statusNames(in.WF, rep.Issues) {
		b := byStatus[st]
		if b == nil || b.working+b.waiting+b.idle == 0 {
			continue
		}
		bar := chart.Bar{Label: st, Segments: timeSegments(b.working, b.waiting, b.idle), Href: d.drillHref("status:"+st, "time"), Selected: drillKind == "status" && drillVal == st}
		if share {
			bar = bar.AsShare()
		}
		h.Bars = append(h.Bars, bar)
	}
	if len(h.Bars) == 0 {
		status.Empty = "No issues with CLI calls done in this range"
	} else {
		status.SVG, status.Table = h.SVG(), h.Table()
		status.Table.Head[0] = "Status"
	}

	// By the group-by option.
	meta := issueMeta(in.Issues)
	groupLabel := "type"
	for _, g := range statsGroups {
		if g.Key == in.Opts.Group {
			groupLabel = strings.ToLower(g.Label)
		}
	}
	byGroup := map[string]*sums{}
	for _, s := range rep.Issues {
		if !s.Known {
			continue
		}
		k := groupOf(in.Opts.Group, s, meta)
		b := byGroup[k]
		if b == nil {
			b = &sums{}
			byGroup[k] = b
		}
		for _, t := range s.Statuses {
			b.working += t.Working
			b.waiting += t.Waiting
			b.idle += t.Idle
		}
	}
	keys := make([]string, 0, len(byGroup))
	for k := range byGroup {
		keys = append(keys, k)
	}
	tot := func(k string) time.Duration { b := byGroup[k]; return b.working + b.waiting + b.idle }
	sort.Slice(keys, func(i, j int) bool {
		if tot(keys[i]) != tot(keys[j]) {
			return tot(keys[i]) > tot(keys[j])
		}
		return keys[i] < keys[j]
	})
	group := ChartCard{ID: "group", Title: "By " + groupLabel, Sub: "Group by: " + groupLabel + " · click a bar for its issues"}
	g := chart.HBar{Label: "Time by " + groupLabel + ": agent working, waiting on you and idle", Unit: unit}
	var rest sums
	restN := 0
	for i, k := range keys {
		b := byGroup[k]
		if i >= maxGroups {
			rest.working += b.working
			rest.waiting += b.waiting
			rest.idle += b.idle
			restN++
			continue
		}
		bar := chart.Bar{Label: shortGroup(k), Segments: timeSegments(b.working, b.waiting, b.idle), Href: d.drillHref("group:"+k, "group"), Selected: drillKind == "group" && drillVal == k}
		if share {
			bar = bar.AsShare()
		}
		g.Bars = append(g.Bars, bar)
	}
	if restN > 0 {
		bar := chart.Bar{Label: plural(restN, "other", "others"), Segments: timeSegments(rest.working, rest.waiting, rest.idle)}
		if share {
			bar = bar.AsShare()
		}
		g.Bars = append(g.Bars, bar)
	}
	if len(g.Bars) == 0 {
		group.Empty = "No issues with CLI calls done in this range"
	} else {
		group.SVG, group.Table = g.SVG(), g.Table()
		group.Table.Head[0] = strings.ToUpper(groupLabel[:1]) + groupLabel[1:]
	}
	return []ChartCard{status, group}
}

// shortGroup keeps a group label to the chart's label column: the last part
// of an epic slug, clipped.
func shortGroup(k string) string {
	if i := strings.LastIndex(k, "/"); i >= 0 && i < len(k)-1 {
		k = k[i+1:]
	}
	return clip(k, 16)
}

// periodOf is the column an issue done at t falls in: its ISO week, or its
// month for ranges over half a year.
func periodOf(t time.Time, monthly bool) (key, label string) {
	if monthly {
		return t.Format("2006-01"), t.Format("Jan")
	}
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w), fmt.Sprintf("W%02d", w)
}

func weeklyColumns(in dashIn, d *Dashboard, rep timeline.Report) ChartCard {
	monthly := d.To.Sub(d.From) > 182*24*time.Hour
	title := "Issues done per week, by type"
	if monthly {
		title = "Issues done per month, by type"
	}
	c := ChartCard{ID: "weekly", Title: title, Sub: "Click a segment for its issues"}
	var keys []string
	labels := map[string]string{}
	for t := d.From; !t.After(d.To); t = t.AddDate(0, 0, 1) {
		k, l := periodOf(t, monthly)
		if _, ok := labels[k]; !ok {
			keys = append(keys, k)
			labels[k] = l
		}
	}
	count := map[string]map[string]int{}
	seen := map[string]bool{}
	var types []string
	for _, s := range rep.Issues {
		k, _ := periodOf(s.DoneAt, monthly)
		if count[k] == nil {
			count[k] = map[string]int{}
		}
		count[k][s.Type]++
		if !seen[s.Type] {
			seen[s.Type] = true
			types = append(types, s.Type)
		}
	}
	types = typeOrder(types)
	for _, t := range types {
		c.Legend = append(c.Legend, LegendItem{Name: typeName(t), Class: chart.TypeClass(t, types)})
	}
	drillKind, rest, _ := strings.Cut(in.Opts.Drill, ":")
	selKey, selType, _ := strings.Cut(rest, ":")
	cols := chart.Columns{Label: title, Unit: chart.Count}
	for _, k := range keys {
		bar := chart.Bar{Label: labels[k], SegmentHref: map[string]string{}}
		for _, t := range types {
			n := count[k][t]
			if n == 0 {
				continue
			}
			bar.Segments = append(bar.Segments, chart.Segment{Name: typeName(t), Class: chart.TypeClass(t, types), Value: float64(n)})
			bar.SegmentHref[typeName(t)] = d.drillHref("week:"+k+":"+t, "weekly")
		}
		if drillKind == "week" && selKey == k {
			bar.SelectedSegment = typeName(selType)
		}
		cols.Bars = append(cols.Bars, bar)
	}
	if len(rep.Issues) == 0 {
		c.Empty = "No issues done in this range"
		return c
	}
	c.SVG, c.Table = cols.SVG(), cols.Table()
	c.Table.Head[0] = "Week"
	if monthly {
		c.Table.Head[0] = "Month"
	}
	return c
}

// parseDrillPoint reads point:<chart>:<date>:<series>.
func parseDrillPoint(drill string) (at time.Time, series, chartID string) {
	parts := strings.SplitN(drill, ":", 4)
	if len(parts) != 4 || parts[0] != "point" {
		return time.Time{}, "", ""
	}
	day, err := time.ParseInLocation("2006-01-02", parts[2], time.Local)
	if err != nil {
		return time.Time{}, "", ""
	}
	return day, parts[3], parts[1]
}

// drill lists the issues behind the drill option.
func drill(in dashIn, d *Dashboard, rep timeline.Report) *Drill {
	if in.Opts.Drill == "" {
		return nil
	}
	meta := issueMeta(in.Issues)
	var list []timeline.IssueStat
	var title, anchor string
	kind, rest, _ := strings.Cut(in.Opts.Drill, ":")
	switch kind {
	case "point":
		day, series, chartID := parseDrillPoint(in.Opts.Drill)
		if day.IsZero() {
			return nil
		}
		// A point covers the issues done in the window before its
		// snapshot, as the workflow family counted them.
		end := day.Add(24*time.Hour - time.Nanosecond)
		for _, s := range in.Wide.Filter(end.Add(-timeline.Window), end).Issues {
			if series == timeline.All || s.Type == series {
				list = append(list, s)
			}
		}
		anchor = chartID
		what := "all types"
		if series != timeline.All {
			what = typeName(series)
		}
		title = fmt.Sprintf("%s · %s · %s done in the 30 days before", day.Format("2 Jan"), what, plural(len(list), "issue", "issues"))
		sort.SliceStable(list, func(i, j int) bool { return list[i].Cycle > list[j].Cycle })
	case "status":
		for _, s := range rep.Issues {
			if t, ok := s.Statuses[rest]; ok && s.Known && t.Total > 0 {
				list = append(list, s)
			}
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].Statuses[rest].Total > list[j].Statuses[rest].Total })
		anchor, title = "time", fmt.Sprintf("%s · %s", rest, plural(len(list), "issue", "issues"))
	case "group":
		for _, s := range rep.Issues {
			if s.Known && groupOf(in.Opts.Group, s, meta) == rest {
				list = append(list, s)
			}
		}
		anchor, title = "group", fmt.Sprintf("%s · %s", rest, plural(len(list), "issue", "issues"))
	case "week":
		key, typ, _ := strings.Cut(rest, ":")
		monthly := d.To.Sub(d.From) > 182*24*time.Hour
		label := key
		for _, s := range rep.Issues {
			if k, l := periodOf(s.DoneAt, monthly); k == key && s.Type == typ {
				list = append(list, s)
				label = l
			}
		}
		anchor, title = "weekly", fmt.Sprintf("%s · %s · %s", label, typeName(typ), plural(len(list), "issue", "issues"))
	default:
		return nil
	}
	if kind != "point" && kind != "status" {
		sort.SliceStable(list, func(i, j int) bool { return list[i].DoneAt.After(list[j].DoneAt) })
	}
	dr := &Drill{Title: title, Anchor: anchor, Close: d.href(nil) + "#" + anchor}
	for i, s := range list {
		if i == drillLimit {
			dr.More = len(list) - i
			break
		}
		detail := "cycle " + chart.Duration(s.Cycle)
		if kind == "status" {
			t := s.Statuses[rest]
			detail = fmt.Sprintf("in status %s · agent %s · waiting %s · idle %s", chart.Duration(t.Total), chart.Duration(t.Working), chart.Duration(t.Waiting), chart.Duration(t.Idle))
		} else if s.Known {
			detail += " · agent " + chart.Duration(s.Working)
		}
		if s.Loops > 0 {
			detail += " · " + plural(s.Loops, "loop", "loops")
		}
		dr.Issues = append(dr.Issues, DrillIssue{Slug: s.Slug, Href: in.Prefix + "/issue/" + s.Slug + "?tab=timeline", Date: s.DoneAt.Local().Format("Mon 2 Jan"), Detail: detail})
	}
	return dr
}
