package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clibug"
	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Status page (/p/<project>/status) and its text form (status.md) say
// where the project stands: progress, work in flight, next steps, what waits
// on the owner and what shipped. Everything is read from repo files; nothing
// is stored.

const (
	statusReleases   = 3
	statusDoneWindow = 7 * 24 * time.Hour
	statusStaleDays  = 7
	statusVeryStale  = 14
)

// Progress is done/total, with side statuses (parked) left out of total.
type Progress struct {
	Label string
	Done  int
	Total int
}

// Pct is the done share as a whole percentage, for bar widths.
func (p Progress) Pct() int {
	if p.Total == 0 {
		return 0
	}
	return p.Done * 100 / p.Total
}

// EpicProgress is one open epic and how its children are doing.
type EpicProgress struct {
	Issue    *tracker.Issue
	Progress Progress
	// Running is how many children have a live agent.
	Running int
	// AllDone: every child of the epic is done, in every version (Progress
	// may count only one version's children).
	AllDone bool
	// LaterOpen counts the epic's open children outside this version's panel;
	// LaterIn names their versions ("0.55, 0.56").
	LaterOpen int
	LaterIn   string
}

// ReadyToClose: every child is done but the epic itself is still open.
func (e EpicProgress) ReadyToClose() bool { return e.AllDone }

// Note is the epic row's second line: "3 children with agents", "all
// children done", …
func (e EpicProgress) Note() string {
	switch {
	case e.Progress.Total == 0:
		return "no children yet"
	case e.Progress.Done == e.Progress.Total:
		return "all children done"
	case e.Running > 0:
		return plural(e.Running, "child", "children") + " with agents"
	case e.Progress.Done == 0:
		return "nothing started"
	}
	return fmt.Sprintf("%d to go", e.Progress.Total-e.Progress.Done)
}

// ActiveRow is one issue in flight and the agent on it.
type ActiveRow struct {
	Issue *tracker.Issue
	// Agent is working, at gate, question, stopped or "" (no agent).
	Agent string
	Gate  string
	Age   Age
}

// AgentChip is a live agent on an issue not yet in flight (still in design).
type AgentChip struct {
	Issue *tracker.Issue
	State string
}

// Age is how long an issue has sat in its status.
type Age struct {
	Since  time.Time
	Approx bool
	Text   string
	// Level is "", "stale" (7 days or more) or "very-stale" (14 or more).
	Level string
}

// NextSteps is the ## Next section of the project lead's newest handoff note.
type NextSteps struct {
	// File is the note's path relative to the project root; "" when there is
	// no note.
	File  string
	Age   Age
	Text  string
	Found bool
}

// ParkedRow is a parked issue and why.
type ParkedRow struct {
	Issue  *tracker.Issue
	Reason string
}

// Release is one changelog entry or git tag.
type Release struct {
	Version string
	Date    string
	Summary string
}

// DoneRow is an issue that reached the final status recently.
type DoneRow struct {
	Issue  *tracker.Issue
	At     time.Time
	Approx bool
}

// StatusModel is everything the Status page and status.md show.
type StatusModel struct {
	Project string
	Prefix  string
	AsOf    time.Time

	All Progress
	// Versions are the versions not released yet (the newest release and
	// everything before it left out), oldest first; VersionsProgress sums them.
	Versions         []Progress
	VersionsProgress Progress
	Epics            []EpicProgress
	LeadName         string
	LeadState        string
	Active           []ActiveRow
	InDesign         []AgentChip
	Next             NextSteps
	Backlog          []*tracker.Issue
	Parked           []ParkedRow
	Waiting          []WaitingItem
	// NotReady counts gates whose checklist is still open.
	NotReady      int
	Releases      []Release
	ReleaseSource string
	// ChangelogDeclined: the owner declined status-changelog, so an empty
	// Recently released doesn't suggest workbench.changelog.
	ChangelogDeclined bool
	DoneRecent        []DoneRow
	// CI is the Local CI card; nil without a ci: block.
	CI *CIRow
	// Health is the Health card; nil without a health snapshot.
	Health *HealthCard
	// Privacy is the work-files and code badges after the title and their
	// lines on the Health card (privacy_view.go).
	Privacy *PrivacyView
	// ReleaseWait is the Waiting for release card; nil when it has nothing
	// to show.
	ReleaseWait *ReleaseWait

	// Milestone is the Current milestone card; nil without a version to
	// cut (then the card shows All issues).
	Milestone *MilestoneView
	// EpicsByArea is the Epics card: Epics grouped by area per version.
	EpicsByArea EpicsView
	// Shipped is DoneRecent grouped by area.
	Shipped []ShippedArea
	// UnsentBugReports counts issue-cli bug reports queued on this machine
	// because report-bug found no engine project.
	UnsentBugReports int
}

// unsentBugReports counts this user's queued report-bug files. A var so
// tests never read the real queue.
var unsentBugReports = clibug.PendingCount

// IsCurrent reports whether version is the current milestone.
func (m *StatusModel) IsCurrent(version string) bool {
	return m.Milestone != nil && m.Milestone.Version == version
}

// WaitingCount is the number shown on the sidebar's Status entry: the
// waiting items, plus one while local CI is red, plus one while issue-cli
// bug reports wait to be sent.
func (m *StatusModel) WaitingCount() int {
	n := len(m.Waiting)
	if m.CI.Red() {
		n++
	}
	if m.UnsentBugReports > 0 {
		n++
	}
	return n
}

// AgeOf is how long ago t was, as of the page: 25m, 1h 30m, 13d.
func (m *StatusModel) AgeOf(t time.Time) string { return shortDuration(m.AsOf.Sub(t)) }

// Candidate is the release candidate being soaked (silver on the Waiting
// for release card, waiting to be released); nil without one.
func (m *StatusModel) Candidate() *CandidateRow {
	if m.ReleaseWait == nil || m.ReleaseWait.Milestone == nil {
		return nil
	}
	return m.ReleaseWait.Milestone.Candidate
}

// WaitingOf returns the waiting items of one kind (gate, playtest, checks,
// question).
func (m *StatusModel) WaitingOf(kind string) []WaitingItem {
	var out []WaitingItem
	for _, it := range m.Waiting {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

// gitTags lists the newest n tags as releases. A var so tests don't run git.
var gitTags = func(root string, n int) []Release {
	out, err := exec.Command("git", "-C", root, "for-each-ref", "--sort=-creatordate", fmt.Sprintf("--count=%d", n),
		"--format=%(refname:short)\t%(creatordate:short)\t%(contents:subject)", "refs/tags").Output()
	if err != nil {
		return nil
	}
	var rels []Release
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		r := Release{Version: parts[0], Date: parts[1]}
		if len(parts) == 3 && parts[2] != parts[0] {
			r.Summary = parts[2]
		}
		rels = append(rels, r)
	}
	return rels
}

// buildStatus assembles the Status page. agents may be nil (no tmux): every
// issue then shows no agent.
func buildStatus(proj *tracker.Project, issues []*tracker.Issue, agents *AgentsModel, now time.Time) *StatusModel {
	wf := proj.LoadWorkflow()
	cs := newCanvasStatuses(wf)
	m := &StatusModel{Project: proj.Name, AsOf: now}
	if agents != nil {
		m.Prefix = agents.Prefix
	}

	m.All = progressOf("All issues", cs, issues)
	byVersion := map[string][]*tracker.Issue{}
	for _, issue := range issues {
		if issue.Version != "" {
			byVersion[issue.Version] = append(byVersion[issue.Version], issue)
		}
	}
	for v, items := range byVersion {
		m.Versions = append(m.Versions, progressOf(v, cs, items))
	}
	sort.Slice(m.Versions, func(i, j int) bool { return compareVersions(m.Versions[i].Label, m.Versions[j].Label) < 0 })

	live := map[string]AgentNode{}
	stopped := map[string]AgentNode{}
	if agents != nil {
		m.LeadName, m.LeadState = agents.ProjectLead.Name, agents.ProjectLead.State
		for _, n := range agents.Workers {
			if n.Issue != nil {
				live[n.Issue.Slug] = n
			}
		}
		for _, n := range agents.Unfinished {
			if n.Issue != nil {
				stopped[n.Issue.Slug] = n
			}
		}
	}

	sorted := append([]*tracker.Issue(nil), issues...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })
	for _, issue := range sorted {
		if cs.closed(issue.Status) || !tracker.IsEpic(issue, issues) {
			continue
		}
		children := tracker.EpicChildren(issues, issue.Slug)
		e := EpicProgress{Issue: issue, Progress: progressOf(issue.Title, cs, children)}
		for _, c := range children {
			if _, ok := live[c.Slug]; ok {
				e.Running++
			}
		}
		m.Epics = append(m.Epics, e)
	}

	for _, issue := range sorted {
		iwf := proj.LoadWorkflowForIssue(issue)
		a := ownerAttention(proj, issue)
		if a.Gate != nil && !a.Gate.Ready && !cs.closed(issue.Status) {
			m.NotReady++
		}
		switch {
		case activeStatus(iwf, issue.Status):
			row := ActiveRow{Issue: issue, Age: ageOf(a.Since, a.SinceApprox, now)}
			if n, ok := live[issue.Slug]; ok {
				row.Agent, row.Gate = agentLabel(n.State), n.Gate
			} else if _, ok := stopped[issue.Slug]; ok {
				row.Agent = "stopped"
			}
			m.Active = append(m.Active, row)
		case a.Parked:
			m.Parked = append(m.Parked, ParkedRow{Issue: issue, Reason: a.ParkReason})
		case strings.EqualFold(issue.Status, "backlog"):
			m.Backlog = append(m.Backlog, issue)
		}
		if n, ok := live[issue.Slug]; ok && !activeStatus(iwf, issue.Status) && !cs.closed(issue.Status) {
			m.InDesign = append(m.InDesign, AgentChip{Issue: issue, State: agentLabel(n.State)})
		}
		if statusKey(issue.Status) == cs.final {
			if at, approx := doneAt(issue); now.Sub(at) <= statusDoneWindow {
				m.DoneRecent = append(m.DoneRecent, DoneRow{Issue: issue, At: at, Approx: approx})
			}
		}
	}
	sort.SliceStable(m.Active, func(i, j int) bool {
		return wf.GetStatusIndex(m.Active[i].Issue.Status) > wf.GetStatusIndex(m.Active[j].Issue.Status)
	})
	sortBacklog(m.Backlog)
	sort.Slice(m.DoneRecent, func(i, j int) bool { return m.DoneRecent[i].At.After(m.DoneRecent[j].At) })

	m.Waiting = waitingOnOwner(proj, sorted)
	m.CI = ciRow(proj, issues, now)
	m.Health = healthCard(proj, now)
	m.Privacy = privacyView(proj, m.Prefix, now)
	m.UnsentBugReports = unsentBugReports()
	root := projectRoot(proj)
	m.Next = leadNextSteps(root, tracker.LeadSessionName(proj), now)
	text, changelog := readChangelog(root, wf.Workbench.Changelog)
	m.Releases, m.ReleaseSource = recentReleasesFrom(root, text, changelog, statusReleases)
	m.ChangelogDeclined = wf.DeclinesFeature("status-changelog")
	released := ""
	if len(m.Releases) > 0 {
		released = m.Releases[0].Version
	}
	m.Versions, m.VersionsProgress = upcomingVersions(m.Versions, released)
	m.ReleaseWait = buildReleaseWait(proj, wf, cs, byVersion, released, changelog, text, now)
	if m.ReleaseWait != nil && m.ReleaseWait.Milestone != nil {
		ms := m.ReleaseWait.Milestone
		ms.Epic = map[string]bool{}
		for _, issue := range ms.Open {
			if tracker.IsEpic(issue, issues) {
				ms.Epic[issue.Slug] = true
			}
		}
	}
	if v, p, items := pickMilestone(byVersion, cs, released); v != "" {
		m.Milestone = buildMilestoneView(wf, cs, v, p, items)
		for _, issue := range items {
			if !cs.closed(issue.Status) && tracker.IsEpic(issue, issues) {
				m.Milestone.OpenEpics++
			}
		}
	}
	current := ""
	if m.Milestone != nil {
		current = m.Milestone.Version
	}
	m.EpicsByArea = buildEpicsView(cs, issues, m.Epics, current, released)
	m.Shipped = groupShipped(m.DoneRecent)
	return m
}

func progressOf(label string, cs *canvasStatuses, items []*tracker.Issue) Progress {
	p := Progress{Label: label}
	for _, it := range items {
		k := statusKey(it.Status)
		if cs.side[k] {
			continue
		}
		p.Total++
		if k == cs.final {
			p.Done++
		}
	}
	return p
}

// agentLabel turns an agents-canvas state into the page's wording.
func agentLabel(state string) string {
	if state == "at-gate" {
		return "at gate"
	}
	return state
}

// sortBacklog orders by priority (critical first, none last), then oldest
// created first.
func sortBacklog(issues []*tracker.Issue) {
	rank := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	r := func(p string) int {
		if v, ok := rank[strings.ToLower(p)]; ok {
			return v
		}
		return 4
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if ri, rj := r(issues[i].Priority), r(issues[j].Priority); ri != rj {
			return ri < rj
		}
		return issues[i].Created < issues[j].Created
	})
}

// doneAt is when the issue reached its final status: the stats sidecar's
// last transition, else done_at, else the file's mtime (approx).
func doneAt(issue *tracker.Issue) (time.Time, bool) {
	store, _ := tracker.LoadStats(issue.FilePath)
	for i := len(store.Transitions) - 1; i >= 0; i-- {
		if strings.EqualFold(store.Transitions[i].To, issue.Status) {
			return store.Transitions[i].TS, false
		}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, issue.DoneAt, time.Local); err == nil {
			return t, false
		}
	}
	return issue.ModTime, true
}

func ageOf(since time.Time, approx bool, now time.Time) Age {
	a := Age{Since: since, Approx: approx, Text: shortDuration(now.Sub(since))}
	switch days := int(now.Sub(since).Hours() / 24); {
	case days >= statusVeryStale:
		a.Level = "very-stale"
	case days >= statusStaleDays:
		a.Level = "stale"
	}
	return a
}

// shortDuration is 25m, 1h 30m or 13d.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h, mins := int(d.Hours()), int(d.Minutes())%60
		if mins == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, mins)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// leadNextSteps reads the ## Next section of the project lead's newest
// handoff note under <root>/sessions.
func leadNextSteps(root, lead string, now time.Time) NextSteps {
	path := tracker.NewestHandoffNote(filepath.Join(root, "sessions"), lead)
	if path == "" {
		return NextSteps{}
	}
	ns := NextSteps{File: filepath.ToSlash(relTo(root, path))}
	if info, err := os.Stat(path); err == nil {
		ns.Age = ageOf(info.ModTime(), false, now)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ns
	}
	ns.Text, ns.Found = tracker.SectionText(string(data), "Next")
	ns.Found = ns.Found && ns.Text != ""
	return ns
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// recentReleases reads the newest n releases from the changelog (configured,
// else CHANGELOG.md), else from git tags. source names where they came from.
func recentReleases(root, configured string, n int) (rels []Release, source string) {
	text, name := readChangelog(root, configured)
	return recentReleasesFrom(root, text, name, n)
}

// readChangelog reads the changelog (configured, else CHANGELOG.md). name is
// its slash path, "" when there is none.
func readChangelog(root, configured string) (text, name string) {
	name = configured
	if name == "" {
		name = "CHANGELOG.md"
	}
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return "", ""
	}
	return string(data), filepath.ToSlash(name)
}

// recentReleasesFrom is recentReleases on a changelog already read.
func recentReleasesFrom(root, text, name string, n int) (rels []Release, source string) {
	if name != "" {
		if rels = parseChangelog(text, n); len(rels) > 0 {
			return rels, name
		}
	}
	if rels = gitTags(root, n); len(rels) > 0 {
		return rels, "git tags"
	}
	return nil, ""
}

// changelogHeading is milestone.ChangelogHeading.
var changelogHeading = milestone.ChangelogHeading

// parseChangelog returns the first n "## <version> — <date>" entries, each
// summarised by its first bullet (whole, markdown and all: SummaryText and
// SummaryHTML clip or render it).
func parseChangelog(text string, n int) []Release {
	var rels []Release
	var cur *Release
	inSummary := false
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			if len(rels) == n {
				break
			}
			mm := changelogHeading.FindStringSubmatch(line)
			if mm == nil {
				cur = nil
				continue
			}
			rels = append(rels, Release{Version: mm[1], Date: mm[2]})
			cur, inSummary = &rels[len(rels)-1], false
			continue
		}
		if cur == nil {
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case cur.Summary == "" && strings.HasPrefix(t, "- "):
			cur.Summary, inSummary = strings.TrimPrefix(t, "- "), true
		case inSummary && t != "" && !strings.HasPrefix(t, "- ") && line != t:
			cur.Summary += " " + t
		default:
			inSummary = false
		}
	}
	return rels
}

// clip shortens s to at most n runes, ending in "…".
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// statusMarkdown is the text form of the page (status.md), for pasting as a
// stand-up. Slugs, not titles, so the text still works with issue-cli. It
// follows the page: what needs the owner and what is running first, then
// counts and groups; every list stops at a cap with "+K more" and, where one
// exists, the issue-cli command that lists the rest.
func statusMarkdown(m *StatusModel) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s status, %s\n", m.Project, m.AsOf.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "%d/%d done", m.All.Done, m.All.Total)
	if ev := m.EpicsByArea; ev.Epics > 0 {
		fmt.Fprintf(&b, " · %s in %s", plural(ev.Epics, "epic", "epics"), plural(ev.Areas, "area", "areas"))
	}
	b.WriteString("\n")
	for _, pb := range m.Privacy.badges() {
		fmt.Fprintf(&b, "%s: %s\n", pb.Label, pb.Sentence)
	}

	fmt.Fprintf(&b, "\n## Waiting on you (%d)\n", m.WaitingCount())
	if m.CI.Red() {
		fmt.Fprintf(&b, "- Local CI red on %s since %s: %s (issue-cli ci status)\n", m.CI.Branch, m.CI.Sha, m.CI.Failed)
	}
	if m.UnsentBugReports > 0 {
		fmt.Fprintf(&b, "- %d issue-cli bug report(s) not sent (issue-cli report-bug --send)\n", m.UnsentBugReports)
	}
	for i, it := range m.Waiting {
		if i == capMDList {
			fmt.Fprintf(&b, "- +%d more on the Status page\n", len(m.Waiting)-i)
			break
		}
		switch it.Kind {
		case "gate":
			fmt.Fprintf(&b, "- Approve %s: %s → %s\n", it.Issue.Slug, it.Issue.Status, it.Target)
		case "playtest":
			fmt.Fprintf(&b, "- Playtest %s: session %s, no judge run\n", it.Issue.Slug, strings.Join(it.Sessions, ", "))
		case "checks":
			fmt.Fprintf(&b, "- Run checks %s: issue-cli check-script %s (%s), no owner run\n", it.Issue.Slug, it.Issue.Slug, strings.Join(it.Sessions, ", "))
		case "question":
			fmt.Fprintf(&b, "- Answer %s: open question: comment\n", it.Issue.Slug)
		}
	}
	if m.WaitingCount() == 0 {
		b.WriteString("- Nothing\n")
	}

	b.WriteString("\n## Working on\n")
	if m.LeadName != "" {
		fmt.Fprintf(&b, "Lead: %s (%s)\n", m.LeadName, strings.ReplaceAll(m.LeadState, "-", " "))
	}
	for i, r := range m.Active {
		if i == capMDList {
			fmt.Fprintf(&b, "- +%d more on the Status page\n", len(m.Active)-i)
			break
		}
		agent := "no agent"
		if r.Agent != "" {
			agent = "agent " + r.Agent
			if r.Gate != "" {
				agent += " → " + r.Gate
			}
		}
		approx := ""
		if r.Age.Approx {
			approx = "≈"
		}
		fmt.Fprintf(&b, "- [%s %s%s] %s (%s)\n", r.Issue.Status, approx, r.Age.Text, r.Issue.Slug, agent)
	}
	if len(m.Active) == 0 {
		b.WriteString("- Nothing between in progress and shipping\n")
	}
	if len(m.InDesign) > 0 {
		var parts []string
		for i, c := range m.InDesign {
			if i == capChips {
				parts = append(parts, fmt.Sprintf("+%d more", len(m.InDesign)-i))
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%s)", c.Issue.Slug, c.State))
		}
		fmt.Fprintf(&b, "- Agents on issues not yet in progress: %s\n", strings.Join(parts, ", "))
	}

	if ms := m.Milestone; ms != nil {
		fmt.Fprintf(&b, "\n## Current milestone %s: %d/%d done\n", ms.Version, ms.Progress.Done, ms.Progress.Total)
		var parts []string
		for _, sg := range ms.Legend {
			parts = append(parts, fmt.Sprintf("%d %s", sg.Count, sg.Status))
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, "- By status: %s\n", strings.Join(parts, ", "))
		}
		if len(ms.OpenAreas) > 0 {
			fmt.Fprintf(&b, "- Open by area: %s\n", areaCountsLine(ms.OpenAreas, capMilestoneAreas))
		}
	} else {
		b.WriteString("\n## Where we are\n")
		note := ""
		if len(m.Versions) == 0 {
			note = " (no versions set)"
		}
		fmt.Fprintf(&b, "- All issues: %d/%d done%s\n", m.All.Done, m.All.Total, note)
	}
	ciMarkdown(&b, m.CI)
	healthMarkdown(&b, m.Health)
	if m.ReleaseWait != nil {
		releaseWaitMarkdown(&b, m.ReleaseWait, m.AsOf)
	}

	epicsMarkdown(&b, m.EpicsByArea)

	b.WriteString("\n## Next\n")
	if m.Next.Found {
		fmt.Fprintf(&b, "From %s:\n%s\n", m.Next.File, m.Next.Text)
	} else {
		b.WriteString("No ## Next section in the lead's handoff note.\n")
	}
	if len(m.Backlog) > 0 {
		fmt.Fprintf(&b, "Backlog (%d):\n", len(m.Backlog))
		for i, issue := range m.Backlog {
			if i == capNext {
				fmt.Fprintf(&b, "- +%d more: issue-cli list --status backlog\n", len(m.Backlog)-i)
				break
			}
			prio := issue.Priority
			if prio == "" {
				prio = "no priority"
			}
			fmt.Fprintf(&b, "- %s (%s)\n", issue.Slug, prio)
		}
	}
	if len(m.Parked) == 0 {
		b.WriteString("Parked: none\n")
	} else {
		fmt.Fprintf(&b, "Parked (%d):\n", len(m.Parked))
		for i, p := range m.Parked {
			if i == capNext {
				fmt.Fprintf(&b, "- +%d more: issue-cli list --status parked\n", len(m.Parked)-i)
				break
			}
			fmt.Fprintf(&b, "- %s: %s\n", p.Issue.Slug, p.Reason)
		}
	}

	fmt.Fprintf(&b, "\n## Recently shipped: %d in 7 days\n", len(m.DoneRecent))
	for _, g := range m.Shipped {
		var slugs []string
		for i, d := range g.Rows {
			if i == capShippedItems {
				more := fmt.Sprintf("+%d more", len(g.Rows)-i)
				if sys := g.Rows[0].Issue.System; !g.Merged && sys != "" {
					more += " (issue-cli list --status done --system " + sys + ")"
				}
				slugs = append(slugs, more)
				break
			}
			slugs = append(slugs, d.Issue.Slug)
		}
		fmt.Fprintf(&b, "- %s %d: %s\n", g.Area, len(g.Rows), strings.Join(slugs, ", "))
	}
	if len(m.DoneRecent) == 0 {
		b.WriteString("- Nothing\n")
	}

	if len(m.Versions) > 0 {
		b.WriteString("\n## Versions\n")
		var parts []string
		for i, v := range m.Versions {
			if i == capVersions {
				parts = append(parts, fmt.Sprintf("+%d more", len(m.Versions)-i))
				break
			}
			cur := ""
			if m.IsCurrent(v.Label) {
				cur = " (current)"
			}
			parts = append(parts, fmt.Sprintf("%s %d/%d%s", v.Label, v.Done, v.Total, cur))
		}
		fmt.Fprintf(&b, "- %s\n", strings.Join(parts, " · "))
	}

	b.WriteString("\n## Recently released\n")
	for _, r := range m.Releases {
		date := ""
		if r.Date != "" {
			date = " (" + r.Date + ")"
		}
		fmt.Fprintf(&b, "- %s%s: %s\n", r.Version, date, r.SummaryText())
	}
	if len(m.Releases) == 0 {
		b.WriteString("- None yet\n")
	}
	return b.String()
}

// ciMarkdown is the Local CI line, with what landed and the log tail when
// red.
func ciMarkdown(b *strings.Builder, c *CIRow) {
	if c == nil {
		return
	}
	fmt.Fprintf(b, "- Local CI: %s\n", c.Line)
	if !c.Red() {
		return
	}
	if len(c.Blamed) > 0 {
		var slugs []string
		for _, issue := range c.Blamed {
			slugs = append(slugs, issue.Slug)
		}
		fmt.Fprintf(b, "  - Landed since green: %s\n", strings.Join(slugs, ", "))
	}
	if len(c.LogTail) > 0 {
		tail := c.LogTail
		if len(tail) > 20 {
			tail = tail[len(tail)-20:]
		}
		fmt.Fprintf(b, "\n```\n%s\n```\n", strings.Join(tail, "\n"))
	}
}

// epicsMarkdown is the Epics card for the current panel: one line per
// active area with its epics, then the areas with nothing started on one
// line.
func epicsMarkdown(b *strings.Builder, ev EpicsView) {
	p := ev.CurrentPanel()
	if p == nil {
		return
	}
	scope := ""
	if p.Key != epicsAll {
		scope = " (" + p.Key + ")"
	}
	var started, idle []AreaRow
	for _, a := range p.Areas {
		if a.Started() {
			started = append(started, a)
		} else {
			idle = append(idle, a)
		}
	}
	fmt.Fprintf(b, "\n## Epics%s: %d active, %d not started, in %s\n", scope, p.Active, p.NotStarted, plural(len(p.Areas), "area", "areas"))
	for _, a := range started {
		var parts []string
		for i, e := range a.Epics {
			if i == capMDAreaEpics {
				parts = append(parts, fmt.Sprintf("+%d more", len(a.Epics)-i))
				break
			}
			parts = append(parts, fmt.Sprintf("%s %d/%d", e.Issue.Slug, e.Progress.Done, e.Progress.Total))
		}
		fmt.Fprintf(b, "- %s %d/%d, %s: %s\n", a.Area, a.Progress.Done, a.Progress.Total, plural(len(a.Epics), "epic", "epics"), strings.Join(parts, ", "))
	}
	if len(idle) > 0 {
		var parts []string
		for i, a := range idle {
			if i == capMDNotStarted {
				parts = append(parts, fmt.Sprintf("+%d more areas", len(idle)-i))
				break
			}
			parts = append(parts, fmt.Sprintf("%s 0/%d (%d)", a.Area, a.Progress.Total, len(a.Epics)))
		}
		fmt.Fprintf(b, "- Not started: %s\n", strings.Join(parts, ", "))
	}
	if len(ev.Panels) > 1 {
		fmt.Fprintf(b, "- Other versions on the Status page's Version select; all versions: %s\n", plural(ev.Epics, "open epic", "open epics"))
	}
}

// areaCountsLine is "campaign 14, ui 10, +4 areas".
func areaCountsLine(areas []AreaCount, n int) string {
	var parts []string
	for i, a := range areas {
		if i == n {
			parts = append(parts, fmt.Sprintf("+%s", plural(len(areas)-i, "area", "areas")))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %d", a.Area, a.Count))
	}
	return strings.Join(parts, ", ")
}

// upcomingVersions keeps the versions after released (and the non-numeric
// ones, such as "future") and sums their progress.
func upcomingVersions(all []Progress, released string) ([]Progress, Progress) {
	rel := milestone.Norm(released)
	var out []Progress
	var sum Progress
	for _, v := range all {
		if n := milestone.Norm(v.Label); n != "" && rel != "" && milestone.Compare(n, rel) <= 0 {
			continue
		}
		out = append(out, v)
		sum.Done += v.Done
		sum.Total += v.Total
	}
	return out, sum
}
