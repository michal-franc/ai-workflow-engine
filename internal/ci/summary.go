package ci

import (
	"fmt"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// OneLine is where the branch stands, in one line for agents and the lead:
//
//	main green at abc1234 (12m ago)
//	main red since def5678: make validate: TestA, TestB
//	main: CI can't run: <reason> — issue-cli ci reset
//
// A live run is appended: "· running at abc1234 (3m)".
func OneLine(st *State, lock *Lock, alive bool, branch string, now time.Time) string {
	if st != nil && st.Branch != "" {
		branch = st.Branch
	}
	if branch == "" {
		branch = "branch"
	}
	var line string
	switch {
	case st == nil || st.State == "":
		line = branch + ": no CI run yet"
	case st.State == Green:
		line = fmt.Sprintf("%s green at %s (%s ago)", branch, Short(st.GreenSha), Ago(now.Sub(st.GreenAt)))
	case st.State == Red:
		line = fmt.Sprintf("%s red since %s: %s", branch, Short(st.RedSince), st.Failed)
		if f := FailureList(st.Failures); f != "" {
			line += ": " + f
		}
	default:
		line = fmt.Sprintf("%s: CI can't run: %s — issue-cli ci reset", branch, firstLine(st.Error))
	}
	if lock != nil && alive {
		line += fmt.Sprintf(" · running at %s (%s)", Short(lock.Sha), Ago(now.Sub(lock.Started)))
	}
	return line
}

// Ago is a short duration: 40s, 12m, 3h, 2d.
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func init() {
	tracker.CIStatusLine = AgentLine
}

// AgentLine is the CI line for agents: the state of the branch they merge
// into, so a failure they see can be told apart from one they caused. ""
// without a ci: block.
func AgentLine(workDir string, wf *tracker.WorkflowConfig) string {
	if wf == nil || !wf.CI.Enabled() {
		return ""
	}
	root := RepoRoot(workDir)
	st, _ := ReadState(root)
	lock, alive := ReadLock(root)
	branch, _ := ResolveBranch(root, wf.CI)
	line := "Local CI: " + OneLine(st, lock, alive, branch, time.Now())
	if st.IsRed() {
		line += " — inherited from " + st.Branch + ", not caused by your branch"
	}
	return line
}
