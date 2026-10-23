package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// timelineSources is everything the Timeline tab reads for one issue,
// gathered by loadTimelineSources and turned into a page by buildTimeline.
// The fields up to Runs are timeline.Sources, what the split reads; the rest
// only the tab shows. Missing lists the sources that could not be read; Read
// describes the ones that were.
type timelineSources struct {
	Transitions []statRow
	Dispatches  []time.Time
	MergeSHA    string
	Calls       []cliCall
	Commits     []gitCommit
	Runs        []playtestRun
	Comments    []tracker.Comment
	Decisions   []tracker.Decision
	CI          []ciRun
	Boxes       []tracker.CheckboxItem
	Prompt      string
	Missing     []missingSource
	Read        []string
}

// core is what timeline.Split reads.
func (s timelineSources) core() timeline.Sources {
	return timeline.Sources{Transitions: s.Transitions, Dispatches: s.Dispatches, MergeSHA: s.MergeSHA, Calls: s.Calls, Commits: s.Commits, Runs: s.Runs}
}

type missingSource = timeline.Missing

// statRow is one stats-sidecar transition (tracker.TransitionStat): ts,
// approved_at, and branch_head / merge_sha from v0.54 on.
type statRow = tracker.TransitionStat

// cliCall is one clilog record as the timeline reads it (timeline.Call).
type cliCall = timeline.Call

// callRecord is the call as timelineEventFromRecord takes it: the logged
// record with global flags stripped.
func callRecord(c cliCall) clilog.Record {
	r := c.Record
	r.Args = c.Args
	return r
}

type gitCommit = timeline.Commit

type playtestRun = timeline.PlaytestRun

type playtestStep = timeline.PlaytestStep

type ciRun struct {
	SHA      string
	Started  time.Time
	State    string
	Commands []string
	Duration time.Duration
}

// loadTimelineSources reads what timeline.Load reads (stats sidecar,
// clilogs, work branch, playtest runs), and the comments, decisions,
// dispatch prompt and local CI runs only the tab shows. A source that can't
// be read is listed in Missing; it never fails the page.
func loadTimelineSources(proj *tracker.Project, issue *tracker.Issue, wf *tracker.WorkflowConfig) timelineSources {
	core := timeline.Load(proj, issue, nil)
	src := timelineSources{
		Transitions: core.Transitions,
		Dispatches:  core.Dispatches,
		MergeSHA:    core.MergeSHA,
		Calls:       core.Calls,
		Commits:     core.Commits,
		Runs:        core.Runs,
		Missing:     core.Missing,
	}
	root := projectRoot(proj)
	workDir := resolveProjectWorkDir(proj)
	worktree := filepath.Join(root, ".worktrees", issue.Slug)
	for _, d := range []string{worktree, workDir} {
		if p := LoadDispatchPrompt(d, tracker.AgentSessionName(issue.Slug)); p != "" {
			src.Prompt = p
			break
		}
		if p := LoadDispatchPrompt(d, issue.Assignee); p != "" {
			src.Prompt = p
			break
		}
	}

	src.Comments, _ = tracker.LoadComments(issue.FilePath)
	src.Decisions = tracker.ParseDecisions(issue.Slug, issue.BodyRaw)
	src.Boxes = tracker.ListCheckboxes(issue.BodyRaw)
	// The comments read goes after the stats and CLI log reads.
	for _, r := range core.Read {
		if strings.HasPrefix(r, "git (") || strings.HasPrefix(r, "playtest runs") {
			if !hasPrefix(src.Read, "comments (") {
				src.Read = append(src.Read, "comments ("+strconv.Itoa(len(src.Comments))+")")
			}
		}
		src.Read = append(src.Read, r)
	}
	if src.MergeSHA != "" {
		src.CI = loadCIRunsFrom(root, src.MergeSHA)
	}
	return src
}

func hasPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

type ciRunFile struct {
	SHA      string    `json:"sha"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	State    string    `json:"state"`
	Commands []struct {
		Cmd string `json:"cmd"`
	} `json:"commands"`
}

// loadCIRunsFrom returns the local CI run on sha and every run after it.
// Nil when sha was never run.
func loadCIRunsFrom(root, sha string) []ciRun {
	files, _ := filepath.Glob(filepath.Join(root, ".ci", "runs", "*.json"))
	var all []ciRun
	var first time.Time
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r ciRunFile
		if json.Unmarshal(data, &r) != nil || r.Started.IsZero() {
			continue
		}
		run := ciRun{SHA: r.SHA, Started: r.Started, State: r.State, Duration: r.Finished.Sub(r.Started)}
		for _, c := range r.Commands {
			run.Commands = append(run.Commands, c.Cmd)
		}
		if strings.HasPrefix(r.SHA, sha) || strings.HasPrefix(sha, r.SHA) {
			first = r.Started
		}
		all = append(all, run)
	}
	if first.IsZero() {
		return nil
	}
	var out []ciRun
	for _, r := range all {
		if !r.Started.Before(first) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

func shortSHA(sha string) string { return timeline.ShortSHA(sha) }

// timelineTotalLabel is the Timeline tab's label on every tab: the life from
// dispatch (or the agent's first call) to done, or "so far". It reads the
// stats sidecar and the first line of each per-agent clilog only.
func timelineTotalLabel(proj *tracker.Project, issue *tracker.Issue) string {
	stats, err := tracker.LoadStats(issue.FilePath)
	if err != nil {
		return ""
	}
	src := timelineSources{Transitions: stats.Transitions}
	for _, d := range stats.Dispatches {
		src.Dispatches = append(src.Dispatches, d.At)
	}
	if len(src.Dispatches) == 0 {
		worktree := filepath.Join(projectRoot(proj), ".worktrees", issue.Slug)
		paths := timeline.LogPaths(resolveProjectWorkDir(proj), worktree, issue)
		for _, p := range paths[:len(paths)-2] { // per-agent logs only
			if c, ok := firstCliCall(p, proj.Slug); ok && (len(src.Calls) == 0 || c.At.Before(src.Calls[0].At)) {
				src.Calls = []cliCall{c}
			}
		}
	}
	start := timeline.LifeStart(src.core())
	end, inFlight := timeline.LifeEnd(src.core(), timeline.In{Status: issue.Status, Now: time.Now()})
	if start.IsZero() || !end.After(start) {
		return ""
	}
	label := fmtSpan(end.Sub(start))
	if inFlight {
		label += " so far"
	}
	return label
}

// firstCliCall is the first record in a per-agent clilog for projSlug.
func firstCliCall(path, projSlug string) (cliCall, bool) {
	var first cliCall
	found := false
	clilog.Read(path, func(rec clilog.Record) {
		if found {
			return
		}
		if p := clilog.FlagValue(rec.Args, "--project"); p != "" && projSlug != "" && p != projSlug {
			return
		}
		if at := rec.Start(); !at.IsZero() {
			first, found = cliCall{Record: rec, At: at, Args: clilog.StripGlobal(rec.Args), Agent: true}, true
		}
	})
	return first, found
}
