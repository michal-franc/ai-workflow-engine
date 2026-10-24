package main

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/chart"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Health view (/p/<project>/health): the derived health score and the
// agent opinion as gauges, the components behind the derived score, the
// review's recommendations (File as issue / Dismiss: the owner's click is the
// approval), and the trends: both scores, unit and e2e count vs runtime. Its
// numbers come only from the health snapshots and .health/opinions/. See
// docs/workbench-files.md, "Scores" and "The agent opinion".

// HealthPage is the view's model.
type HealthPage struct {
	Prefix, ProjectName, Project string
	Ranges                       []statsOption
	RangeLabel                   string
	Latest                       string
	Snapshots                    int
	Derived, Opinion             HealthGauge
	Components                   []HealthComponent
	Coverage                     string
	Review                       *ReviewView
	Scores                       ChartCard
	Pairs                        []CountRuntime
	Lists                        []HealthList
	// Warnings are the newest snapshot's family warnings (a glob that
	// matched no file), by family name.
	Warnings []HealthWarning
	// Lint is the Lint section: issue-cli health lint run live.
	Lint     *LintPanel
	Flash    string
	FlashErr bool
}

// HealthWarning is one family warning.
type HealthWarning struct{ Family, Text string }

// HealthGauge is one score's gauge.
type HealthGauge struct {
	Title, Value, Change, Context, Empty string
	SVG                                  template.HTML
	Has                                  bool
}

// HealthComponent is one row of the derived score.
type HealthComponent struct {
	Name, Help, Measured, Range, Missing string
	Sub, Weight                          float64
	Has                                  bool
	Pct                                  int
}

// ReviewView is the newest opinion.
type ReviewView struct {
	Date, Age, Reviewer, Assessment string
	Stale                           bool
	Proposed                        int
	Recs                            []RecView
}

// RecView is one recommendation.
type RecView struct {
	ID, Kind, Title, Effort, Effect, State, Issue, IssueHref, Reason string
	Evidence                                                         []string
	Proposed                                                         bool
}

// CountRuntime is the count-vs-runtime chart for one test kind: two aligned
// panels on one time axis (never a dual-axis chart).
type CountRuntime struct {
	ID, Title, Summary string
	Count, Runtime     ChartCard
	Empty              string
}

// HealthList is a short ranked list from the latest snapshot.
type HealthList struct {
	Title, Sub string
	Rows       []HealthListRow
}

// HealthListRow is one entry: a label and its value.
type HealthListRow struct{ Label, Value string }

type healthIn struct {
	Proj   *tracker.Project
	Prefix string
	Range  string
	Snaps  []*health.Snapshot // oldest first
	Root   string
	WF     *tracker.WorkflowConfig
	Now    time.Time
}

// healthRanges are the view's range options (the stats page's).
func healthRange(key string) statsRange {
	for _, r := range statsRanges {
		if r.Key == key {
			return r
		}
	}
	return statsRanges[1] // 30 days
}

func buildHealthPage(in healthIn) *HealthPage {
	p := &HealthPage{Prefix: in.Prefix, ProjectName: in.Proj.Name, Project: in.Proj.Slug, Snapshots: len(in.Snaps)}
	rng := healthRange(in.Range)
	p.RangeLabel = strings.ToLower(rng.Label)
	for _, r := range statsRanges {
		p.Ranges = append(p.Ranges, statsOption{Key: r.Key, Label: r.Label, Href: in.Prefix + "/health?range=" + r.Key, On: r.Key == rng.Key})
	}
	var cfg *tracker.HealthConfig
	if in.WF != nil {
		cfg = in.WF.Health
	}
	var latest *health.Snapshot
	if n := len(in.Snaps); n > 0 {
		latest = in.Snaps[n-1]
		p.Latest = latest.Run.Date
	}
	p.Derived = derivedGauge(latest, in.Root, in.Now)
	o, _ := health.LatestOpinion(in.Root, time.Time{})
	p.Opinion = opinionGauge(o, in.Now)
	p.Review = reviewView(o, in.Prefix, in.Now)
	if latest != nil {
		p.Components, p.Coverage = healthComponents(latest, cfg)
		p.Lists = healthLists(latest)
		p.Warnings = healthWarnings(latest)
	}

	from := in.Now.AddDate(0, 0, -rng.Days)
	if rng.Days == 0 {
		from = in.Now.AddDate(0, 0, -30)
		if len(in.Snaps) > 0 && in.Snaps[0].Time().Before(from) {
			from = in.Snaps[0].Time()
		}
	}
	var snaps []*health.Snapshot
	for _, s := range in.Snaps {
		if t := s.Time(); !t.Before(from) && !t.After(in.Now) {
			snaps = append(snaps, s)
		}
	}
	p.Lint = lintPanel(in.Proj, in.Now)
	p.Scores = scoresChart(snaps, o, in.Root, from, in.Now)
	for _, kind := range health.TestKinds {
		p.Pairs = append(p.Pairs, countRuntime(kind, snaps, from, in.Now))
	}
	return p
}

// healthWarnings lists s's family warnings, by family name.
func healthWarnings(s *health.Snapshot) []HealthWarning {
	names := make([]string, 0, len(s.Families))
	for name := range s.Families {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []HealthWarning
	for _, name := range names {
		for _, w := range s.Families[name].Warnings {
			out = append(out, HealthWarning{Family: name, Text: w})
		}
	}
	return out
}

func derivedGauge(s *health.Snapshot, root string, now time.Time) HealthGauge {
	g := HealthGauge{Title: "Derived health"}
	if s == nil {
		g.Empty = "No snapshot yet: issue-cli health snapshot (the viewer takes one daily)"
		return g
	}
	v, ok := s.Value("score.derived", nil)
	if !ok {
		cov, _ := s.Value("score.coverage", nil)
		g.Empty = fmt.Sprintf("Not enough data: %.0f%% of the score's weight is measured, 50%% is needed. The components below say what is missing.", cov*100)
		return g
	}
	g.Has, g.Value = true, fmt.Sprintf("%.0f", v)
	g.SVG = chart.Gauge{Label: "Derived health " + g.Value + " of 100", Value: v, Max: 100, Class: "s-derived"}.SVG()
	g.Change = scoreChange(v, root, now)
	cov, _ := s.Value("score.coverage", nil)
	g.Context = fmt.Sprintf("from %.0f%% of the weight", cov*100)
	return g
}

// scoreChange is the derived score's change over 7 and 30 days, in words.
func scoreChange(v float64, root string, now time.Time) string {
	var parts []string
	for _, d := range []int{7, 30} {
		then, ok := health.ScoreAt(root, now.AddDate(0, 0, -d))
		if !ok {
			continue
		}
		parts = append(parts, changeWords(v-then, fmt.Sprintf("in %d days", d)))
	}
	if len(parts) == 0 {
		return "no change yet: the score starts today"
	}
	return strings.Join(parts, " · ")
}

func changeWords(delta float64, window string) string {
	d := math.Round(delta)
	switch {
	case d > 0:
		return fmt.Sprintf("▲ +%.0f %s", d, window)
	case d < 0:
		return fmt.Sprintf("▼ −%.0f %s", -d, window)
	}
	return "no change " + window
}

func opinionGauge(o *health.Opinion, now time.Time) HealthGauge {
	g := HealthGauge{Title: "Agent opinion"}
	if o == nil {
		g.Empty = "No review yet. Run review starts a review agent; or ask the lead: issue-cli health review"
		return g
	}
	g.Has, g.Value = true, fmt.Sprintf("%d", o.Score)
	g.SVG = chart.Gauge{Label: "Agent opinion " + g.Value + " of 100", Value: float64(o.Score), Max: 100, Class: "s-opinion"}.SVG()
	g.Change = reviewedAgo(o, now)
	if o.Derived != nil {
		g.Context = fmt.Sprintf("derived was %.0f then", *o.Derived)
	}
	return g
}

func reviewedAgo(o *health.Opinion, now time.Time) string {
	days := o.AgeDays(now)
	switch {
	case days > float64(health.ReviewStaleDays):
		return fmt.Sprintf("review %.0f days old", days)
	case days < 1:
		return "reviewed today"
	case days < 2:
		return "reviewed yesterday"
	}
	return fmt.Sprintf("reviewed %.0f days ago", days)
}

func reviewView(o *health.Opinion, prefix string, now time.Time) *ReviewView {
	if o == nil {
		return nil
	}
	v := &ReviewView{Date: o.Date, Age: reviewedAgo(o, now), Reviewer: o.Reviewer, Assessment: o.Assessment,
		Stale: o.AgeDays(now) > float64(health.ReviewStaleDays), Proposed: o.Proposed()}
	for _, r := range o.Recommendations {
		rv := RecView{ID: r.ID, Kind: strings.ReplaceAll(r.Kind, "_", " "), Title: r.Title, Effort: r.Effort, Effect: r.ExpectedEffect,
			State: r.State, Issue: r.Issue, Reason: r.Reason}
		if rv.State == "" {
			rv.State = health.StateProposed
		}
		rv.Proposed = rv.State == health.StateProposed
		if r.Issue != "" {
			rv.IssueHref = prefix + "/issue/" + r.Issue
		}
		for _, e := range r.Evidence {
			ref := e.Metric
			if ref == "" {
				ref = e.Path
				if e.Lines != "" {
					ref += ":" + e.Lines
				}
			}
			rv.Evidence = append(rv.Evidence, ref+": "+e.Shows)
		}
		v.Recs = append(v.Recs, rv)
	}
	return v
}

func healthComponents(s *health.Snapshot, cfg *tracker.HealthConfig) ([]HealthComponent, string) {
	var out []HealthComponent
	for _, c := range health.Components(cfg) {
		l := map[string]string{"component": c.Name}
		weight, ok := s.Value("score.component.weight", l)
		if !ok {
			continue
		}
		hc := HealthComponent{Name: c.Name, Help: c.Help, Weight: weight, Missing: c.Missing,
			Range: health.FormatValue(c.Unit, c.Good) + " → " + health.FormatValue(c.Unit, c.Bad)}
		if sub, has := s.Value("score.component", l); has {
			v, _ := s.Value("score.component.value", l)
			hc.Has, hc.Sub, hc.Measured, hc.Pct = true, sub, health.FormatValue(c.Unit, v), int(math.Max(sub, 2))
		}
		out = append(out, hc)
	}
	cov, ok := s.Value("score.coverage", nil)
	if !ok {
		return out, ""
	}
	measured := 0
	for _, c := range out {
		if c.Has {
			measured++
		}
	}
	return out, fmt.Sprintf("%d of %d components, %.0f%% of the weight", measured, len(out), cov*100)
}

func scoresChart(snaps []*health.Snapshot, o *health.Opinion, root string, from, to time.Time) ChartCard {
	c := ChartCard{ID: "scores", Title: "Health scores", Sub: "Derived health from every snapshot; the agent opinion holds until the next review",
		Legend: []LegendItem{{Name: "Derived health", Class: "s-derived"}, {Name: "Agent opinion", Class: "s-opinion"}}}
	l := chart.Line{Label: "Health scores", From: from, To: to, Unit: chart.Count, Gap: pointGap, Top: 100, Step: 25}
	derived := chart.Series{Name: "Derived health", Class: "s-derived"}
	opinion := chart.Series{Name: "Agent opinion", Class: "s-opinion"}
	for _, s := range snaps {
		if v, ok := s.Value("score.derived", nil); ok {
			derived.Points = append(derived.Points, chart.Point{At: s.Time(), Value: v})
		}
		if v, ok := s.Value("opinion.score", nil); ok {
			opinion.Points = append(opinion.Points, chart.Point{At: s.Time(), Value: v})
		}
	}
	l.Series = []chart.Series{derived, opinion}
	c.Points = l.Points()
	if l.Empty() {
		c.Empty = "No score in this range yet. Snapshots compute the derived score from now on; issue-cli health backfill computes past days."
		return c
	}
	c.SVG, c.Table = l.SVG(), l.Table()
	return c
}

// countRuntime draws count above runtime for one kind, on the same time axis.
func countRuntime(kind string, snaps []*health.Snapshot, from, to time.Time) CountRuntime {
	k := map[string]string{"kind": kind}
	all := map[string]string{"kind": kind, "result": "all"}
	title := map[string]string{"unit": "Unit tests", "e2e": "End-to-end tests"}[kind]
	cr := CountRuntime{ID: kind + "_tests", Title: title + ": count vs runtime"}
	count := chart.Series{Name: "Tests", Class: "s-count"}
	runtime := chart.Series{Name: "Wall time", Class: "s-runtime"}
	var first, last *health.Snapshot
	for _, s := range snaps {
		n, ok1 := s.Value("tests.count", all)
		w, ok2 := s.Value("tests.wall_seconds", k)
		if !ok1 || !ok2 {
			continue
		}
		if first == nil {
			first = s
		}
		last = s
		count.Points = append(count.Points, chart.Point{At: s.Time(), Value: n})
		runtime.Points = append(runtime.Points, chart.Point{At: s.Time(), Value: w, Note: perTest(s, kind)})
	}
	if last == nil {
		cr.Empty = "No " + strings.ToLower(title) + " results from local CI in this range. A ci.run command needs a results: entry of kind " + kind + " (docs/workbench-files.md, \"Test results\")."
		if kind == "e2e" {
			cr.Empty += " E2E runs only with issue-cli ci run --tier slow."
		}
		return cr
	}
	n0, _ := first.Value("tests.count", all)
	n1, _ := last.Value("tests.count", all)
	w0, _ := first.Value("tests.wall_seconds", k)
	w1, _ := last.Value("tests.wall_seconds", k)
	cr.Summary = fmt.Sprintf("%s → %s tests · %s → %s · per test %s", chart.Format(chart.Count, n0), chart.Format(chart.Count, n1), chart.Format(chart.Seconds, w0), chart.Format(chart.Seconds, w1), perTest(last, kind))
	lc := chart.Line{Label: title + " count", From: from, To: to, Unit: chart.Count, Gap: pointGap, Series: []chart.Series{count}}
	lr := chart.Line{Label: title + " wall time", From: from, To: to, Unit: chart.Seconds, Gap: pointGap, Series: []chart.Series{runtime}}
	cr.Count = ChartCard{ID: kind + "_count", Title: "Tests", Sub: "tests.count{kind=" + kind + ",result=all}", SVG: lc.SVG(), Table: lc.Table(), Legend: []LegendItem{{Name: "Tests", Class: "s-count"}}}
	cr.Runtime = ChartCard{ID: kind + "_runtime", Title: "Wall time", Sub: "tests.wall_seconds{kind=" + kind + "}", SVG: lr.SVG(), Table: lr.Table(), Legend: []LegendItem{{Name: "Wall time", Class: "s-runtime"}}}
	return cr
}

func perTest(s *health.Snapshot, kind string) string {
	v, ok := s.Value("tests.per_test_ms", map[string]string{"kind": kind, "stat": "mean"})
	if !ok {
		return "–"
	}
	if v >= 1000 {
		return fmt.Sprintf("%.1f s", v/1000)
	}
	return fmt.Sprintf("%.1f ms", v)
}

// healthLists are where to look: the review's usual evidence, top 5 each.
func healthLists(s *health.Snapshot) []HealthList {
	var out []HealthList
	add := func(title, sub, metric, label string, format func(float64) string) {
		rows := s.Rows(metric)
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Value > rows[j].Value })
		l := HealthList{Title: title, Sub: sub}
		for i, r := range rows {
			if i == 5 {
				break
			}
			name := r.Labels[label]
			if label == "func" {
				name = r.Labels["path"] + " " + r.Labels["func"]
			}
			l.Rows = append(l.Rows, HealthListRow{Label: name, Value: format(r.Value)})
		}
		if len(l.Rows) > 0 {
			out = append(out, l)
		}
	}
	lines := func(v float64) string { return chart.Format(chart.Count, v) + " lines" }
	secs := func(v float64) string { return fmt.Sprintf("%.1f s", v) }
	add("Hotspots", "big files changed in 5+ commits in 30 days, by lines × commits", "shape.hotspot", "path", func(v float64) string { return chart.Format(chart.Count, v) })
	add("Longest functions", "code, not tests", "shape.long_func.lines", "func", lines)
	add("Slowest tests", "from the newest CI run with results", "tests.slowest.seconds", "test", secs)
	add("Slowest parts", "a package, test class or session: the wall clock's critical path", "tests.part.seconds", "part", secs)
	add("Systems without a playtest", "issues done in 90 days", "tests.e2e.uncovered", "system", func(v float64) string { return chart.Format(chart.Count, v) + " done" })
	return out
}

// latestHealth is the newest snapshot (through the snapshot parse cache),
// nil without one.
func latestHealth(proj *tracker.Project, now time.Time) *health.Snapshot {
	snaps := health.Range(ciRootFor(proj), time.Time{}, now)
	if len(snaps) == 0 {
		return nil
	}
	return snaps[len(snaps)-1]
}

// healthNavNote is the derived score beside Health in the sidebar.
func healthNavNote(proj *tracker.Project) string {
	if s := latestHealth(proj, time.Now()); s != nil {
		if v, ok := s.Value("score.derived", nil); ok {
			return fmt.Sprintf("%.0f", v)
		}
	}
	return ""
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	root := ciRootFor(proj)
	page := buildHealthPage(healthIn{Proj: proj, Prefix: prefix, Range: r.URL.Query().Get("range"),
		Snaps: health.Range(root, time.Time{}, time.Now()), Root: root, WF: proj.LoadWorkflow(), Now: time.Now()})
	q := r.URL.Query()
	switch {
	case q.Get("filed") != "":
		page.Flash = "Filed as " + q.Get("filed") + " (status idea). Your click is recorded as the decision."
	case q.Get("dismissed") != "":
		page.Flash = "Dismissed " + q.Get("dismissed") + "."
	case q.Get("review") != "":
		page.Flash = "Review agent " + q.Get("review") + " started. Its opinion shows here once it records it."
	case q.Get("error") != "":
		page.Flash, page.FlashErr = q.Get("error"), true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "health.html", page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// healthRecID reads health/recommendation/<id>/<action>.
func healthRecID(rest string) string {
	parts := strings.Split(rest, "/")
	if len(parts) != 4 {
		return ""
	}
	return parts[2]
}

func healthRedirect(w http.ResponseWriter, r *http.Request, prefix, key, value string) {
	http.Redirect(w, r, prefix+"/health?"+key+"="+url.QueryEscape(value)+"#review", http.StatusSeeOther)
}

// handleHealthFile files a recommendation: the owner's click is the approval.
func (s *Server) handleHealthFile(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix, rest string) {
	slug, err := health.FileRecommendation(proj, proj.LoadWorkflow(), ciRootFor(proj), healthRecID(rest),
		health.Decision{How: "viewer", Quote: "File as issue (Health view)"}, time.Now())
	if err != nil {
		healthRedirect(w, r, prefix, "error", err.Error())
		return
	}
	healthRedirect(w, r, prefix, "filed", slug)
}

func (s *Server) handleHealthDismiss(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix, rest string) {
	id := healthRecID(rest)
	reason := strings.TrimSpace(r.FormValue("reason"))
	if _, err := health.DismissRecommendation(ciRootFor(proj), id, reason, health.Decision{How: "viewer", Quote: "Dismiss (Health view)"}, time.Now()); err != nil {
		healthRedirect(w, r, prefix, "error", err.Error())
		return
	}
	healthRedirect(w, r, prefix, "dismissed", strings.ToUpper(id))
}

// startReview launches the review agent; a var so tests can stub it.
var startReview = agentrun.Start

// handleHealthReview starts a review agent with the brief (issue-cli health
// review --spawn does the same).
func (s *Server) handleHealthReview(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	now := time.Now()
	env, err := health.NewEnv(proj, projectRoot(proj), now)
	if err != nil {
		healthRedirect(w, r, prefix, "error", err.Error())
		return
	}
	snap, _, err := health.Latest(env.Root)
	if err == nil && snap == nil {
		err = fmt.Errorf("no health snapshot yet: issue-cli health snapshot")
	}
	var rev *health.Review
	if err == nil {
		rev, err = health.PrepareReview(health.ReviewInput{Env: env, Snap: snap, RetrosDir: filepath.Join(projectRoot(proj), "retros")})
	}
	if err != nil {
		healthRedirect(w, r, prefix, "error", err.Error())
		return
	}
	resp := startReview(proj, agentrun.Spec{Session: "review-" + proj.Slug, Prompt: rev.Brief, AgentType: "claude",
		WorkDir: env.Root, Workflow: env.Workflow, Headless: true})
	if resp.Status == "error" {
		var details []string
		for _, st := range resp.Steps {
			if st.Status == "error" {
				details = append(details, st.Name+": "+st.Detail)
			}
		}
		healthRedirect(w, r, prefix, "error", "The review agent did not start: "+strings.Join(details, "; "))
		return
	}
	healthRedirect(w, r, prefix, "review", resp.Session)
}

// HealthCard is the Status page's Health card: the two scores, their bars,
// one line each, the weakest component and what waits for the owner.
type HealthCard struct {
	Derived, DerivedLine     string
	DerivedPct               int
	HasDerived               bool
	Opinion, OpinionLine     string
	OpinionPct               int
	HasOpinion, OpinionStale bool
	Weakest                  string
	Proposed                 int
	// Lint is "2 lint warnings" from the snapshot's lint family, "" when
	// it has none; LintCommand lists them.
	Lint, LintCommand string
}

// healthCard reads the newest snapshot and opinion; nil without a snapshot.
func healthCard(proj *tracker.Project, now time.Time) *HealthCard {
	root := ciRootFor(proj)
	s := latestHealth(proj, now)
	if s == nil {
		return nil
	}
	c := &HealthCard{Derived: "–", Opinion: "–", OpinionLine: "no review yet"}
	if v, ok := s.Value("score.derived", nil); ok {
		c.HasDerived, c.Derived, c.DerivedPct = true, fmt.Sprintf("%.0f", v), int(v)
		c.DerivedLine = "no change yet"
		if then, ok := health.ScoreAt(root, now.AddDate(0, 0, -7)); ok {
			c.DerivedLine = changeWords(v-then, "in 7 days")
		}
	} else {
		c.DerivedLine = "not enough data yet"
	}
	if o, _ := health.LatestOpinion(root, time.Time{}); o != nil {
		c.HasOpinion, c.Opinion, c.OpinionPct = true, fmt.Sprintf("%d", o.Score), o.Score
		c.OpinionLine, c.OpinionStale = reviewedAgo(o, now), o.AgeDays(now) > float64(health.ReviewStaleDays)
		c.Proposed = o.Proposed()
	}
	c.Weakest = weakest(s, proj.LoadWorkflow().Health)
	c.Lint, c.LintCommand = lintLine(s), lintCommandFor(proj)
	return c
}

// weakest names the lowest-scoring component with data: "big files 47
// (17.8% source lines in big files)".
func weakest(s *health.Snapshot, cfg *tracker.HealthConfig) string {
	best, out := 101.0, ""
	for _, c := range health.Components(cfg) {
		l := map[string]string{"component": c.Name}
		sub, ok := s.Value("score.component", l)
		if !ok || sub >= best || sub >= 100 {
			continue
		}
		v, _ := s.Value("score.component.value", l)
		best = sub
		out = fmt.Sprintf("%s %.0f (%s %s)", strings.ReplaceAll(c.Name, "_", " "), sub, health.FormatValue(c.Unit, v), strings.ToLower(c.Help[:1])+c.Help[1:])
	}
	return out
}

// healthMarkdown is the Health section of /status.md, for the lead.
func healthMarkdown(b *strings.Builder, c *HealthCard) {
	if c == nil {
		return
	}
	b.WriteString("\n## Health\n")
	fmt.Fprintf(b, "- Derived %s (%s)", c.Derived, c.DerivedLine)
	if c.Weakest != "" {
		fmt.Fprintf(b, " · weakest: %s", c.Weakest)
	}
	b.WriteString("\n")
	switch {
	case !c.HasOpinion:
		b.WriteString("- Opinion: no review yet · issue-cli health review --spawn\n")
	case c.OpinionStale:
		fmt.Fprintf(b, "- Opinion %s: %s · issue-cli health review --spawn\n", c.Opinion, c.OpinionLine)
	default:
		fmt.Fprintf(b, "- Opinion %s (%s)\n", c.Opinion, c.OpinionLine)
	}
	if c.Proposed > 0 {
		fmt.Fprintf(b, "- %d recommendation(s) wait for the owner on the Health view\n", c.Proposed)
	}
	lintMarkdown(b, c.Lint, c.LintCommand)
}
