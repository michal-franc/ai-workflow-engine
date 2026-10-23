package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Timeline tab (?tab=timeline on the detail page): where an issue's time
// went, split into agent working, waiting on you and idle, and its life by
// status with commits, decisions, comments, tests and CI inline. It replaces
// the Agent Timeline list; every call that list showed is under the CLI calls
// filter. The rules are in docs/workflow-stats.md, "Timeline tab".

// Timeline filters (&show=).
const (
	showAll     = "all"
	showYou     = "you"
	showCommits = "commits"
	showTests   = "tests"
	showCLI     = "cli"
)

var timelineFilters = []struct{ Key, Label string }{
	{showAll, "All"}, {showYou, "Your actions"}, {showCommits, "Commits"}, {showTests, "Tests and CI"}, {showCLI, "CLI calls"},
}

// Segment kinds on the life bar.
const (
	segAgent   = "agent"
	segWaiting = "waiting"
	segIdle    = "idle"
	segRework  = "rework"
)

// Timeline is the Timeline tab's view model.
type Timeline struct {
	Show     string
	InFlight bool
	// Known is false when no CLI call was found: agent, waiting and idle
	// time can't be split, only status times show.
	Known bool

	Start, End               time.Time
	Total                    time.Duration
	Working, Waiting, Idle   time.Duration
	Rework                   time.Duration
	Waits                    int
	IdleGaps                 int
	Loops                    []reworkLoop
	Commits, Merges, Calls   int
	Segments                 []barSegment
	StatusMarks              []barMark
	Ticks                    []barMark
	Bands                    []timelineBand
	Part                     []waitItem
	Words                    []wordItem
	WordCount, ApprovalCount int
	Open                     []openItem
	OpenCount                int
	CI                       *ciSummary
	Missing                  []missingSource
	SourceCount              int
	Read                     string
}

type barSegment struct {
	Kind string
	Pct  float64
	From time.Time
	To   time.Time
}

type barMark struct {
	Label string
	Pct   float64
	Width float64
}

type reworkLoop struct {
	From, To time.Time
	Label    string
	Agent    time.Duration
}

type timelineBand struct {
	Status   string
	Start    time.Time
	End      time.Time
	Open     bool
	Waiting  time.Duration
	Idle     time.Duration
	Rework   time.Duration
	Note     string
	Events   []tlEvent
	CallRows int
}

// tlEvent is one row in a band. Row is "event", "wait", "idle", "loop" or
// "fold"; Chip and Tone style the kind chip.
type tlEvent struct {
	At      time.Time
	Row     string
	Chip    string
	Tone    string
	Text    string
	Note    string
	Mono    string
	Detail  string
	Actions []TimelineAction
	Folded  []string
	InLoop  bool
	Muted   bool
	show    map[string]bool
}

type waitItem struct {
	Label string
	Dur   time.Duration
	Pct   float64
}

type wordItem struct {
	Ref   string
	How   string
	Quote string
	Muted bool
}

type openItem struct {
	Text string
	Chip string
	Tone string
}

type ciSummary struct {
	SHA      string
	State    string
	Command  string
	Duration time.Duration
	At       time.Time
	Since    int
	SinceRed int
}

// timelineInput is what buildTimeline needs besides the sources: kept apart
// so tests can build a timeline without a project on disk.
type timelineInput struct {
	Status string
	Now    time.Time
	Show   string
	// OpenGated: the current status's exit needs your approval (only for an
	// issue in flight).
	OpenGated bool
	WF        *tracker.WorkflowConfig
}

// buildTimeline turns the sources into the tab. It reads nothing from disk.
func buildTimeline(src timelineSources, in timelineInput) Timeline {
	tl := Timeline{Show: normalizeShow(in.Show), Missing: src.Missing, SourceCount: 5}
	tl.Read = strings.Join(src.Read, " · ")

	tl.Start = lifeStart(src)
	tl.End, tl.InFlight = lifeEnd(src, in)
	if tl.Start.IsZero() || !tl.End.After(tl.Start) {
		return tl
	}
	tl.Total = tl.End.Sub(tl.Start)
	tl.Known = len(src.Calls) > 0

	bands := lifeBands(src, in, tl.Start, tl.End)
	activity := activityTimes(src, tl.Start, tl.End)
	var segs []seg
	if tl.Known {
		segs = classifyGaps(activity, approvedTimes(src.Transitions), bands, tl.Start, tl.End)
		segs = overlayWaits(segs, tracker.OwnerWaits(tracker.StatsStore{Transitions: src.Transitions}, agentCallEnds(src.Calls)))
	}
	tl.Loops = reworkLoops(src, in.WF, tl.End)
	segs = markRework(segs, tl.Loops)
	for i := range tl.Loops {
		tl.Loops[i].Agent = kindWithin(segs, segRework, tl.Loops[i].From, tl.Loops[i].To)
	}
	segs = mergeSegs(segs)
	for _, s := range segs {
		d := s.To.Sub(s.From)
		switch s.Kind {
		case segAgent:
			tl.Working += d
		case segRework:
			tl.Working += d
			tl.Rework += d
		case segWaiting:
			tl.Waiting += d
			tl.Waits++
		case segIdle:
			tl.Idle += d
			tl.IdleGaps++
		}
	}
	tl.Segments = barSegments(segs, tl.Start, tl.Total)
	tl.StatusMarks = statusMarks(bands, tl.Start, tl.Total)
	tl.Ticks = timeTicks(tl.Start, tl.End)

	for _, c := range src.Commits {
		if c.Merge {
			tl.Merges++
		} else {
			tl.Commits++
		}
	}
	tl.Calls = len(src.Calls)

	events := timelineEvents(src, in, segs, tl.Loops, tl.End)
	tl.Bands = fillBands(bands, events, segs, tl.Show, tl.End, len(src.Calls) > 0)
	tl.Part = waitItems(segs, src.Transitions, bands)
	tl.Words, tl.WordCount, tl.ApprovalCount = wordItems(src)
	if in.Status == "done" {
		tl.Open, tl.OpenCount = openItems(src)
	}
	tl.CI = ciSummaryFrom(src)
	return tl
}

func normalizeShow(s string) string {
	for _, f := range timelineFilters {
		if f.Key == s {
			return s
		}
	}
	return showAll
}

// lifeEnd is the done transition's time, or now for an issue in flight.
func lifeEnd(src timelineSources, in timelineInput) (time.Time, bool) {
	if in.Status == "done" {
		for i := len(src.Transitions) - 1; i >= 0; i-- {
			if src.Transitions[i].To == "done" {
				return src.Transitions[i].TS, false
			}
		}
	}
	return in.Now, in.Status != "done"
}

// lifeBand is one status's span.
type lifeBand struct {
	Status string
	From   time.Time
	To     time.Time
	Gated  bool // its exit needs your approval
	Open   bool
}

func lifeBands(src timelineSources, in timelineInput, start, end time.Time) []lifeBand {
	rows := src.Transitions
	if len(rows) == 0 {
		return []lifeBand{{Status: in.Status, From: start, To: end, Gated: in.OpenGated, Open: in.Status != "done"}}
	}
	var bands []lifeBand
	from := start
	for _, r := range rows {
		if r.TS.After(end) {
			break
		}
		gated := r.ApprovedAt != nil || (in.WF != nil && in.WF.RequiredHumanApproval(r.From, r.To) != "")
		if r.TS.After(from) || len(bands) == 0 {
			bands = append(bands, lifeBand{Status: r.From, From: from, To: r.TS, Gated: gated})
		}
		from = r.TS
	}
	last := rows[len(rows)-1].To
	if last != "done" {
		bands = append(bands, lifeBand{Status: last, From: from, To: end, Gated: in.OpenGated, Open: true})
	}
	return bands
}

func bandAt(bands []lifeBand, t time.Time) *lifeBand {
	for i := range bands {
		if !t.Before(bands[i].From) && t.Before(bands[i].To) {
			return &bands[i]
		}
	}
	if len(bands) > 0 {
		return &bands[len(bands)-1]
	}
	return nil
}

// activityTimes are the moments the agent was active inside the life: each
// CLI call's end and each commit (D-5).
func activityTimes(src timelineSources, start, end time.Time) []time.Time {
	var ts []time.Time
	for _, c := range src.Calls {
		at := c.End()
		if !at.Before(start) && !at.After(end) {
			ts = append(ts, at)
		}
	}
	for _, c := range src.Commits {
		if !c.At.Before(start) && !c.At.After(end) {
			ts = append(ts, c.At)
		}
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	return ts
}

func approvedTimes(rows []statRow) []time.Time {
	var out []time.Time
	for _, r := range rows {
		if r.ApprovedAt != nil {
			out = append(out, *r.ApprovedAt)
		}
	}
	return out
}

// agentCallEnds are when the agent's calls ended, for tracker.OwnerWaits:
// the per-agent log's calls, or every call when no per-agent log was found.
func agentCallEnds(calls []cliCall) []time.Time {
	var ends, all []time.Time
	for _, c := range calls {
		all = append(all, c.End())
		if c.Agent {
			ends = append(ends, c.End())
		}
	}
	if len(ends) == 0 {
		return all
	}
	return ends
}

type seg struct {
	From, To time.Time
	Kind     string
}

// classifyGaps splits the life at each activity and at each approval, and
// classifies every gap: in a status only you can move on, a gap of 15 min or
// more is waiting on you; any other gap of 15 min or more is idle; the rest
// is agent working. The waits up to your approvals come on top, from
// overlayWaits; splitting at the approval keeps the time after it apart.
func classifyGaps(activity, approved []time.Time, bands []lifeBand, start, end time.Time) []seg {
	points := append([]time.Time{start}, activity...)
	for _, t := range approved {
		if t.After(start) && t.Before(end) {
			points = append(points, t)
		}
	}
	points = append(points, end)
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
	var segs []seg
	add := func(a, b time.Time, kind string) {
		if b.After(a) {
			segs = append(segs, seg{a, b, kind})
		}
	}
	for i := 0; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		if !b.After(a) {
			continue
		}
		switch {
		case b.Sub(a) >= agentIdleAfter:
			if band := bandAt(bands, a); band != nil && band.Gated {
				add(a, b, segWaiting)
			} else {
				add(a, b, segIdle)
			}
		default:
			add(a, b, segAgent)
		}
	}
	return segs
}

// overlayWaits marks each approval wait (tracker.OwnerWaits: from the
// agent's last call in the status to your approval, D-8 of the clilog issue)
// as waiting on you, whatever the gap rules made of that time.
func overlayWaits(segs []seg, waits []tracker.GateWait) []seg {
	for _, w := range waits {
		var out []seg
		for _, s := range segs {
			if !s.To.After(w.Since) || !w.ApprovedAt.After(s.From) {
				out = append(out, s)
				continue
			}
			lo, hi := maxTime(s.From, w.Since), minTime(s.To, w.ApprovedAt)
			if lo.After(s.From) {
				out = append(out, seg{s.From, lo, s.Kind})
			}
			out = append(out, seg{lo, hi, segWaiting})
			if s.To.After(hi) {
				out = append(out, seg{hi, s.To, s.Kind})
			}
		}
		segs = out
	}
	return segs
}

// markRework turns the agent time inside a rework loop purple.
func markRework(segs []seg, loops []reworkLoop) []seg {
	for _, l := range loops {
		var out []seg
		for _, s := range segs {
			if s.Kind != segAgent || !s.To.After(l.From) || !l.To.After(s.From) {
				out = append(out, s)
				continue
			}
			lo, hi := maxTime(s.From, l.From), minTime(s.To, l.To)
			if lo.After(s.From) {
				out = append(out, seg{s.From, lo, segAgent})
			}
			out = append(out, seg{lo, hi, segRework})
			if s.To.After(hi) {
				out = append(out, seg{hi, s.To, segAgent})
			}
		}
		segs = out
	}
	return segs
}

func mergeSegs(segs []seg) []seg {
	var out []seg
	for _, s := range segs {
		if n := len(out); n > 0 && out[n-1].Kind == s.Kind && out[n-1].To.Equal(s.From) {
			out[n-1].To = s.To
			continue
		}
		out = append(out, s)
	}
	return out
}

func kindWithin(segs []seg, kind string, from, to time.Time) time.Duration {
	var d time.Duration
	for _, s := range segs {
		if s.Kind != kind {
			continue
		}
		lo, hi := maxTime(s.From, from), minTime(s.To, to)
		if hi.After(lo) {
			d += hi.Sub(lo)
		}
	}
	return d
}

// reworkLoops finds the loops (D-4): a backward transition, until the issue
// is back in the status it left; and a failed playtest run from testing on,
// until the next run in the same mode where the failed steps pass. Failed
// runs before testing are the agent iterating, not rework.
func reworkLoops(src timelineSources, wf *tracker.WorkflowConfig, end time.Time) []reworkLoop {
	var loops []reworkLoop
	if wf != nil {
		rows := src.Transitions
		for i, r := range rows {
			fi, ti := wf.GetStatusIndex(r.From), wf.GetStatusIndex(r.To)
			if fi < 0 || ti < 0 || ti >= fi {
				continue
			}
			to := end
			for _, later := range rows[i+1:] {
				if later.To == r.From {
					to = later.TS
					break
				}
			}
			loops = append(loops, reworkLoop{From: r.TS, To: to, Label: "back to " + r.To + " from " + r.From})
		}
	}
	testingFrom := testingStart(src.Transitions, wf)
	if !testingFrom.IsZero() {
		for i, run := range src.Runs {
			failed := run.failed()
			if len(failed) == 0 || run.Started.Before(testingFrom) || run.Started.After(end) {
				continue
			}
			to := end
			for _, later := range src.Runs[i+1:] {
				if later.Mode != run.Mode {
					continue
				}
				all := true
				for _, id := range failed {
					if !later.passed(id) {
						all = false
					}
				}
				if all {
					to = later.Started
					break
				}
			}
			who := "ux-test"
			if run.Mode == "judge" {
				who = "your playtest"
			}
			loops = append(loops, reworkLoop{From: run.Started, To: to, Label: who + " failed " + strings.Join(failed, ", ")})
		}
	}
	sort.SliceStable(loops, func(i, j int) bool { return loops[i].From.Before(loops[j].From) })
	// Overlapping windows are one loop.
	var out []reworkLoop
	for _, l := range loops {
		if n := len(out); n > 0 && !l.From.After(out[n-1].To) {
			if l.To.After(out[n-1].To) {
				out[n-1].To = l.To
			}
			continue
		}
		out = append(out, l)
	}
	return out
}

// testingStart is when the issue first reached testing or a later status.
func testingStart(rows []statRow, wf *tracker.WorkflowConfig) time.Time {
	if wf == nil {
		return time.Time{}
	}
	ti := wf.GetStatusIndex("testing")
	if ti < 0 {
		return time.Time{}
	}
	for _, r := range rows {
		if idx := wf.GetStatusIndex(r.To); idx >= ti && r.To != "parked" {
			return r.TS
		}
	}
	return time.Time{}
}

func barSegments(segs []seg, start time.Time, total time.Duration) []barSegment {
	if total <= 0 {
		return nil
	}
	var out []barSegment
	for _, s := range segs {
		out = append(out, barSegment{Kind: s.Kind, Pct: durPct(s.To.Sub(s.From), total), From: s.From, To: s.To})
	}
	return out
}

func statusMarks(bands []lifeBand, start time.Time, total time.Duration) []barMark {
	if total <= 0 {
		return nil
	}
	var out []barMark
	for _, b := range bands {
		out = append(out, barMark{Label: b.Status, Pct: durPct(b.From.Sub(start), total), Width: durPct(b.To.Sub(b.From), total)})
	}
	return out
}

// timeTicks puts at most eight round clock times under the bar.
func timeTicks(start, end time.Time) []barMark {
	total := end.Sub(start)
	steps := []time.Duration{5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}
	step := steps[len(steps)-1]
	for _, s := range steps {
		if total/s <= 7 {
			step = s
			break
		}
	}
	format := "15:04"
	if total > 36*time.Hour {
		format = "Mon 15:04"
	}
	ticks := []barMark{{Label: start.Local().Format(format), Pct: 0}}
	for t := start.Local().Truncate(step).Add(step); t.Before(end); t = t.Add(step) {
		p := durPct(t.Sub(start), total)
		if p < 7 || p > 93 {
			continue
		}
		ticks = append(ticks, barMark{Label: t.Format(format), Pct: p})
	}
	return append(ticks, barMark{Label: end.Local().Format(format), Pct: 100})
}

func durPct(d, total time.Duration) float64 {
	if total <= 0 {
		return 0
	}
	return float64(d) / float64(total) * 100
}

// Read-only issue-cli commands fold into one line; they change nothing.
var readCommands = map[string]bool{
	"show": true, "checklist": true, "context": true, "process": true, "next": true, "list": true,
	"search": true, "stats": true, "decisions": true, "whoami": true, "lead": true, "start": true,
	"help": true, "version": true, "ci": true, "telemetry": true, "data": true,
}

func isReadCall(c cliCall) bool {
	cmd := c.cmd()
	if strings.HasPrefix(cmd, "-") {
		return true // a bare --help or --version
	}
	if cmd == "transition" && hasArg(c.Args, "--dry-run") {
		return c.Exit == nil || *c.Exit == 0
	}
	return readCommands[cmd]
}

func refusedCall(c cliCall) bool {
	return c.Exit != nil && *c.Exit != 0
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

// timelineEvents builds every row the bands can show, tagged with the
// filters that show it.
func timelineEvents(src timelineSources, in timelineInput, segs []seg, loops []reworkLoop, end time.Time) []tlEvent {
	var evs []tlEvent
	tag := func(e tlEvent, filters ...string) {
		e.show = map[string]bool{}
		for _, f := range filters {
			e.show[f] = true
		}
		for _, l := range loops {
			if !e.At.Before(l.From) && !e.At.After(l.To) && e.Row == "event" {
				e.InLoop = true
			}
		}
		evs = append(evs, e)
	}
	start := lifeStart(src)

	tag(tlEvent{At: start, Row: "event", Chip: "dispatch", Tone: "gray", Text: "Agent started", Detail: src.Prompt}, showAll, showCLI)

	// Each call as the old Agent Timeline showed it: comment text the clilog
	// redacted is filled from the issue's comments, and transitions carry
	// the workflow actions they ran.
	tes := make([]TimelineEvent, len(src.Calls))
	for i := range src.Calls {
		tes[i] = timelineEventFromRecord(src.Calls[i].record())
	}
	tes = FillRedactedComments(tes, src.Comments)
	tes = EnrichTimelineWithWorkflow(tes, in.WF, "")

	// Agent's work, grouped: same-kind writes in a row become one line.
	var group []int
	flush := func() {
		if len(group) == 0 {
			return
		}
		calls := make([]cliCall, len(group))
		for k, i := range group {
			calls[k] = src.Calls[i]
		}
		if e, ok := groupEvent(calls, tes[group[0]], src); ok {
			tag(e, eventFilters(calls[0], tes[group[0]])...)
		}
		group = nil
	}
	var before []cliCall
	for i, c := range src.Calls {
		if c.At.Before(start) {
			before = append(before, c)
			continue
		}
		if c.At.After(end) || isReadCall(c) {
			flush()
			continue
		}
		if len(group) > 0 {
			first, last := src.Calls[group[0]], src.Calls[group[len(group)-1]]
			if groupKind(first) != groupKind(c) || c.At.Sub(last.At) > 2*time.Minute || groupKind(c) == "" {
				flush()
			}
		}
		group = append(group, i)
	}
	flush()

	if len(before) > 0 {
		tag(tlEvent{At: before[0].At, Row: "fold", Chip: "folded", Tone: "gray", Muted: true,
			Text:   fmt.Sprintf("%s on the issue before the agent started (%s)", plural(len(before), "call", "calls"), callNames(before)),
			Folded: callLines(before)}, showAll)
	}

	// After done: every call folds into one line.
	var after []cliCall
	for _, c := range src.Calls {
		if c.At.After(end) {
			after = append(after, c)
		}
	}
	if len(after) > 0 {
		tag(tlEvent{At: after[0].At, Row: "fold", Chip: "folded", Tone: "gray", Muted: true,
			Text:   fmt.Sprintf("%s (%s) until %s", plural(len(after), "call", "calls"), callNames(after), clock(after[len(after)-1].At, end)),
			Folded: callLines(after)}, showAll)
	}

	for _, r := range src.Transitions {
		if r.ApprovedAt != nil {
			tag(tlEvent{At: *r.ApprovedAt, Row: "event", Chip: "you", Tone: "amber", Text: "Approved → " + r.To, Note: "no words recorded"}, showAll, showYou)
		}
		if r.To == "done" && !r.TS.After(end) {
			tag(tlEvent{At: r.TS, Row: "event", Chip: "done", Tone: "green", Text: r.From + " → done"}, showAll)
		}
	}

	for _, c := range src.Commits {
		if c.Merge {
			tag(tlEvent{At: c.At, Row: "event", Chip: "merge", Tone: "gray", Mono: shortSHA(c.SHA), Text: "main merged into the branch", Muted: true}, showAll, showCommits)
			continue
		}
		tag(tlEvent{At: c.At, Row: "event", Chip: "commit", Tone: "purple", Mono: shortSHA(c.SHA), Text: c.Subject}, showAll, showCommits)
	}

	for i, r := range src.Runs {
		if r.Started.After(end) {
			continue
		}
		failed := r.failed()
		if r.Mode == "judge" {
			tone, text := "green", "Your playtest: "+stepSummary(r)
			if len(failed) > 0 {
				tone = "red"
			}
			tag(tlEvent{At: r.Started, Row: "event", Chip: "playtest", Tone: tone, Text: text}, showAll, showYou, showTests)
			continue
		}
		text := fmt.Sprintf("ux-test %s: all %d steps pass", r.Session, len(r.Steps))
		filters := []string{showTests}
		if len(failed) > 0 {
			text = fmt.Sprintf("ux-test %s: %d of %d steps failed", r.Session, len(failed), len(r.Steps))
			if next := nextPassingRun(src.Runs[i+1:], r); next != nil {
				text += ", all pass at " + next.Started.Local().Format("15:04")
			}
			filters = append(filters, showAll)
		}
		tone := "green"
		if len(failed) > 0 {
			tone = "gray"
		}
		tag(tlEvent{At: r.Started, Row: "event", Chip: "ux-test", Tone: tone, Text: text}, filters...)
	}

	if len(src.CI) > 0 {
		r := src.CI[0]
		tone := "green"
		if r.State != "green" {
			tone = "red"
		}
		tag(tlEvent{At: r.Started, Row: "event", Chip: "CI", Tone: tone, Mono: shortSHA(r.SHA),
			Text: fmt.Sprintf("Local CI on main: %s, %s, %s", r.State, strings.Join(r.Commands, ", "), fmtDur(r.Duration))}, showAll, showTests)
	}

	for _, s := range segs {
		switch s.Kind {
		case segWaiting:
			tag(tlEvent{At: s.From, Row: "wait", Text: "Waiting on you · " + fmtDur(s.To.Sub(s.From)), Note: waitLabel(s, src.Transitions)}, showAll, showYou, showCLI)
		case segIdle:
			tag(tlEvent{At: s.From, Row: "idle", Text: "Idle · " + fmtDur(s.To.Sub(s.From)), Note: "no CLI call or commit"}, showAll, showCLI)
		}
	}
	for _, l := range loops {
		note := "until " + clock(l.To, end)
		if len(src.Calls) > 0 {
			note = fmtDur(l.Agent) + " of agent time, " + note
		}
		tag(tlEvent{At: l.From, Row: "loop", Text: l.Label, Note: note}, showAll, showCLI)
	}

	// CLI calls: the old Agent Timeline's rows.
	var reads []cliCall
	var readIdx []int
	flushReads := func() {
		switch len(reads) {
		case 0:
		case 1:
			c := reads[0]
			tag(tlEvent{At: c.At, Row: "event", Chip: c.cmd(), Tone: "gray", Text: cliSummary(tes[readIdx[0]], c, src), Muted: true, Mono: c.At.Local().Format("15:04:05")}, showCLI)
		default:
			tag(tlEvent{At: reads[0].At, Row: "fold", Chip: "folded", Tone: "gray", Muted: true, Mono: reads[0].At.Local().Format("15:04:05"),
				Text: fmt.Sprintf("%d reads: %s", len(reads), callNames(reads)), Folded: callLines(reads)}, showCLI)
		}
		reads, readIdx = nil, nil
	}
	for i, c := range src.Calls {
		if isReadCall(c) {
			reads = append(reads, c)
			readIdx = append(readIdx, i)
			continue
		}
		flushReads()
		te := tes[i]
		e := tlEvent{At: c.At, Row: "event", Chip: te.Kind, Tone: "gray", Text: cliSummary(te, c, src), Detail: resolveText(te.Detail, src), Actions: te.Actions, Mono: c.At.Local().Format("15:04:05")}
		if len(e.Text) > 160 {
			// update --body, decide --text …: the line stays short, the
			// call opens in full.
			if e.Detail == "" {
				e.Detail = resolveText(strings.Join(c.Args, " "), src)
			}
			e.Text = truncRunes(e.Text, 157) + "..."
		}
		switch {
		case refusedCall(c):
			e.Chip, e.Tone = "refused", "red"
		case te.Kind == "transition":
			e.Tone = "blue"
		case te.Kind == "decide":
			e.Tone = "green"
		}
		tag(e, showCLI)
	}
	flushReads()
	// At the same second a loop opens before its events, and a wait or an
	// idle gap starts after the calls that led into it.
	rank := map[string]int{"loop": 0, "event": 1, "fold": 1, "wait": 2, "idle": 2}
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].At.Equal(evs[j].At) {
			return rank[evs[i].Row] < rank[evs[j].Row]
		}
		return evs[i].At.Before(evs[j].At)
	})
	return evs
}

func nextPassingRun(later []playtestRun, r playtestRun) *playtestRun {
	for i := range later {
		if later[i].Mode == r.Mode && later[i].Session == r.Session && len(later[i].failed()) == 0 {
			return &later[i]
		}
	}
	return nil
}

func stepSummary(r playtestRun) string {
	var parts []string
	for _, s := range r.Steps {
		parts = append(parts, s.ID+" "+s.Status)
	}
	return strings.Join(parts, ", ")
}

func cliSummary(te TimelineEvent, c cliCall, src timelineSources) string {
	s := resolveText(te.Summary, src)
	if c.cmd() == "check" && len(c.Args) > 2 {
		s = "check: " + strings.Join(c.Args[2:], " ")
	}
	if te.FromStatus != "" {
		s += " (" + te.FromStatus + " → " + te.ToStatus + ")"
	}
	if hasArg(c.Args, "--dry-run") {
		s += " --dry-run"
	}
	return s
}

func callNames(calls []cliCall) string {
	count := map[string]int{}
	var order []string
	for _, c := range calls {
		n := c.cmd()
		if count[n] == 0 {
			order = append(order, n)
		}
		count[n]++
	}
	var parts []string
	for _, n := range order {
		if count[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", n, count[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}

func callLines(calls []cliCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.At.Local().Format("15:04:05") + "  " + strings.Join(c.Args, " ")
	}
	return out
}

// groupKind is how writes group into one line; "" never groups.
func groupKind(c cliCall) string {
	switch c.cmd() {
	case "decide":
		return "decide"
	case "append", "replace":
		return "section"
	case "check":
		return "check"
	}
	return ""
}

func eventFilters(c cliCall, te TimelineEvent) []string {
	if c.cmd() == "comment" {
		prefix := commentPrefix(callText(c))
		if clilog.IsPlaceholder(callText(c)) {
			prefix = strings.TrimPrefix(te.Kind, "comment-")
		}
		switch prefix {
		case "tests", "playtest", "ci":
			return []string{showAll, showTests}
		}
	}
	if c.cmd() == "decide" {
		return []string{showAll, showYou}
	}
	return []string{showAll}
}

// groupEvent is the All-filter line for a run of same-kind writes.
func groupEvent(group []cliCall, te TimelineEvent, src timelineSources) (tlEvent, bool) {
	c := group[0]
	e := tlEvent{At: c.At, Row: "event", Tone: "gray"}
	switch c.cmd() {
	case "decide":
		e.Chip, e.Tone = "decide", "green"
		var parts []string
		for _, g := range group {
			parts = append(parts, decisionLabel(g, src.Decisions))
		}
		e.Text = plural(len(group), "decision", "decisions") + ": " + strings.Join(parts, " · ")
	case "append", "replace":
		e.Chip = "section"
		var names []string
		seen := map[string]bool{}
		for _, g := range group {
			n := flagValue(g.Args, "--section")
			if n == "" {
				n = "body"
			}
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		e.Text = joinAnd(names) + " written"
	case "check":
		e.Chip = "check"
		var ids []string
		for _, g := range group {
			if len(g.Args) > 2 {
				ids = append(ids, g.Args[2:]...)
			}
		}
		e.Text = plural(len(ids), "box", "boxes") + " ticked: " + strings.Join(ids, " ")
		if len(ids) == 1 {
			e.Text = "1 box ticked: " + ids[0]
		}
	case "comment":
		text := commentText(c, te, src)
		e.Chip = "comment"
		if p := commentPrefix(text); p != "" {
			e.Chip = p
		}
		if e.Chip == "tests" {
			e.Tone = "green"
		}
		e.Text = firstLine(text)
		if len(text) > len(e.Text) {
			e.Detail = text
		}
	case "transition":
		if !refusedCall(c) {
			return e, false
		}
		e.Chip, e.Tone = "refused", "red"
		e.Text = "transition → " + flagValue(c.Args, "--to") + " refused"
		if c.ErrClass != "" {
			e.Text += ": " + strings.ReplaceAll(c.ErrClass, "_", " ")
		}
		if hasArg(c.Args, "--dry-run") {
			e.Text += " (dry run)"
		}
	case "retrospective":
		e.Chip, e.Text = "retro", "retrospective saved"
	default:
		e.Chip = c.cmd()
		e.Text = resolveText(strings.Join(c.Args, " "), src)
		if len(e.Text) > 140 {
			e.Text = truncRunes(e.Text, 137) + "..."
		}
	}
	return e, true
}

// commentText is a comment call's text: what FillRedactedComments put in
// the event when the clilog redacted it, else the call's own text.
func commentText(c cliCall, te TimelineEvent, src timelineSources) string {
	text := callText(c)
	if clilog.IsPlaceholder(text) {
		if te.Detail != "" {
			return te.Detail
		}
		if te.Summary != "comment" {
			return te.Summary
		}
		return resolveText(text, src)
	}
	return text
}

// callText is a comment call's text: --text, else the positional after the
// slug.
func callText(c cliCall) string {
	if t := flagValue(c.Args, "--text"); t != "" {
		return t
	}
	if len(c.Args) > 2 && !strings.HasPrefix(c.Args[2], "--") {
		return c.Args[2]
	}
	return ""
}

func commentPrefix(text string) string {
	if i := strings.Index(text, ":"); i > 0 && i < 20 {
		p := strings.ToLower(strings.TrimSpace(text[:i]))
		if isSimpleWord(p) {
			return p
		}
	}
	return ""
}

// decisionLabel names the D-n a decide call recorded. The decision line
// drops a trailing period from --text, so a redacted call is also matched by
// its --quote.
func decisionLabel(c cliCall, decisions []tracker.Decision) string {
	text, quote := flagValue(c.Args, "--text"), flagValue(c.Args, "--quote")
	norm := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), ".") }
	for _, d := range decisions {
		q := strings.Trim(d.Quote, `"`)
		redacted := clilog.IsPlaceholder(text) && (text == clilog.Placeholder(d.Text) || text == clilog.Placeholder(d.Text+".") ||
			(q != "" && quote == clilog.Placeholder(q)))
		if text != "" && (norm(d.Text) == norm(text) || redacted) {
			if d.Quote != "" {
				return d.ID + ` "` + strings.Trim(d.Quote, `"`) + `"`
			}
			return d.ID
		}
	}
	return "decision recorded"
}

// resolveText swaps each redacted value (clilog.Placeholder, cli/clilog-…
// D-6) for the comment or decision text from the issue it was made from. A
// value with no match stays a placeholder.
func resolveText(s string, src timelineSources) string {
	if !strings.Contains(s, "‹text") {
		return s
	}
	for _, c := range src.Comments {
		s = strings.ReplaceAll(s, clilog.Placeholder(c.Text), c.Text)
	}
	for _, d := range src.Decisions {
		s = strings.ReplaceAll(s, clilog.Placeholder(d.Text), d.Text)
	}
	return s
}

// waitLabel names what a wait was for: the approval that ended it, or the
// status it sat in.
func waitLabel(s seg, rows []statRow) string {
	for _, r := range rows {
		if r.ApprovedAt != nil && r.ApprovedAt.Equal(s.To) {
			return "approval to leave " + r.From
		}
	}
	for i, r := range rows {
		if !r.TS.After(s.From) && (i+1 == len(rows) || rows[i+1].TS.After(s.From)) {
			return "in " + r.To + ", no agent call for 15 min or more"
		}
	}
	return ""
}

// fillBands puts each event in the status band it happened in, after the
// filter, and sums each band's waiting, idle and rework time.
func fillBands(bands []lifeBand, events []tlEvent, segs []seg, show string, end time.Time, known bool) []timelineBand {
	out := make([]timelineBand, 0, len(bands)+1)
	for _, b := range bands {
		if !b.To.After(b.From) && !b.Open && len(bands) > 1 {
			continue // e.g. the first status when nothing marks the start
		}
		tb := timelineBand{Status: b.Status, Start: b.From, End: b.To, Open: b.Open}
		if known {
			tb.Waiting = kindWithin(segs, segWaiting, b.From, b.To)
			tb.Idle = kindWithin(segs, segIdle, b.From, b.To)
			tb.Rework = kindWithin(segs, segRework, b.From, b.To)
		}
		out = append(out, tb)
	}
	after := timelineBand{Status: "after done", Start: end}
	before := timelineBand{Status: "before dispatch"}
	for _, e := range events {
		if !e.show[show] {
			continue
		}
		if len(bands) > 0 && e.At.Before(bands[0].From) {
			before.Events = append(before.Events, e)
			continue
		}
		if e.At.After(end) {
			after.Events = append(after.Events, e)
			continue
		}
		i := len(out) - 1
		for j, b := range out {
			if !e.At.Before(b.Start) && e.At.Before(b.End) {
				i = j
				break
			}
		}
		if i < 0 {
			continue
		}
		out[i].Events = append(out[i].Events, e)
	}
	for i := range out {
		for _, e := range out[i].Events {
			if e.Row == "event" || e.Row == "fold" {
				out[i].CallRows++
			}
		}
	}
	if len(before.Events) > 0 {
		before.Start = before.Events[0].At // no span: it isn't the agent's time
		out = append([]timelineBand{before}, out...)
	}
	if len(after.Events) > 0 {
		out = append(out, after)
	}
	return out
}

// waitItems is "Your part": each wait with what it was for.
func waitItems(segs []seg, rows []statRow, bands []lifeBand) []waitItem {
	var items []waitItem
	var max time.Duration
	for _, s := range segs {
		if s.Kind != segWaiting {
			continue
		}
		d := s.To.Sub(s.From)
		label := waitLabel(s, rows)
		if strings.HasPrefix(label, "approval to leave ") {
			label = "Approval to leave " + strings.TrimPrefix(label, "approval to leave ")
		} else if b := bandAt(bands, s.From); b != nil {
			label = "Your part in " + b.Status
		}
		items = append(items, waitItem{Label: label, Dur: d})
		if d > max {
			max = d
		}
	}
	for i := range items {
		items[i].Pct = durPct(items[i].Dur, max)
	}
	return items
}

// wordItems is "Your words": decisions with your quote, then each approval
// with its words, or "no words recorded".
func wordItems(src timelineSources) (items []wordItem, words, approvalsN int) {
	for _, d := range src.Decisions {
		if d.Quote == "" {
			continue
		}
		items = append(items, wordItem{Ref: d.ID, How: d.How, Quote: d.Quote})
		words++
	}
	for _, r := range src.Transitions {
		if r.ApprovedAt == nil {
			continue
		}
		approvalsN++
		items = append(items, wordItem{Ref: r.ApprovedAt.Local().Format("15:04"), How: "approve → " + r.To, Quote: "no words recorded", Muted: true})
	}
	return items, words, approvalsN
}

// openItems is "Left open at done": unticked boxes, a section with many
// shown as a count, and failed playtest steps marked.
func openItems(src timelineSources) ([]openItem, int) {
	failed := map[string]bool{}
	latest := map[string]string{}
	for _, r := range src.Runs {
		for _, s := range r.Steps {
			latest[s.ID] = s.Status
		}
	}
	for id, st := range latest {
		if st == "fail" {
			failed[id] = true
		}
	}
	type sec struct {
		name          string
		total, ticked int
		open          []tracker.CheckboxItem
	}
	var secs []*sec
	idx := map[string]*sec{}
	total := 0
	for _, b := range src.Boxes {
		s := idx[b.Section]
		if s == nil {
			s = &sec{name: b.Section}
			idx[b.Section] = s
			secs = append(secs, s)
		}
		s.total++
		if b.Checked {
			s.ticked++
		} else {
			s.open = append(s.open, b)
			total++
		}
	}
	var items []openItem
	for _, s := range secs {
		if len(s.open) == 0 {
			continue
		}
		if len(s.open) > 3 {
			items = append(items, openItem{Text: s.name, Chip: fmt.Sprintf("%d / %d ticked", s.ticked, s.total), Tone: "amber"})
			continue
		}
		for _, b := range s.open {
			it := openItem{Text: strings.TrimSpace(b.ID + " " + firstLine(b.Label())), Chip: "unticked", Tone: "gray"}
			if failed[b.ID] {
				it.Chip, it.Tone = "failed", "red"
			}
			items = append(items, it)
		}
	}
	return items, total
}

func ciSummaryFrom(src timelineSources) *ciSummary {
	if len(src.CI) == 0 {
		return nil
	}
	r := src.CI[0]
	s := &ciSummary{SHA: shortSHA(r.SHA), State: r.State, Command: strings.Join(r.Commands, ", "), Duration: r.Duration, At: r.Started, Since: len(src.CI) - 1}
	for _, later := range src.CI[1:] {
		if later.State != "green" {
			s.SinceRed++
		}
	}
	return s
}

// clock is a time as HH:MM, with the weekday when it's on another day than ref.
func clock(t, ref time.Time) string {
	t, ref = t.Local(), ref.Local()
	if t.YearDay() != ref.YearDay() || t.Year() != ref.Year() {
		return t.Format("15:04 Mon")
	}
	return t.Format("15:04")
}

// fmtDur: "13s", "45m 48s", "3h 48m", "2d 4h".
func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		m, s := int(d/time.Minute), int(d%time.Minute/time.Second)
		if s == 0 {
			return strconv.Itoa(m) + "m"
		}
		return fmt.Sprintf("%dm %ds", m, s)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	default:
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	}
}

// fmtSpan is a band's length, rounded: "13s", "50m", "3h 14m".
func fmtSpan(d time.Duration) string {
	if d >= time.Minute && d < time.Hour {
		return strconv.Itoa(int(d.Round(time.Minute)/time.Minute)) + "m"
	}
	return fmtDur(d)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// truncRunes cuts s to at most n bytes without splitting a UTF-8 rune.
func truncRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
