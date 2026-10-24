// Package milestone finds a project's current milestone: the first version
// after the newest release that still has issues. The Status page's release
// card and the `milestone` health family both use it, so they always agree.
package milestone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func init() {
	health.Register("milestone", family)
	health.Describe("milestone", "gauge: /stats")
}

// Norm drops a leading "v" and pads to three parts ("0.53" → "0.53.0"), so
// issue versions compare with release headings. "" for a version that
// doesn't start with a digit.
func Norm(v string) string {
	v = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
	if v == "" || v[0] < '0' || v[0] > '9' {
		return ""
	}
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

// Compare orders dotted versions numerically (0.2 < 0.10); parts that
// aren't numbers compare as text.
func Compare(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		if ex == nil && ey == nil {
			if nx != ny {
				return nx - ny
			}
			continue
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}

// Statuses says which statuses count as done (Final) and which are left out
// of a milestone's total (Side: optional statuses such as parked). Keys are
// lower case.
type Statuses struct {
	Final string
	Side  map[string]bool
}

// StatusesOf reads them from the workflow: the last non-optional status is
// final, optional ones are side statuses.
func StatusesOf(wf *tracker.WorkflowConfig) Statuses {
	st := Statuses{Side: map[string]bool{}}
	if wf == nil {
		return st
	}
	for _, s := range wf.Statuses {
		k := key(s.Name)
		if s.Optional {
			st.Side[k] = true
		} else {
			st.Final = k
		}
	}
	return st
}

func key(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Count is done/total over items, side statuses left out of total.
func (st Statuses) Count(items []*tracker.Issue) (done, total int) {
	for _, it := range items {
		k := key(it.Status)
		if st.Side[k] {
			continue
		}
		total++
		if k == st.Final {
			done++
		}
	}
	return done, total
}

// Pick is the lowest version strictly after released that has an issue not
// parked. With no release it is the lowest version with an open issue, else
// the highest one (all done: ready to cut). "" when no version qualifies.
func Pick(byVersion map[string][]*tracker.Issue, st Statuses, released string) (version string, done, total int, items []*tracker.Issue) {
	var versions []string
	for v := range byVersion {
		if Norm(v) != "" {
			versions = append(versions, v)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return Compare(Norm(versions[i]), Norm(versions[j])) < 0 })
	rel := Norm(released)
	var last string
	for _, v := range versions {
		d, t := st.Count(byVersion[v])
		if t == 0 {
			continue
		}
		if rel != "" {
			if Compare(Norm(v), rel) > 0 {
				return v, d, t, byVersion[v]
			}
			continue
		}
		if d < t {
			return v, d, t, byVersion[v]
		}
		last = v
	}
	if rel == "" && last != "" {
		d, t := st.Count(byVersion[last])
		return last, d, t, byVersion[last]
	}
	return "", 0, 0, nil
}

// ChangelogHeading matches "## v0.40.0 — 2026-10-21" (also "-" or "–"
// between version and date, or no date): group 1 is the version, 2 the
// date.
var ChangelogHeading = regexp.MustCompile(`^##\s+\[?(v?\d[^\s\]]*)\]?(?:\s+[—–-]\s+(\d{4}-\d{2}-\d{2}))?`)

// Released is the newest released version: the first version heading of
// the changelog (configured, else CHANGELOG.md) under root, else the newest
// git tag that is not a pre-release (a candidate tag is not released). ""
// when there is neither.
func Released(ctx context.Context, root, changelog string) string {
	if changelog == "" {
		changelog = "CHANGELOG.md"
	}
	if data, err := os.ReadFile(filepath.Join(root, changelog)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if m := ChangelogHeading.FindStringSubmatch(line); m != nil {
				return m[1]
			}
		}
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "for-each-ref", "--sort=-creatordate", "--format=%(refname:short)", "refs/tags").Output()
	if err != nil {
		return ""
	}
	return newestRelease(string(out))
}

// newestRelease is the first tag of a newest-first list that is not a
// pre-release.
func newestRelease(tags string) string {
	for _, t := range strings.Split(tags, "\n") {
		if t = strings.TrimSpace(t); t != "" && !IsPreRelease(t) {
			return t
		}
	}
	return ""
}

// ByVersion groups issues by their version: field; issues without one are
// left out.
func ByVersion(issues []*tracker.Issue) map[string][]*tracker.Issue {
	out := map[string][]*tracker.Issue{}
	for _, is := range issues {
		if is.Version != "" {
			out[is.Version] = append(out[is.Version], is)
		}
	}
	return out
}

// family is the `milestone` health family: the current milestone's done and
// total issues, labelled with its version. It knows only the present, so a
// backfill leaves it out.
func family(ctx context.Context, env health.Env) ([]health.Metric, error) {
	if env.Backfill {
		return nil, health.ErrNoPast
	}
	if env.Project == nil || env.Project.IssueDir == "" {
		return nil, nil
	}
	issues, err := tracker.LoadIssues(env.Project.IssueDir)
	if err != nil {
		return nil, err
	}
	changelog := ""
	if env.Workflow != nil {
		changelog = env.Workflow.Workbench.Changelog
	}
	v, done, total, _ := Pick(ByVersion(issues), StatusesOf(env.Workflow), Released(ctx, env.Root, changelog))
	if v == "" {
		return nil, nil
	}
	l := map[string]string{"version": v}
	return []health.Metric{
		{Name: "milestone.done", Labels: l, Value: float64(done), Unit: health.UnitCount, Help: "Current milestone: issues done"},
		{Name: "milestone.total", Labels: l, Value: float64(total), Unit: health.UnitCount, Help: "Current milestone: issues, parked left out"},
	}, nil
}
