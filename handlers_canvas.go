package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// CanvasOptions is the toolbar state of the issues canvas, read from the
// query string so a view can be linked.
type CanvasOptions struct {
	Rows          string   // "epic" (default) or "system"
	CollapseDone  bool     // ?done=collapse
	ShowNoVersion bool     // ?nover=1
	Highlight     string   // ?hl=<epic slug>
	Open          []string // ?open=<key>,<key>: unfolded lanes
	Epic          string   // ?epic=<epic slug>: only that epic and its sub-epics (epic rows)
}

// CanvasData is the issues canvas view model: version columns × epic or
// system lanes.
type CanvasData struct {
	Prefix      string
	ProjectName string

	TotalIssues int
	ShownIssues int

	Options         CanvasOptions
	EpicRows        bool
	NoVersionForced bool // no issue has a version: the column is always shown
	HiddenNoVersion int  // issues hidden because they have no version
	HighlightName   string
	OpenKeys        string             // the unfolded lanes for the toolbar's hidden open field
	EpicChoices     []CanvasEpicChoice // the Epic filter's options, in lane order

	Columns []CanvasColumn
	Lanes   []CanvasLane
}

// CanvasColumn is one version column header.
type CanvasColumn struct {
	Name     string
	Progress string
	Bar      []CanvasSegment
	Extra    string
}

// CanvasEpicChoice is one option of the Epic filter; sub-epics are indented.
type CanvasEpicChoice struct {
	Slug  string
	Label string
}

// CanvasSegment is one status's share of a progress bar.
type CanvasSegment struct {
	Status string
	Count  int
	Side   bool
}

// CanvasLane is one row: an epic, "No epic", or a system.
type CanvasLane struct {
	Kind        string // EPIC, UMBRELLA, NO EPIC, SYSTEM
	Key         string // test id segment: epic slug, "no epic", or the system
	Slug        string // epic slug ("" for other lanes)
	Title       string
	ShortTitle  string // epic name on the Highlight chip
	Status      string
	ParentTitle string
	Depth       int
	Summary     string
	Bar         []CanvasSegment
	Epics       string // space-separated epic slugs the lane holds, for highlight
	Lit         bool
	Open        bool // cards shown; a folded lane shows each cell's counts

	// Note replaces the cells when an epic has nothing to show.
	Note          string
	NoteNoVersion bool
	Cells         []CanvasCell
}

// CanvasCell is the cards of one lane in one version column.
type CanvasCell struct {
	Column string // the column's name, for test ids
	Cards  []CanvasCard
	Closed string // "17 done · 20 obsolete" when closed cards are collapsed
	Empty  bool

	// Fold is the folded lane's summary: a count per open status in
	// workflow order, then every closed card as one ✓ count.
	Fold       []CanvasSegment
	FoldClosed int
}

// CanvasCard is one issue on the canvas.
type CanvasCard struct {
	Slug          string
	Title         string
	Status        string
	Type          string
	Closed        bool
	Epic          string // the epic the card belongs to (its own slug for an epic)
	EpicName      string // badge text, shown in system rows
	UnknownParent string
	Lit           bool
	Order         int // render order without a highlight, for the client to restore
}

func (s *Server) handleCanvas(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	opts := CanvasOptions{
		Rows:          q.Get("rows"),
		CollapseDone:  q.Get("done") == "collapse",
		ShowNoVersion: q.Get("nover") == "1",
		Highlight:     q.Get("hl"),
		Epic:          q.Get("epic"),
	}
	for _, k := range strings.Split(q.Get("open"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			opts.Open = append(opts.Open, k)
		}
	}

	data := buildCanvas(issues, proj.LoadWorkflow(), opts)
	data.Prefix = prefix
	data.ProjectName = proj.Name

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "canvas.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// epicParentSlug is the epic an issue points at: `parent:`, else the legacy
// `umbrella:`. It mirrors tracker.ParentSlug on the lead-engineer branch;
// swap this for that helper once it lands on main.
func epicParentSlug(issue *tracker.Issue) string {
	var umbrella string
	for _, f := range issue.ExtraFields {
		if f.IsList {
			continue
		}
		switch f.Key {
		case "parent":
			if v := strings.TrimSpace(f.Value); v != "" {
				return v
			}
		case "umbrella":
			umbrella = strings.TrimSpace(f.Value)
		}
	}
	return umbrella
}

// epicIndex resolves the parent links of a project's issues.
type epicIndex struct {
	bySlug  map[string]*tracker.Issue
	parent  map[*tracker.Issue]*tracker.Issue // resolved epic parent
	unknown map[*tracker.Issue]string         // parent value that matched no issue
	isEpic  map[*tracker.Issue]bool
}

func newEpicIndex(issues []*tracker.Issue) *epicIndex {
	idx := &epicIndex{
		bySlug:  map[string]*tracker.Issue{},
		parent:  map[*tracker.Issue]*tracker.Issue{},
		unknown: map[*tracker.Issue]string{},
		isEpic:  map[*tracker.Issue]bool{},
	}
	for _, issue := range issues {
		idx.bySlug[strings.ToLower(issue.Slug)] = issue
	}
	for _, issue := range issues {
		if strings.EqualFold(issue.Type, "epic") {
			idx.isEpic[issue] = true
		}
		p := epicParentSlug(issue)
		if p == "" {
			continue
		}
		target := idx.bySlug[strings.ToLower(p)]
		switch {
		case target == nil:
			idx.unknown[issue] = p
		case target != issue:
			idx.parent[issue] = target
			idx.isEpic[target] = true
		}
	}
	return idx
}

// epicShortName is an epic's title up to the first ":" or " (".
func epicShortName(title string) string {
	if i := strings.Index(title, ":"); i > 0 {
		title = title[:i]
	}
	if i := strings.Index(title, " ("); i > 0 {
		title = title[:i]
	}
	return strings.TrimSpace(title)
}

// compareVersions orders dotted versions numerically (milestone.Compare).
func compareVersions(a, b string) int { return milestone.Compare(a, b) }

// canvasStatuses classifies statuses against the workflow: the final status
// is "done", optional statuses are side statuses (obsolete, parked).
type canvasStatuses struct {
	index map[string]int
	final string
	side  map[string]bool
	order []string // bar order: done, then later statuses first, then side
}

func newCanvasStatuses(wf *tracker.WorkflowConfig) *canvasStatuses {
	cs := &canvasStatuses{index: map[string]int{}, side: map[string]bool{}}
	var main, side []string
	for i, st := range wf.Statuses {
		name := statusKey(st.Name)
		cs.index[name] = i
		if st.Optional {
			cs.side[name] = true
			side = append(side, name)
		} else {
			main = append(main, name)
		}
	}
	if len(main) > 0 {
		cs.final = main[len(main)-1]
	}
	if cs.final != "" {
		cs.order = append(cs.order, cs.final)
	}
	for i := len(main) - 2; i >= 0; i-- {
		cs.order = append(cs.order, main[i])
	}
	cs.order = append(cs.order, side...)
	return cs
}

func statusKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (cs *canvasStatuses) closed(status string) bool {
	k := statusKey(status)
	return k == cs.final || cs.side[k]
}

func (cs *canvasStatuses) pos(status string) int {
	if i, ok := cs.index[statusKey(status)]; ok {
		return i
	}
	return -1
}

// bar counts items per status in bar order; statuses outside the workflow
// go before the side statuses.
func (cs *canvasStatuses) bar(items []*tracker.Issue) []CanvasSegment {
	counts := map[string]int{}
	var unknown []string
	for _, it := range items {
		k := statusKey(it.Status)
		if _, ok := cs.index[k]; !ok && counts[k] == 0 {
			unknown = append(unknown, k)
		}
		counts[k]++
	}
	sort.Strings(unknown)
	var segs []CanvasSegment
	add := func(k string) {
		if counts[k] > 0 {
			segs = append(segs, CanvasSegment{Status: k, Count: counts[k], Side: cs.side[k]})
		}
	}
	nSide := len(cs.side)
	for _, k := range cs.order[:len(cs.order)-nSide] {
		add(k)
	}
	for _, k := range unknown {
		add(k)
	}
	for _, k := range cs.order[len(cs.order)-nSide:] {
		add(k)
	}
	return segs
}

// progress is "d/t done" with side statuses left out of t.
func (cs *canvasStatuses) progress(items []*tracker.Issue) string {
	done, total := 0, 0
	for _, it := range items {
		k := statusKey(it.Status)
		if cs.side[k] {
			continue
		}
		total++
		if k == cs.final {
			done++
		}
	}
	return fmt.Sprintf("%d/%d done", done, total)
}

// sideCounts is "20 obsolete · 1 parked", in workflow order.
func (cs *canvasStatuses) sideCounts(items []*tracker.Issue) string {
	counts := map[string]int{}
	for _, it := range items {
		counts[statusKey(it.Status)]++
	}
	var parts []string
	for _, k := range cs.order {
		if cs.side[k] && counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
		}
	}
	return strings.Join(parts, " · ")
}

// fold counts items per open status in workflow order (statuses outside the
// workflow last, by name) and returns the closed ones as a single count.
func (cs *canvasStatuses) fold(items []*tracker.Issue) ([]CanvasSegment, int) {
	counts := map[string]int{}
	closed := 0
	for _, it := range items {
		if cs.closed(it.Status) {
			closed++
		} else {
			counts[statusKey(it.Status)]++
		}
	}
	keys := sortedKeys(counts)
	sort.SliceStable(keys, func(i, j int) bool {
		pi, pj := cs.pos(keys[i]), cs.pos(keys[j])
		if (pi < 0) != (pj < 0) {
			return pj < 0
		}
		return pi < pj
	})
	var segs []CanvasSegment
	for _, k := range keys {
		segs = append(segs, CanvasSegment{Status: k, Count: counts[k]})
	}
	return segs, closed
}

func joinParts(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// buildCanvas lays out the issues canvas: version columns, and one lane per
// epic (plus "No epic") or per system.
func buildCanvas(issues []*tracker.Issue, wf *tracker.WorkflowConfig, opts CanvasOptions) CanvasData {
	if opts.Rows != "system" {
		opts.Rows = "epic"
	}
	cs := newCanvasStatuses(wf)
	idx := newEpicIndex(issues)
	epicMode := opts.Rows == "epic"

	hl := idx.bySlug[strings.ToLower(opts.Highlight)]
	if hl != nil && !idx.isEpic[hl] {
		hl = nil
	}
	if hl == nil {
		opts.Highlight = ""
	} else {
		opts.Highlight = hl.Slug
	}

	versionSet := map[string]bool{}
	noVersion := 0
	for _, issue := range issues {
		if issue.Version != "" {
			versionSet[issue.Version] = true
		} else {
			noVersion++
		}
	}
	var versions []string
	for v := range versionSet {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return compareVersions(versions[i], versions[j]) < 0 })

	data := CanvasData{
		TotalIssues: len(issues),
		Options:     opts,
		EpicRows:    epicMode,
	}
	showNoVersion := opts.ShowNoVersion || len(versions) == 0
	data.NoVersionForced = len(versions) == 0
	colKeys := versions
	if showNoVersion {
		colKeys = append(colKeys, "")
	} else {
		data.HiddenNoVersion = noVersion
	}
	visible := func(issue *tracker.Issue) bool {
		return issue.Version != "" || showNoVersion
	}

	epicOf := func(issue *tracker.Issue) *tracker.Issue { return idx.parent[issue] }
	inHighlight := func(issue *tracker.Issue) bool {
		return hl != nil && (issue == hl || epicOf(issue) == hl)
	}

	// Open cards first, then done, then side statuses.
	closedRank := func(issue *tracker.Issue) int {
		switch {
		case !cs.closed(issue.Status):
			return 0
		case statusKey(issue.Status) == cs.final:
			return 1
		default:
			return 2
		}
	}
	sortCards := func(items []*tracker.Issue, withHighlight bool) {
		sort.SliceStable(items, func(i, j int) bool {
			if withHighlight {
				if hi, hj := inHighlight(items[i]), inHighlight(items[j]); hi != hj {
					return hi
				}
			}
			if ri, rj := closedRank(items[i]), closedRank(items[j]); ri != rj {
				return ri < rj
			}
			if pi, pj := cs.pos(items[i].Status), cs.pos(items[j].Status); pi != pj {
				return pi > pj
			}
			return items[i].Title < items[j].Title
		})
	}

	card := func(issue *tracker.Issue, order int) CanvasCard {
		c := CanvasCard{
			Slug:          issue.Slug,
			Title:         issue.Title,
			Status:        issue.Status,
			Type:          issue.Type,
			Closed:        cs.closed(issue.Status),
			UnknownParent: idx.unknown[issue],
			Lit:           inHighlight(issue),
			Order:         order,
		}
		if e := epicOf(issue); e != nil {
			c.Epic, c.EpicName = e.Slug, epicShortName(e.Title)
		} else if idx.isEpic[issue] {
			c.Epic, c.EpicName = issue.Slug, epicShortName(issue.Title)
		}
		if c.Type == "" && idx.isEpic[issue] {
			c.Type = "epic"
		}
		return c
	}

	cells := func(items []*tracker.Issue) []CanvasCell {
		var out []CanvasCell
		for _, v := range colKeys {
			var here []*tracker.Issue
			for _, it := range items {
				if it.Version == v {
					here = append(here, it)
				}
			}
			sortCards(here, false)
			order := map[*tracker.Issue]int{}
			for i, it := range here {
				order[it] = i
			}
			sortCards(here, true)

			column := v
			if column == "" {
				column = "no version"
			}
			cell := CanvasCell{Column: column, Empty: len(here) == 0}
			cell.Fold, cell.FoldClosed = cs.fold(here)
			var closed []*tracker.Issue
			for _, it := range here {
				if cs.closed(it.Status) {
					closed = append(closed, it)
					if opts.CollapseDone {
						continue
					}
				}
				cell.Cards = append(cell.Cards, card(it, order[it]))
			}
			if opts.CollapseDone && len(closed) > 0 {
				done := 0
				for _, it := range closed {
					if statusKey(it.Status) == cs.final {
						done++
					}
				}
				doneText := ""
				if done > 0 {
					doneText = fmt.Sprintf("%d done", done)
				}
				cell.Closed = joinParts(doneText, cs.sideCounts(closed))
			}
			out = append(out, cell)
		}
		return out
	}

	filterVisible := func(items []*tracker.Issue) []*tracker.Issue {
		var out []*tracker.Issue
		for _, it := range items {
			if visible(it) {
				out = append(out, it)
			}
		}
		return out
	}

	epic := idx.bySlug[strings.ToLower(opts.Epic)]
	if !epicMode || epic == nil || !idx.isEpic[epic] {
		epic = nil
		data.Options.Epic = ""
	} else {
		data.Options.Epic = epic.Slug
	}

	if epicMode {
		data.Lanes = epicLanes(issues, idx, cs, hl, visible, showNoVersion, cells)
		for _, l := range data.Lanes {
			data.EpicChoices = append(data.EpicChoices, CanvasEpicChoice{
				Slug:  l.Slug,
				Label: strings.Repeat("\u00a0\u00a0", l.Depth) + l.ShortTitle,
			})
		}
	}
	if epic != nil {
		data.Lanes = epicSubtree(data.Lanes, epic.Slug)
	} else if epicMode {
		var loose []*tracker.Issue
		for _, issue := range issues {
			if epicOf(issue) == nil && !idx.isEpic[issue] {
				loose = append(loose, issue)
			}
		}
		shown := filterVisible(loose)
		count := plural(len(loose), "issue", "issues")
		if len(shown) != len(loose) {
			count = fmt.Sprintf("%d shown of %d", len(shown), len(loose))
		}
		data.Lanes = append(data.Lanes, CanvasLane{
			Kind:    "NO EPIC",
			Key:     "no epic",
			Title:   "No epic",
			Summary: joinParts(count, cs.progress(shown), cs.sideCounts(shown)),
			Bar:     cs.bar(shown),
			Cells:   cells(shown),
		})
	} else {
		bySystem := map[string][]*tracker.Issue{}
		for _, issue := range filterVisible(issues) {
			bySystem[issue.System] = append(bySystem[issue.System], issue)
		}
		var systems []string
		for sys := range bySystem {
			systems = append(systems, sys)
		}
		sort.Slice(systems, func(i, j int) bool {
			a, b := systems[i], systems[j]
			if (a == "") != (b == "") {
				return b == ""
			}
			if len(bySystem[a]) != len(bySystem[b]) {
				return len(bySystem[a]) > len(bySystem[b])
			}
			return a < b
		})
		for _, sys := range systems {
			items := bySystem[sys]
			title := sys
			if title == "" {
				title = "No system"
			}
			epics := map[string]bool{}
			lit := false
			for _, it := range items {
				if e := epicOf(it); e != nil {
					epics[e.Slug] = true
				} else if idx.isEpic[it] {
					epics[it.Slug] = true
				}
				lit = lit || inHighlight(it)
			}
			data.Lanes = append(data.Lanes, CanvasLane{
				Kind:    "SYSTEM",
				Key:     title,
				Title:   title,
				Summary: joinParts(plural(len(items), "issue", "issues"), cs.progress(items), cs.sideCounts(items)),
				Bar:     cs.bar(items),
				Epics:   strings.Join(sortedKeys(epics), " "),
				Lit:     lit,
				Cells:   cells(items),
			})
		}
	}

	// The Epic filter unfolds its lanes; otherwise only ?open= lanes are open.
	open := map[string]bool{}
	for _, k := range opts.Open {
		open[k] = true
	}
	var openKeys []string
	for i := range data.Lanes {
		l := &data.Lanes[i]
		l.Open = epic != nil || open[l.Key]
		if l.Open && epic == nil {
			openKeys = append(openKeys, l.Key)
		}
	}
	data.OpenKeys = strings.Join(openKeys, ",")
	data.Options.Open = openKeys

	for _, v := range colKeys {
		var items []*tracker.Issue
		for _, issue := range issues {
			if issue.Version == v && !(epicMode && idx.isEpic[issue]) {
				items = append(items, issue)
			}
		}
		name := v
		if name == "" {
			name = "no version"
		}
		data.Columns = append(data.Columns, CanvasColumn{
			Name:     name,
			Progress: cs.progress(items),
			Bar:      cs.bar(items),
			Extra:    joinParts(plural(len(items), "issue", "issues"), cs.sideCounts(items)),
		})
	}

	for _, issue := range issues {
		if visible(issue) {
			data.ShownIssues++
		}
	}
	if hl != nil {
		data.HighlightName = epicShortName(hl.Title)
	}
	return data
}

// epicLanes is one lane per epic: top-level epics by open-children count,
// each followed by its sub-epics, indented.
func epicLanes(issues []*tracker.Issue, idx *epicIndex, cs *canvasStatuses, hl *tracker.Issue,
	visible func(*tracker.Issue) bool, showNoVersion bool, cells func([]*tracker.Issue) []CanvasCell) []CanvasLane {

	children := map[*tracker.Issue][]*tracker.Issue{}
	subEpics := map[*tracker.Issue][]*tracker.Issue{}
	var epics []*tracker.Issue
	for _, issue := range issues {
		if idx.isEpic[issue] {
			epics = append(epics, issue)
		}
		p := idx.parent[issue]
		if p == nil {
			continue
		}
		if idx.isEpic[issue] {
			subEpics[p] = append(subEpics[p], issue)
		} else {
			children[p] = append(children[p], issue)
		}
	}
	openKids := func(e *tracker.Issue) int {
		n := 0
		for _, c := range children[e] {
			if !cs.closed(c.Status) {
				n++
			}
		}
		return n
	}
	byOpen := func(list []*tracker.Issue) {
		sort.SliceStable(list, func(i, j int) bool {
			if a, b := openKids(list[i]), openKids(list[j]); a != b {
				return a > b
			}
			return list[i].Title < list[j].Title
		})
	}

	var lanes []CanvasLane
	seen := map[*tracker.Issue]bool{}
	var add func(e *tracker.Issue, depth int)
	add = func(e *tracker.Issue, depth int) {
		if seen[e] {
			return
		}
		seen[e] = true
		lanes = append(lanes, epicLane(e, depth, idx, cs, hl, children[e], len(subEpics[e]) > 0, visible, showNoVersion, cells))
		subs := append([]*tracker.Issue(nil), subEpics[e]...)
		byOpen(subs)
		for _, s := range subs {
			add(s, depth+1)
		}
	}
	var roots []*tracker.Issue
	for _, e := range epics {
		if idx.parent[e] == nil || !idx.isEpic[idx.parent[e]] {
			roots = append(roots, e)
		}
	}
	byOpen(roots)
	for _, e := range roots {
		add(e, 0)
	}
	// Epics in a parent cycle have no root; list them at the top level.
	rest := append([]*tracker.Issue(nil), epics...)
	byOpen(rest)
	for _, e := range rest {
		add(e, 0)
	}
	return lanes
}

// epicSubtree keeps the lane of epic slug and the sub-epic lanes under it,
// with depths starting at 0.
func epicSubtree(lanes []CanvasLane, slug string) []CanvasLane {
	var out []CanvasLane
	base := -1
	for _, l := range lanes {
		switch {
		case base < 0 && l.Slug == slug:
			base = l.Depth
		case base >= 0 && l.Depth <= base:
			return out
		case base < 0:
			continue
		}
		l.Depth -= base
		out = append(out, l)
	}
	return out
}

func epicLane(e *tracker.Issue, depth int, idx *epicIndex, cs *canvasStatuses, hl *tracker.Issue, kids []*tracker.Issue,
	hasSubEpics bool, visible func(*tracker.Issue) bool, showNoVersion bool, cells func([]*tracker.Issue) []CanvasCell) CanvasLane {

	kind := "UMBRELLA"
	if strings.EqualFold(e.Type, "epic") {
		kind = "EPIC"
	}
	var shown []*tracker.Issue
	for _, k := range kids {
		if visible(k) {
			shown = append(shown, k)
		}
	}
	hidden := len(kids) - len(shown)

	summary := plural(len(kids), "child", "children")
	if len(kids) > 0 {
		var hiddenText string
		if hidden > 0 && len(shown) > 0 {
			hiddenText = fmt.Sprintf("%d without a version", hidden)
		}
		summary = joinParts(summary, cs.progress(kids), cs.sideCounts(kids), hiddenText)
	}

	lane := CanvasLane{
		Kind:       kind,
		Key:        e.Slug,
		Slug:       e.Slug,
		Title:      e.Title,
		ShortTitle: epicShortName(e.Title),
		Status:     e.Status,
		Depth:      depth,
		Summary:    summary,
		Bar:        cs.bar(kids),
		Epics:      e.Slug,
		Lit:        hl == e,
	}
	if depth > 0 {
		lane.ParentTitle = epicShortName(idx.parent[e].Title)
	}
	switch {
	case len(kids) == 0 && hasSubEpics:
		lane.Note = "No issues directly under this epic; see its sub-epics."
	case len(kids) == 0:
		lane.Note = "No children yet. Link one with parent: " + e.Slug
	case len(shown) == 0:
		lane.Note = fmt.Sprintf("All %s have no version.", plural(len(kids), "child", "children"))
		lane.NoteNoVersion = !showNoVersion
	default:
		lane.Cells = cells(shown)
	}
	return lane
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isCanvasEpic reports whether issue is an epic on the canvas: typed `epic`
// or named as parent by another issue.
func isCanvasEpic(issue *tracker.Issue, issues []*tracker.Issue) bool {
	return newEpicIndex(issues).isEpic[issue]
}
