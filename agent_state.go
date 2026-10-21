package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// agentIdleAfter is how long a live agent may go without an issue-cli call
// before it reads as idle rather than working (D-7). A long build with no
// call reads as idle too; the footer says "no issue-cli call", not stopped.
const agentIdleAfter = 15 * time.Minute

// cliTailBytes is how much of a clilog's end lastCliCall reads: enough for
// the newest record even when its args carry a long --body.
const cliTailBytes = 64 * 1024

// lastCall is an agent's newest issue-cli call.
type lastCall struct {
	At  time.Time
	Cmd string // e.g. "check ui/foo D3", without --project
}

// lastCliCall reads the newest record of the first clilog that has one for
// project projSlug. Records naming another --project are skipped: test runs
// in the agent's shell inherit ISSUE_CLI_LOG and log their fixture projects.
// Callers pass the worktree's log before the project's.
func lastCliCall(projSlug string, paths ...string) (lastCall, bool) {
	for _, p := range paths {
		if c, ok := tailCliRecord(p, projSlug); ok {
			return c, true
		}
	}
	return lastCall{}, false
}

func tailCliRecord(path, projSlug string) (lastCall, bool) {
	f, err := os.Open(path)
	if err != nil {
		return lastCall{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return lastCall{}, false
	}
	start := info.Size() - cliTailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return lastCall{}, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return lastCall{}, false
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var rec cliLogRecord
		if json.Unmarshal([]byte(lines[i]), &rec) != nil {
			continue // the first line may be cut mid-record
		}
		if p := flagValue(rec.Args, "--project"); p != "" && projSlug != "" && p != projSlug {
			continue
		}
		at, err := time.Parse(time.RFC3339, rec.TS)
		if err != nil {
			continue
		}
		return lastCall{At: at, Cmd: strings.Join(stripGlobalFlags(rec.Args), " ")}, true
	}
	return lastCall{}, false
}

// agentLogPaths are where a worker's clilog may be: its worktree (dispatch
// writes <workDir>/.agent-logs/<session>/<session>.clilog), then the project
// work dir under the session name and under the assignee.
func agentLogPaths(workDir, worktree, session, assignee string) []string {
	var paths []string
	if worktree != "" {
		paths = append(paths, filepath.Join(worktree, ".agent-logs", session, session+".clilog"))
	}
	if workDir != "" {
		paths = append(paths, filepath.Join(workDir, ".agent-logs", session, session+".clilog"))
		if assignee != "" && assignee != session {
			paths = append(paths, filepath.Join(workDir, ".agent-logs", assignee, assignee+".clilog"))
		}
	}
	return paths
}

// workerState is a live worker's state, shared by the agents canvas and the
// board footer: an open question wins over a gate, a gate over idle, idle over
// working. gate is the pending approval's status, if any.
func workerState(wf *tracker.WorkflowConfig, issue *tracker.Issue, comments []tracker.Comment, call lastCall, known bool, now time.Time) (state, gate string) {
	gate = pendingGate(wf, issue)
	switch {
	case openQuestion(comments):
		return "question", gate
	case gate != "":
		return "at-gate", gate
	case known && now.Sub(call.At) >= agentIdleAfter:
		return "idle", ""
	default:
		return "working", ""
	}
}

// agoText is a short "N m/h/d" age for footers and chips.
func agoText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h"
	default:
		return strconv.Itoa(int(d/(24*time.Hour))) + " d"
	}
}

// flagValue is the value after flag in args, or "".
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
