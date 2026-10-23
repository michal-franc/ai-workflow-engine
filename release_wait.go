package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Status page's "Waiting for release" card: what has merged and not been
// released (## Unreleased in the changelog), what is on the local branch and
// not on its upstream, and whether the current milestone (the first version
// after the newest release) is ready to cut. It costs one git call per
// render and reads nothing the page doesn't already read.

// ReleaseWait is the card; nil when it has nothing to show.
type ReleaseWait struct {
	Milestone *MilestoneRow
	// Changelog is the changelog's path; "" without one (no Unreleased row,
	// no changelog check).
	Changelog  string
	Unreleased []string
	Unpushed   *UnpushedRow
}

// MilestoneRow is the version the next release cuts.
type MilestoneRow struct {
	Version  string
	Cut      string // "v0.53.0"
	Progress Progress
	// Open are its issues neither done nor parked, furthest along first.
	Open   []*tracker.Issue
	Checks []ReleaseCheck
	Ready  bool
	Today  string
}

// ReleaseCheck is one condition for cutting the release.
type ReleaseCheck struct {
	Key  string // issues, ci, changelog
	Text string
	Meta string
	// Detail is a second line saying why the check fails.
	Detail string
	OK     bool
}

// UnpushedRow is the local branch against its upstream.
type UnpushedRow struct {
	Branch   string
	Upstream string
	Count    int
	NewestAt time.Time
}

// Reasons lists why the milestone is not ready, for status.md.
func (m *MilestoneRow) Reasons() []string {
	var out []string
	for _, c := range m.Checks {
		if c.OK {
			continue
		}
		switch c.Key {
		case "issues":
			out = append(out, plural(len(m.Open), "issue", "issues")+" open")
		case "changelog":
			out = append(out, "nothing under ## Unreleased")
		default:
			out = append(out, c.Detail)
		}
	}
	return out
}

// BranchState is a local branch and its upstream, from one git call.
type BranchState struct {
	Branch   string
	Upstream string // "" without one (or when it is gone)
	Ahead    int
	Head     string
	HeadAt   time.Time
}

// gitBranchState reads branch (else the checked-out one) and its upstream in
// one for-each-ref call. A var so tests don't run git.
var gitBranchState = func(root, branch string) (BranchState, bool) {
	out, err := exec.Command("git", "-C", root, "for-each-ref",
		"--format=%(HEAD)%00%(refname:short)%00%(upstream:short)%00%(upstream:track)%00%(objectname)%00%(committerdate:unix)",
		"refs/heads").Output()
	if err != nil {
		return BranchState{}, false
	}
	return parseBranchState(string(out), branch)
}

// parseBranchState picks branch's row of gitBranchState's output, or the
// checked-out one ("*") when branch is "".
func parseBranchState(out, branch string) (BranchState, bool) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 6 {
			continue
		}
		if (branch == "" && f[0] != "*") || (branch != "" && f[1] != branch) {
			continue
		}
		st := BranchState{Branch: f[1], Upstream: f[2], Head: f[4]}
		if unix, err := strconv.ParseInt(f[5], 10, 64); err == nil {
			st.HeadAt = time.Unix(unix, 0)
		}
		if f[3] == "[gone]" {
			st.Upstream = ""
		}
		if m := aheadRe.FindStringSubmatch(f[3]); m != nil {
			st.Ahead, _ = strconv.Atoi(m[1])
		}
		return st, true
	}
	return BranchState{}, false
}

var aheadRe = regexp.MustCompile(`ahead (\d+)`)

// buildReleaseWait assembles the card. released is the newest released
// version ("" for none); changelog and text are the changelog's path and
// contents ("" without one).
func buildReleaseWait(proj *tracker.Project, wf *tracker.WorkflowConfig, cs *canvasStatuses, byVersion map[string][]*tracker.Issue,
	released, changelog, text string, now time.Time) *ReleaseWait {
	rw := &ReleaseWait{Changelog: changelog}
	entries, found := parseUnreleased(text)
	rw.Unreleased = entries

	var ciBranch string
	if wf.CI.Enabled() {
		ciBranch = strings.TrimSpace(wf.CI.Branch)
	}
	bs, haveBranch := gitBranchState(projectRoot(proj), ciBranch)
	if haveBranch && bs.Upstream != "" {
		rw.Unpushed = &UnpushedRow{Branch: bs.Branch, Upstream: bs.Upstream, Count: bs.Ahead, NewestAt: bs.HeadAt}
	}

	if v, p, items := pickMilestone(byVersion, cs, released); v != "" {
		ms := &MilestoneRow{Version: v, Cut: cutName(v), Progress: p, Today: now.Format("2006-01-02")}
		for _, issue := range items {
			k := statusKey(issue.Status)
			if !cs.side[k] && k != cs.final {
				ms.Open = append(ms.Open, issue)
			}
		}
		sort.SliceStable(ms.Open, func(i, j int) bool {
			return wf.GetStatusIndex(ms.Open[i].Status) > wf.GetStatusIndex(ms.Open[j].Status)
		})
		issuesCheck := ReleaseCheck{Key: "issues", Text: "Every " + v + " issue done", OK: p.Done == p.Total}
		if issuesCheck.OK {
			issuesCheck.Meta = fmt.Sprintf("%d/%d", p.Done, p.Total)
		} else {
			issuesCheck.Meta = fmt.Sprintf("%d open", len(ms.Open))
		}
		ms.Checks = append(ms.Checks, issuesCheck)
		if st, on := ciState(proj); on {
			branch := bs.Branch
			if branch == "" {
				branch = "the branch"
			}
			ms.Checks = append(ms.Checks, ciCheck(st, branch, bs.Head))
		}
		if changelog != "" {
			c := ReleaseCheck{Key: "changelog", Text: "Entries under ## Unreleased", OK: len(entries) > 0}
			if c.OK {
				c.Meta = strconv.Itoa(len(entries))
			} else {
				c.Meta = "none"
			}
			ms.Checks = append(ms.Checks, c)
		}
		ms.Ready = true
		for _, c := range ms.Checks {
			ms.Ready = ms.Ready && c.OK
		}
		rw.Milestone = ms
	}

	// A changelog without the Unreleased convention says nothing here
	// unless there is a milestone to cut.
	if !found && rw.Milestone == nil {
		rw.Changelog = ""
	}
	if rw.Milestone == nil && rw.Changelog == "" && rw.Unpushed == nil {
		return nil
	}
	return rw
}

// releaseWaitMarkdown is the card in status.md: slugs, one verdict line.
func releaseWaitMarkdown(b *strings.Builder, rw *ReleaseWait, now time.Time) {
	b.WriteString("\n## Waiting for release\n")
	if ms := rw.Milestone; ms != nil {
		fmt.Fprintf(b, "- Milestone %s: %d/%d done; ", ms.Version, ms.Progress.Done, ms.Progress.Total)
		switch {
		case !ms.Ready:
			fmt.Fprintf(b, "%s not ready: %s\n", ms.Cut, strings.Join(ms.Reasons(), "; "))
		case rw.Changelog != "":
			fmt.Fprintf(b, "ready to cut %s: rename ## Unreleased in %s to ## %s — %s, then push (with the owner's go-ahead)\n", ms.Cut, rw.Changelog, ms.Cut, ms.Today)
		default:
			fmt.Fprintf(b, "ready to cut %s: tag it and push (with the owner's go-ahead)\n", ms.Cut)
		}
		for i, issue := range ms.Open {
			if i == capMDList {
				fmt.Fprintf(b, "  - +%d more: issue-cli list --version %s --status open\n", len(ms.Open)-i, ms.Version)
				break
			}
			who := issue.Assignee
			if who == "" {
				who = "no assignee"
			}
			fmt.Fprintf(b, "  - %s (%s, %s)\n", issue.Slug, issue.Status, who)
		}
	}
	if rw.Changelog != "" {
		if len(rw.Unreleased) == 0 {
			fmt.Fprintf(b, "- Unreleased in %s: nothing under ## Unreleased\n", rw.Changelog)
		} else {
			fmt.Fprintf(b, "- Unreleased in %s (%d):\n", rw.Changelog, len(rw.Unreleased))
			for i, e := range rw.Unreleased {
				if i == capMDList {
					fmt.Fprintf(b, "  - +%d more in %s\n", len(rw.Unreleased)-i, rw.Changelog)
					break
				}
				fmt.Fprintf(b, "  - %s\n", e)
			}
		}
	}
	if u := rw.Unpushed; u != nil {
		if u.Count == 0 {
			fmt.Fprintf(b, "- Not pushed: nothing, %s matches %s\n", u.Branch, u.Upstream)
		} else {
			fmt.Fprintf(b, "- Not pushed: %s on %s not on %s, newest %s ago\n",
				plural(u.Count, "commit", "commits"), u.Branch, u.Upstream, shortDuration(now.Sub(u.NewestAt)))
		}
	}
}

// ciCheck passes when local CI is green on the branch head. head "" (git
// failed) can't be compared, so green is taken as is.
func ciCheck(st *ci.State, branch, head string) ReleaseCheck {
	c := ReleaseCheck{Key: "ci", Text: "Local CI green on " + branch + "'s head"}
	switch {
	case st == nil || st.State == "":
		c.Meta, c.Detail = "no run", "no CI run yet"
	case st.State == ci.Red:
		c.Meta = "red"
		if st.Branch != "" {
			branch = st.Branch
		}
		c.Detail = fmt.Sprintf("%s red since %s: %s", branch, ci.Short(st.RedSince), st.Failed)
	case st.State == ci.Green && (head == "" || sameSha(head, st.GreenSha)):
		c.OK, c.Meta = true, ci.Short(st.GreenSha)
	case st.State == ci.Green:
		c.Meta = "untested"
		c.Detail = fmt.Sprintf("head %s not tested yet (green at %s)", ci.Short(head), ci.Short(st.GreenSha))
	default:
		c.Meta = "error"
		c.Detail = "CI can't run: " + strings.SplitN(st.Error, "\n", 2)[0]
	}
	return c
}

func sameSha(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// pickMilestone is milestone.Pick with the page's status classes, as a
// Progress.
func pickMilestone(byVersion map[string][]*tracker.Issue, cs *canvasStatuses, released string) (string, Progress, []*tracker.Issue) {
	v, done, total, items := milestone.Pick(byVersion, milestone.Statuses{Final: cs.final, Side: cs.side}, released)
	return v, Progress{Label: v, Done: done, Total: total}, items
}

// normVersion is milestone.Norm.
func normVersion(v string) string { return milestone.Norm(v) }

// cutName is the release a milestone cuts: "0.53" → "v0.53.0".
func cutName(v string) string { return "v" + normVersion(v) }

// unreleasedHeading matches "## Unreleased" (also "## [Unreleased]").
var unreleasedHeading = regexp.MustCompile(`(?i)^##\s+\[?unreleased\]?(\s|$)`)

// parseUnreleased returns the top-level bullets under ## Unreleased, wrapped
// lines joined and nested bullets left out, each clipped like a release
// summary. found is false when there is no such heading.
func parseUnreleased(text string) (entries []string, found bool) {
	inEntry := false
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			if found {
				break
			}
			found = unreleasedHeading.MatchString(line)
			continue
		}
		if !found {
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			entries = append(entries, strings.TrimSpace(line[2:]))
			inEntry = true
		case inEntry && t != "" && line != t && !strings.HasPrefix(t, "- ") && !strings.HasPrefix(t, "* "):
			entries[len(entries)-1] += " " + t
		default:
			inEntry = false
		}
	}
	for i := range entries {
		entries[i] = clip(entries[i], 140)
	}
	return entries, found
}
