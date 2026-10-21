package main

import (
	"net/http"
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

// workboardColumnCap is how many cards a column shows before "N more".
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
	Progress    *EpicProgress
	Markers     []Marker
	Agent       *CardAgent
	Search      string // lower-case title, slug and system for the search box
	waiting     bool
	blocked     bool
	since       time.Time
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
	// Folded columns (Done) show only the count, linking FoldHref.
	Folded   bool
	FoldHref string
	Progress *EpicProgress
}

// Hidden is how many cards sit behind the column's "N more".
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
	Total        int
	Cap          int
}

// GroupHref is the page URL with group set and the other filters kept.
func (d WorkboardData) GroupHref(group string) string {
	return d.href(group, d.WaitingOnly)
}

// WaitingHref toggles the waiting-on-you filter.
func (d WorkboardData) WaitingHref() string {
	return d.href(d.Group, !d.WaitingOnly)
}

func (d WorkboardData) href(group string, waiting bool) string {
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
		Cap:         workboardColumnCap,
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
	data.Total = len(cards)

	switch data.Group {
	case "system":
		data.Columns = columnsBySystem(cards)
	case "epic":
		data.Columns = columnsByEpic(prefix, cards, issues)
	default:
		data.Columns = columnsByStatus(prefix, wf, cards)
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
	card.AgeOld = now.Sub(since) >= staleAfter && ageCounts(wf, issue.Status)
	if issue.Status != "done" {
		card.Markers = cardMarkers(mctx, issue)
	}
	for _, m := range card.Markers {
		card.waiting = card.waiting || m.Waiting()
		card.blocked = card.blocked || m.Tone == "red"
	}
	card.Agent = agents.footer(wf, issue, att)
	return card
}

// ageCounts reports whether time in status can go amber: not in the first
// status (the idea pile), done, or a global side status such as parked.
func ageCounts(wf *tracker.WorkflowConfig, status string) bool {
	order := wf.GetStatusOrder()
	if len(order) > 0 && strings.EqualFold(order[0], status) {
		return false
	}
	if strings.EqualFold(status, "done") {
		return false
	}
	if st := wf.GetStatus(status); st != nil && st.Global {
		return false
	}
	return true
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

func columnsByStatus(prefix string, wf *tracker.WorkflowConfig, cards []*WorkCard) []*WorkColumn {
	byStatus := map[string][]*WorkCard{}
	for _, c := range cards {
		byStatus[c.Status] = append(byStatus[c.Status], c)
	}
	var cols []*WorkColumn
	added := map[string]bool{}
	add := func(st string) {
		added[st] = true
		col := &WorkColumn{Key: testids.Segment(st), Name: statusTitle(st), Cards: byStatus[st], Count: len(byStatus[st])}
		if strings.EqualFold(st, "done") {
			col.Folded, col.FoldHref, col.Cards = true, prefix+"/?status=done", nil
		}
		sortCards(col.Cards)
		cols = append(cols, col)
	}
	for _, st := range wf.GetBoardColumns() {
		if !strings.EqualFold(st, "done") {
			add(st)
		}
	}
	var extra []string
	for st := range byStatus {
		if !added[st] && !strings.EqualFold(st, "done") {
			extra = append(extra, st)
		}
	}
	sort.Strings(extra)
	for _, st := range extra {
		add(st)
	}
	add("done")
	return cols
}

func columnsBySystem(cards []*WorkCard) []*WorkColumn {
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
		cols = append(cols, &WorkColumn{Key: testids.Segment(name), Name: name, Cards: bySystem[name], Count: len(bySystem[name])})
	}
	if none := bySystem[""]; len(none) > 0 {
		sortCards(none)
		cols = append(cols, &WorkColumn{Key: "none", Name: "No system", Cards: none, Count: len(none)})
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
		col := &WorkColumn{Key: testids.Segment(slug), Name: epic.Title, Href: prefix + "/issue/" + epic.Slug, Cards: list, Count: len(list), Progress: p}
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
		cols = append(cols, &WorkColumn{Key: "none", Name: "No epic", Cards: none, Count: len(none)})
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
