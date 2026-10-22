package main

import (
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// What the pages show of local CI, read from .ci/ (internal/ci): the Status
// page's "Local CI" card, its line in status.md and "Waiting on you", the
// sidebar count, and the ci-red card markers. Nothing shows without a ci:
// block.

// CIRow is the Status page's Local CI card.
type CIRow struct {
	// Line is the one-liner `issue-cli ci status` prints.
	Line   string
	State  string // green, red, error, or "" before the first run
	Branch string
	// Sha is the green commit, or the first red one.
	Sha      string
	At       time.Time
	Failed   string
	Failures string
	Error    string
	Blamed   []*tracker.Issue
	// Others counts what landed since green without a matching issue.
	Others   int
	RedIssue *tracker.Issue
	Log      string
	LogTail  []string
	Running  bool
	RunSha   string
}

// Red reports whether the branch is red.
func (r *CIRow) Red() bool { return r != nil && r.State == ci.Red }

// ciState reads proj's CI state; nil when the project has no ci: block.
func ciState(proj *tracker.Project) (*ci.State, bool) {
	if !proj.LoadWorkflow().CI.Enabled() {
		return nil, false
	}
	st, _ := ci.ReadState(ciRootFor(proj))
	return st, true
}

// ciRedState is the state while local CI is red, else nil.
func ciRedState(proj *tracker.Project) *ci.State {
	if st, _ := ciState(proj); st.IsRed() {
		return st
	}
	return nil
}

// ciRow builds the Local CI card; nil without a ci: block.
func ciRow(proj *tracker.Project, issues []*tracker.Issue, now time.Time) *CIRow {
	st, on := ciState(proj)
	if !on {
		return nil
	}
	root := ciRootFor(proj)
	lock, alive := ci.ReadLock(root)
	branch, _ := ci.ResolveBranch(root, proj.LoadWorkflow().CI)
	row := &CIRow{Line: ci.OneLine(st, lock, alive, branch, now), Branch: branch, Running: alive}
	if alive {
		row.RunSha = ci.Short(lock.Sha)
	}
	if st == nil {
		return row
	}
	row.State, row.Branch, row.Error = st.State, st.Branch, st.Error
	switch st.State {
	case ci.Green:
		row.Sha, row.At = ci.Short(st.GreenSha), st.GreenAt
	case ci.Red:
		row.Sha, row.At, row.Failed, row.Failures = ci.Short(st.RedSince), st.Updated, st.Failed, ci.FailureList(st.Failures)
		row.Log, row.LogTail = st.Log, st.LogTail
		for _, slug := range st.Blamed {
			if issue := tracker.FindIssue(issues, slug); issue != nil {
				row.Blamed = append(row.Blamed, issue)
			}
		}
		for _, m := range st.Merged {
			if m.Slug == "" {
				row.Others++
			}
		}
		row.RedIssue = tracker.FindIssue(issues, st.RedIssue)
	default:
		row.Sha, row.At, row.Log, row.LogTail = ci.Short(st.LastSha), st.Updated, st.Log, st.LogTail
	}
	return row
}
