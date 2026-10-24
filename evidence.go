package main

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/proof"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The detail page's Evidence tab answers "is it proven?": the playtest shots
// each criterion points to, the proof report (the one prove --list and the
// criteria_proven gate read), the verdict, the change, the playtest per
// step, risk: comments and stale body text. Built only on ?tab=evidence;
// the label is built on every tab.

// EvidenceLabel is the tab's count chip. Class is green, amber or gray;
// Text is empty when the issue has no criteria checkboxes (D-3).
type EvidenceLabel struct {
	Text, Class string
}

// evidenceLabel follows D-3: every criterion proven is green; short is amber
// when the gate covers the issue or any link exists; criteria with no link
// at all on an issue the gate doesn't cover are grey ("n ticked · 0 linked").
func evidenceLabel(r proof.Report, required bool) EvidenceLabel {
	total := len(r.Criteria)
	if total == 0 {
		return EvidenceLabel{}
	}
	proven, ticked, linked := 0, 0, false
	for _, c := range r.Criteria {
		if c.Proven() {
			proven++
		}
		if c.Checked {
			ticked++
		}
		if len(c.Links) > 0 {
			linked = true
		}
	}
	switch {
	case proven == total:
		return EvidenceLabel{Text: strconv.Itoa(proven) + " / " + strconv.Itoa(total) + " proven", Class: "green"}
	case !required && !linked:
		return EvidenceLabel{Text: strconv.Itoa(ticked) + " ticked · 0 linked", Class: "gray"}
	}
	return EvidenceLabel{Text: strconv.Itoa(proven) + " / " + strconv.Itoa(total) + " proven", Class: "amber"}
}

// EvidenceTab is the tab's model.
type EvidenceTab struct {
	Label                 EvidenceLabel
	Proven, Total, Ticked int
	Required              bool
	Unlinked              bool
	NotStarted            bool

	Shots      []evShot
	ShotsRun   string
	ShotsBy    string
	ShotsEmpty string
	Other      []evOther

	Criteria []evCriterion
	Strays   []evStray
	Ways     []string
	WaysFor  string
	Missing  []string

	Verdict  evVerdict
	Change   evChange
	Steps    []evStep
	Judged   string
	Words    []evWords
	Watch    []evRisk
	Stale    []StaleLine
	StaleRun bool
}

type evShot struct {
	Step, Title, Session string
	Proves               []string
	ProvesText           string
	State, StateClass    string
	Views                []evView
	Src                  string
	FailLine             string
	NoRun, Reduced       bool
	Large, Narrow        bool
}

// evView is one screen size of a shot. Src is empty when its file is gone.
type evView struct {
	Name, Label, Src string
	Failed, On       bool
	// Narrow is a phone-sized screen, shown at its own width.
	Narrow bool
}

type evOther struct {
	Session   string
	Steps     []string
	StepsText string
}

type evCriterion struct {
	ID, Text                string
	Visual, Checked, Proven bool
	Links                   []evLink
	Fixes                   []string
}

type evLink struct {
	Kind, Ref, Source string
	State, Class      string
	Changed           bool
}

type evStray struct {
	ID, Link string
}

type evVerdict struct {
	Status         string
	Merge, MergeAt string
	Head           string
	HasTests       bool
	TestsAt, Tests string
	TestsAfter     bool
	LastChangeSHA  string
	LastChangeAt   string
	FromDispatch   string
	Waiting        string
	CI             string
	CIClass        string
	Short          string
}

type evStep struct {
	ID, Session, Title  string
	Machine, MachineCls string
	Screens             string
	You, YouCls         string
}

type evWords struct {
	Step, At, Text, Source string
}

type evRisk struct {
	At, Text string
	None     bool
}

// evidenceIn is what buildEvidence reads: everything is loaded beforehand,
// so tests build the tab without a project on disk.
type evidenceIn struct {
	Prefix, Slug, Root string
	Status             string
	Report             proof.Report
	Required           bool
	PastDesign         bool
	Runs               []timeline.PlaytestRun
	Comments           []tracker.Comment
	Decisions          []tracker.Decision
	Body               string
	Transitions        []tracker.TransitionStat
	Change             evChange
	Total              string
	Waiting            time.Duration
	WaitKnown          bool
	CI                 *ciSummary
	Now                time.Time
	// Exists reports whether a shot file is on disk (os.Stat when nil).
	Exists func(string) bool
}

func buildEvidence(in evidenceIn) *EvidenceTab {
	if in.Exists == nil {
		in.Exists = func(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
	}
	r := in.Report
	t := &EvidenceTab{Label: evidenceLabel(r, in.Required), Required: in.Required, Total: len(r.Criteria), Missing: r.Missing}
	linked := false
	for _, c := range r.Criteria {
		if c.Proven() {
			t.Proven++
		}
		if c.Checked {
			t.Ticked++
		}
		linked = linked || len(c.Links) > 0
	}
	t.Unlinked = t.Total > 0 && !linked && !in.Required
	t.NotStarted = !in.PastDesign && r.Ref == "" && len(in.Runs) == 0

	t.Criteria, t.Strays = evidenceCriteria(r)
	for _, c := range t.Criteria {
		if !c.Proven && !t.Unlinked {
			t.WaysFor = c.ID
			t.Ways = proof.Ways(r.Slug, c.ID)
			break
		}
	}
	t.Shots, t.ShotsRun, t.ShotsBy, t.Other = evidenceShots(in)
	switch {
	case len(t.Shots) == 0 && len(in.Runs) == 0:
		t.ShotsEmpty = "No playtest run for this issue in its worktree or the project checkout."
	case len(t.Shots) == 0:
		t.ShotsEmpty = "No step proves a criterion yet; the latest run's steps are listed below."
	}
	t.Steps, t.Judged, t.Words = evidenceSteps(in)
	t.Verdict = evidenceVerdict(in, t)
	t.Change = in.Change
	for i := range t.Change.Commits {
		t.Change.Commits[i].When = evWhen(t.Change.Commits[i].At, in.Now)
	}
	t.Watch = evidenceWatch(in.Comments, in.Now)
	t.StaleRun = in.PastDesign
	if in.PastDesign {
		t.Stale = staleLines(in.Body, in.Decisions)
	}
	return t
}

func evidenceCriteria(r proof.Report) ([]evCriterion, []evStray) {
	var out []evCriterion
	for _, c := range r.Criteria {
		ec := evCriterion{ID: c.ID, Text: c.Text, Visual: c.Visual, Checked: c.Checked, Proven: c.Proven()}
		for _, l := range c.Links {
			el := evLink{Kind: l.Kind, Ref: l.Ref, Source: linkSource(l.Link), State: l.Check.State, Changed: l.Changed}
			switch {
			case l.Check.OK && l.Kind == tracker.ProofValue:
				el.Class = "rp"
			case l.Check.OK:
				el.Class = "ok"
			case strings.HasPrefix(l.Check.State, "failed") || l.Check.State == "not found" || l.Check.State == "missing":
				el.Class = "bad"
			default:
				el.Class = "st"
			}
			if l.Kind == tracker.ProofCommit {
				el.Ref = timeline.ShortSHA(l.Ref)
			}
			if strings.HasPrefix(el.State, "stale") {
				el.State = "stale"
			}
			ec.Links = append(ec.Links, el)
			if !l.Check.OK && l.Check.Fix != "" && !c.Proven() {
				ec.Fixes = append(ec.Fixes, l.Check.Fix)
			}
		}
		out = append(out, ec)
	}
	var strays []evStray
	for _, s := range r.Strays {
		strays = append(strays, evStray{ID: s.ID, Link: proof.Describe(s.Link)})
	}
	return out, strays
}

// linkSource says where a link came from, as the criteria table shows it.
func linkSource(l proof.Link) string {
	switch l.Source {
	case proof.SourceTrailer:
		if l.Kind == tracker.ProofCommit {
			return "trailer"
		}
		return "trailer " + l.From
	case proof.SourcePlaytest:
		return "proves: in " + l.From
	}
	return "prove"
}

// linkedSteps are the step ids the criteria point to, in criterion order,
// each with the criteria it proves and its session when declared there.
type linkedStep struct {
	ID, Session string
	Proves      []string
}

func linkedSteps(r proof.Report) []linkedStep {
	var out []linkedStep
	at := map[string]int{}
	for _, c := range r.Criteria {
		for _, l := range c.Links {
			if l.Kind != tracker.ProofStep {
				continue
			}
			id := strings.ToUpper(l.Ref)
			i, ok := at[id]
			if !ok {
				i = len(out)
				at[id] = i
				out = append(out, linkedStep{ID: id})
			}
			if l.Source == proof.SourcePlaytest && out[i].Session == "" {
				out[i].Session = l.From
			}
			if !containsFold(out[i].Proves, c.ID) {
				out[i].Proves = append(out[i].Proves, c.ID)
			}
		}
	}
	return out
}

// latestRunWith is the newest run of mode that has step id (in session when
// set).
func latestRunWith(runs []timeline.PlaytestRun, mode, session, id string) (*timeline.PlaytestRun, *timeline.PlaytestStep) {
	for i := len(runs) - 1; i >= 0; i-- {
		r := &runs[i]
		if r.Mode != mode || (session != "" && r.Session != session) {
			continue
		}
		for j := range r.Steps {
			if strings.EqualFold(r.Steps[j].ID, id) {
				return r, &r.Steps[j]
			}
		}
	}
	return nil, nil
}

// latestRuns is the newest run of each session in mode, sessions sorted.
func latestRuns(runs []timeline.PlaytestRun, mode string) []*timeline.PlaytestRun {
	by := map[string]*timeline.PlaytestRun{}
	for i := range runs {
		r := &runs[i]
		if r.Mode != mode {
			continue
		}
		if cur := by[r.Session]; cur == nil || r.Started.After(cur.Started) {
			by[r.Session] = r
		}
	}
	var out []*timeline.PlaytestRun
	for _, r := range by {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out
}

// latestShotRun is latestRunWith, preferring the newest run whose shot of
// the step is on disk: a game's -cached run keeps only its results.yaml.
func latestShotRun(in evidenceIn, mode, session, id string) (*timeline.PlaytestRun, *timeline.PlaytestStep) {
	var first *timeline.PlaytestRun
	var firstSt *timeline.PlaytestStep
	for i := len(in.Runs) - 1; i >= 0; i-- {
		r := &in.Runs[i]
		if r.Mode != mode || (session != "" && r.Session != session) {
			continue
		}
		for j := range r.Steps {
			st := &r.Steps[j]
			if !strings.EqualFold(st.ID, id) {
				continue
			}
			if hasShot(in, r, st) {
				return r, st
			}
			if first == nil {
				first, firstSt = r, st
			}
			break
		}
	}
	return first, firstSt
}

func hasShot(in evidenceIn, run *timeline.PlaytestRun, st *timeline.PlaytestStep) bool {
	for _, vp := range st.Viewports {
		if shotURL(in, run.Dir, shotFile(vp.Screenshot, st.ID, vp.Viewport), in.Exists) != "" {
			return true
		}
	}
	return false
}

// fillShot fills s from step st of run: title, state and views.
func fillShot(in evidenceIn, s *evShot, run *timeline.PlaytestRun, st *timeline.PlaytestStep) {
	s.Session, s.Title, s.Reduced = run.Session, st.Title, run.Reduced != ""
	s.State, s.StateClass = stepState(st.Status)
	if _, jst := latestRunWith(in.Runs, "judge", run.Session, st.ID); jst != nil && st.Judgement {
		if v := judgeVerdict(jst); v != "" {
			s.State, s.StateClass = "judged "+v, "blue"
		}
	}
	s.Views, s.FailLine = shotViews(in, run, st)
	for _, v := range s.Views {
		if v.On {
			s.Src, s.Narrow = v.Src, v.Narrow
		}
	}
}

// evidenceShots is the Shots card: one shot per step a criterion points
// to; when none does, every step of each session's latest ux-test run.
func evidenceShots(in evidenceIn) ([]evShot, string, string, []evOther) {
	var shots []evShot
	var newest *timeline.PlaytestRun
	by := "one per step a criterion points to"
	shown := map[string]bool{} // session|STEP, or STEP for a linked step
	key := func(session, id string) string { return session + "|" + strings.ToUpper(id) }
	linked := linkedSteps(in.Report)
	for _, ls := range linked {
		shown[key("", ls.ID)] = true
		s := evShot{Step: ls.ID, Session: ls.Session, Proves: ls.Proves, ProvesText: strings.Join(ls.Proves, " ")}
		run, st := latestShotRun(in, "auto", ls.Session, ls.ID)
		if run == nil {
			if jr, jst := latestShotRun(in, "judge", ls.Session, ls.ID); jr != nil {
				run, st = jr, jst
			}
		}
		if run == nil {
			s.NoRun, s.State, s.StateClass = true, "not run", "amber"
			shots = append(shots, s)
			continue
		}
		if newest == nil || run.Started.After(newest.Started) {
			newest = run
		}
		if len(st.Viewports) == 0 {
			// It ran but saved no shot (a judgement step): the Playtest card
			// has its verdict.
			continue
		}
		fillShot(in, &s, run, st)
		shots = append(shots, s)
	}
	if len(linked) == 0 {
		by = "each session's latest ux-test run; no step proves a criterion yet"
		for _, r := range latestRuns(in.Runs, "auto") {
			for _, rs := range r.Steps {
				run, st := latestShotRun(in, "auto", r.Session, rs.ID)
				if !hasShot(in, run, st) {
					continue
				}
				shown[key(r.Session, rs.ID)] = true
				s := evShot{Step: st.ID}
				fillShot(in, &s, run, st)
				shots = append(shots, s)
				if newest == nil || run.Started.After(newest.Started) {
					newest = run
				}
			}
		}
	}
	if len(shots) > 0 {
		shots[0].Large = true
	}

	var other []evOther
	for _, r := range latestRuns(in.Runs, "auto") {
		var ids []string
		for _, st := range r.Steps {
			if !shown[key("", st.ID)] && !shown[key(r.Session, st.ID)] {
				ids = append(ids, st.ID)
			}
		}
		if len(ids) > 0 {
			other = append(other, evOther{Session: r.Session, Steps: ids, StepsText: strings.Join(ids, ", ")})
		}
		// With shots, the run named is the newest one they come from.
		if len(shots) == 0 && (newest == nil || r.Started.After(newest.Started)) {
			newest = r
		}
	}
	runText := ""
	if newest != nil {
		kind := "ux-test"
		if newest.Mode == "judge" {
			kind = "playtest"
		}
		runText = "latest " + kind + " run, " + evWhen(newest.Started, in.Now)
		if b := buildSHA(newest.Build); b != "" {
			runText += " at " + b
		}
	}
	if len(shots) == 0 {
		by = ""
	}
	return shots, runText, by, other
}

func buildSHA(build string) string {
	b, _, _ := strings.Cut(build, "+")
	return b
}

func stepState(status string) (string, string) {
	switch status {
	case "pass":
		return "passed", "green"
	case "fail":
		return "failed", "red"
	case "skip", "skipped":
		return "skipped", "gray"
	}
	if status == "" {
		return "no result", "gray"
	}
	return status, "gray"
}

func judgeVerdict(st *timeline.PlaytestStep) string {
	v := st.Status
	if st.Judge != nil && st.Judge.Status != "" {
		v = st.Judge.Status
	}
	switch v {
	case "pass":
		return "pass"
	case "fail":
		return "fail"
	}
	return ""
}

var viewportRe = regexp.MustCompile(`^(\d+)x(\d+)$`)

func viewportWidth(v string) int {
	if m := viewportRe.FindStringSubmatch(v); m != nil {
		w, _ := strconv.Atoi(m[1])
		return w
	}
	return 0
}

// screenName is how the Playtest card names a viewport.
func screenName(v string) string {
	switch w := viewportWidth(v); {
	case w == 0:
		return v
	case w <= 480:
		return "phone"
	case w <= 1024:
		return "tablet"
	}
	return "desktop"
}

// shotViews lists a step's shots, widest first and on; a failed viewport is
// on instead, and its first failing check is the fail line.
func shotViews(in evidenceIn, run *timeline.PlaytestRun, st *timeline.PlaytestStep) ([]evView, string) {
	var views []evView
	fail := ""
	for _, vp := range st.Viewports {
		v := evView{Name: vp.Viewport, Label: strings.ReplaceAll(vp.Viewport, "x", "×"), Failed: vp.Status == "fail", Narrow: screenName(vp.Viewport) == "phone"}
		v.Src = shotURL(in, run.Dir, shotFile(vp.Screenshot, st.ID, vp.Viewport), in.Exists)
		views = append(views, v)
		if v.Failed && fail == "" {
			fail = failLine(vp)
		}
	}
	sort.SliceStable(views, func(i, j int) bool { return viewportWidth(views[i].Name) > viewportWidth(views[j].Name) })
	on := 0
	for i, v := range views {
		if v.Failed {
			on = i
			break
		}
	}
	if len(views) > 0 {
		views[on].On = true
	}
	return views, fail
}

// shotFile is the shot's base name: the one results.yaml recorded (its
// folder is ignored, runs move), else the runner's <step>-<viewport>.png.
func shotFile(recorded, step, viewport string) string {
	if recorded != "" {
		return path.Base(filepath.ToSlash(recorded))
	}
	return step + "-" + viewport + ".png"
}

// shotURL serves dir/shots/name, else dir/name (a game's runner saves its
// shots beside results.yaml), through the project's /files/ route; a
// reduced run's .png became a .jpg. Empty when the file is gone.
func shotURL(in evidenceIn, dir, name string, exists func(string) bool) string {
	candidates := []string{filepath.Join("shots", name)}
	if strings.HasSuffix(name, ".png") {
		candidates = append(candidates, filepath.Join("shots", strings.TrimSuffix(name, ".png")+".jpg"))
	}
	candidates = append(candidates, name)
	for _, n := range candidates {
		p := filepath.Join(dir, n)
		if !exists(p) {
			continue
		}
		rel, err := filepath.Rel(in.Root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		return in.Prefix + "/files/" + (&url.URL{Path: filepath.ToSlash(rel)}).EscapedPath()
	}
	return ""
}

func failLine(vp timeline.PlaytestViewport) string {
	for _, c := range vp.Checks {
		if !c.Pass {
			s := c.Check
			if c.Actual != "" {
				s += ": " + c.Actual
			}
			return truncate(s, 160)
		}
	}
	for _, l := range vp.Layout {
		return truncate(strings.TrimSpace(l.Rule+" "+l.ID)+": "+l.Detail, 160)
	}
	return truncate(vp.Error, 160)
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// evidenceSteps is the Playtest card: each step's latest machine result and
// your verdict (the latest judge run, else an approval: / playtest: comment
// naming the step).
func evidenceSteps(in evidenceIn) ([]evStep, string, []evWords) {
	var steps []evStep
	judged := ""
	var words []evWords
	seen := map[string]bool{}
	add := func(session string, st timeline.PlaytestStep, machine *timeline.PlaytestStep) {
		key := session + "|" + strings.ToUpper(st.ID)
		if seen[key] {
			return
		}
		seen[key] = true
		row := evStep{ID: st.ID, Session: session, Title: st.Title, You: "—", YouCls: "muted"}
		if machine != nil {
			row.Machine, row.MachineCls = stepState(machine.Status)
			var names []string
			for _, vp := range machine.Viewports {
				if n := screenName(vp.Viewport); !containsFold(names, n) {
					names = append(names, n)
				}
			}
			row.Screens = strings.Join(names, " · ")
			// A judgement step has no machine verdict: the runner leaves it
			// pending for you.
			if machine.Judgement && machine.Status != "pass" && machine.Status != "fail" {
				row.Machine, row.MachineCls = "judgement step", "muted"
			}
		} else {
			row.Machine, row.MachineCls = "not run", "amber"
		}
		if jr, jst := latestRunWith(in.Runs, "judge", session, st.ID); jst != nil {
			if v := judgeVerdict(jst); v != "" {
				row.You = v + ", " + evWhen(jr.Started, in.Now)
				row.YouCls = "ok"
				if v == "fail" {
					row.YouCls = "bad"
				}
				judged = "judged in the viewer"
			}
		}
		if c, ok := stepComment(in.Comments, st.ID); ok {
			at, _ := tracker.CommentTime(c.Date)
			prefix, text, _ := strings.Cut(c.Text, ":")
			words = append(words, evWords{Step: st.ID, At: evWhen(at, in.Now), Text: strings.TrimSpace(text), Source: strings.ToLower(strings.TrimSpace(prefix)) + ": comment"})
			if row.YouCls == "muted" {
				row.You, row.YouCls = "said in a comment", "ok"
				if judged == "" {
					judged = "judged in chat"
				}
			}
		}
		steps = append(steps, row)
	}
	for _, r := range latestRuns(in.Runs, "auto") {
		for i := range r.Steps {
			add(r.Session, r.Steps[i], &r.Steps[i])
		}
	}
	for _, r := range latestRuns(in.Runs, "judge") {
		for _, st := range r.Steps {
			add(r.Session, st, nil)
		}
	}
	return steps, judged, words
}

// stepComment is the newest approval: or playtest: comment naming step id.
func stepComment(comments []tracker.Comment, id string) (tracker.Comment, bool) {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(id) + `\b`)
	var best tracker.Comment
	var bestAt time.Time
	found := false
	for _, c := range comments {
		lower := strings.ToLower(strings.TrimSpace(c.Text))
		if !strings.HasPrefix(lower, "approval:") && !strings.HasPrefix(lower, "playtest:") {
			continue
		}
		if !re.MatchString(c.Text) {
			continue
		}
		at, _ := tracker.CommentTime(c.Date)
		if !found || !at.Before(bestAt) {
			best, bestAt, found = c, at, true
		}
	}
	return best, found
}

func evidenceVerdict(in evidenceIn, t *EvidenceTab) evVerdict {
	r := in.Report
	v := evVerdict{Status: in.Status, FromDispatch: in.Total}
	for i := len(in.Transitions) - 1; i >= 0; i-- {
		if in.Transitions[i].MergeSHA != "" {
			v.Merge = timeline.ShortSHA(in.Transitions[i].MergeSHA)
			v.MergeAt = evWhen(in.Transitions[i].TS, in.Now)
			break
		}
	}
	if v.Merge == "" && strings.HasPrefix(r.RefName, "work/") {
		v.Head = timeline.ShortSHA(r.Ref)
	}
	if !r.TestsAt.IsZero() {
		v.HasTests = true
		v.TestsAt = evWhen(r.TestsAt, in.Now)
		v.Tests = testsSummary(in.Comments)
		v.TestsAfter = r.LastChange.IsZero() || !r.TestsAt.Before(r.LastChange)
	}
	if r.LastChangeSHA != "" {
		v.LastChangeSHA = timeline.ShortSHA(r.LastChangeSHA)
		v.LastChangeAt = evWhen(r.LastChange, in.Now)
	}
	if in.WaitKnown && in.Waiting > 0 {
		v.Waiting = fmtSpan(in.Waiting)
	}
	if in.CI != nil && in.CI.State != "" {
		v.CI = "local CI " + in.CI.State + " after the merge"
		v.CIClass = map[string]string{"green": "ok", "red": "bad"}[in.CI.State]
	}
	var short []string
	for _, c := range t.Criteria {
		if c.Proven || t.Unlinked {
			continue
		}
		why := "no link"
		if len(c.Links) > 0 {
			why = c.Links[0].Kind + " " + c.Links[0].State
		}
		short = append(short, c.ID+" ("+why+")")
	}
	v.Short = strings.Join(short, ", ")
	return v
}

// testsSummary is the newest tests: comment's text, cut short.
func testsSummary(comments []tracker.Comment) string {
	var text string
	var last time.Time
	for _, c := range comments {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Text)), "tests:") {
			continue
		}
		at, _ := tracker.CommentTime(c.Date)
		if text == "" || !at.Before(last) {
			_, text, _ = strings.Cut(c.Text, ":")
			last = at
		}
	}
	return truncate(strings.TrimSpace(text), 180)
}

// evidenceWatch lists the risk: comments, newest last.
func evidenceWatch(comments []tracker.Comment, now time.Time) []evRisk {
	var out []evRisk
	for _, c := range comments {
		text := strings.TrimSpace(c.Text)
		if !strings.HasPrefix(strings.ToLower(text), "risk:") {
			continue
		}
		body := strings.TrimSpace(text[len("risk:"):])
		at, _ := tracker.CommentTime(c.Date)
		none := strings.EqualFold(strings.TrimRight(body, ". "), "none") || strings.HasPrefix(strings.ToLower(body), "none ")
		out = append(out, evRisk{At: evWhen(at, now), Text: body, None: none})
	}
	return out
}

// evWhen is a time as the tab shows it: the clock today, else the day and
// the clock this year, else the date.
func evWhen(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(time.Local)
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(time.Local)
	switch {
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("2 Jan 15:04")
	}
	return t.Format("2006-01-02")
}

// pastDesign: the issue's status comes after its path's design status (the
// first one named "… design"), or is a side status.
func pastDesign(wf *tracker.WorkflowConfig, status string) bool {
	design, cur := -1, -1
	i := 0
	for _, s := range wf.Statuses {
		if s.Optional {
			continue
		}
		if design < 0 && strings.Contains(strings.ToLower(s.Name), "design") {
			design = i
		}
		if strings.EqualFold(s.Name, status) {
			cur = i
		}
		i++
	}
	if cur < 0 {
		return true
	}
	if design < 0 {
		design = 0
	}
	return cur > design
}

// evidenceReport is the proof report the label and the tab read: the one
// issue-cli prove --list and the criteria_proven gate read.
func evidenceReport(proj *tracker.Project, issue *tracker.Issue, comments []tracker.Comment) proof.Report {
	root := projectRoot(proj)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return proof.Collect(root, issue, comments)
}

// loadEvidenceTab gathers the tab's sources: the proof report, the playtest
// runs in the checkout and the worktree, comments, the change and the
// Timeline's totals.
func loadEvidenceTab(proj *tracker.Project, prefix string, issue *tracker.Issue, wf *tracker.WorkflowConfig, report proof.Report) *EvidenceTab {
	root := projectRoot(proj)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	worktree := filepath.Join(root, ".worktrees", issue.Slug)
	stats, _ := tracker.LoadStats(issue.FilePath)
	src := loadTimelineSources(proj, issue, wf)
	tl := buildTimeline(src, timelineInput{Status: issue.Status, Now: time.Now(), WF: wf})
	// A run that can't be read is in report.Missing (proof.Collect).
	runs, _ := timeline.LoadPlaytestRuns(nil, issue.Slug, root, worktree)
	return buildEvidence(evidenceIn{
		Prefix:      prefix,
		Slug:        issue.Slug,
		Root:        root,
		Status:      issue.Status,
		Report:      report,
		Required:    wf.ProofRequired(issue),
		PastDesign:  pastDesign(wf, issue.Status),
		Runs:        runs,
		Comments:    src.Comments,
		Decisions:   src.Decisions,
		Body:        issue.BodyRaw,
		Transitions: stats.Transitions,
		Change:      loadEvidenceChange(root, report.Ref, report.RefName, stats),
		Total:       timelineTotalLabel(proj, issue),
		Waiting:     tl.Waiting,
		WaitKnown:   tl.Known,
		CI:          tl.CI,
		Now:         time.Now(),
	})
}
