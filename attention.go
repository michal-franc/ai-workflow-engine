package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// "Waiting on you" means the same on every view: the status page panel, the
// system pages' chip, the board card markers and the agents canvas all read
// it from ownerAttention / waitingOnOwner.

// GateInfo is a transition waiting on the human's approval.
type GateInfo struct {
	// Target is the status the approval is for.
	Target string
	// Ready is true when the approval is the only unmet requirement (every
	// validate rule of the transition passes).
	Ready bool
}

// Attention is what one issue needs from the owner right now.
type Attention struct {
	Gate         *GateInfo
	OpenQuestion bool
	// PlaytestDue is set in human-testing when a playtest session names the
	// issue and has no judge (owner) run yet; Playtests lists those sessions.
	PlaytestDue bool
	Playtests   []string
	Parked      bool
	ParkReason  string
	// Since is when the issue entered its current status: the last stats
	// sidecar transition into it, else the file's mtime (SinceApprox).
	Since       time.Time
	SinceApprox bool
}

// WaitingItem is one thing the owner has to do on an issue.
type WaitingItem struct {
	Issue *tracker.Issue
	// Kind is gate, playtest or question.
	Kind string
	// Target is the gate's target status; Sessions the playtests to run.
	Target   string
	Sessions []string
	Since    time.Time
}

// pendingGate returns the status the issue waits on the human for: its next
// status, when that transition needs approval not yet given.
func pendingGate(wf *tracker.WorkflowConfig, issue *tracker.Issue) string {
	_, gate := gateTransition(wf, issue)
	return gate
}

// gateTransition returns the issue's next status and, when that transition
// needs approval not yet given, the status the approval is for.
func gateTransition(wf *tracker.WorkflowConfig, issue *tracker.Issue) (next, gate string) {
	order := wf.GetStatusOrder()
	idx := wf.GetStatusIndex(issue.Status)
	if idx < 0 || idx+1 >= len(order) {
		return "", ""
	}
	next = order[idx+1]
	required := wf.RequiredHumanApproval(issue.Status, next)
	if required == "" || strings.EqualFold(issue.HumanApproval, required) {
		return next, ""
	}
	return next, required
}

// openQuestion reports whether the issue's newest comment is a question:
// nobody has answered yet.
func openQuestion(comments []tracker.Comment) bool {
	if len(comments) == 0 {
		return false
	}
	last := strings.ToLower(strings.TrimSpace(comments[len(comments)-1].Text))
	return strings.HasPrefix(last, "question:")
}

// ownerAttention collects what the issue needs from the owner.
func ownerAttention(proj *tracker.Project, issue *tracker.Issue) Attention {
	wf := proj.LoadWorkflowForIssue(issue)
	comments, _ := tracker.LoadComments(issue.FilePath)
	var a Attention
	if next, gate := gateTransition(wf, issue); gate != "" {
		a.Gate = &GateInfo{Target: gate, Ready: wf.ReadyForApproval(issue, issue.Status, next, comments)}
	}
	a.OpenQuestion = openQuestion(comments)
	if strings.EqualFold(issue.Status, "human-testing") {
		a.Playtests = playtestsToRun(playtestRoots(proj, issue, wf), issue.Slug)
		a.PlaytestDue = len(a.Playtests) > 0
	}
	if strings.EqualFold(issue.Status, "parked") {
		a.Parked = true
		a.ParkReason = parkReason(issue.BodyRaw)
	}
	a.Since, a.SinceApprox = statusSince(issue)
	return a
}

// waitingOnOwner lists what the owner has to do across issues: ready gates,
// playtests to run and open questions, oldest first. Callers scope it by
// passing a subset (one system's issues).
func waitingOnOwner(proj *tracker.Project, issues []*tracker.Issue) []WaitingItem {
	var items []WaitingItem
	for _, issue := range issues {
		a := ownerAttention(proj, issue)
		if a.Gate != nil && a.Gate.Ready {
			items = append(items, WaitingItem{Issue: issue, Kind: "gate", Target: a.Gate.Target, Since: a.Since})
		}
		if a.PlaytestDue {
			items = append(items, WaitingItem{Issue: issue, Kind: "playtest", Sessions: a.Playtests, Since: a.Since})
		}
		if a.OpenQuestion {
			items = append(items, WaitingItem{Issue: issue, Kind: "question", Since: a.Since})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Since.Before(items[j].Since) })
	return items
}

// statusSince is when the issue entered its current status, from the stats
// sidecar; approx is true when it falls back to the file's mtime.
func statusSince(issue *tracker.Issue) (since time.Time, approx bool) {
	store, _ := tracker.LoadStats(issue.FilePath)
	for i := len(store.Transitions) - 1; i >= 0; i-- {
		if strings.EqualFold(store.Transitions[i].To, issue.Status) {
			return store.Transitions[i].TS, false
		}
	}
	return issue.ModTime, true
}

// parkReason is the first line of the issue's ## Parked Record section,
// without the "**Why is it parked? …:**" field label the transition writes.
func parkReason(body string) string {
	text, _ := tracker.SectionText(body, "Parked Record")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if strings.HasPrefix(line, "**") {
			if end := strings.Index(line[2:], "**"); end >= 0 {
				line = strings.TrimSpace(line[end+4:])
			}
		}
		if line != "" {
			return line
		}
	}
	return ""
}

// playtestRoots are the checkouts that may hold the issue's playtest sessions
// and runs: the project root and the issue's worktree.
func playtestRoots(proj *tracker.Project, issue *tracker.Issue, wf *tracker.WorkflowConfig) []string {
	roots := []string{projectRoot(proj)}
	if wt, _, enabled := resolveWorktree(resolveProjectWorkDir(proj), issue.Slug, wf); enabled && wt != "" && fileExists(wt) {
		roots = append(roots, wt)
	}
	return roots
}

// playtestsToRun returns the sessions under <root>/playtest/sessions that
// name slug and have no judge run under <root>/.playtest/runs/<session>/ in
// any of the roots.
func playtestsToRun(roots []string, slug string) []string {
	names := map[string]bool{}
	for _, root := range roots {
		files, _ := filepath.Glob(filepath.Join(root, "playtest", "sessions", "*.yaml"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var head struct {
				Session string `yaml:"session"`
				Issue   string `yaml:"issue"`
			}
			if yaml.Unmarshal(data, &head) != nil || head.Issue != slug {
				continue
			}
			name := head.Session
			if name == "" {
				name = strings.TrimSuffix(filepath.Base(f), ".yaml")
			}
			names[name] = true
		}
	}
	var due []string
	for name := range names {
		judged := false
		for _, root := range roots {
			if runs, _ := filepath.Glob(filepath.Join(root, ".playtest", "runs", name, "*-judge")); len(runs) > 0 {
				judged = true
				break
			}
		}
		if !judged {
			due = append(due, name)
		}
	}
	sort.Strings(due)
	return due
}
