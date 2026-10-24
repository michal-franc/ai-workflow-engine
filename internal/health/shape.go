package health

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/testresults"
)

// The shape family: function length, the share of source in big files, and
// where change concentrates (churn and hotspots), read from git at the
// snapshot's commit like codebase. See docs/workbench-files.md, "The shape
// family".
func init() {
	Register("shape", shapeFamily)
	Describe("shape", "longest functions and hotspots: issue-cli health --family shape")
}

const (
	shapeTop       = 20
	churnDays      = 30
	hotspotCommits = 5
)

type shapeFile struct {
	lines int
	big   bool
}

func shapeFamily(ctx context.Context, env Env) ([]Metric, error) {
	if env.Commit == "" {
		if env.Backfill {
			return nil, ErrNoPast
		}
		return nil, fmt.Errorf("no commit to measure")
	}
	rules := rulesFor(env)
	longFrom := env.Config.LongFuncLimit()
	source := map[string]shapeFile{}
	var all []float64
	type longFn struct {
		path string
		fn
	}
	var long []longFn
	funcsByLang := map[string]int{}
	unparsed := map[string]int{}
	_, err := walkBlobs(ctx, env.Root, env.Commit, rules.skip, func(p string, data []byte) {
		if bytes.IndexByte(data[:min(len(data), sniffBytes)], 0) >= 0 {
			return
		}
		lang := classify(p)
		if lang.kind != KindCode {
			return
		}
		lines := bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		source[p] = shapeFile{lines, lines > rules.bigFrom}
		if matchAny(rules.tests, p) {
			return // function length is measured on code, not tests
		}
		fs, measured, ok := funcs(lang.name, data)
		switch {
		case !measured:
			return
		case !ok:
			unparsed[lang.name]++
			return
		}
		funcsByLang[lang.name] += len(fs)
		for _, f := range fs {
			all = append(all, float64(f.lines))
			if f.lines > longFrom {
				long = append(long, longFn{p, f})
			}
		}
	})
	if err != nil {
		return nil, err
	}

	var out []Metric
	for _, lang := range sortedCounts(funcsByLang) {
		if funcsByLang[lang] == 0 {
			continue
		}
		out = append(out, Metric{Name: "shape.funcs", Labels: map[string]string{"lang": lang}, Value: float64(funcsByLang[lang]), Unit: UnitCount, Help: "Functions parsed in code files"})
	}
	sort.Float64s(all)
	if len(all) > 0 {
		for _, st := range []struct {
			label string
			v     float64
		}{{"p50", testresults.Percentile(all, 50)}, {"p90", testresults.Percentile(all, 90)}, {"max", all[len(all)-1]}} {
			out = append(out, Metric{Name: "shape.func_lines", Labels: map[string]string{"stat": st.label}, Value: st.v, Unit: UnitCount, Help: "Function length in lines (code, not tests)"})
		}
		out = append(out,
			Metric{Name: "shape.long_funcs", Value: float64(len(long)), Unit: UnitCount, Help: fmt.Sprintf("Functions over %d lines", longFrom)},
			Metric{Name: "shape.long_funcs.share", Value: shareOf(len(long), len(all)), Unit: UnitShare, Help: "Long functions / all functions"})
	}
	sort.Slice(long, func(i, j int) bool {
		if long[i].lines != long[j].lines {
			return long[i].lines > long[j].lines
		}
		return long[i].path+long[i].name < long[j].path+long[j].name
	})
	for i, l := range long {
		if i == shapeTop {
			break
		}
		out = append(out, Metric{Name: "shape.long_func.lines", Labels: map[string]string{"path": l.path, "func": l.name}, Value: float64(l.lines), Unit: UnitCount, Help: "Lines in each of the longest functions"})
	}
	for _, lang := range sortedCounts(unparsed) {
		out = append(out, Metric{Name: "shape.unparsed_files", Labels: map[string]string{"lang": lang}, Value: float64(unparsed[lang]), Unit: UnitCount, Help: "Code files whose functions were not read"})
	}

	total, inBig := 0, 0
	for _, f := range source {
		total += f.lines
		if f.big {
			inBig += f.lines
		}
	}
	if total > 0 {
		out = append(out, Metric{Name: "shape.big_lines.share", Value: shareOf(inBig, total), Unit: UnitShare, Help: fmt.Sprintf("Source lines in files over %d lines / source lines", rules.bigFrom)})
	}

	churn, err := churnMetrics(ctx, env, source)
	if err != nil {
		return nil, err
	}
	return append(out, churn...), nil
}

// churnMetrics reads the last churnDays of history up to the commit: lines
// changed and commits per source file still in the tree.
func churnMetrics(ctx context.Context, env Env, source map[string]shapeFile) ([]Metric, error) {
	since := env.Now.AddDate(0, 0, -churnDays).Format(time.RFC3339)
	log, err := gitOut(ctx, env.Root, "log", env.Commit, "--no-renames", "--numstat", "--format=", "--since="+since)
	if err != nil {
		return nil, err
	}
	lines, commits := map[string]int{}, map[string]int{}
	for _, l := range strings.Split(log, "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 || f[0] == "-" {
			continue
		}
		if _, ok := source[f[2]]; !ok {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		lines[f[2]] += a + d
		commits[f[2]]++
	}
	total, inBig := 0, 0
	for p, n := range lines {
		total += n
		if source[p].big {
			inBig += n
		}
	}
	window := map[string]string{"window": fmt.Sprintf("%dd", churnDays)}
	var out []Metric
	if total > 0 {
		out = append(out, Metric{Name: "shape.churn.big_share", Labels: window, Value: shareOf(inBig, total), Unit: UnitShare, Help: "Share of source churn (lines changed) in big files"})
	}
	paths := make([]string, 0, len(lines))
	for p := range lines {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if lines[paths[i]] != lines[paths[j]] {
			return lines[paths[i]] > lines[paths[j]]
		}
		return paths[i] < paths[j]
	})
	for i, p := range paths {
		if i == shapeTop {
			break
		}
		labels := map[string]string{"path": p, "window": window["window"]}
		out = append(out,
			Metric{Name: "shape.churn.lines", Labels: labels, Value: float64(lines[p]), Unit: UnitCount, Help: "Lines changed in the most-churned source files"},
			Metric{Name: "shape.churn.commits", Labels: labels, Value: float64(commits[p]), Unit: UnitCount, Help: "Commits touching the most-churned source files"})
	}
	var hot []string
	for p, f := range source {
		if f.big && commits[p] >= hotspotCommits {
			hot = append(hot, p)
		}
	}
	sort.Slice(hot, func(i, j int) bool {
		a, b := source[hot[i]].lines*commits[hot[i]], source[hot[j]].lines*commits[hot[j]]
		if a != b {
			return a > b
		}
		return hot[i] < hot[j]
	})
	for i, p := range hot {
		if i == shapeTop {
			break
		}
		out = append(out, Metric{Name: "shape.hotspot", Labels: map[string]string{"path": p}, Value: float64(source[p].lines * commits[p]), Unit: UnitCount, Help: fmt.Sprintf("Big files changed in %d+ commits in %d days: lines × commits", hotspotCommits, churnDays)})
	}
	return out, nil
}

func shareOf(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(int(float64(a)/float64(b)*1000+0.5)) / 1000
}

func sortedCounts(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
