package health

import (
	"context"
	"math"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/testresults"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The tests family: unit and e2e test counts and runtimes from the newest
// local CI run that wrote results for each kind (ci.run[].results), plus the
// playtest sessions in the tree and which systems' work they cover. It runs
// no tests. See docs/workbench-files.md, "The tests family".
func init() {
	Register("tests", testsFamily)
	Describe("tests", "slowest tests and uncovered systems: issue-cli health --family tests")
}

// TestKinds are the kinds a results: entry may name, in output order.
var TestKinds = []string{"unit", "e2e"}

const (
	slowestTop   = 10
	coverageDays = 90
)

// slowFrom is when one test counts as slow, per kind.
var slowFrom = map[string]float64{"unit": 1, "e2e": 30}

func testsFamily(ctx context.Context, env Env) ([]Metric, error) {
	var out []Metric
	for _, kind := range TestKinds {
		run, sum, ok := ci.LatestResults(env.Root, kind, env.Now)
		if !ok {
			continue
		}
		out = append(out, resultsMetrics(kind, run, sum, ci.ReadTests(env.Root, sum), env.Now)...)
	}
	e2e, err := sessionMetrics(ctx, env)
	if err != nil {
		return nil, err
	}
	return append(out, e2e...), ctx.Err()
}

func resultsMetrics(kind string, run *ci.Run, s testresults.Summary, tests []testresults.Test, now time.Time) []Metric {
	k := map[string]string{"kind": kind}
	with := func(key, v string) map[string]string { return map[string]string{"kind": kind, key: v} }
	out := []Metric{
		{Name: "tests.count", Labels: with("result", "all"), Value: float64(s.Tests), Unit: UnitCount, Help: "Tests in the newest CI run with results (top-level; e2e: steps)"},
		{Name: "tests.count", Labels: with("result", "pass"), Value: float64(s.Passed), Unit: UnitCount},
		{Name: "tests.count", Labels: with("result", "fail"), Value: float64(s.Failed), Unit: UnitCount},
		{Name: "tests.count", Labels: with("result", "skip"), Value: float64(s.Skipped), Unit: UnitCount},
		{Name: "tests.subtests", Labels: k, Value: float64(s.Subtests), Unit: UnitCount, Help: "Subtests (t.Run, test cases)"},
		{Name: "tests.wall_seconds", Labels: k, Value: s.WallS, Unit: UnitSeconds, Help: "The suite's own wall time"},
		{Name: "tests.sum_seconds", Labels: k, Value: s.SumS, Unit: UnitSeconds, Help: "Per-test time summed (more than the wall when tests run in parallel)"},
		{Name: "tests.per_test_ms", Labels: with("stat", "mean"), Value: s.MeanMS, Help: "Per-test runtime, ms"},
		{Name: "tests.per_test_ms", Labels: with("stat", "p50"), Value: s.P50MS},
		{Name: "tests.per_test_ms", Labels: with("stat", "p90"), Value: s.P90MS},
		{Name: "tests.run_age_days", Labels: k, Value: math.Round(now.Sub(run.Finished).Hours()/24*10) / 10, Help: "Age of the CI run the numbers come from, in days"},
	}
	for _, p := range s.Parts {
		out = append(out, Metric{Name: "tests.part.seconds", Labels: with("part", p.Name), Value: p.Seconds, Unit: UnitSeconds, Help: "The slowest parts (package, test class, session): their own time"})
	}
	if tests == nil {
		return out
	}
	slow := 0
	areas := map[string]int{}
	for _, t := range tests {
		if t.Seconds > slowFrom[kind] {
			slow++
		}
		areas[t.Area]++
	}
	out = append(out, Metric{Name: "tests.slow", Labels: k, Value: float64(slow), Unit: UnitCount, Help: "Tests over 1 s (unit) or steps over 30 s (e2e)"})
	for _, a := range sortedCounts(areas) {
		out = append(out, Metric{Name: "tests.count_by_area", Labels: with("area", a), Value: float64(areas[a]), Unit: UnitCount, Help: "Tests per area (top folder or namespace), next to codebase.lines{area}"})
	}
	sorted := append([]testresults.Test(nil), tests...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seconds > sorted[j].Seconds })
	for i, t := range sorted {
		if i == slowestTop || t.Seconds == 0 {
			break
		}
		name := t.Name
		if t.Part != "" && t.Part != "." && kind == "unit" {
			name = t.Part + "." + t.Name
		}
		out = append(out, Metric{Name: "tests.slowest.seconds", Labels: with("test", name), Value: t.Seconds, Unit: UnitSeconds, Help: "The slowest tests"})
	}
	return out
}

// session is the part of a playtest session file the family reads.
type session struct {
	Session string `yaml:"session"`
	Issue   string `yaml:"issue"`
	Steps   []struct {
		ID string `yaml:"id"`
	} `yaml:"steps"`
}

// sessionMetrics counts the playtest sessions and steps at the commit, per
// system, and the share of recent done work in systems that have one.
func sessionMetrics(ctx context.Context, env Env) ([]Metric, error) {
	globs := env.Config.E2ESessionGlobs()
	var sessions []session
	matched := 0
	_, err := walkBlobs(ctx, env.Root, env.Commit, func(p string) bool { return !matchAny(globs, p) }, func(p string, data []byte) {
		matched++
		var s session
		if yaml.Unmarshal(data, &s) == nil && len(s.Steps) > 0 {
			sessions = append(sessions, s)
		}
	})
	if err == nil && matched == 0 {
		Warn(ctx, "%s matched no file, so no e2e session rows", globNote("e2e_sessions", env.Config != nil && len(env.Config.E2ESessions) > 0, globs))
	}
	if err != nil || len(sessions) == 0 {
		return nil, err
	}
	var issues []*tracker.Issue
	if env.Project != nil && env.Project.IssueDir != "" {
		issues, _ = tracker.LoadIssues(env.Project.IssueDir)
	}
	system := map[string]string{}
	for _, is := range issues {
		system[is.Slug] = is.System
	}
	bySys, steps := map[string]int{}, map[string]int{}
	total := 0
	for _, s := range sessions {
		sys := system[s.Issue]
		if sys == "" {
			sys = "none"
		}
		bySys[sys]++
		steps[sys] += len(s.Steps)
		total += len(s.Steps)
	}
	out := []Metric{
		{Name: "tests.e2e.sessions", Labels: map[string]string{"system": "all"}, Value: float64(len(sessions)), Unit: UnitCount, Help: "Playtest sessions in the tree, per system of their issue"},
		{Name: "tests.e2e.steps", Labels: map[string]string{"system": "all"}, Value: float64(total), Unit: UnitCount, Help: "Playtest steps in the tree"},
	}
	for _, sys := range sortedCounts(bySys) {
		l := map[string]string{"system": sys}
		out = append(out,
			Metric{Name: "tests.e2e.sessions", Labels: l, Value: float64(bySys[sys]), Unit: UnitCount},
			Metric{Name: "tests.e2e.steps", Labels: l, Value: float64(steps[sys]), Unit: UnitCount})
	}
	return append(out, coverageMetrics(issues, bySys, env.Now)...), nil
}

// coverageMetrics: of the issues done in the last coverageDays (with a
// system), the share in systems with at least one session, and how many were
// done in each system that has none.
func coverageMetrics(issues []*tracker.Issue, sessions map[string]int, now time.Time) []Metric {
	from := now.AddDate(0, 0, -coverageDays)
	done := map[string]int{}
	total, covered := 0, 0
	for _, is := range issues {
		if is.Status != "done" || is.System == "" {
			continue
		}
		at, ok := doneAt(is.FilePath)
		if !ok || at.Before(from) || at.After(now) {
			continue
		}
		total++
		done[is.System]++
		if sessions[is.System] > 0 {
			covered++
		}
	}
	if total == 0 {
		return nil
	}
	out := []Metric{{Name: "tests.e2e.coverage", Value: shareOf(covered, total), Unit: UnitShare, Help: "Issues done in 90 days whose system has a playtest session / issues done with a system"}}
	for _, sys := range sortedCounts(done) {
		if sessions[sys] == 0 {
			out = append(out, Metric{Name: "tests.e2e.uncovered", Labels: map[string]string{"system": sys}, Value: float64(done[sys]), Unit: UnitCount, Help: "Issues done in 90 days in a system with no playtest session"})
		}
	}
	return out
}

// doneAt is the last recorded transition to done in the issue's stats.
func doneAt(issuePath string) (time.Time, bool) {
	st, err := tracker.LoadStats(issuePath)
	if err != nil {
		return time.Time{}, false
	}
	for i := len(st.Transitions) - 1; i >= 0; i-- {
		if st.Transitions[i].To == "done" {
			return st.Transitions[i].TS, true
		}
	}
	return time.Time{}, false
}
