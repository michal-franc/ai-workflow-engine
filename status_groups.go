package main

import (
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Status page at scale: every list is grouped by area (the issue's
// system) and capped, so a 700-issue project still fits on one screen.
// Lists show this many rows, then "+K more" opens the rest in place.
const (
	capNeeds          = 5  // Needs you, per kind
	capWorking        = 8  // Working on rows
	capChips          = 6  // agents on issues not yet in progress
	capNext           = 5  // backlog, parked
	capOpenInline     = 5  // release-wait open list shown unfolded up to this
	capOpen           = 10 // release-wait open list, inside its fold
	capUnreleased     = 5  // ## Unreleased entries
	capVersions       = 8  // Versions card
	capAreaEpics      = 8  // epics inside one area row
	capShippedAreas   = 5  // Recently shipped groups before "N more areas"
	capShippedItems   = 3  // newest done issues per shipped group
	capMilestoneAreas = 5  // Open by area chips
	capMDList         = 10 // status.md lists
	capMDAreaEpics    = 5  // status.md epics per area
	capMDNotStarted   = 8  // status.md "Not started" areas
)

// statusAreaOther is the area of an issue with neither a system nor a slug
// prefix.
const statusAreaOther = "other"

// areaOf is the issue's area: its system, else its slug prefix, lower case.
func areaOf(issue *tracker.Issue) string {
	if s := strings.ToLower(strings.TrimSpace(issue.System)); s != "" {
		return s
	}
	if i := strings.Index(issue.Slug, "/"); i > 0 {
		return strings.ToLower(issue.Slug[:i])
	}
	return statusAreaOther
}

// MilestoneView is the Current milestone card: the version the next release
// cuts, its issues split by status and its open issues by area.
type MilestoneView struct {
	Version string
	// Cut is the release the milestone becomes: v0.53.0.
	Cut      string
	Progress Progress
	// Segments are the bar: done, then each open status, furthest along
	// first. The workflow's first status is the bar's empty track.
	Segments []StatusSegment
	// Legend is every status with issues, in the bar's order, the track
	// last.
	Legend    []StatusSegment
	OpenAreas []AreaCount
	Open      int
}

// StatusSegment is one status's share of the milestone.
type StatusSegment struct {
	Status string
	Count  int
	// Pct is the share of the bar, for its width.
	Pct string
	// Track: the workflow's first status, drawn as the empty track.
	Track bool
	Done  bool
}

// AreaCount is a count of issues in one area.
type AreaCount struct {
	Area  string
	Count int
}

func buildMilestoneView(wf *tracker.WorkflowConfig, cs *canvasStatuses, version string, p Progress, items []*tracker.Issue) *MilestoneView {
	mv := &MilestoneView{Version: version, Cut: cutName(version), Progress: p}
	byStatus := map[string]int{}
	areas := map[string]int{}
	for _, it := range items {
		k := statusKey(it.Status)
		if cs.side[k] || k == cs.final {
			continue
		}
		byStatus[k]++
		areas[areaOf(it)]++
		mv.Open++
	}
	pct := func(n int) string {
		if p.Total == 0 {
			return "0"
		}
		return fmt.Sprintf("%.2f", float64(n)*100/float64(p.Total))
	}
	done := StatusSegment{Status: "done", Count: p.Done, Pct: pct(p.Done), Done: true}
	if p.Done > 0 {
		mv.Segments = append(mv.Segments, done)
		mv.Legend = append(mv.Legend, done)
	}
	var open []StatusSegment
	for i, st := range wf.Statuses {
		k := statusKey(st.Name)
		if n := byStatus[k]; n > 0 {
			open = append(open, StatusSegment{Status: st.Name, Count: n, Pct: pct(n), Track: i == 0})
			delete(byStatus, k)
		}
	}
	// Statuses the workflow doesn't list (a typo, an old name) still count.
	var rest []string
	for k := range byStatus {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		open = append(open, StatusSegment{Status: k, Count: byStatus[k], Pct: pct(byStatus[k])})
	}
	// Bar and legend run furthest along first; the track status comes last.
	var track []StatusSegment
	for i := len(open) - 1; i >= 0; i-- {
		if open[i].Track {
			track = append(track, open[i])
			continue
		}
		mv.Segments = append(mv.Segments, open[i])
		mv.Legend = append(mv.Legend, open[i])
	}
	mv.Legend = append(mv.Legend, track...)
	mv.OpenAreas = sortedAreaCounts(areas)
	return mv
}

func sortedAreaCounts(m map[string]int) []AreaCount {
	var out []AreaCount
	for a, n := range m {
		out = append(out, AreaCount{Area: a, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Area < out[j].Area
	})
	return out
}

// EpicsView is the Epics card: open epics grouped by area, one panel per
// version (progress counts only children in that version) plus "all".
type EpicsView struct {
	// Current is the key of the panel shown first: the milestone, else "all".
	Current string
	Panels  []EpicsPanel
	// Epics and Areas count open epics and their areas, over all versions.
	Epics int
	Areas int
}

// EpicsPanel is the Epics card for one version.
type EpicsPanel struct {
	// Key is the version, or "all"; Label is what the Version select shows.
	Key   string
	Label string
	Areas []AreaRow
	// Active epics have a done child; NotStarted ones don't.
	Active     int
	NotStarted int
}

// AreaRow is one area in an Epics panel.
type AreaRow struct {
	Area     string
	Progress Progress
	Epics    []EpicProgress
	Active   int
}

// Started: some epic in the area has a done child.
func (a AreaRow) Started() bool { return a.Active > 0 }

// HasNotStarted: some epic in the area has no done child.
func (a AreaRow) HasNotStarted() bool { return a.Active < len(a.Epics) }

const epicsAll = "all"

// buildEpicsView groups open epics by area for each version that has epic
// children, current (the milestone) first, then "all".
func buildEpicsView(cs *canvasStatuses, issues []*tracker.Issue, epics []EpicProgress, current string) EpicsView {
	ev := EpicsView{Epics: len(epics)}
	children := map[string][]*tracker.Issue{}
	versions := map[string]bool{}
	areas := map[string]bool{}
	for _, e := range epics {
		areas[areaOf(e.Issue)] = true
		children[e.Issue.Slug] = tracker.EpicChildren(issues, e.Issue.Slug)
		for _, c := range children[e.Issue.Slug] {
			if c.Version != "" {
				versions[c.Version] = true
			}
		}
	}
	ev.Areas = len(areas)
	if len(epics) == 0 {
		return ev
	}

	var keys []string
	for v := range versions {
		if v != current {
			keys = append(keys, v)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return compareVersions(keys[i], keys[j]) < 0 })
	if versions[current] {
		keys = append([]string{current}, keys...)
		ev.Current = current
	} else {
		ev.Current = epicsAll
	}
	keys = append(keys, epicsAll)

	for _, key := range keys {
		panel := EpicsPanel{Key: key, Label: key}
		switch {
		case key == epicsAll:
			panel.Label = "All"
		case key == current:
			panel.Label = key + " (current)"
		}
		byArea := map[string]*AreaRow{}
		for _, e := range epics {
			kids := children[e.Issue.Slug]
			if key != epicsAll {
				var in []*tracker.Issue
				for _, c := range kids {
					if c.Version == key {
						in = append(in, c)
					}
				}
				if len(in) == 0 {
					continue
				}
				kids = in
			}
			row := EpicProgress{Issue: e.Issue, Progress: progressOf(e.Issue.Title, cs, kids), Running: e.Running}
			a := areaOf(e.Issue)
			ar := byArea[a]
			if ar == nil {
				ar = &AreaRow{Area: a, Progress: Progress{Label: a}}
				byArea[a] = ar
			}
			ar.Epics = append(ar.Epics, row)
			ar.Progress.Done += row.Progress.Done
			ar.Progress.Total += row.Progress.Total
			if row.Progress.Done > 0 {
				ar.Active++
				panel.Active++
			} else {
				panel.NotStarted++
			}
		}
		for _, ar := range byArea {
			sortEpicRows(ar.Epics)
			panel.Areas = append(panel.Areas, *ar)
		}
		sortAreaRows(panel.Areas)
		ev.Panels = append(ev.Panels, panel)
	}
	return ev
}

// sortEpicRows puts the furthest along first: done share, then done, then
// slug.
func sortEpicRows(rows []EpicProgress) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Progress, rows[j].Progress
		if a.Pct() != b.Pct() {
			return a.Pct() > b.Pct()
		}
		if a.Done != b.Done {
			return a.Done > b.Done
		}
		return rows[i].Issue.Slug < rows[j].Issue.Slug
	})
}

// sortAreaRows puts areas with progress first (most done first), then the
// rest by size.
func sortAreaRows(rows []AreaRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Started() != b.Started() {
			return a.Started()
		}
		if a.Progress.Done != b.Progress.Done {
			return a.Progress.Done > b.Progress.Done
		}
		if a.Progress.Total != b.Progress.Total {
			return a.Progress.Total > b.Progress.Total
		}
		return a.Area < b.Area
	})
}

// ShippedArea is one Recently shipped group: done issues in one area, newest
// first. Merged is the last group, "N more areas".
type ShippedArea struct {
	Area   string
	Rows   []DoneRow
	Merged bool
	Areas  int
	// Pct is the group's bar, relative to the largest group.
	Pct int
}

// groupShipped groups done issues (newest first) by area, largest first; past
// capShippedAreas the rest merge into one group.
func groupShipped(rows []DoneRow) []ShippedArea {
	idx := map[string]int{}
	var groups []ShippedArea
	for _, r := range rows {
		a := areaOf(r.Issue)
		i, ok := idx[a]
		if !ok {
			i = len(groups)
			idx[a] = i
			groups = append(groups, ShippedArea{Area: a, Areas: 1})
		}
		groups[i].Rows = append(groups[i].Rows, r)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Rows) != len(groups[j].Rows) {
			return len(groups[i].Rows) > len(groups[j].Rows)
		}
		return groups[i].Area < groups[j].Area
	})
	if len(groups) > capShippedAreas+1 {
		merged := ShippedArea{Merged: true, Areas: len(groups) - capShippedAreas}
		for _, g := range groups[capShippedAreas:] {
			merged.Rows = append(merged.Rows, g.Rows...)
		}
		sort.SliceStable(merged.Rows, func(i, j int) bool { return merged.Rows[i].At.After(merged.Rows[j].At) })
		merged.Area = fmt.Sprintf("%d more areas", merged.Areas)
		groups = append(groups[:capShippedAreas], merged)
	}
	largest := 0
	for _, g := range groups {
		if len(g.Rows) > largest {
			largest = len(g.Rows)
		}
	}
	for i := range groups {
		groups[i].Pct = len(groups[i].Rows) * 100 / largest
	}
	return groups
}

// OpenSummary is the release-wait fold's label: "1 in progress, 2 in
// design, 41 idea", in the open list's order (started first).
func (ms *MilestoneRow) OpenSummary() string {
	var parts []string
	var last string
	n := 0
	flush := func() {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, last))
		}
	}
	for _, issue := range ms.Open {
		if issue.Status != last {
			flush()
			last, n = issue.Status, 0
		}
		n++
	}
	flush()
	return strings.Join(parts, ", ")
}

var (
	mdLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdStrongRe = regexp.MustCompile(`(\*\*|__)(.+?)(\*\*|__)`)
)

// SummaryText is the release summary as plain text, clipped: what status.md
// and status.json show.
func (r Release) SummaryText() string {
	s := mdLinkRe.ReplaceAllString(r.Summary, "$1")
	s = mdStrongRe.ReplaceAllString(s, "$2")
	s = strings.ReplaceAll(s, "`", "")
	return clip(s, 140)
}

// SummaryHTML is the release summary with its inline markdown (bold, code,
// links) rendered; raw HTML in it is dropped. The page clamps it to 3 lines.
func (r Release) SummaryHTML() template.HTML { return inlineMarkdown(r.Summary) }

// inlineMarkdown renders one line of changelog markdown (bold, code, links)
// without the paragraph around it; raw HTML in it is dropped.
func inlineMarkdown(text string) template.HTML {
	html := strings.TrimSpace(renderMarkdown(text))
	html = strings.TrimSuffix(strings.TrimPrefix(html, "<p>"), "</p>")
	return template.HTML(html)
}

// StatusCaps are the caps above, for the template's head/rest splits.
type StatusCaps struct {
	Needs, Working, Chips, Next, OpenInline, Open, Unreleased, Versions, AreaEpics, ShippedItems, MilestoneAreas int
}

// Cap is the page's list caps.
func (m *StatusModel) Cap() StatusCaps {
	return StatusCaps{
		Needs: capNeeds, Working: capWorking, Chips: capChips, Next: capNext,
		OpenInline: capOpenInline, Open: capOpen, Unreleased: capUnreleased,
		Versions: capVersions, AreaEpics: capAreaEpics, ShippedItems: capShippedItems,
		MilestoneAreas: capMilestoneAreas,
	}
}

// All is every epic in the panel.
func (p EpicsPanel) All() int { return p.Active + p.NotStarted }

// CurrentPanel is the panel shown first.
func (ev EpicsView) CurrentPanel() *EpicsPanel {
	for i := range ev.Panels {
		if ev.Panels[i].Key == ev.Current {
			return &ev.Panels[i]
		}
	}
	return nil
}

// The top row's tiles show one line each; the full card opens over the page.

// NeedsSummary is the Needs you tile's line: "7 to approve · 1 question".
func (m *StatusModel) NeedsSummary() string {
	var parts []string
	if m.CI.Red() {
		parts = append(parts, "local CI red")
	}
	for _, k := range []struct{ kind, label string }{{"gate", "to approve"}, {"playtest", "to playtest"}, {"question", "question"}} {
		if n := len(m.WaitingOf(k.kind)); n > 0 {
			label := k.label
			if k.kind == "question" && n > 1 {
				label = "questions"
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	if len(parts) == 0 {
		return "Nothing waits on you"
	}
	return strings.Join(parts, " · ")
}

// WorkingSummary is the Working on tile's line: "3 agents working · 9
// without an agent · lead not running".
func (m *StatusModel) WorkingSummary() string {
	if len(m.Active) == 0 && m.LeadName == "" {
		return "Nothing in flight"
	}
	counts := map[string]int{}
	var order []string
	for _, r := range m.Active {
		state := r.Agent
		if state == "" {
			state = "no agent"
		}
		if counts[state] == 0 {
			order = append(order, state)
		}
		counts[state]++
	}
	var parts []string
	for _, s := range order {
		if s == "no agent" {
			parts = append(parts, fmt.Sprintf("%d without an agent", counts[s]))
			continue
		}
		parts = append(parts, plural(counts[s], "agent", "agents")+" "+s)
	}
	if m.LeadName != "" {
		if m.LeadState == "live" {
			parts = append(parts, "lead live")
		} else {
			parts = append(parts, "lead not running")
		}
	}
	return strings.Join(parts, " · ")
}

// NextSummary is the What's next tile's line: the lead's list, or the
// backlog and parked counts.
func (m *StatusModel) NextSummary() string {
	var parts []string
	if m.Next.Found {
		parts = append(parts, "the lead's ## Next")
	}
	parts = append(parts, fmt.Sprintf("%d backlog", len(m.Backlog)), fmt.Sprintf("%d parked", len(m.Parked)))
	return strings.Join(parts, " · ")
}
