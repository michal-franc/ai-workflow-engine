package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, text string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func metricByID(r MetricsReport, id string) *GuardMetric {
	for i := range r.Metrics {
		if r.Metrics[i].ID == id {
			return &r.Metrics[i]
		}
	}
	return nil
}

const runJSON = `{
  "run": {"date": "2026-10-01", "commit": "abc1234", "n": 200, "command": "make balance"},
  "metrics": [
    {"id": "full-holds", "name": "Full holds", "group": "Headline", "band": {"max": 20, "unit": "pct"}, "value": 7, "n": 200, "verdict": "Pass", "probe": "Pacing"},
    {"id": "open-ladder", "name": "Opened by the ladder", "group": "Headline", "band": {"min": 15, "unit": "pct"}, "value": 11, "n": 200, "verdict": "Near"},
    {"id": "stuck", "group": "Headline", "band": {"max": 0, "unit": "count"}, "value": 10},
    {"id": "series-ms", "group": "Slice", "band": {"max": 2000, "unit": "ms", "max_exclusive": true}, "value": 268},
    {"id": "no-band", "group": "Slice", "value": "n/a"}
  ]
}`

func TestLoadMetricsReadsTheContract(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "probe/old.json", `{"run":{"date":"2026-09-01"},"metrics":[{"id":"full-holds","value":99}]}`)
	writeFile(t, root, "probe/new.json", runJSON)
	wb := WorkbenchConfig{
		Metrics: WorkbenchMetrics{ProbeResults: "probe/*.json"},
		Systems: map[string]WorkbenchSystem{
			"Sim": {Metrics: []string{"full-holds", "st*"}},
			"AI":  {Families: map[string]WorkbenchFamily{"plays": {Metrics: []string{"open-ladder"}}}},
		},
	}
	rep := LoadMetrics(root, wb, nil)
	if rep.Run == nil || rep.Run.File != "probe/new.json" || rep.Run.Commit != "abc1234" || rep.Run.N != 200 {
		t.Fatalf("run = %+v", rep.Run)
	}
	if len(rep.Metrics) != 5 {
		t.Fatalf("metrics = %d", len(rep.Metrics))
	}
	ok, near, off := rep.Counts()
	if ok != 2 || near != 1 || off != 1 {
		t.Errorf("counts = %d/%d/%d", ok, near, off)
	}
	stuck := metricByID(rep, "stuck")
	if stuck.Name != "stuck" || stuck.Master.Verdict != VerdictOff || stuck.Band.String() != "≤ 0" {
		t.Errorf("stuck = %+v band %s", stuck, stuck.Band)
	}
	if got := metricByID(rep, "series-ms"); got.Band.String() != "< 2000 ms" || got.FormatValue(got.Master) != "268 ms" || got.Master.Verdict != VerdictOK {
		t.Errorf("series-ms band %s value %s verdict %s", got.Band, got.FormatValue(got.Master), got.Master.Verdict)
	}
	if fh := metricByID(rep, "full-holds"); fh.FormatValue(fh.Master) != "7%" || fh.Band.String() != "≤ 20%" {
		t.Errorf("full-holds %s %s", fh.FormatValue(fh.Master), fh.Band)
	}
	nb := metricByID(rep, "no-band")
	if nb.Band != nil || nb.Master.Verdict != VerdictUnmeasured || nb.FormatValue(nb.Master) != "n/a" {
		t.Errorf("no-band = %+v", nb.Master)
	}
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "1 of 5 metrics have no band in probe/new.json") {
		t.Errorf("problems = %v", rep.Problems)
	}
	if g := strings.Join(rep.Groups(), ","); g != "Headline,Slice" {
		t.Errorf("groups = %s", g)
	}

	if s := metricByID(rep, "stuck").Systems; len(s) != 1 || s[0] != "Sim" {
		t.Errorf("stuck systems = %v", s)
	}
	if s := metricByID(rep, "open-ladder").Systems; len(s) != 1 || s[0] != "AI" {
		t.Errorf("open-ladder systems = %v", s)
	}
	sim := MetricsForSystem(rep, wb, "sim", "")
	if len(sim) != 2 || sim[0].ID != "stuck" {
		t.Errorf("sim metrics = %+v (worst first)", sim)
	}
	if ai := MetricsForSystem(rep, wb, "AI", "plays"); len(ai) != 1 || ai[0].ID != "open-ladder" {
		t.Errorf("AI/plays = %+v", ai)
	}
	if none := MetricsForSystem(rep, wb, "Economy", ""); none != nil {
		t.Errorf("unknown system = %+v", none)
	}
}

func TestLoadMetricsRustManagerSnapshot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/balance/snapshot.json", `{"date":"2026-09-29","commit":"208225f","dirty":false,"n":200,"economy":true,
	  "metrics":[{"id":"full-holds","value":7,"n":200,"verdict":"Pass"},{"id":"dominant-plays","value":1,"n":0,"verdict":"Fail"},{"id":"margins","value":58.208954,"n":67,"verdict":"Near"}]}`)
	rep := LoadMetrics(root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "docs/balance/snapshot.json"}}, nil)
	if rep.Run == nil || rep.Run.Date != "2026-09-29" || rep.Run.Commit != "208225f" || rep.Run.N != 200 {
		t.Fatalf("run = %+v", rep.Run)
	}
	m := metricByID(rep, "margins")
	if m.Name != "margins" || m.Band != nil || m.Master.Verdict != VerdictNear || m.FormatValue(m.Master) != "58.21" {
		t.Errorf("margins = %+v %s", m, m.FormatValue(m.Master))
	}
	if len(rep.Problems) != 1 || !strings.HasPrefix(rep.Problems[0], "3 of 3 metrics have no band") {
		t.Errorf("problems = %v", rep.Problems)
	}
}

func TestLoadMetricsTargetsWinAndProblems(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "targets.json", `{"metrics":[
	  {"id":"full-holds","name":"Full holds (targets)","band":{"max":5,"unit":"pct"}},
	  {"id":"later","name":"Not measured yet","band":{"min":1}}]}`)
	writeFile(t, root, "run.json", runJSON)
	rep := LoadMetrics(root, WorkbenchConfig{Metrics: WorkbenchMetrics{Targets: "targets.json", ProbeResults: "run.json"}}, nil)
	fh := metricByID(rep, "full-holds")
	if fh.Name != "Full holds (targets)" || fh.Band.String() != "≤ 5%" || fh.Master.Verdict != VerdictOK {
		t.Errorf("full-holds = %+v (the file's own verdict stands)", fh)
	}
	later := metricByID(rep, "later")
	if later == nil || later.Master.Verdict != VerdictUnmeasured {
		t.Errorf("later = %+v", later)
	}

	writeFile(t, root, "bad.json", "{\n  \"metrics\": [\n    {\"id\": 1}\n  ]\n}")
	rep = LoadMetrics(root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "bad.json"}}, nil)
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "bad.json: line 3") {
		t.Errorf("bad json problems = %v", rep.Problems)
	}
	rep = LoadMetrics(root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "missing/*.json"}}, nil)
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "no file matches") {
		t.Errorf("missing problems = %v", rep.Problems)
	}
	big := strings.Repeat(" ", MaxWorkbenchFile+1)
	writeFile(t, root, "big.json", big)
	rep = LoadMetrics(root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "big.json"}}, nil)
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "limit") {
		t.Errorf("big problems = %v", rep.Problems)
	}
	outside := t.TempDir()
	writeFile(t, outside, "x.json", runJSON)
	if err := os.Symlink(filepath.Join(outside, "x.json"), filepath.Join(root, "link.json")); err != nil {
		t.Fatal(err)
	}
	rep = LoadMetrics(root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "link.json"}}, nil)
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "outside the project root") {
		t.Errorf("symlink problems = %v", rep.Problems)
	}
	if rep := LoadMetrics(root, WorkbenchConfig{}, nil); rep.Configured || len(rep.Metrics) != 0 {
		t.Errorf("unconfigured = %+v", rep)
	}
}

func TestLoadMetricsBranchValues(t *testing.T) {
	r := newGitRepo(t)
	r.write("probe/run.json", runJSON)
	r.commit("master run")
	runCommit := r.git("rev-parse", "--short", "HEAD")

	// A worktree with its own, uncommitted run: stuck improves, the ladder
	// band is changed on the branch, no-band is not measured, and a new id.
	wt := r.worktree("work/ui/wt", "main")
	r.writeIn(wt, "other.txt", "x\n")
	r.commitIn(wt, "branch work")
	r.writeIn(wt, "probe/run.json", strings.NewReplacer(
		`"value": 10}`, `"value": 2}`,
		`"band": {"min": 15, "unit": "pct"}, "value": 11, "n": 200, "verdict": "Near"`, `"band": {"min": 10, "unit": "pct"}, "value": 11, "n": 60`,
		`{"id": "no-band", "group": "Slice", "value": "n/a"}`, `{"id": "fresh", "band": {"max": 1}, "value": 3}`,
		`"commit": "abc1234"`, `"commit": "`+r.gitIn(wt, "rev-parse", "--short", "HEAD")+`"`,
	).Replace(runJSON))

	// A branch with no worktree whose committed run is behind its head.
	r.git("checkout", "-q", "-b", "work/ui/committed", "main")
	r.write("probe/run.json", strings.Replace(strings.Replace(runJSON, `"value": 7,`, `"value": 9,`, 1), `"commit": "abc1234"`, `"commit": "`+runCommit+`"`, 1))
	r.commit("committed run")
	r.write("later.txt", "l\n")
	r.commit("after the run")
	r.git("checkout", "-q", "main")

	issues := []*Issue{{Slug: "ui/wt", Status: "in progress"}, {Slug: "ui/committed", Status: "in progress"}}
	branches := OpenBranches(r.root, "main", issues)
	rep := LoadMetrics(r.root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "probe/*.json"}}, branches)

	if len(rep.BranchRuns) != 2 {
		t.Fatalf("branch runs = %+v", rep.BranchRuns)
	}
	byBranch := map[string]MetricRun{}
	for _, br := range rep.BranchRuns {
		byBranch[br.Branch] = br
	}
	if wtRun := byBranch["work/ui/wt"]; wtRun.Source != "worktree" || wtRun.Behind != 0 {
		t.Errorf("worktree run = %+v", wtRun)
	}
	if cRun := byBranch["work/ui/committed"]; cRun.Source != "commit" || cRun.Behind != 2 {
		t.Errorf("committed run = %+v (its commit is 2 behind the head)", cRun)
	}

	stuck := metricByID(rep, "stuck")
	if len(stuck.Branches) != 1 || stuck.Branches[0].Branch != "work/ui/wt" || stuck.Branches[0].Raw != "2" || stuck.Branches[0].Verdict != VerdictOff {
		t.Errorf("stuck branches = %+v", stuck.Branches)
	}
	ladder := metricByID(rep, "open-ladder")
	if len(ladder.Branches) != 1 || ladder.Branches[0].Band == nil || ladder.Branches[0].Band.String() != "≥ 10%" || ladder.Branches[0].Verdict != VerdictOK {
		t.Errorf("ladder branches = %+v (branch band, verdict against it)", ladder.Branches)
	}
	fh := metricByID(rep, "full-holds")
	if len(fh.Branches) != 1 || fh.Branches[0].Source != "commit" || fh.Branches[0].Raw != "9" || fh.Branches[0].Behind != 2 {
		t.Errorf("full-holds branches = %+v", fh.Branches)
	}
	nb := metricByID(rep, "no-band")
	if len(nb.Branches) != 0 {
		t.Errorf("no-band is unmeasured on master, so no missing marker: %+v", nb.Branches)
	}
	sm := metricByID(rep, "series-ms")
	if len(sm.Branches) != 0 {
		t.Errorf("series-ms equal on both branches: %+v", sm.Branches)
	}
	fresh := metricByID(rep, "fresh")
	if fresh == nil || !fresh.BranchOnly || fresh.Master != nil || len(fresh.Branches) != 1 || fresh.Verdict() != VerdictOff {
		t.Errorf("fresh = %+v", fresh)
	}
	ok, near, off := rep.Counts()
	if ok != 2 || near != 1 || off != 1 {
		t.Errorf("counts count master only: %d/%d/%d", ok, near, off)
	}
}

func TestLoadMetricsMissingOnBranch(t *testing.T) {
	r := newGitRepo(t)
	r.write("run.json", runJSON)
	r.commit("run")
	wt := r.worktree("work/x", "main")
	r.writeIn(wt, "run.json", `{"run":{"date":"2026-10-02"},"metrics":[{"id":"stuck","band":{"max":0,"unit":"count"},"value":10}]}`)
	r.commitIn(wt, "partial run")
	rep := LoadMetrics(r.root, WorkbenchConfig{Metrics: WorkbenchMetrics{ProbeResults: "run.json"}}, OpenBranches(r.root, "main", []*Issue{{Slug: "x", Status: "in progress"}}))
	fh := metricByID(rep, "full-holds")
	if len(fh.Branches) != 1 || !fh.Branches[0].Missing {
		t.Errorf("full-holds = %+v", fh.Branches)
	}
	if st := metricByID(rep, "stuck"); len(st.Branches) != 0 {
		t.Errorf("stuck equal on the branch = %+v", st.Branches)
	}
}

func TestBandAndValueFormatting(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		band MetricBand
		want string
	}{
		{MetricBand{Min: f(45), Max: f(55), Unit: "pct"}, "45–55%"},
		{MetricBand{Max: f(0.1), Unit: "count"}, "≤ 0.1"},
		{MetricBand{Min: f(0.5)}, "≥ 0.5"},
		{MetricBand{Max: f(90), Unit: "seconds"}, "≤ 90 s"},
		{MetricBand{Max: f(10), Unit: "points"}, "≤ +10"},
	}
	for _, c := range cases {
		if got := c.band.String(); got != c.want {
			t.Errorf("%+v = %q, want %q", c.band, got, c.want)
		}
	}
	if !(&MetricBand{Max: f(5), MaxExclusive: true}).Contains(4.9) || (&MetricBand{Max: f(5), MaxExclusive: true}).Contains(5) {
		t.Error("exclusive max")
	}
	if NormalizeVerdict("NotMeasured") != VerdictUnmeasured || NormalizeVerdict("FAIL") != VerdictOff || NormalizeVerdict("what") != "" {
		t.Error("verdict normalisation")
	}
}
