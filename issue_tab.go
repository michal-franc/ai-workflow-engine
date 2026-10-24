package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/checks"
	"github.com/michal-franc/issue-viewer/internal/testids"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Issue tab (ui/issue-tab-brief-needs-you-bar-and-a-folded-body): a band
// that says what the issue is, where it is on its path and what it needs from
// the owner, then what the owner is approving since the last gate, mockups,
// decisions, the body folded by section and a side column. Every part reads
// data recorded today; buildIssueTab reads nothing from disk itself.

// IssueTab is the Issue tab's model.
type IssueTab struct {
	Brief string // markdown; "" when the body has no lead paragraph
	Words *briefWords
	Epic  *CardProgress
	Path  []pathStep
	Time  briefTime
	Needs needsYou

	Approving   approving
	Mockups     []mockupThumb
	MockupsURL  string
	Decisions   []briefDecision
	Recommended int

	// Fold summarises each "## " section for the folded body, keyed in the
	// page by its heading; NotStarted are the sections later statuses add.
	Fold       []foldSection
	NotStarted []string

	Agent        *agentCard
	Criteria     []tracker.CheckboxItem
	CriteriaDone int
}

// briefWords is the owner's own words next to the brief.
type briefWords struct {
	Quote string
	Who   string // "You, lead chat, 23 Oct"
	Ref   string // "D-1", "epic D-1", "approval"
	Href  string
}

// pathStep is one status on the issue's path.
type pathStep struct {
	Status   string
	State    string // done, current, next
	Gate     bool   // entered through the owner's approval
	Playtest bool   // the owner's playtest happens here
	Dur      time.Duration
}

type briefTime struct {
	Known                   bool // agent and waiting time could be split
	Total, Working, Waiting time.Duration
	InFlight                bool
}

// needsYou is the band's third column: what the owner has to do now, or,
// quiet, who has the issue.
type needsYou struct {
	State string // gate, playtest, checks, question, working, idle, stopped, queued, parked, done, none
	Amber bool
	Label string // "Needs you", "With the agent", "Parked", …
	Title string
	Text  string
	Meta  string // the quiet states' right-hand label

	Targets  []string // gate: approve buttons, primary first
	Command  string   // gate: the lead's approve line; playtest, checks: the command to run
	Sessions []string
	Question string
	AskedAt  time.Time

	Since  time.Time
	Approx bool
	Wait   string
	Also   []string

	OpenTmux string // idle: the agent session Open in tmux opens
}

// approving is "What you're approving": what was recorded since the last
// gate the owner passed, or since dispatch.
type approving struct {
	Since  time.Time
	From   string // "dispatch", "in design → in progress"
	Counts []approvingCount
	Events []tlEvent
	More   int
	Known  bool // a CLI log was read
}

type approvingCount struct {
	Key    string
	N      int
	Label  string
	Detail string
}

type mockupThumb struct {
	Title string
	Src   string
}

type briefDecision struct {
	ID          string
	Text        string
	Quote       string
	Recommended bool
}

// foldSection is one "## " section's folded row.
type foldSection struct {
	Key   string   `json:"key"` // lower-cased heading text, matched against the rendered <h2>
	ID    string   `json:"id"`  // test id segment and URL hash
	Title string   `json:"title"`
	Done  int      `json:"done"`
	Total int      `json:"total"`
	Lead  string   `json:"lead"`
	Subs  []string `json:"subs,omitempty"`
}

type agentCard struct {
	Name    string
	Href    string
	State   string
	Tone    string
	Text    string
	Live    bool
	Known   bool
	LastAt  time.Time
	LastCmd string
	Calls   int
	Since   time.Time
}

// issueTabInput is what buildIssueTab needs: gathered by the handler, kept
// apart so tests build the tab without a project on disk.
type issueTabInput struct {
	Project string
	Issue   *tracker.Issue
	WF      *tracker.WorkflowConfig
	Src     timelineSources
	TL      Timeline
	Att     Attention
	Targets []string // the current status's required approvals (issueApprovals)
	Parent  *tracker.Issue
	Epic    *CardProgress
	Footer  *CardAgent // the board card's agent line; nil when no agent applies
	Call    lastCall
	Known   bool
	Now     time.Time
	Prefix  string
	// ImgSrc maps an image path written in the body to its URL.
	ImgSrc func(string) string
}

const (
	foldLeadMax     = 160
	approvingEvents = 10
)

func buildIssueTab(in issueTabInput) IssueTab {
	issue := in.Issue
	t := IssueTab{
		Brief: tracker.Brief(issue.BodyRaw),
		Words: yourWords(in),
		Epic:  in.Epic,
		Path:  issuePath(in.WF, issue, in.TL),
		Time:  briefTime{Known: in.TL.Known, Total: in.TL.Total, Working: in.TL.Working, Waiting: in.TL.Waiting, InFlight: in.TL.InFlight},
	}
	t.Approving = approvingSince(in.Src, in.TL)
	t.Mockups = mockupThumbs(issue.BodyRaw, in.ImgSrc)
	for _, f := range issue.ExtraFields {
		if f.Key == "mockups" && f.IsURL {
			t.MockupsURL = f.Value
		}
	}
	for _, d := range in.Src.Decisions {
		bd := briefDecision{ID: d.ID, Text: d.Text, Quote: unquote(d.Quote)}
		bd.Recommended = strings.Contains(bd.Quote, "(Recommended)")
		if bd.Recommended {
			t.Recommended++
		}
		t.Decisions = append(t.Decisions, bd)
	}
	t.Fold = foldSections(issue.BodyRaw, in.Src.Boxes, len(t.Decisions))
	t.NotStarted = notStartedSections(in.WF, issue)
	t.Agent = agentCardFor(in)
	for _, b := range in.Src.Boxes {
		if strings.EqualFold(b.Section, "Acceptance Criteria") {
			t.Criteria = append(t.Criteria, b)
			if b.Checked {
				t.CriteriaDone++
			}
		}
	}
	t.Needs = needsYouFor(in, t)
	return t
}

// yourWords picks the owner's words for the brief: the first decision quote
// on the issue the owner said (not an agent's record of a "(Recommended)"
// pick), then the newest approval words, then the parent epic's.
func yourWords(in issueTabInput) *briefWords {
	if w := ownerQuote(in.Src.Decisions, ""); w != nil {
		return w
	}
	for i := len(in.Src.Comments) - 1; i >= 0; i-- {
		if a, ok := tracker.ParseApprovalComment(in.Src.Comments[i].Text); ok && strings.TrimSpace(a.Words) != "" {
			return &briefWords{Quote: a.Words, Who: whoSaid(a.How, a.Date), Ref: "approval"}
		}
	}
	if in.Parent != nil {
		if w := ownerQuote(tracker.ParseDecisions(in.Parent.Slug, in.Parent.BodyRaw), "epic "); w != nil {
			w.Href = in.Prefix + "/issue/" + in.Parent.Slug
			return w
		}
	}
	return nil
}

func ownerQuote(decisions []tracker.Decision, refPrefix string) *briefWords {
	for _, d := range decisions {
		q := unquote(d.Quote)
		if q == "" || strings.EqualFold(d.How, "recorded by agent") || strings.Contains(q, "(Recommended)") {
			continue
		}
		date := ""
		if !d.Date.IsZero() {
			date = d.Date.Format("2 Jan")
		}
		return &briefWords{Quote: q, Who: whoSaid(d.How, date), Ref: refPrefix + d.ID}
	}
	return nil
}

func whoSaid(how, date string) string {
	parts := []string{"You"}
	if how != "" {
		parts = append(parts, how)
	}
	if date != "" {
		parts = append(parts, date)
	}
	return strings.Join(parts, ", ")
}

func unquote(q string) string {
	q = strings.TrimSpace(q)
	if s, err := strconv.Unquote(q); err == nil {
		return s
	}
	return strings.Trim(q, `"`)
}

// issuePath is the type's path with the owner's gates and the time spent in
// each status.
func issuePath(wf *tracker.WorkflowConfig, issue *tracker.Issue, tl Timeline) []pathStep {
	var order []string
	for _, s := range wf.Statuses {
		if !s.Optional {
			order = append(order, s.Name)
		}
	}
	cur := -1
	for i, s := range order {
		if strings.EqualFold(s, issue.Status) {
			cur = i
		}
	}
	durs := map[string]time.Duration{}
	for _, b := range tl.Bands {
		if b.End.After(b.Start) {
			durs[b.Status] += b.End.Sub(b.Start)
		}
	}
	steps := make([]pathStep, 0, len(order))
	for i, s := range order {
		st := pathStep{Status: s, State: "next", Dur: durs[s], Playtest: strings.EqualFold(s, "human-testing")}
		switch {
		case cur >= 0 && i < cur:
			st.State = "done"
		case i == cur:
			st.State = "current"
		}
		if i > 0 && wf.RequiredHumanApproval(order[i-1], s) != "" {
			st.Gate = true
		}
		steps = append(steps, st)
	}
	return steps
}

// approvingSince collects what was recorded since the newest transition that
// passed the owner's gate, else since the newest dispatch, else since the
// issue's first call.
func approvingSince(src timelineSources, tl Timeline) approving {
	a := approving{Known: len(src.Calls) > 0, From: "dispatch"}
	for i := len(src.Transitions) - 1; i >= 0; i-- {
		if r := src.Transitions[i]; r.ApprovedAt != nil {
			a.Since, a.From = r.TS, r.From+" → "+r.To
			break
		}
	}
	if a.Since.IsZero() {
		if n := len(src.Dispatches); n > 0 {
			a.Since = src.Dispatches[n-1]
		} else {
			a.Since = timeline.LifeStart(src.core())
		}
	}
	after := func(t time.Time) bool { return !t.Before(a.Since) }

	sections, boxes, prefixes := []string{}, []string{}, []string{}
	seen := map[string]bool{}
	decisions, comments := 0, 0
	for _, c := range src.Calls {
		if !after(c.At) || refusedCall(c) {
			continue
		}
		switch c.Cmd() {
		case "append", "replace":
			n := flagValue(c.Args, "--section")
			if n == "" {
				n = "body"
			}
			if !seen["s:"+n] {
				seen["s:"+n] = true
				sections = append(sections, n)
			}
		case "decide":
			decisions++
		case "check":
			for _, id := range c.Args[min(2, len(c.Args)):] {
				if !strings.HasPrefix(id, "-") && !seen["b:"+id] {
					seen["b:"+id] = true
					boxes = append(boxes, id)
				}
			}
		case "comment":
			comments++
			if p := commentPrefix(commentText(c, TimelineEvent{}, src)); p != "" && !seen["c:"+p] {
				seen["c:"+p] = true
				prefixes = append(prefixes, p)
			}
		}
	}
	commits := 0
	for _, c := range src.Commits {
		if !c.Merge && after(c.At) {
			commits++
		}
	}
	add := func(key string, n int, one, many, detail string) {
		if n > 0 {
			a.Counts = append(a.Counts, approvingCount{Key: key, N: n, Label: pluralWord(n, one, many), Detail: detail})
		}
	}
	add("sections", len(sections), "section written", "sections written", strings.Join(sections, ", "))
	var ids []string
	for _, d := range src.Decisions {
		if !d.Date.IsZero() && !d.Date.Before(a.Since.Truncate(24*time.Hour)) {
			ids = append(ids, d.ID)
		}
	}
	add("decisions", decisions, "decision", "decisions", idRange(ids))
	add("boxes", len(boxes), "box ticked", "boxes ticked", strings.Join(boxes, " "))
	add("comments", comments, "comment", "comments", strings.Join(prefixes, ", "))
	add("commits", commits, "commit", "commits", "")

	var evs []tlEvent
	for _, b := range tl.Bands {
		for _, e := range b.Events {
			// A refused dry run is the agent checking its own gate.
			dryRun := e.Chip == "refused" && strings.HasSuffix(e.Text, "(dry run)")
			if e.Row == "event" && !e.Muted && !dryRun && after(e.At) {
				evs = append(evs, e)
			}
		}
	}
	for _, r := range src.Transitions {
		if after(r.TS) {
			evs = append(evs, tlEvent{At: r.TS, Row: "event", Chip: "status", Tone: "purple", Text: r.From + " → " + r.To})
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	if len(evs) > approvingEvents {
		a.More = len(evs) - approvingEvents
		evs = evs[len(evs)-approvingEvents:]
	}
	a.Events = evs
	return a
}

// pluralWord is the counter's label: "box ticked" or "boxes ticked".
func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// idRange shows D-n ids as "D-2 to D-5" when they run on, else listed.
func idRange(ids []string) string {
	if len(ids) > 2 {
		return ids[0] + " to " + ids[len(ids)-1]
	}
	return strings.Join(ids, ", ")
}

var mockupImgRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)`)

// mockupThumbs takes the first image under each "### " of "## Mockups" (or
// the section's own images when it has no subsections).
func mockupThumbs(body string, imgSrc func(string) string) []mockupThumb {
	text, ok := tracker.SectionText(body, "Mockups")
	if !ok {
		return nil
	}
	var out []mockupThumb
	sub, taken := "", false
	for _, line := range strings.Split(text, "\n") {
		if level, title, ok := headingLine(line); ok && level >= 3 {
			sub, taken = title, false
			continue
		}
		m := mockupImgRe.FindStringSubmatch(line)
		if m == nil || (taken && sub != "") {
			continue
		}
		title := sub
		if title == "" {
			title = m[1]
		}
		src := m[2]
		if imgSrc != nil {
			src = imgSrc(src)
		}
		out = append(out, mockupThumb{Title: title, Src: src})
		taken = true
	}
	return out
}

// headingLine parses a markdown ATX heading.
func headingLine(line string) (level int, title string, ok bool) {
	t := strings.TrimSpace(line)
	for level < len(t) && t[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(t) || t[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(t[level+1:]), true
}

// foldSections summarises each "## " section of the body for its folded row;
// the rendered text before the first one is the "Intro" row.
func foldSections(body string, boxes []tracker.CheckboxItem, decisions int) []foldSection {
	var out []foldSection
	if intro := strings.TrimSpace(introText(body)); intro != "" {
		out = append(out, foldSection{Key: "", ID: "intro", Title: "Intro", Lead: tracker.Lead(intro, foldLeadMax)})
	}
	for _, s := range tracker.SplitSections(body) {
		key := headingKey(s.Title)
		f := foldSection{Key: key, ID: testids.Segment(key), Title: s.Title, Lead: tracker.Lead(s.Text, foldLeadMax)}
		for _, b := range boxes {
			if strings.EqualFold(b.Section, s.Title) {
				f.Total++
				if b.Checked {
					f.Done++
				}
			}
		}
		for _, line := range strings.Split(s.Text, "\n") {
			if level, title, ok := headingLine(line); ok && level == 3 {
				f.Subs = append(f.Subs, title)
			}
		}
		if strings.EqualFold(s.Title, "Decisions") && decisions > 0 {
			f.Lead = plural(decisions, "decision", "decisions") + ", shown above"
		}
		out = append(out, f)
	}
	return out
}

// introText is the body before its first "## " heading.
func introText(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if level, _, ok := headingLine(line); ok && level <= 2 {
			return strings.Join(lines[:i], "\n")
		}
	}
	return body
}

var headingMarkupRe = regexp.MustCompile("[`*]|\\[([^\\]]*)\\]\\([^)]*\\)")

// headingKey is a heading as the rendered <h2> reads it, lower-cased with
// spaces collapsed: what the page's foldBody matches on.
func headingKey(title string) string {
	plain := headingMarkupRe.ReplaceAllStringFunc(title, func(m string) string {
		if sub := headingMarkupRe.FindStringSubmatch(m); sub[1] != "" {
			return sub[1]
		}
		return ""
	})
	return strings.ToLower(strings.Join(strings.Fields(plain), " "))
}

// notStartedSections are the sections the transitions still ahead on the
// path append, which the body does not have yet.
func notStartedSections(wf *tracker.WorkflowConfig, issue *tracker.Issue) []string {
	var order []string
	for _, s := range wf.Statuses {
		if !s.Optional {
			order = append(order, s.Name)
		}
	}
	cur := -1
	for i, s := range order {
		if strings.EqualFold(s, issue.Status) {
			cur = i
		}
	}
	if cur < 0 {
		return nil
	}
	have := map[string]bool{}
	for _, s := range tracker.SplitSections(issue.BodyRaw) {
		have[strings.ToLower(s.Title)] = true
	}
	var out []string
	for i := cur; i+1 < len(order); i++ {
		for _, tr := range wf.Transitions {
			if tr.From != order[i] || tr.To != order[i+1] {
				continue
			}
			for _, a := range tr.Actions {
				title := strings.TrimSpace(a.Title)
				if a.Type == "append_section" && title != "" && !have[strings.ToLower(title)] {
					have[strings.ToLower(title)] = true
					out = append(out, title)
				}
			}
		}
	}
	return out
}

func agentCardFor(in issueTabInput) *agentCard {
	if in.Footer == nil && in.Issue.Assignee == "" {
		return nil
	}
	name := tracker.AgentSessionName(in.Issue.Slug)
	c := &agentCard{Name: name, Href: in.Prefix + "/agents?agent=" + name, Known: in.Known, LastAt: in.Call.At, LastCmd: in.Call.Cmd}
	if in.Footer != nil {
		c.State, c.Tone, c.Text = in.Footer.State, in.Footer.Tone, in.Footer.Text
		c.Live = in.Footer.State != "stopped"
	} else {
		c.State, c.Text = "stopped", "no agent session"
	}
	// The card's chip uses the Timeline's tones; the board's grey and
	// stopped are its gray.
	if c.Tone != "amber" && c.Tone != "green" {
		c.Tone = "gray"
	}
	for _, call := range in.Src.Calls {
		if call.Agent {
			c.Calls++
		}
	}
	if n := len(in.Src.Dispatches); n > 0 {
		c.Since = in.Src.Dispatches[n-1]
	}
	return c
}

// needsYouFor picks the band's third column. A question wins over a
// playtest, a playtest over check scripts and those over a ready gate, as on the board cards (you play
// it before you approve it); the others are listed as "Also waiting".
func needsYouFor(in issueTabInput, t IssueTab) needsYou {
	a, issue := in.Att, in.Issue
	n := needsYou{Since: a.Since, Approx: a.SinceApprox, Wait: shortDuration(in.Now.Sub(a.Since))}
	gate := a.Gate != nil && a.Gate.Ready
	targets := gateTargets(in.Targets, a)

	var kinds []string
	if a.OpenQuestion {
		kinds = append(kinds, "question")
	}
	if a.PlaytestDue {
		kinds = append(kinds, "playtest")
	}
	if a.ChecksDue {
		kinds = append(kinds, "checks")
	}
	if gate {
		kinds = append(kinds, "gate")
	}
	if len(kinds) > 0 {
		n.Amber, n.Label = true, "Needs you"
		for _, k := range kinds[1:] {
			switch k {
			case "gate":
				n.Also = append(n.Also, "approve → "+strings.Join(targets, " or "))
			case "playtest":
				n.Also = append(n.Also, "run the playtest")
			case "checks":
				n.Also = append(n.Also, "run the checks")
			}
		}
	}
	switch {
	case len(kinds) > 0 && kinds[0] == "question":
		n.State, n.Title = "question", "Answer the agent's question."
		comments := in.Src.Comments
		last := comments[len(comments)-1]
		n.Question = strings.TrimSpace(strings.TrimSpace(last.Text)[len("question:"):])
		n.AskedAt, _ = tracker.CommentTime(last.Date)
	case len(kinds) > 0 && kinds[0] == "gate":
		n.State, n.Targets = "gate", targets
		n.Title = gateTitle(issue.Status, targets)
		n.Text = gateCheck(t)
		n.Command = `issue-cli approve ` + issue.Slug + ` --to "` + targets[0] + `" --quote "…"`
	case len(kinds) > 0 && kinds[0] == "playtest":
		n.State, n.Sessions = "playtest", a.Playtests
		n.Title = "Run the playtest: " + plural(len(a.Playtests), "session", "sessions") + "."
		n.Text = "Session " + joinAnd(a.Playtests) + " names this issue and has no run judged by you yet."
		if len(a.Playtests) > 1 {
			n.Text = "Sessions " + joinAnd(a.Playtests) + " name this issue and have no run judged by you yet."
		}
		n.Command = "make playtest ISSUE=" + issue.Slug
	case len(kinds) > 0 && kinds[0] == "checks":
		n.State, n.Sessions = "checks", a.Checks
		n.Title = "Run the guided checks: " + plural(len(a.Checks), "script", "scripts") + "."
		n.Text = "Script " + joinAnd(a.Checks) + " walks you through this issue's manual checks and ticks the boxes that pass. It runs in tmux window " + checks.WindowName(issue.Slug) + "."
		if len(a.Checks) > 1 {
			n.Text = "Scripts " + joinAnd(a.Checks) + " walk you through this issue's manual checks and tick the boxes that pass. They run in tmux window " + checks.WindowName(issue.Slug) + "."
		}
		n.Command = "issue-cli --project " + in.Project + " check-script " + issue.Slug
	default:
		quietState(&n, in)
	}
	return n
}

// gateTargets orders the approve buttons: the required path first, a
// handoff status (backlog) last; the attention's own gate when the status
// has no listed approvals.
func gateTargets(targets []string, a Attention) []string {
	out := append([]string(nil), targets...)
	if len(out) == 0 && a.Gate != nil {
		out = []string{a.Gate.Target}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return !tracker.IsHandoffStatus(out[i]) && tracker.IsHandoffStatus(out[j])
	})
	return out
}

// gateNoun names what an approval out of a status approves.
var gateNoun = map[string]string{
	"idea":          "the idea",
	"in design":     "the design",
	"backlog":       "the start",
	"in progress":   "the work",
	"testing":       "the tests",
	"human-testing": "the human test",
	"documentation": "the docs",
	"shipping":      "the release",
}

func gateTitle(status string, targets []string) string {
	noun := gateNoun[strings.ToLower(status)]
	if noun == "" {
		noun = status
	}
	if len(targets) == 2 && tracker.IsHandoffStatus(targets[1]) {
		return "Approve " + noun + ": start it now, or queue it in " + targets[1] + "."
	}
	return "Approve " + noun + ": move it to " + strings.Join(targets, " or ") + "."
}

// gateCheck is the line under the gate's title: what to look at first.
func gateCheck(t IssueTab) string {
	var parts []string
	if n := len(t.Mockups); n > 0 {
		parts = append(parts, "the "+plural(n, "mockup", "mockups"))
	}
	if n := len(t.Decisions); n > 0 {
		parts = append(parts, "the "+plural(n, "decision", "decisions"))
	}
	what := "what you're approving below"
	if len(parts) > 0 {
		what = joinAnd(parts) + " below"
	}
	return "Check " + what + ". The agent has stopped at the gate until you approve."
}

// quietState fills the column when nothing waits on the owner: who has the
// issue now.
func quietState(n *needsYou, in issueTabInput) {
	issue, a := in.Issue, in.Att
	n.Meta = issue.Status + " " + shortDuration(in.Now.Sub(a.Since))
	switch {
	case a.Parked:
		n.State, n.Label, n.Title, n.Text = "parked", "Parked", "Nobody is on it.", a.ParkReason
		n.Meta = "since " + a.Since.Local().Format("2 Jan")
		return
	case strings.EqualFold(issue.Status, "done"):
		n.State, n.Label, n.Title = "done", "Done", "Nothing needs you."
		n.Text = "Done " + a.Since.Local().Format("2 Jan 15:04") + "."
		n.Meta = ""
		return
	}
	var note string
	switch {
	case issue.HumanApproval != "":
		note = "You approved → " + issue.HumanApproval + "; the agent continues."
	case a.Gate != nil:
		note = "Heading for the " + a.Gate.Target + " gate; its checklist is still open."
	}
	f := in.Footer
	switch {
	case f != nil && f.State == "idle":
		n.State, n.Label, n.Title = "idle", "With the agent", "Nothing needs you yet. The agent is quiet."
		n.Text = joinSentences(note, "No issue-cli call since "+in.Call.At.Local().Format("15:04")+".")
		n.OpenTmux = tracker.AgentSessionName(issue.Slug)
	case f != nil && f.State != "stopped":
		n.State, n.Label, n.Title = "working", "With the agent", "Nothing needs you. The agent is working."
		last := ""
		if in.Known {
			last = "Last call " + agoText(in.Now.Sub(in.Call.At)) + " ago · " + in.Call.Cmd + "."
		}
		n.Text = joinSentences(note, last)
	case f != nil:
		n.State, n.Label, n.Title = "stopped", "Nobody on it", "Nothing needs you. The agent stopped."
		n.Text = joinSentences(note, "Its tmux session is not running; dispatch it again from the side column.")
	case tracker.IsHandoffStatus(issue.Status):
		n.State, n.Label, n.Title = "queued", "Queued", "Nobody is on it yet."
		n.Text = joinSentences(note, "Queued in "+issue.Status+".")
	default:
		n.State, n.Label, n.Title = "none", "Nobody on it", "Nothing needs you."
		n.Text = joinSentences(note, "No agent is working on it.")
	}
}

func joinSentences(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}
