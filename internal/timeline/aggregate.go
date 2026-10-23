package timeline

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Window is the aggregate's default: issues done in the last 30 days (D-2
// of the workflow timeline issue).
const Window = 30 * 24 * time.Hour

// All is the label of a rollup row: every type, or every status.
const All = "all"

// NoType is the type label of an issue without a type: field.
const NoType = "none"

// IssueStat is one done issue as the aggregate counts it: Split's numbers,
// summed per status (a status visited twice is one entry).
type IssueStat struct {
	Slug, Title, Type string
	DoneAt            time.Time
	// Known: the issue had CLI calls, so its time is split. Without them
	// only Cycle and each status's Total count.
	Known      bool
	Cycle      time.Duration
	Statuses   map[string]StatusTime
	Working    time.Duration
	Rework     time.Duration
	Waits      []tracker.GateWait
	Loops      int
	Backward   int
	Failures   []GateFailure
	Dispatches int
}

// StatusTime is one status's time in one issue.
type StatusTime struct {
	Total, Working, Waiting, Idle, Rework time.Duration
}

// GateFailure is one refused transition call: the status it was made in,
// the --to it asked for, and the error class v0.54 records. DryRun: the
// agent checked with --dry-run, as the workflow guidance tells it to, so
// most refusals are dry runs.
type GateFailure struct {
	From, To, ErrClass string
	DryRun             bool
}

// Pct is a p50/p90 pair.
type Pct struct {
	P50, P90 time.Duration
}

// StatusRow is one status's time across the issues of one type. N issues
// spent time in it; Split of them had CLI calls, and only those count for
// Working, Waiting and Idle.
type StatusRow struct {
	Type, Status                  string
	N, Split                      int
	Total, Working, Waiting, Idle Pct
}

// TypeRow is one type of work's cycle, rework and dispatches.
type TypeRow struct {
	Type        string
	N, Unsplit  int
	Cycle       Pct
	Dispatches  [2]float64 // p50, p90
	Loops       int
	Backward    int
	ReworkShare float64 // rework agent time / working, over the split issues
	// Working, Waiting and Idle are summed over the split issues;
	// WorkingPer is the p50/p90 of each split issue's agent working time.
	Working    time.Duration
	Waiting    time.Duration
	Idle       time.Duration
	WorkingPer Pct
	ReworkTime time.Duration
}

// WaitingShare is the share of the split issues' time spent waiting on the
// owner; 0 without split time.
func (t TypeRow) WaitingShare() float64 {
	all := t.Working + t.Waiting + t.Idle
	if all <= 0 {
		return 0
	}
	return float64(t.Waiting) / float64(all)
}

// ApprovalRow is the owner's wait at one gated transition.
type ApprovalRow struct {
	Transition string // from→to
	N          int
	Wait       Pct
}

// FailureRow counts refused transition calls by transition, error class
// and whether they were dry runs.
type FailureRow struct {
	Transition, ErrClass string
	DryRun               bool
	N                    int
}

// Report is the project aggregate: the issues done in [From, To].
type Report struct {
	From, To time.Time
	Issues   []IssueStat
	// Unread are done issues in no window because no transition to done
	// was recorded for them (no stats sidecar).
	Unread    []string
	Types     []TypeRow
	Statuses  []StatusRow
	Approvals []ApprovalRow
	Failures  []FailureRow

	wf *tracker.WorkflowConfig
}

// Filter is the report over the issues done in [from, to] only, summarized
// again, so one Aggregate over a wide window serves narrower ones. Unread
// is kept as it is.
func (r Report) Filter(from, to time.Time) Report {
	out := Report{From: from, To: to, Unread: r.Unread, wf: r.wf}
	for _, s := range r.Issues {
		if !s.DoneAt.Before(from) && !s.DoneAt.After(to) {
			out.Issues = append(out.Issues, s)
		}
	}
	out.summarize(r.wf)
	return out
}

// Unsplit names the issues in the window without CLI calls.
func (r Report) Unsplit() []string {
	var out []string
	for _, s := range r.Issues {
		if !s.Known {
			out = append(out, s.Slug)
		}
	}
	return out
}

// Aggregate builds the project report over the issues done in the window
// before now, each through Load and Split as the Timeline tab builds it. wf
// is the project workflow (nil loads it); each issue gets its own overlay.
// When ctx ends it stops and summarizes the issues it has.
func Aggregate(ctx context.Context, proj *tracker.Project, wf *tracker.WorkflowConfig, issues []*tracker.Issue, now time.Time, window time.Duration) Report {
	if wf == nil && proj != nil {
		wf = proj.LoadWorkflow()
	}
	rep := Report{From: now.Add(-window), To: now, wf: wf}
	cache := NewCache()
	for _, issue := range issues {
		if ctx.Err() != nil {
			break
		}
		if issue.Status != "done" {
			continue
		}
		stats, err := tracker.LoadStats(issue.FilePath)
		if err != nil {
			rep.Unread = append(rep.Unread, issue.Slug)
			continue
		}
		doneAt, ok := DoneAt(stats.Transitions)
		if !ok {
			rep.Unread = append(rep.Unread, issue.Slug)
			continue
		}
		if doneAt.Before(rep.From) || doneAt.After(now) {
			continue
		}
		iwf := wf
		if wf != nil {
			iwf = wf.ForIssue(issue)
		}
		src := Load(proj, issue, cache)
		rep.Issues = append(rep.Issues, issueStat(issue, src, Split(src, In{Status: "done", Now: now, WF: iwf}), iwf))
	}
	sort.SliceStable(rep.Issues, func(i, j int) bool { return rep.Issues[i].DoneAt.Before(rep.Issues[j].DoneAt) })
	rep.summarize(wf)
	return rep
}

func issueStat(issue *tracker.Issue, src Sources, r Result, wf *tracker.WorkflowConfig) IssueStat {
	s := IssueStat{
		Slug: issue.Slug, Title: issue.Title, Type: issue.Type,
		DoneAt: r.End, Known: r.Known, Cycle: r.Total(),
		Statuses: map[string]StatusTime{},
		Working:  r.Working, Rework: r.Rework,
		Waits: r.Waits, Loops: len(r.Loops), Dispatches: len(src.Dispatches),
	}
	if s.Type == "" {
		s.Type = NoType
	}
	for _, b := range r.Bands {
		st := s.Statuses[b.Status]
		st.Total += b.To.Sub(b.From)
		st.Working += b.Working
		st.Waiting += b.Waiting
		st.Idle += b.Idle
		st.Rework += b.Rework
		s.Statuses[b.Status] = st
	}
	if wf != nil {
		for _, t := range src.Transitions {
			if Backward(wf, t) {
				s.Backward++
			}
		}
	}
	for _, c := range src.Calls {
		if c.Cmd() != "transition" || !c.Failed() {
			continue
		}
		from := ""
		if b := BandAt(r.Bands, c.At); b != nil {
			from = b.Status
		}
		class := c.ErrClass
		if class == "" {
			class = "other"
		}
		s.Failures = append(s.Failures, GateFailure{From: from, To: clilog.FlagValue(c.Args, "--to"), ErrClass: class, DryRun: hasArg(c.Args, "--dry-run")})
	}
	return s
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

// summarize fills the tables: per type (with an All row), per status and
// type in workflow order, approval waits and gate failures.
func (rep *Report) summarize(wf *tracker.WorkflowConfig) {
	types := []string{All}
	seenType := map[string]bool{}
	for _, s := range rep.Issues {
		if !seenType[s.Type] {
			seenType[s.Type] = true
			types = append(types, s.Type)
		}
	}
	sort.Strings(types[1:])
	of := func(typ string) []IssueStat {
		if typ == All {
			return rep.Issues
		}
		var out []IssueStat
		for _, s := range rep.Issues {
			if s.Type == typ {
				out = append(out, s)
			}
		}
		return out
	}

	statuses := statusOrder(wf, rep.Issues)
	for _, typ := range types {
		group := of(typ)
		row := TypeRow{Type: typ, N: len(group)}
		var cycle, dispatches, working []float64
		for _, s := range group {
			cycle = append(cycle, float64(s.Cycle))
			dispatches = append(dispatches, float64(s.Dispatches))
			row.Loops += s.Loops
			row.Backward += s.Backward
			if !s.Known {
				row.Unsplit++
				continue
			}
			row.Working += s.Working
			row.ReworkTime += s.Rework
			working = append(working, float64(s.Working))
			for _, st := range s.Statuses {
				row.Waiting += st.Waiting
				row.Idle += st.Idle
			}
		}
		row.Cycle = pct(cycle)
		row.WorkingPer = pct(working)
		row.Dispatches = [2]float64{percentile(dispatches, 0.5), percentile(dispatches, 0.9)}
		if row.Working > 0 {
			row.ReworkShare = float64(row.ReworkTime) / float64(row.Working)
		}
		rep.Types = append(rep.Types, row)

		for _, st := range statuses {
			sr := StatusRow{Type: typ, Status: st}
			var total, working, waiting, idle []float64
			for _, s := range group {
				t, ok := s.Statuses[st]
				if !ok {
					continue
				}
				sr.N++
				total = append(total, float64(t.Total))
				if s.Known {
					sr.Split++
					working = append(working, float64(t.Working))
					waiting = append(waiting, float64(t.Waiting))
					idle = append(idle, float64(t.Idle))
				}
			}
			if sr.N == 0 {
				continue
			}
			sr.Total, sr.Working, sr.Waiting, sr.Idle = pct(total), pct(working), pct(waiting), pct(idle)
			rep.Statuses = append(rep.Statuses, sr)
		}
	}

	waits := map[string][]float64{}
	var order []string
	type failKey struct {
		transition, class string
		dry               bool
	}
	failures := map[failKey]int{}
	for _, s := range rep.Issues {
		for _, w := range s.Waits {
			k := w.From + "→" + w.To
			if _, ok := waits[k]; !ok {
				order = append(order, k)
			}
			waits[k] = append(waits[k], float64(w.Wait))
		}
		for _, f := range s.Failures {
			failures[failKey{f.From + "→" + f.To, f.ErrClass, f.DryRun}]++
		}
	}
	rank := map[string]int{}
	for i, st := range statuses {
		rank[st] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := transitionRank(order[i], rank), transitionRank(order[j], rank)
		return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1])
	})
	for _, k := range order {
		rep.Approvals = append(rep.Approvals, ApprovalRow{Transition: k, N: len(waits[k]), Wait: pct(waits[k])})
	}
	for k, n := range failures {
		rep.Failures = append(rep.Failures, FailureRow{Transition: k.transition, ErrClass: k.class, DryRun: k.dry, N: n})
	}
	sort.Slice(rep.Failures, func(i, j int) bool {
		a, b := rep.Failures[i], rep.Failures[j]
		if a.DryRun != b.DryRun {
			return !a.DryRun
		}
		if a.N != b.N {
			return a.N > b.N
		}
		if a.Transition != b.Transition {
			return a.Transition < b.Transition
		}
		return a.ErrClass < b.ErrClass
	})
}

// transitionRank orders "from→to" by from's then to's place in the
// workflow.
func transitionRank(t string, rank map[string]int) [2]int {
	from, to, _ := strings.Cut(t, "→")
	r := func(s string) int {
		if i, ok := rank[s]; ok {
			return i
		}
		return len(rank)
	}
	return [2]int{r(from), r(to)}
}

// statusOrder is the workflow's status order, then any other status the
// issues spent time in, alphabetically.
func statusOrder(wf *tracker.WorkflowConfig, issues []IssueStat) []string {
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

func pct(vals []float64) Pct {
	return Pct{P50: time.Duration(percentile(vals, 0.5)), P90: time.Duration(percentile(vals, 0.9))}
}

// percentile is the nearest-rank percentile, 0 for no values.
func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return s[i]
}
