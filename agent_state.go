package main

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// agentIdleAfter is how long a live agent may go without an issue-cli call
// before it reads as idle rather than working (D-7). A long build with no
// call reads as idle too; the footer says "no issue-cli call", not stopped.
// It is the Timeline's idle gap, so the canvas and the split agree.
const agentIdleAfter = timeline.IdleAfter

// cliTailBytes is how much of a clilog's end lastCliCall reads: enough for
// the newest record even when its args carry a long --body.
const cliTailBytes = 64 * 1024

// lastCall is an agent's newest issue-cli call.
type lastCall struct {
	At  time.Time
	Cmd string // e.g. "check ui/foo D3", without --project
}

// lastCliCall is the newest call across the clilogs at paths for project
// projSlug: the one that ended last (start + duration; old records have
// only the start). Records naming another --project are skipped: test runs
// in the agent's shell inherit ISSUE_CLI_LOG and log their fixture projects.
func lastCliCall(projSlug string, paths ...string) (lastCall, bool) {
	var newest lastCall
	found := false
	for _, p := range paths {
		if c, ok := tailCliRecord(p, projSlug); ok && (!found || c.At.After(newest.At)) {
			newest, found = c, true
		}
	}
	return newest, found
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
		var rec clilog.Record
		if json.Unmarshal([]byte(lines[i]), &rec) != nil {
			continue // the first line may be cut mid-record
		}
		if p := flagValue(rec.Args, "--project"); p != "" && projSlug != "" && p != projSlug {
			continue
		}
		at := rec.End()
		if at.IsZero() {
			continue
		}
		return lastCall{At: at, Cmd: strings.Join(stripGlobalFlags(rec.Args), " ")}, true
	}
	return lastCall{}, false
}

// agentLogPaths are where a worker's clilog may be: the project work dir
// under the session name (dispatch writes it there since v0.54) and under the
// assignee, then its worktree, where dispatch wrote it before. lastCliCall
// takes the newest call across them.
func agentLogPaths(workDir, worktree, session, assignee string) []string {
	var paths []string
	if workDir != "" {
		paths = append(paths, clilog.AgentPath(workDir, session))
		if assignee != "" && assignee != session {
			paths = append(paths, clilog.AgentPath(workDir, assignee))
		}
	}
	if worktree != "" {
		paths = append(paths, clilog.AgentPath(worktree, session))
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
