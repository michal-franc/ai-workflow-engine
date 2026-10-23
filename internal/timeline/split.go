package timeline

import (
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// In is what Split needs besides the sources.
type In struct {
	Status string
	Now    time.Time
	// OpenGated: the current status's exit needs the owner's approval (only
	// for an issue in flight).
	OpenGated bool
	WF        *tracker.WorkflowConfig
}

// Band is one status's span. Working (rework included), Waiting, Idle and
// Rework are its share of the split, zero when the split is not Known.
type Band struct {
	Status string
	From   time.Time
	To     time.Time
	Gated  bool // its exit needs the owner's approval
	Open   bool

	Working, Waiting, Idle, Rework time.Duration
}

// Seg is one stretch of the life, of one kind.
type Seg struct {
	From, To time.Time
	Kind     string
}

// Loop is a rework loop (D-4 of the Timeline tab issue); Agent is the
// agent's time inside it.
type Loop struct {
	From, To time.Time
	Label    string
	Agent    time.Duration
}

// Result is one issue's life, split.
type Result struct {
	Start, End time.Time
	InFlight   bool
	// Known is false when no CLI call was found: agent, waiting and idle
	// time can't be split, only status times are known.
	Known bool

	Bands []Band
	Segs  []Seg
	Loops []Loop
	Waits []tracker.GateWait

	// Working includes Rework.
	Working, Waiting, Idle, Rework time.Duration
	WaitCount, IdleGaps            int
}

// Total is the life from Start to End, zero when there is none.
func (r Result) Total() time.Duration {
	if r.Start.IsZero() || !r.End.After(r.Start) {
		return 0
	}
	return r.End.Sub(r.Start)
}

// Split turns the sources into the issue's life by status, split into agent
// working, waiting on the owner, idle and rework. It reads nothing from disk.
func Split(src Sources, in In) Result {
	var r Result
	r.Start = LifeStart(src)
	r.End, r.InFlight = LifeEnd(src, in)
	if r.Total() == 0 {
		return r
	}
	r.Known = len(src.Calls) > 0

	bands := lifeBands(src, in, r.Start, r.End)
	var segs []Seg
	if r.Known {
		segs = ClassifyGaps(activityTimes(src, r.Start, r.End), approvedTimes(src.Transitions), bands, r.Start, r.End)
		r.Waits = tracker.OwnerWaits(tracker.StatsStore{Transitions: src.Transitions}, agentCallEnds(src.Calls))
		segs = OverlayWaits(segs, r.Waits)
	}
	r.Loops = ReworkLoops(src, in.WF, r.End)
	segs = MarkRework(segs, r.Loops)
	for i := range r.Loops {
		r.Loops[i].Agent = KindWithin(segs, KindRework, r.Loops[i].From, r.Loops[i].To)
	}
	r.Segs = MergeSegs(segs)
	for _, s := range r.Segs {
		d := s.To.Sub(s.From)
		switch s.Kind {
		case KindAgent:
			r.Working += d
		case KindRework:
			r.Working += d
			r.Rework += d
		case KindWaiting:
			r.Waiting += d
			r.WaitCount++
		case KindIdle:
			r.Idle += d
			r.IdleGaps++
		}
	}
	if r.Known {
		for i := range bands {
			b := &bands[i]
			b.Rework = KindWithin(r.Segs, KindRework, b.From, b.To)
			b.Working = KindWithin(r.Segs, KindAgent, b.From, b.To) + b.Rework
			b.Waiting = KindWithin(r.Segs, KindWaiting, b.From, b.To)
			b.Idle = KindWithin(r.Segs, KindIdle, b.From, b.To)
		}
	}
	r.Bands = bands
	return r
}

// LifeStart is when the issue's agent life began: the first dispatch, else
// the agent's first call, else the first call, else the first transition.
// Calls on the issue before it (the lead writing it up) are not the agent's.
func LifeStart(src Sources) time.Time {
	if len(src.Dispatches) > 0 {
		return src.Dispatches[0]
	}
	for _, c := range src.Calls {
		if c.Agent {
			return c.At
		}
	}
	if len(src.Calls) > 0 {
		return src.Calls[0].At
	}
	if len(src.Transitions) > 0 {
		return src.Transitions[0].TS
	}
	return time.Time{}
}

// LifeEnd is the last done transition's time, or now for an issue in flight.
func LifeEnd(src Sources, in In) (time.Time, bool) {
	if in.Status == "done" {
		if t, ok := DoneAt(src.Transitions); ok {
			return t, false
		}
	}
	return in.Now, in.Status != "done"
}

// DoneAt is the time of the last transition to done.
func DoneAt(rows []tracker.TransitionStat) (time.Time, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].To == "done" {
			return rows[i].TS, true
		}
	}
	return time.Time{}, false
}

func lifeBands(src Sources, in In, start, end time.Time) []Band {
	rows := src.Transitions
	if len(rows) == 0 {
		return []Band{{Status: in.Status, From: start, To: end, Gated: in.OpenGated, Open: in.Status != "done"}}
	}
	var bands []Band
	from := start
	for _, r := range rows {
		if r.TS.After(end) {
			break
		}
		gated := r.ApprovedAt != nil || (in.WF != nil && in.WF.RequiredHumanApproval(r.From, r.To) != "")
		if r.TS.After(from) || len(bands) == 0 {
			bands = append(bands, Band{Status: r.From, From: from, To: r.TS, Gated: gated})
		}
		from = r.TS
	}
	last := rows[len(rows)-1].To
	if last != "done" {
		bands = append(bands, Band{Status: last, From: from, To: end, Gated: in.OpenGated, Open: true})
	}
	return bands
}

// BandAt is the band t falls in, else the last band.
func BandAt(bands []Band, t time.Time) *Band {
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
func activityTimes(src Sources, start, end time.Time) []time.Time {
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

func approvedTimes(rows []tracker.TransitionStat) []time.Time {
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
func agentCallEnds(calls []Call) []time.Time {
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

// ClassifyGaps splits the life at each activity and at each approval, and
// classifies every gap: in a status only the owner can move on, a gap of
// IdleAfter or more is waiting on the owner; any other gap of IdleAfter or
// more is idle; the rest is agent working. The waits up to the owner's
// approvals come on top, from OverlayWaits; splitting at the approval keeps
// the time after it apart.
func ClassifyGaps(activity, approved []time.Time, bands []Band, start, end time.Time) []Seg {
	points := append([]time.Time{start}, activity...)
	for _, t := range approved {
		if t.After(start) && t.Before(end) {
			points = append(points, t)
		}
	}
	points = append(points, end)
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
	var segs []Seg
	add := func(a, b time.Time, kind string) {
		if b.After(a) {
			segs = append(segs, Seg{a, b, kind})
		}
	}
	for i := 0; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		if !b.After(a) {
			continue
		}
		switch {
		case b.Sub(a) >= IdleAfter:
			if band := BandAt(bands, a); band != nil && band.Gated {
				add(a, b, KindWaiting)
			} else {
				add(a, b, KindIdle)
			}
		default:
			add(a, b, KindAgent)
		}
	}
	return segs
}

// OverlayWaits marks each approval wait (tracker.OwnerWaits: from the
// agent's last call in the status to the approval, D-8 of the clilog issue)
// as waiting on the owner, whatever the gap rules made of that time.
func OverlayWaits(segs []Seg, waits []tracker.GateWait) []Seg {
	for _, w := range waits {
		var out []Seg
		for _, s := range segs {
			if !s.To.After(w.Since) || !w.ApprovedAt.After(s.From) {
				out = append(out, s)
				continue
			}
			lo, hi := maxTime(s.From, w.Since), minTime(s.To, w.ApprovedAt)
			if lo.After(s.From) {
				out = append(out, Seg{s.From, lo, s.Kind})
			}
			out = append(out, Seg{lo, hi, KindWaiting})
			if s.To.After(hi) {
				out = append(out, Seg{hi, s.To, s.Kind})
			}
		}
		segs = out
	}
	return segs
}

// MarkRework turns the agent time inside a rework loop into rework.
func MarkRework(segs []Seg, loops []Loop) []Seg {
	for _, l := range loops {
		var out []Seg
		for _, s := range segs {
			if s.Kind != KindAgent || !s.To.After(l.From) || !l.To.After(s.From) {
				out = append(out, s)
				continue
			}
			lo, hi := maxTime(s.From, l.From), minTime(s.To, l.To)
			if lo.After(s.From) {
				out = append(out, Seg{s.From, lo, KindAgent})
			}
			out = append(out, Seg{lo, hi, KindRework})
			if s.To.After(hi) {
				out = append(out, Seg{hi, s.To, KindAgent})
			}
		}
		segs = out
	}
	return segs
}

// MergeSegs joins neighbouring segments of the same kind.
func MergeSegs(segs []Seg) []Seg {
	var out []Seg
	for _, s := range segs {
		if n := len(out); n > 0 && out[n-1].Kind == s.Kind && out[n-1].To.Equal(s.From) {
			out[n-1].To = s.To
			continue
		}
		out = append(out, s)
	}
	return out
}

// KindWithin is the time of the given kind between from and to.
func KindWithin(segs []Seg, kind string, from, to time.Time) time.Duration {
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

// ReworkLoops finds the loops (D-4 of the Timeline tab issue): a backward
// transition, until the issue is back in the status it left; and a failed
// playtest run from testing on, until the next run in the same mode where
// the failed steps pass. Failed runs before testing are the agent
// iterating, not rework.
func ReworkLoops(src Sources, wf *tracker.WorkflowConfig, end time.Time) []Loop {
	var loops []Loop
	if wf != nil {
		rows := src.Transitions
		for i, r := range rows {
			if !Backward(wf, r) {
				continue
			}
			to := end
			for _, later := range rows[i+1:] {
				if later.To == r.From {
					to = later.TS
					break
				}
			}
			loops = append(loops, Loop{From: r.TS, To: to, Label: "back to " + r.To + " from " + r.From})
		}
	}
	testingFrom := TestingStart(src.Transitions, wf)
	if !testingFrom.IsZero() {
		for i, run := range src.Runs {
			failed := run.Failed()
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
					if !later.Passed(id) {
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
			loops = append(loops, Loop{From: run.Started, To: to, Label: who + " failed " + strings.Join(failed, ", ")})
		}
	}
	sort.SliceStable(loops, func(i, j int) bool { return loops[i].From.Before(loops[j].From) })
	// Overlapping windows are one loop.
	var out []Loop
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

// Backward: the transition goes to an earlier status in the workflow.
func Backward(wf *tracker.WorkflowConfig, r tracker.TransitionStat) bool {
	fi, ti := wf.GetStatusIndex(r.From), wf.GetStatusIndex(r.To)
	return fi >= 0 && ti >= 0 && ti < fi
}

// TestingStart is when the issue first reached testing or a later status.
func TestingStart(rows []tracker.TransitionStat, wf *tracker.WorkflowConfig) time.Time {
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
