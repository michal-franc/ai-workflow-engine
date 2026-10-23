// Package timeline derives where an issue's time went: its life by status,
// split into agent working, waiting on the owner, idle and rework. The
// detail page's Timeline tab and the project aggregate (Aggregate, the
// `workflow` health family, /stats) both build on Split, so the numbers they
// show for one issue are the same. The rules are in docs/workflow-stats.md,
// "Timeline tab".
package timeline

import (
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// IdleAfter is the gap between two activities after which the agent counts
// as idle, or as waiting on the owner in a status only the owner can move
// on. The agents canvas uses it for its idle marker too.
const IdleAfter = 15 * time.Minute

// Segment kinds.
const (
	KindAgent   = "agent"
	KindWaiting = "waiting"
	KindIdle    = "idle"
	KindRework  = "rework"
)

// Sources is what Split reads for one issue, gathered by Load. Missing lists
// the sources that could not be read; Read describes the ones that were.
type Sources struct {
	Transitions []tracker.TransitionStat
	Dispatches  []time.Time
	MergeSHA    string
	Calls       []Call
	Commits     []Commit
	Runs        []PlaytestRun
	Missing     []Missing
	Read        []string
}

// Missing is a source that could not be read.
type Missing struct {
	Name   string // "CLI log", "Work branch", "Stats sidecar"
	Detail string // the paths tried and what that hides
}

// Call is one clilog record (internal/clilog owns the format) as the
// timeline reads it. Exit, ErrClass and DurMS are empty in records written
// before v0.54.
type Call struct {
	clilog.Record
	At time.Time // Record.Start()
	// Args are the record's args without issue-cli's global flags, so
	// Args[0] is the command. Record.Args keeps them as logged.
	Args []string
	// Agent: read from a per-agent clilog, so the issue's agent made it.
	// The project log also holds the lead's and the owner's calls on the
	// issue.
	Agent bool
}

// Cmd is the command name, "" for a record without args.
func (c Call) Cmd() string {
	if len(c.Args) == 0 {
		return ""
	}
	return c.Args[0]
}

// Failed: the call ran and exited non-zero. Records before v0.54 have no
// exit and never count as failed.
func (c Call) Failed() bool {
	return c.Exit != nil && *c.Exit != 0
}

// Commit is one first-parent commit on the issue's work branch.
type Commit struct {
	SHA     string
	At      time.Time
	Subject string
	Merge   bool
}

// PlaytestRun is one playtest run's results.yaml for the issue.
type PlaytestRun struct {
	Session string
	Mode    string // auto (ux-test) or judge (your playtest)
	Started time.Time
	Steps   []PlaytestStep
}

type PlaytestStep struct {
	ID     string `yaml:"id"`
	Status string `yaml:"status"`
}

// Failed lists the ids of the steps that failed.
func (r PlaytestRun) Failed() []string {
	var ids []string
	for _, s := range r.Steps {
		if s.Status == "fail" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// Passed: step id passed in this run.
func (r PlaytestRun) Passed(id string) bool {
	for _, s := range r.Steps {
		if s.ID == id {
			return s.Status == "pass"
		}
	}
	return false
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
