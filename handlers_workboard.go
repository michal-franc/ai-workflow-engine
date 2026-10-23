package main

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/testids"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The workboard (/p/<project>/workboard) is the board in the workbench frame:
// read-only cards that carry coordination state (markers, epic progress, the
// agent on the card). The classic /board keeps drag-and-drop until the retire
// child removes it.
//
// It stays readable with hundreds of issues (D-10): the idle piles (the first
// status, parked, done) fold to count tiles, it opens on the cards that need
// attention, quiet cards shrink to one line under Show all, and a column past
// workboardColumnCap links to the list instead of growing.

// workboardColumnCap is how many cards a column shows before linking the rest.
const workboardColumnCap = 12

// staleAfter turns a card's time-in-status amber, outside the first status,
// done and global side statuses (parked).
const staleAfter = 24 * time.Hour

// WorkCard is one card on the workboard.
type WorkCard struct {
	Slug        string
	Title       string
	System      string
	Status      string
	StatusColor string
	Epic        bool
	Age         string // time in the current status
	AgeTitle    string
	AgeOld      bool
	Progress    *CardProgress
	Markers     []Marker
	Agent       *CardAgent
	Search      string // lower-case title, slug and system for the search box
	// Quiet: no marker, no agent, not an epic, and not old in an active
	// status. Quiet cards are hidden until Show all, then drawn as one line.
	Quiet   bool
	idle    bool   // in an idle pile: the first status, parked or done
	parent  string // the open epic the card belongs to, if any
	waiting bool
	blocked bool
	since   time.Time
}

// CardAgent is the card footer: the agent on the issue and what it is doing.
type CardAgent struct {
	State string // working, idle, gate, question, playtest or stopped
	Tone  string // green, grey, amber or stopped (hollow dot)
	Text  string
	Title string
	Href  string
}

// WorkColumn is one column: a status, a system or an epic.
type WorkColumn struct {
	Key   string // test id suffix
	Name  string
	Href  string // epic columns link the epic
	Note  string // epic columns: "shipping · 2 of 10 done"
	Count int
	Cards []*WorkCard
	// Folded columns (the idle piles) show only a count tile linking FoldHref,
	// with how many of them wait on the owner.
	Folded      bool
	FoldHref    string
	FoldWaiting int
	Progress    *CardProgress
	// MoreHref lists the whole column (the list view, or the epic's page)
	// once it has more than workboardColumnCap cards.
	MoreHref string
	// Quiet is how many of the column's cards Needs attention hides; the
	// column links them with AllHref.
	Quiet   int
	AllHref string
}

// Shown is the cards drawn on the board: the first workboardColumnCap.
func (c *WorkColumn) Shown() []*WorkCard {
	if len(c.Cards) <= workboardColumnCap {
		return c.Cards
	}
	return c.Cards[:workboardColumnCap]
}

// Hidden is how many cards the column's "N more" link leads to.
func (c *WorkColumn) Hidden() int {
	if len(c.Cards) <= workboardColumnCap {
		return 0
	}
	return len(c.Cards) - workboardColumnCap
}

// WorkboardData is the workboard page.
type WorkboardData struct {
	Prefix       string
	ProjectName  string
	Hash         string
	Group        string // status, system or epic
	Columns      []*WorkColumn
	Versions     []string
	Version      string
	System       string
	WaitingOnly  bool
	WaitingCount int
	// ShowAll also draws the quiet cards; QuietCount is how many are hidden
	// without it.
	ShowAll    bool
	QuietCount int
	Total      int
}

// GroupHref is the page URL with group set and the other filters kept.
func (d WorkboardData) GroupHref(group string) string {
	return d.href(group, d.WaitingOnly, d.ShowAll)
}

// WaitingHref toggles the waiting-on-you filter.
func (d WorkboardData) WaitingHref() string {
	return d.href(d.Group, !d.WaitingOnly, d.ShowAll)
}

// AllHref toggles Show all (quiet cards too) and Needs attention.
func (d WorkboardData) AllHref() string {
	return d.href(d.Group, d.WaitingOnly, !d.ShowAll)
}

func (d WorkboardData) href(group string, waiting, all bool) string {
	var q []string
	if group != "" && group != "status" {
		q = append(q, "group="+group)
	}
	if d.Version != "" {
		q = append(q, "version="+d.Version)
	}
	if d.System != "" {
		q = append(q, "system="+d.System)
	}
	if waiting {
		q = append(q, "waiting=1")
	}
	if all {
		q = append(q, "all=1")
	}
	h := d.Prefix + "/workboard"
	if len(q) > 0 {
		h += "?" + strings.Join(q, "&")
	}
	return h
}

func (s *Server) handleWorkboard(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	data := WorkboardData{
		Prefix:      prefix,
		ProjectName: proj.Name,
		Group:       q.Get("group"),
		Version:     q.Get("version"),
		System:      q.Get("system"),
		WaitingOnly: q.Get("waiting") == "1",
		ShowAll:     q.Get("all") == "1",
	}
	if data.Group != "system" && data.Group != "epic" {
		data.Group = "status"
	}
	sessionMap, activeBots := sessionsByIssueSlug(proj, issues)
	data.Hash = issuesHash(issues, sessionMap, activeBots)

	versions := map[string]bool{}
	for _, issue := range issues {
		if issue.Version != "" {
			versions[issue.Version] = true
		}
	}
	for v := range versions {
		data.Versions = append(data.Versions, v)
	}
	sort.Strings(data.Versions)

	now := time.Now()
	wf := proj.LoadWorkflow()
	mctx := newMarkerCtx(proj, prefix, issues, now)
	agents := newCardAgents(proj, prefix, now)

	var cards []*WorkCard
	for _, issue := range issues {
		if data.Version != "" && issue.Version != data.Version {
			continue
		}
		if data.System != "" && !strings.EqualFold(issue.System, data.System) {
			continue
		}
		card := buildWorkCard(proj, prefix, issue, issues, mctx, agents, now)
		if card.waiting {
			data.WaitingCount++
		}
		if data.WaitingOnly && !card.waiting {
			continue
		}
		cards = append(cards, card)
	}

	// The idle piles fold to tiles in the status view (they are counted
	// there, quiet or not); elsewhere only their waiting cards show.
	var shown []*WorkCard
	quietBy := map[string]int{}
	for _, c := range cards {
		switch {
		case c.idle:
			if data.Group == "status" || !c.waiting {
				continue
			}
		case c.Quiet && !data.ShowAll:
			data.QuietCount++
			quietBy[columnKey(data.Group, c)]++
			continue
		}
		shown = append(shown, c)
	}
	data.Total = len(shown)

	switch data.Group {
	case "system":
		data.Columns = columnsBySystem(prefix, shown)
	case "epic":
		data.Columns = columnsByEpic(prefix, shown, issues)
	default:
		data.Columns = columnsByStatus(prefix, wf, shown, cards, data.System)
	}
	for _, col := range data.Columns {
		if col.Quiet = quietBy[col.Key]; col.Quiet > 0 {
			col.AllHref = data.AllHref()
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "workboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buildWorkCard(proj *tracker.Project, prefix string, issue *tracker.Issue, issues []*tracker.Issue, mctx *markerCtx, agents *cardAgents, now time.Time) *WorkCard {
	wf := proj.LoadWorkflowForIssue(issue)
	card := &WorkCard{
		Slug:        issue.Slug,
		Title:       issue.Title,
		System:      issue.System,
		Status:      issue.Status,
		StatusColor: statusColorOf(issue.Status),
		Epic:        tracker.IsEpic(issue, issues),
		Progress:    epicProgress(prefix, issue, issues),
		Search:      strings.ToLower(issue.Title + " " + issue.Slug + " " + issue.System),
	}
	if p := tracker.FindIssue(issues, tracker.ParentSlug(issue)); p != nil && p.Status != "done" {
		card.parent = p.Slug
	}
	att := mctx.attention[issue.Slug]
	since, approx := att.Since, att.SinceApprox
	if since.IsZero() {
		since, approx = statusSince(issue)
	}
	card.since = since
	card.Age = agoText(now.Sub(since))
	card.AgeTitle = "In " + issue.Status + " since " + since.Format("2006-01-02 15:04")
	if approx {
		card.AgeTitle = "Last file change " + since.Format("2006-01-02 15:04") + " (no recorded transition)"
	}
	card.idle = idleStatus(wf, issue.Status)
	card.AgeOld = now.Sub(since) >= staleAfter && !card.idle
	if issue.Status != "done" {
		card.Markers = cardMarkers(mctx, issue)
	} else {
		card.Markers = ciMarkers(mctx, issue)
	}
	for _, m := range card.Markers {
		card.waiting = card.waiting || m.Waiting()
		card.blocked = card.blocked || m.Tone == "red"
	}
	card.Agent = agents.footer(wf, issue, att)
	// An old card needs attention only where work should be moving (in
	// progress up to done); an old design or backlog item is just waiting.
	stuck := card.AgeOld && activeStatus(wf, issue.Status)
	card.Quiet = len(card.Markers) == 0 && card.Agent == nil && !stuck && !card.Epic
	return card
}

// columnKey is the column a card lands in under group (its WorkColumn.Key).
// An epic child's column is its parent; other cards go to "none".
func columnKey(group string, c *WorkCard) string {
	switch group {
	case "system":
		if c.System == "" {
			return "none"
		}
		return testids.Segment(c.System)
	case "epic":
		if c.parent != "" {
			return testids.Segment(c.parent)
		}
		return "none"
	}
	return testids.Segment(c.Status)
}

// idleStatus reports an idle pile: the first status (the idea pile), done, or
// a global side status such as parked. Idle cards never turn amber with age,
// and the status view folds them to a count tile.
func idleStatus(wf *tracker.WorkflowConfig, status string) bool {
	order := wf.GetStatusOrder()
	if len(order) > 0 && strings.EqualFold(order[0], status) {
		return true
	}
	if strings.EqualFold(status, "done") {
		return true
	}
	if st := wf.GetStatus(status); st != nil && st.Global {
		return true
	}
	return false
}

// listHref is the list view filtered on status and system.
func listHref(prefix, status, system string) string {
	var q []string
	if status != "" {
		q = append(q, "status="+url.QueryEscape(status))
	}
	if system != "" {
		q = append(q, "system="+url.QueryEscape(system))
	}
	if len(q) == 0 {
		return prefix + "/"
	}
	return prefix + "/?" + strings.Join(q, "&")
}

// cardAgents resolves the footer for each card from the project's tmux
// sessions, with the same rules as the agents canvas (workerState).
type cardAgents struct {
	prefix   string
	projSlug string
	now      time.Time
	workDir  string
	live     map[string]bool
}

func newCardAgents(proj *tracker.Project, prefix string, now time.Time) *cardAgents {
	ca := &cardAgents{prefix: prefix, projSlug: proj.Slug, now: now, workDir: resolveProjectWorkDir(proj), live: map[string]bool{}}
	for _, sess := range projectSessions(proj) {
		ca.live[agentrun.BareName(sess.Name)] = true
	}
	return ca
}

// footer is the card's agent line, or nil when no agent applies (none live,
// and nothing unfinished left behind).
func (ca *cardAgents) footer(wf *tracker.WorkflowConfig, issue *tracker.Issue, att Attention) *CardAgent {
	name := tracker.AgentSessionName(issue.Slug)
	href := ca.prefix + "/agents?agent=" + name
	wt, _, _ := resolveWorktree(ca.workDir, issue.Slug, wf)
	if !ca.live[name] {
		if issue.Status == "done" {
			return nil
		}
		if activeStatus(wf, issue.Status) || (wt != "" && fileExists(wt)) {
			return &CardAgent{State: "stopped", Tone: "stopped", Text: "agent stopped · not running", Href: href,
				Title: name + " has no tmux session; dispatch it again from the issue"}
		}
		return nil
	}
	comments, _ := tracker.LoadComments(issue.FilePath)
	call, known := lastCliCall(ca.projSlug, agentLogPaths(ca.workDir, wt, name, issue.Assignee)...)
	state, _ := workerState(wf, issue, comments, call, known, ca.now)
	// The canvas calls any pending approval "at gate"; on a card only a ready
	// gate waits on the owner, otherwise the agent is still at work.
	if state == "at-gate" && (att.Gate == nil || !att.Gate.Ready) {
		state = "working"
		if known && ca.now.Sub(call.At) >= agentIdleAfter {
			state = "idle"
		}
	}
	if (state == "working" || state == "idle" || state == "at-gate") && att.PlaytestDue {
		state = "playtest"
	}
	a := &CardAgent{State: state, Href: href, Title: name}
	if known {
		a.Title = name + " · last issue-cli call " + call.At.Local().Format("15:04") + ": " + call.Cmd
	}
	lastText := ""
	if known {
		lastText = " · last call " + agoText(ca.now.Sub(call.At)) + " ago"
		if ca.now.Sub(call.At) < time.Minute {
			lastText = " · last call just now"
		}
	}
	switch state {
	case "question":
		a.Tone, a.Text = "amber", "agent asked you a question"
	case "at-gate":
		a.State, a.Tone, a.Text = "gate", "amber", "agent waiting on your approval"
	case "playtest":
		a.Tone, a.Text = "amber", "agent waiting on your playtest"
	case "idle":
		a.Tone, a.Text = "grey", "agent idle"+lastText
		a.Title += " (no issue-cli call for " + agoText(ca.now.Sub(call.At)) + ")"
	default:
		a.Tone, a.Text = "green", "agent working"+lastText
	}
	return a
}

// sortCards puts what waits on the owner first, then what is blocked, then
// the rest by longest time in status.
func sortCards(cards []*WorkCard) {
	rank := func(c *WorkCard) int {
		switch {
		case c.waiting:
			return 0
		case c.blocked:
			return 1
		}
		return 2
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if ri, rj := rank(cards[i]), rank(cards[j]); ri != rj {
			return ri < rj
		}
		return cards[i].since.Before(cards[j].since)
	})
}

// columnsByStatus gives each status a column in workflow order. The idle piles
// fold to count tiles over all (every matching card, quiet or not): the first
// status at the start, global statuses and done at the end.
func columnsByStatus(prefix string, wf *tracker.WorkflowConfig, shown, all []*WorkCard, system string) []*WorkColumn {
	byStatus := map[string][]*WorkCard{}
	for _, c := range shown {
		byStatus[c.Status] = append(byStatus[c.Status], c)
	}
	idleCount, idleWaiting := map[string]int{}, map[string]int{}
	seen := map[string]bool{}
	for _, c := range all {
		seen[c.Status] = true
		if c.idle {
			idleCount[c.Status]++
			if c.waiting {
				idleWaiting[c.Status]++
			}
		}
	}
	column := func(st string) *WorkColumn {
		col := &WorkColumn{Key: testids.Segment(st), Name: statusTitle(st)}
		if idleStatus(wf, st) {
			col.Folded, col.Count, col.FoldWaiting = true, idleCount[st], idleWaiting[st]
			col.FoldHref = listHref(prefix, st, system)
			return col
		}
		col.Cards, col.Count = byStatus[st], len(byStatus[st])
		col.MoreHref = listHref(prefix, st, system)
		sortCards(col.Cards)
		return col
	}
	order := wf.GetBoardColumns()
	listed := map[string]bool{}
	for _, st := range order {
		listed[st] = true
	}
	var extra []string
	for st := range seen {
		if !listed[st] {
			extra = append(extra, st)
		}
	}
	sort.Strings(extra)
	order = append(append([]string{}, order...), extra...)

	var head, body, tail, done []*WorkColumn
	for i, st := range order {
		col := column(st)
		switch {
		case !col.Folded:
			body = append(body, col)
		case i == 0:
			head = append(head, col)
		case strings.EqualFold(st, "done"):
			done = append(done, col)
		default:
			tail = append(tail, col)
		}
	}
	return append(append(append(head, body...), tail...), done...)
}

func columnsBySystem(prefix string, cards []*WorkCard) []*WorkColumn {
	bySystem := map[string][]*WorkCard{}
	for _, c := range cards {
		if c.Status == "done" {
			continue
		}
		bySystem[c.System] = append(bySystem[c.System], c)
	}
	var names []string
	for name := range bySystem {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	var cols []*WorkColumn
	for _, name := range names {
		sortCards(bySystem[name])
		cols = append(cols, &WorkColumn{Key: testids.Segment(name), Name: name, Cards: bySystem[name], Count: len(bySystem[name]), MoreHref: listHref(prefix, "", name)})
	}
	if none := bySystem[""]; len(none) > 0 {
		sortCards(none)
		cols = append(cols, &WorkColumn{Key: "none", Name: "No system", Cards: none, Count: len(none), MoreHref: prefix + "/"})
	}
	return cols
}

// columnsByEpic gives every epic with open children a column (most open
// children first) and puts the rest under "No epic". A nested epic is a card
// in its parent's column and has a column of its own.
func columnsByEpic(prefix string, cards []*WorkCard, issues []*tracker.Issue) []*WorkColumn {
	byEpic := map[string][]*WorkCard{}
	var none []*WorkCard
	for _, c := range cards {
		if c.Status == "done" {
			continue
		}
		issue := tracker.FindIssue(issues, c.Slug)
		parent := tracker.FindIssue(issues, tracker.ParentSlug(issue))
		if parent != nil && parent.Status != "done" {
			byEpic[parent.Slug] = append(byEpic[parent.Slug], c)
			continue
		}
		if c.Epic && hasOpenChild(issues, c.Slug) {
			continue // shown as its own column's header
		}
		none = append(none, c)
	}
	var cols []*WorkColumn
	for slug, list := range byEpic {
		epic := tracker.FindIssue(issues, slug)
		p := epicProgress(prefix, epic, issues)
		sortCards(list)
		col := &WorkColumn{Key: testids.Segment(slug), Name: epic.Title, Href: prefix + "/issue/" + epic.Slug, Cards: list, Count: len(list), Progress: p, MoreHref: prefix + "/issue/" + epic.Slug}
		col.Note = epic.Status
		cols = append(cols, col)
	}
	sort.Slice(cols, func(i, j int) bool {
		if cols[i].Count != cols[j].Count {
			return cols[i].Count > cols[j].Count
		}
		return cols[i].Name < cols[j].Name
	})
	if len(none) > 0 {
		sortCards(none)
		cols = append(cols, &WorkColumn{Key: "none", Name: "No epic", Cards: none, Count: len(none), MoreHref: prefix + "/"})
	}
	return cols
}

func hasOpenChild(issues []*tracker.Issue, slug string) bool {
	for _, c := range tracker.EpicChildren(issues, slug) {
		if c.Status != "done" {
			return true
		}
	}
	return false
}

// statusTitle capitalises a status for a column heading ("in design" → "In design").
func statusTitle(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
