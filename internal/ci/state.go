// Package ci is the local CI: it runs a project's `ci:` commands on the head
// of its integration branch in a persistent worktree and records the result
// under <root>/.ci/. issue-cli runs it (`ci run`); the viewer only starts runs
// and reads .ci/state.json. See docs/workflow.md, "Local CI".
package ci

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Run states.
const (
	Green = "green"
	Red   = "red"
	// Error means CI could not run (git or the worktree failed): nobody is
	// blamed and nothing is marked red.
	Error = "error"
)

// keepRuns is how many run records .ci/runs keeps.
const keepRuns = 50

// CommandResult is one command of a run.
type CommandResult struct {
	Cmd        string   `json:"cmd"`
	Tier       string   `json:"tier"`
	Exit       int      `json:"exit"`
	DurationMS int64    `json:"duration_ms"`
	TimedOut   bool     `json:"timed_out,omitempty"`
	Failures   []string `json:"failures,omitempty"`
}

// Merged is one branch (or a commit made straight on the branch) that landed
// since the last green run. Slug is "" when no issue matches.
type Merged struct {
	Branch  string `json:"branch,omitempty"`
	Slug    string `json:"slug,omitempty"`
	Sha     string `json:"sha"`
	Subject string `json:"subject,omitempty"`
}

// Run is one recorded run, .ci/runs/<sha>.json (<sha>.<tier>.json for a
// non-default tier).
type Run struct {
	Sha      string          `json:"sha"`
	Subject  string          `json:"subject,omitempty"`
	Branch   string          `json:"branch"`
	Tier     string          `json:"tier"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished"`
	State    string          `json:"state"`
	Error    string          `json:"error,omitempty"`
	Commands []CommandResult `json:"commands"`
	// Failed is the command that stopped the run ("" when green).
	Failed    string   `json:"failed,omitempty"`
	Failures  []string `json:"failures,omitempty"`
	Log       string   `json:"log"`
	LogTail   []string `json:"log_tail,omitempty"`
	RangeFrom string   `json:"range_from,omitempty"`
	Merged    []Merged `json:"merged,omitempty"`
}

// TierState is the newest run of a non-default tier.
type TierState struct {
	Sha      string    `json:"sha"`
	State    string    `json:"state"`
	Failed   string    `json:"failed,omitempty"`
	Failures []string  `json:"failures,omitempty"`
	Finished time.Time `json:"finished"`
	Log      string    `json:"log"`
}

// State is .ci/state.json: where the branch stands. It is the one file the
// viewer reads. Blame and nudges follow the default tier only.
type State struct {
	Branch string `json:"branch"`
	// LastSha is the newest default-tier run's commit.
	LastSha  string `json:"last_sha,omitempty"`
	State    string `json:"state,omitempty"`
	GreenSha string `json:"green_sha,omitempty"`
	// GreenAt is when GreenSha's run finished.
	GreenAt time.Time `json:"green_at,omitempty"`
	// RedSince is the first red commit of the current red streak.
	RedSince string   `json:"red_since,omitempty"`
	Failed   string   `json:"failed,omitempty"`
	Failures []string `json:"failures,omitempty"`
	Error    string   `json:"error,omitempty"`
	// Merged / Blamed: what landed since the last green run, and the issues
	// among it (they carry the ci-red marker while red).
	Merged   []Merged  `json:"merged,omitempty"`
	Blamed   []string  `json:"blamed,omitempty"`
	RedIssue string    `json:"red_issue,omitempty"`
	Log      string    `json:"log,omitempty"`
	LogTail  []string  `json:"log_tail,omitempty"`
	Updated  time.Time `json:"updated"`
	// Tiers holds the newest run of each non-default tier.
	Tiers map[string]TierState `json:"tiers,omitempty"`
}

// IsRed reports whether the branch is red now.
func (s *State) IsRed() bool { return s != nil && s.State == Red }

// Tier returns the newest run of a non-default tier.
func (s *State) Tier(name string) (TierState, bool) {
	if s == nil {
		return TierState{}, false
	}
	ts, ok := s.Tiers[name]
	return ts, ok
}

// Dir is the project's CI directory.
func Dir(root string) string { return filepath.Join(root, ".ci") }

// RunsDir holds the run records and logs.
func RunsDir(root string) string { return filepath.Join(Dir(root), "runs") }

// StatePath is .ci/state.json.
func StatePath(root string) string { return filepath.Join(Dir(root), "state.json") }

// WorktreePath is the persistent CI worktree.
func WorktreePath(root string) string { return filepath.Join(root, ".worktrees", ".ci") }

// runName is a run's file stem: <sha>, or <sha>.<tier> off the default tier.
func runName(sha, tier string) string {
	if tier == "" || tier == defaultTier {
		return sha
	}
	return sha + "." + tier
}

// RelLog is a run's log path relative to the project root.
func RelLog(sha, tier string) string {
	return filepath.Join(".ci", "runs", runName(sha, tier)+".log")
}

// ensureDir creates .ci/runs and a .gitignore that ignores the whole
// directory, so no repo has to list .ci/ itself.
func ensureDir(root string) error {
	if err := os.MkdirAll(RunsDir(root), 0o755); err != nil {
		return err
	}
	ignore := filepath.Join(Dir(root), ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(ignore, []byte("# Local CI runs (issue-cli ci); never committed\n*\n"), 0o644)
	}
	return nil
}

// ReadState reads .ci/state.json; a missing file is a nil state, no error.
func ReadState(root string) (*State, error) {
	data, err := os.ReadFile(StatePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", StatePath(root), err)
	}
	return &st, nil
}

// writeJSONFile writes v atomically: a temp file renamed over path, so a
// reader never sees half a file.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// WriteState replaces .ci/state.json.
func WriteState(root string, st *State) error {
	if err := ensureDir(root); err != nil {
		return err
	}
	return writeJSONFile(StatePath(root), st)
}

func writeRun(root string, run *Run) error {
	return writeJSONFile(filepath.Join(RunsDir(root), runName(run.Sha, run.Tier)+".json"), run)
}

// RecentRuns lists the newest n run records, newest first.
func RecentRuns(root string, n int) []Run {
	paths, _ := filepath.Glob(filepath.Join(RunsDir(root), "*.json"))
	var runs []Run
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var r Run
		if json.Unmarshal(data, &r) == nil && r.Sha != "" {
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Started.After(runs[j].Started) })
	if n > 0 && len(runs) > n {
		runs = runs[:n]
	}
	return runs
}

// prune keeps the newest keepRuns records and their logs.
func prune(root string) {
	runs := RecentRuns(root, 0)
	if len(runs) <= keepRuns {
		return
	}
	for _, r := range runs[keepRuns:] {
		stem := filepath.Join(RunsDir(root), runName(r.Sha, r.Tier))
		os.Remove(stem + ".json")
		os.Remove(stem + ".log")
	}
}

// Short is a commit's 7-character form.
func Short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// FailureList is "TestA, TestB, TestC (+4)": the first three names.
func FailureList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s (+%d)", strings.Join(names[:3], ", "), len(names)-3)
}
