package main

import (
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// System pages (/systems, /systems/<System>, /systems/<System>/<Family>) join
// one system's issues, families, design doc, decisions and balance notes. They
// are read-only and rebuilt from the repo files on each request: systems come
// from the issues' `system` field, and `workbench.systems` adds a summary, a
// design doc and families (D-1, D-2).

const (
	systemRowCap = 8  // issue rows on a system page
	familyRowCap = 30 // issue rows on a family page
	recentCap    = 3  // decisions and balance notes
)

// systemGroup is one system with its issues, as found in the project.
type systemGroup struct {
	Name    string // configured key, else the most common spelling
	Members []*tracker.Issue
	Config  tracker.WorkbenchSystem
	Has     bool // configured in workbench.systems
}

// groupSystems collects the project's systems: every distinct `system` value
// (matched case-insensitively) plus the configured ones. Issues without a
// system are returned separately.
func groupSystems(issues []*tracker.Issue, wb tracker.WorkbenchConfig) (groups []*systemGroup, none []*tracker.Issue) {
	byKey := map[string]*systemGroup{}
	spellings := map[string]map[string]int{}
	for _, issue := range issues {
		name := strings.TrimSpace(issue.System)
		if name == "" {
			none = append(none, issue)
			continue
		}
		k := strings.ToLower(name)
		if byKey[k] == nil {
			byKey[k] = &systemGroup{}
			spellings[k] = map[string]int{}
		}
		byKey[k].Members = append(byKey[k].Members, issue)
		spellings[k][name]++
	}
	for k, g := range byKey {
		best := ""
		for name, n := range spellings[k] {
			if best == "" || n > spellings[k][best] || (n == spellings[k][best] && name < best) {
				best = name
			}
		}
		g.Name = best
	}
	for key, sys := range wb.Systems {
		k := strings.ToLower(key)
		if byKey[k] == nil {
			byKey[k] = &systemGroup{}
		}
		byKey[k].Name, byKey[k].Config, byKey[k].Has = key, sys, true
	}
	for _, g := range byKey {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name) })
	return groups, none
}

func findSystem(groups []*systemGroup, name string) *systemGroup {
	for _, g := range groups {
		if strings.EqualFold(g.Name, name) {
			return g
		}
	}
	return nil
}

// statusTones colours a status chip by its place in the workflow: the first
// status is neutral, the final one green, side statuses (parked, obsolete)
// muted, the first half of the way blue and the second half amber.
type statusTones struct {
	*canvasStatuses
	main []string
}

func newStatusTones(wf *tracker.WorkflowConfig) *statusTones {
	st := &statusTones{canvasStatuses: newCanvasStatuses(wf)}
	for _, s := range wf.Statuses {
		if !s.Optional {
			st.main = append(st.main, statusKey(s.Name))
		}
	}
	return st
}

func (st *statusTones) tone(status string) string {
	k := statusKey(status)
	if st.side[k] {
		return "muted"
	}
	i := -1
	for j, m := range st.main {
		if m == k {
			i = j
		}
	}
	switch {
	case i < 0:
		return ""
	case k == st.final:
		return "green"
	case i == 0:
		return ""
	case float64(i) < float64(len(st.main))/2:
		return "blue"
	default:
		return "amber"
	}
}

// SystemSeg is one status segment of a status bar.
type SystemSeg struct {
	Status string
	Count  int
	Tone   string
}

func (st *statusTones) segments(items []*tracker.Issue) []SystemSeg {
	var out []SystemSeg
	// bar() lists done first; the page reads left to right from done to idea.
	for _, seg := range st.bar(items) {
		if seg.Side {
			continue
		}
		out = append(out, SystemSeg{Status: seg.Status, Count: seg.Count, Tone: st.tone(seg.Status)})
	}
	return out
}

// counts is open, done and the side-status note ("7 obsolete") of items.
func (st *statusTones) counts(items []*tracker.Issue) (open, done int, side string) {
	for _, it := range items {
		k := statusKey(it.Status)
		switch {
		case k == st.final:
			done++
		case !st.side[k]:
			open++
		}
	}
	return open, done, st.sideCounts(items)
}

// doneTotal is done/total of items with side statuses left out (the status
// page's progressOf).
func doneTotal(st *statusTones, items []*tracker.Issue) (done, total int) {
	p := progressOf("", st.canvasStatuses, items)
	return p.Done, p.Total
}

func pct(done, total int) int {
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

// SystemCard is one card on the /systems index.
type SystemCard struct {
	Name      string
	Href      string
	Summary   string
	DesignDoc string
	Open      int
	Done      int
	Side      string
	Waiting   int
	Bar       []SystemSeg
	Families  []FamilyChip
}

// FamilyChip links a family from its system's card.
type FamilyChip struct {
	Name     string
	Href     string
	Progress string // "2 of 10 done"
}

// SystemsIndexData renders templates/systems.html.
type SystemsIndexData struct {
	Prefix      string
	ProjectName string
	Hash        string
	NavActive   string // sidebar key: "systems"

	Cards         []SystemCard
	Configured    int // systems with a workbench.systems entry
	OpenInSystems int
	Waiting       int
	NoSystemOpen  int
	NoSystemDone  int

	NotFound string // the system or family a 404 asked for
}

func systemHref(prefix, system string) string {
	return prefix + "/systems/" + url.PathEscape(system)
}

func familyHref(prefix, system, family string) string {
	return systemHref(prefix, system) + "/" + url.PathEscape(family)
}

// openIssues drops the done and side-status issues.
func openIssues(st *statusTones, items []*tracker.Issue) []*tracker.Issue {
	var out []*tracker.Issue
	for _, it := range items {
		if !st.closed(it.Status) {
			out = append(out, it)
		}
	}
	return out
}

func buildSystemsIndex(proj *tracker.Project, issues []*tracker.Issue, prefix string) SystemsIndexData {
	wf := proj.LoadWorkflow()
	st := newStatusTones(wf)
	groups, none := groupSystems(issues, wf.Workbench)
	data := SystemsIndexData{Prefix: prefix, ProjectName: proj.Name, NavActive: "systems"}
	_, data.NoSystemDone, _ = st.counts(none)
	data.NoSystemOpen = len(openIssues(st, none))

	waitingBy := map[string]int{}
	var open []*tracker.Issue
	for _, g := range groups {
		open = append(open, openIssues(st, g.Members)...)
	}
	for _, w := range waitingOnOwner(proj, open) {
		waitingBy[strings.ToLower(strings.TrimSpace(w.Issue.System))]++
	}

	bySlug := issuesBySlug(issues)
	for _, g := range groups {
		card := SystemCard{Name: g.Name, Href: systemHref(prefix, g.Name), Summary: g.Config.Summary, DesignDoc: g.Config.DesignDoc}
		card.Open, card.Done, card.Side = st.counts(g.Members)
		card.Bar = st.segments(g.Members)
		card.Waiting = waitingBy[strings.ToLower(g.Name)]
		for _, fname := range g.Config.FamilyNames() {
			fam := g.Config.Families[fname]
			kids := childrenOf(g.Members, fam.Umbrella)
			chip := FamilyChip{Name: fname, Href: familyHref(prefix, g.Name, fname)}
			if bySlug[strings.ToLower(fam.Umbrella)] != nil {
				d, t := doneTotal(st, kids)
				chip.Progress = fmt.Sprintf("%d of %d done", d, t)
			}
			card.Families = append(card.Families, chip)
		}
		if g.Has {
			data.Configured++
		}
		data.OpenInSystems += card.Open
		data.Waiting += card.Waiting
		data.Cards = append(data.Cards, card)
	}
	sort.SliceStable(data.Cards, func(i, j int) bool { return data.Cards[i].Open > data.Cards[j].Open })
	return data
}

func issuesBySlug(issues []*tracker.Issue) map[string]*tracker.Issue {
	m := make(map[string]*tracker.Issue, len(issues))
	for _, it := range issues {
		m[strings.ToLower(it.Slug)] = it
	}
	return m
}

// childrenOf is the members whose parent: (or legacy umbrella:) is umbrella.
func childrenOf(members []*tracker.Issue, umbrella string) []*tracker.Issue {
	umbrella = strings.TrimSpace(umbrella)
	if umbrella == "" {
		return nil
	}
	var out []*tracker.Issue
	for _, it := range members {
		if strings.EqualFold(tracker.ParentSlug(it), umbrella) {
			out = append(out, it)
		}
	}
	return out
}

// SystemRow is one issue row on a system or family page.
type SystemRow struct {
	Issue  *tracker.Issue
	Href   string
	Tone   string
	Agent  bool   // a tmux session works the issue
	Epic   string // "2/10" when the issue is an epic with children
	Closed bool
}

// UmbrellaView is the header's umbrella chip and link.
type UmbrellaView struct {
	Slug     string
	Href     string
	Found    bool
	Label    string // "Workbench · 2 of 10 done" or "umbrella · 2 of 10 done"
	Whole    string // "27 of 64 done in 9 systems" when the epic spans systems
	Children int
}

// FamilyCard is a configured family on its system's page.
type FamilyCard struct {
	Name     string
	Href     string
	Summary  string
	Umbrella string
	Found    bool
	Waiting  int
	Done     int
	Total    int
	Pct      int
}

// EpicCard is an epic (an issue that others name as parent:) with children in
// the system but no family entry.
type EpicCard struct {
	Title string
	Slug  string
	Href  string
	State string
	Tone  string
	Done  int
	Total int
	Pct   int
	Whole string // "The epic has 66 children in 9 systems (27 of 64 done)"
}

// DecisionItem is one recent decision.
type DecisionItem struct {
	ID       string
	Date     string
	How      string
	Text     template.HTML // **bold** kept, the rest escaped
	Replaces string
	Slug     string
	Href     string
}

// BalanceItem is one open or unsorted balance note.
type BalanceItem struct {
	Metric   string
	State    string
	Text     string
	Date     string
	Slug     string
	Href     string
	Unsorted bool
}

// WaitingChip is the header's "N waiting on you".
type WaitingChip struct {
	Count int
	Href  string
	Title string
}

// SystemPageData renders templates/system.html, for a system or a family.
type SystemPageData struct {
	Prefix      string
	ProjectName string
	Hash        string
	NavActive   string // sidebar key: "systems/<System>[/<Family>]"

	System     string
	SystemHref string
	Family     string // empty on a system page
	Title      string
	Summary    string
	Configured bool
	ConfigKey  string // "workbench.systems.UI" / "...families.Workbench"

	Open, Done int
	Side       string
	Umbrella   *UmbrellaView
	Waiting    WaitingChip

	DesignDoc     string
	DesignDocHref string
	DocInherited  bool // a family page using the system's doc

	BoardHref, ListHref, DecisionsHref string
	DecisionsTotal                     int

	Bar        []SystemSeg
	Rows       []SystemRow
	HiddenNote string // "9 idea and 7 done not shown"
	Total      int

	Families   []FamilyCard
	Epics      []EpicCard
	Unfamilied int

	Doc *DocPanel

	Decisions     []DecisionItem
	DecisionsHint int // members with a free-form ## Decisions section and none indexed

	Balance     []BalanceItem
	BalanceHref string

	// Metrics and Knobs feed the system-guard-metrics / system-knobs partials
	// (templates/_metrics_panels.html, built by systemMetricsPanels). Nil when
	// the project has no such source or the system lists none.
	Metrics any
	Knobs   any
}

func (s *Server) buildSystemPage(proj *tracker.Project, issues []*tracker.Issue, prefix string, g *systemGroup, familyKey string, fam *tracker.WorkbenchFamily) SystemPageData {
	wf := proj.LoadWorkflow()
	wb := wf.Workbench
	st := newStatusTones(wf)
	bySlug := issuesBySlug(issues)
	sessionMap, activeBots := sessionsByIssueSlug(proj, issues)

	d := SystemPageData{
		Prefix:      prefix,
		ProjectName: proj.Name,
		Hash:        issuesHash(issues, sessionMap, activeBots),
		System:      g.Name,
		SystemHref:  systemHref(prefix, g.Name),
		Title:       g.Name,
		Summary:     g.Config.Summary,
		Configured:  g.Has,
		DesignDoc:   g.Config.DesignDoc,
		BoardHref:   prefix + "/board?system=" + url.QueryEscape(g.Name),
		ListHref:    prefix + "/?system=" + url.QueryEscape(g.Name),
	}
	d.DecisionsHref = prefix + "/decisions?system=" + url.QueryEscape(g.Name)
	d.NavActive = "systems/" + g.Name
	if g.Has {
		d.ConfigKey = "workbench.systems." + g.Name
	}
	members := g.Members
	umbrella := g.Config.Umbrella
	if fam != nil {
		d.Family, d.Title, d.Summary = familyKey, familyKey, fam.Summary
		d.NavActive += "/" + familyKey
		d.ConfigKey += ".families." + familyKey
		umbrella = fam.Umbrella
		members = childrenOf(g.Members, fam.Umbrella)
		if fam.DesignDoc != "" {
			d.DesignDoc = fam.DesignDoc
		} else {
			d.DocInherited = d.DesignDoc != ""
		}
	}
	d.Total = len(members)
	d.Open, d.Done, d.Side = st.counts(members)
	d.Bar = st.segments(members)

	if umbrella != "" {
		d.Umbrella = umbrellaView(st, prefix, umbrella, bySlug, issues, members, fam != nil, familyKey)
	}

	open := openIssues(st, members)
	if items := waitingOnOwner(proj, open); len(items) > 0 {
		d.Waiting = waitingChip(prefix, items)
	}

	cap := systemRowCap
	if fam != nil {
		cap = familyRowCap
	}
	d.Rows, d.HiddenNote = systemRows(st, prefix, members, bySlug, issues, sessionMap, cap)

	if fam == nil {
		d.Families, d.Epics, d.Unfamilied = familiesAndEpics(proj, st, prefix, g, bySlug, issues)
	}

	index := tracker.BuildDecisionIndex(issues, tracker.ClosedIssueFunc(proj))
	d.Decisions, d.DecisionsTotal, d.DecisionsHint = recentDecisions(index, members, prefix)

	if d.DesignDoc != "" {
		d.DesignDocHref = designDocHref(proj, prefix, d.DesignDoc)
		d.Doc = s.docPanel(proj, issues, prefix, d.DesignDoc, d.DesignDocHref, index)
	}

	// A typed nil in an `any` field would still pass {{with}}, so only
	// panels with content are set.
	mp, kp := systemMetricsPanels(proj, issues, prefix, g.Name, familyKey)
	if mp != nil {
		d.Metrics = mp
	}
	if kp != nil {
		d.Knobs = kp
	}

	d.Balance = recentBalance(members, wb, prefix)
	if wb.HasBalance() {
		d.BalanceHref = prefix + "/balance?system=" + url.QueryEscape(g.Name)
	}
	return d
}

func umbrellaView(st *statusTones, prefix, slug string, bySlug map[string]*tracker.Issue, issues, members []*tracker.Issue, family bool, familyKey string) *UmbrellaView {
	u := &UmbrellaView{Slug: slug, Href: prefix + "/issue/" + slug}
	epic := bySlug[strings.ToLower(slug)]
	if epic == nil {
		u.Label = "umbrella not found"
		return u
	}
	u.Found, u.Slug, u.Href = true, epic.Slug, prefix+"/issue/"+epic.Slug
	all := childrenOf(issues, epic.Slug)
	local := childrenOf(members, epic.Slug)
	if family {
		local = members
	}
	u.Children = len(all)
	done, total := doneTotal(st, local)
	name := "umbrella"
	if !family && familyKey == "" {
		name = epicShortName(epic.Title)
	}
	u.Label = fmt.Sprintf("%s · %d of %d done", name, done, total)
	if len(all) > len(local) {
		wd, wt := doneTotal(st, all)
		u.Whole = fmt.Sprintf("%d of %d done in %d systems", wd, wt, systemCount(all))
	}
	return u
}

func systemCount(items []*tracker.Issue) int {
	seen := map[string]bool{}
	for _, it := range items {
		seen[strings.ToLower(strings.TrimSpace(it.System))] = true
	}
	return len(seen)
}

func waitingChip(prefix string, items []WaitingItem) WaitingChip {
	c := WaitingChip{Count: len(items), Href: prefix + "/issue/" + items[0].Issue.Slug}
	var lines []string
	for _, it := range items {
		what := it.Kind
		if it.Kind == "gate" {
			what = "approve " + it.Target
		}
		lines = append(lines, what+": "+it.Issue.Slug)
	}
	c.Title = strings.Join(lines, "\n")
	return c
}

// systemRows orders members furthest along first (newest status change
// first within a status), with done and side statuses last, and caps them.
func systemRows(st *statusTones, prefix string, members []*tracker.Issue, bySlug map[string]*tracker.Issue, issues []*tracker.Issue, sessions map[string][]AgentSession, cap int) ([]SystemRow, string) {
	type ranked struct {
		issue *tracker.Issue
		group int // 0 open, 1 done, 2 side
		pos   int
		since time.Time
	}
	var rs []ranked
	for _, it := range members {
		r := ranked{issue: it, pos: st.pos(it.Status)}
		k := statusKey(it.Status)
		switch {
		case k == st.final:
			r.group = 1
		case st.side[k]:
			r.group = 2
		}
		r.since, _ = statusSince(it)
		rs = append(rs, r)
	}
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.group != b.group {
			return a.group < b.group
		}
		if a.pos != b.pos {
			return a.pos > b.pos
		}
		if !a.since.Equal(b.since) {
			return a.since.After(b.since)
		}
		return a.issue.Slug < b.issue.Slug
	})
	var rows []SystemRow
	hidden := map[string]int{}
	var hiddenOrder []string
	for i, r := range rs {
		if i >= cap {
			k := statusKey(r.issue.Status)
			if hidden[k] == 0 {
				hiddenOrder = append(hiddenOrder, k)
			}
			hidden[k]++
			continue
		}
		row := SystemRow{Issue: r.issue, Href: prefix + "/issue/" + r.issue.Slug, Tone: st.tone(r.issue.Status), Agent: len(sessions[r.issue.Slug]) > 0, Closed: r.group > 0}
		if kids := childrenOf(issues, r.issue.Slug); len(kids) > 0 {
			d, t := doneTotal(st, kids)
			row.Epic = fmt.Sprintf("%d/%d", d, t)
		}
		rows = append(rows, row)
	}
	var parts []string
	for _, k := range hiddenOrder {
		parts = append(parts, fmt.Sprintf("%d %s", hidden[k], k))
	}
	note := ""
	if len(parts) > 0 {
		note = joinAnd(parts) + " not shown"
	}
	return rows, note
}

// joinAnd is "a, b and c".
func joinAnd(parts []string) string {
	if len(parts) <= 1 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func familiesAndEpics(proj *tracker.Project, st *statusTones, prefix string, g *systemGroup, bySlug map[string]*tracker.Issue, issues []*tracker.Issue) ([]FamilyCard, []EpicCard, int) {
	covered := map[*tracker.Issue]bool{} // in a family or epic, or its umbrella
	familyUmbrellas := map[string]bool{}
	var fams []FamilyCard
	for _, fname := range g.Config.FamilyNames() {
		fam := g.Config.Families[fname]
		kids := childrenOf(g.Members, fam.Umbrella)
		card := FamilyCard{Name: fname, Href: familyHref(prefix, g.Name, fname), Summary: fam.Summary, Umbrella: fam.Umbrella}
		if u := bySlug[strings.ToLower(fam.Umbrella)]; u != nil {
			card.Found, card.Umbrella = true, u.Slug
			covered[u] = true
		}
		familyUmbrellas[strings.ToLower(fam.Umbrella)] = true
		card.Done, card.Total = doneTotal(st, kids)
		card.Pct = pct(card.Done, card.Total)
		card.Waiting = len(waitingOnOwner(proj, openIssues(st, kids)))
		for _, k := range kids {
			covered[k] = true
		}
		fams = append(fams, card)
	}

	byParent := map[string][]*tracker.Issue{}
	var parents []string
	for _, it := range g.Members {
		p := strings.ToLower(tracker.ParentSlug(it))
		if p == "" || familyUmbrellas[p] || bySlug[p] == nil {
			continue
		}
		if byParent[p] == nil {
			parents = append(parents, p)
		}
		byParent[p] = append(byParent[p], it)
		covered[it] = true
	}
	var epics []EpicCard
	for _, p := range parents {
		epic := bySlug[p]
		covered[epic] = true
		local := byParent[p]
		e := EpicCard{Title: epic.Title, Slug: epic.Slug, Href: prefix + "/issue/" + epic.Slug, State: epic.Status, Tone: st.tone(epic.Status)}
		e.Done, e.Total = doneTotal(st, local)
		e.Pct = pct(e.Done, e.Total)
		if all := childrenOf(issues, epic.Slug); len(all) > len(local) {
			wd, wt := doneTotal(st, all)
			e.Whole = fmt.Sprintf("The epic has %d children in %d systems (%d of %d done); %d of them are %s.", len(all), systemCount(all), wd, wt, len(local), g.Name)
		}
		epics = append(epics, e)
	}
	sort.SliceStable(epics, func(i, j int) bool { return epics[i].Total > epics[j].Total })

	rest := 0
	for _, it := range openIssues(st, g.Members) {
		if !covered[it] {
			rest++
		}
	}
	return fams, epics, rest
}

func recentDecisions(index *tracker.DecisionIndex, members []*tracker.Issue, prefix string) ([]DecisionItem, int, int) {
	in := map[*tracker.Issue]bool{}
	for _, it := range members {
		in[it] = true
	}
	now := time.Now()
	var items []DecisionItem
	total := 0
	indexed := map[*tracker.Issue]bool{}
	for _, e := range index.Entries { // newest first
		if !in[e.Issue] {
			continue
		}
		total++
		indexed[e.Issue] = true
		if len(items) >= recentCap {
			continue
		}
		item := DecisionItem{ID: e.ID, Date: decisionDateLabel(e.Date, now), How: e.How, Text: boldOnly(e.Text), Slug: e.Issue.Slug, Href: prefix + "/issue/" + e.Issue.Slug}
		if len(e.Replaces) > 0 {
			_, id, _ := strings.Cut(e.Replaces[0], "#")
			item.Replaces = "replaces " + id
		}
		items = append(items, item)
	}
	hint := 0
	if total == 0 {
		for _, it := range members {
			if _, ok := tracker.SectionText(it.BodyRaw, "Decisions"); ok && !indexed[it] {
				hint++
			}
		}
	}
	return items, total, hint
}

func recentBalance(members []*tracker.Issue, wb tracker.WorkbenchConfig, prefix string) []BalanceItem {
	bp := wb.Balance.CommentPrefix
	if bp == "" {
		bp = tracker.DefaultBalancePrefix
	}
	board := tracker.BuildBalanceBoard(members, loadIssueCommentsQuiet, bp, func(*tracker.Issue) bool { return false })
	now := time.Now()
	var items []BalanceItem
	for _, n := range board.Notes {
		if !n.IsOpen() {
			continue
		}
		text := n.Hypothesis
		if text == "" {
			text = n.Change
		}
		items = append(items, BalanceItem{Metric: n.Metric, State: n.State, Text: text, Date: n.Date, Slug: n.Slug, Href: prefix + "/issue/" + n.Slug})
	}
	for _, l := range board.Unsorted {
		items = append(items, BalanceItem{State: "unsorted", Text: shortText(l.Text, 180), Date: l.Date, Slug: l.Slug, Href: prefix + "/issue/" + l.Slug, Unsorted: true})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Date > items[j].Date })
	if len(items) > recentCap {
		items = items[:recentCap]
	}
	for i := range items {
		items[i].Date = shortDateLabel(items[i].Date, now)
	}
	return items
}

func shortText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// designDocHref links a design doc to its doc page when it sits in the docs
// dir; elsewhere it is shown as plain text.
func designDocHref(proj *tracker.Project, prefix, doc string) string {
	if proj.DocsDir == "" {
		return ""
	}
	abs := filepath.Join(projectRoot(proj), doc)
	rel, err := filepath.Rel(proj.DocsDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || !strings.HasSuffix(rel, ".md") {
		return ""
	}
	if _, err := os.Stat(abs); err != nil {
		return ""
	}
	return prefix + "/docs/" + strings.TrimSuffix(filepath.ToSlash(rel), ".md")
}
