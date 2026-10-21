package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const metricsTestWorkflow = `statuses:
  - name: idea
  - name: in progress
  - name: done
workbench:
  metrics:
    probe_results: probe/*.json
  tuning:
    - build/knobs.json
  systems:
    Sim:
      metrics: [stuck, full-holds]
      knobs: [Prep*]
`

const metricsTestRun = `{"run": {"date": "2026-10-01", "commit": "%COMMIT%", "n": 200, "command": "make balance"},
 "metrics": [
  {"id": "full-holds", "name": "Full holds", "group": "Headline", "band": {"max": 20, "unit": "pct"}, "value": 7, "n": 200, "verdict": "pass"},
  {"id": "stuck", "name": "Stuck fighters", "group": "Headline", "band": {"max": 0, "unit": "count"}, "value": 10},
  {"id": "margins", "group": "Slice", "value": 58}
 ]}`

// metricsRepo is a git project with metric and knob files, an issue on an
// open worktree branch, and a second branch both change Tune.go on.
type metricsRepo struct {
	t    *testing.T
	root string
	proj tracker.Project
}

func (r *metricsRepo) git(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *metricsRepo) write(dir, rel, text string) {
	r.t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func setupMetricsRepo(t *testing.T, workflow string) *metricsRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(k, v)
	}
	r := &metricsRepo{t: t, root: t.TempDir()}
	r.git(r.root, "init", "-q", "-b", "main")
	r.write(r.root, "workflow.yaml", workflow)
	r.write(r.root, "Sim/Tune.go", "package sim\n\nconst PrepTime = 30 // stage 1 set-up\n")
	r.write(r.root, "issues/Sim/alpha.md", "---\ntitle: \"alpha\"\nstatus: \"in progress\"\nsystem: \"Sim\"\ndepends_on: \"sim/beta\"\n---\n\nBuilt on sim/beta#D-1.\n")
	r.write(r.root, "issues/Sim/beta.md", "---\ntitle: \"beta\"\nstatus: \"in progress\"\nsystem: \"Sim\"\n---\n\n## Decisions\n- **D-1** · 2026-10-01 · lead chat: Keep it small\n")
	r.git(r.root, "add", "-A")
	r.git(r.root, "commit", "-q", "-m", "init")
	r.write(r.root, "Sim/Tune.go", "package sim\n\nconst PrepTime = 20 // stage 1 set-up\n")
	r.git(r.root, "commit", "-q", "-am", "Play calls balance pass 2")
	head := r.git(r.root, "rev-parse", "--short", "HEAD")
	r.write(r.root, "probe/run.json", strings.ReplaceAll(metricsTestRun, "%COMMIT%", head))
	r.write(r.root, "build/knobs.json", `{"knobs":[{"name":"PrepTime","value":"20","comment":"stage 1 set-up","file":"Sim/Tune.go","line":3}]}`)

	for _, b := range []string{"sim/alpha", "sim/beta"} {
		wt := filepath.Join(r.root, ".worktrees", strings.ReplaceAll(b, "/", "-"))
		r.git(r.root, "worktree", "add", "-q", "-b", "work/"+b, wt, "main")
		r.write(wt, "Sim/Tune.go", "package sim\n\nconst PrepTime = 25 // stage 1 set-up\n")
		r.git(wt, "commit", "-q", "-am", "tune "+b)
	}
	// alpha's worktree has its own run: stuck improves.
	alphaWT := filepath.Join(r.root, ".worktrees", "sim-alpha")
	r.write(alphaWT, "probe/run.json", strings.Replace(strings.ReplaceAll(metricsTestRun, "%COMMIT%", r.git(alphaWT, "rev-parse", "--short", "HEAD")), `"value": 10}`, `"value": 3}`, 1))

	r.proj = tracker.Project{Name: "Metrics Project", Slug: "mp", IssueDir: filepath.Join(r.root, "issues"), WorkflowFile: filepath.Join(r.root, "workflow.yaml"), WorkDir: r.root}
	return r
}

func getMetricsBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func assertHas(t *testing.T, page string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(page, w) {
			t.Errorf("page is missing %q", w)
		}
	}
}

func TestMetricsView_GuardMetricsTab(t *testing.T) {
	r := setupMetricsRepo(t, metricsTestWorkflow)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{r.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/mp/metrics")
	assertHas(t, page,
		`data-testid="nav.tab.metrics" aria-current="page">Metrics <span class="nav-note alert">1 off</span>`,
		`data-testid="metrics.tab.metrics" aria-current="page">Guard metrics <span>3</span>`,
		`<span class="wb-stat-num tone-green">1</span><span class="wb-stat-label">OK</span>`,
		`<span class="wb-stat-num tone-red">1</span><span class="wb-stat-label">Off</span>`,
		`data-testid="metrics.row.stuck"`,
		`<td class="mono nowrap">≤ 0</td>`,
		`<span class="chip chip-red verdict">✗ off</span>`,
		`<span class="mono tone-off">3 ✗</span> <span class="mono muted">· sim/alpha</span>`,
		"1 of 3 metrics have no band in probe/run.json",
		`data-testid="metrics.run.master"`,
		"re-run: make balance",
		`<span class="sidebar-panel-key">metrics.probe_results</span><span class="mono">probe/*.json</span>`,
		`<option value="Sim">Sim</option>`,
	)
	if strings.Contains(page, "since this run") {
		t.Error("the run is at HEAD: no commits-since chip")
	}

	off := pageGet(t, ts.URL+"/p/mp/metrics?only=off")
	if strings.Contains(off, `data-testid="metrics.row.full-holds"`) || !strings.Contains(off, `data-testid="metrics.row.stuck"`) {
		t.Error("near-or-off filter")
	}
	sys := pageGet(t, ts.URL+"/p/mp/metrics?system=Sim")
	if strings.Contains(sys, `data-testid="metrics.row.margins"`) {
		t.Error("system filter keeps unlisted metrics")
	}
	q := pageGet(t, ts.URL+"/p/mp/metrics?q=marg")
	if !strings.Contains(q, `data-testid="metrics.row.margins"`) || strings.Contains(q, `data-testid="metrics.row.stuck"`) {
		t.Error("search filter")
	}

	// The tuning file moves past the run: the chip counts the commit.
	r.write(r.root, "Sim/Tune.go", "package sim\n\nconst PrepTime = 15 // stage 1 set-up\n")
	r.git(r.root, "commit", "-q", "-am", "retune")
	assertHas(t, pageGet(t, ts.URL+"/p/mp/metrics"), "1 commit to Sim/ since this run")
}

func TestMetricsView_KnobsAndBranches(t *testing.T) {
	r := setupMetricsRepo(t, metricsTestWorkflow)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{r.proj})
	defer ts.Close()

	knobs := pageGet(t, ts.URL+"/p/mp/metrics?tab=knobs")
	assertHas(t, knobs,
		`data-testid="knobs.row.preptime"`,
		`<span class="mono tone-amber knob-change">30 → 20</span> <span>· Play calls balance pass 2</span>`,
		"also edited on 2 branches",
		`<span class="mono tone-blue knob-change">20 → 25</span>`,
		`data-testid="nav.panel.row.sim/tune_go"`,
	)

	branches := pageGet(t, ts.URL+"/p/mp/metrics?tab=branches")
	assertHas(t, branches,
		`data-testid="branches.row.work/sim/alpha"`,
		`<a href="/p/mp/issue/sim/alpha">alpha</a>`,
		`<span class="label">depends on</span> <a href="/p/mp/issue/sim/beta" class="mono">sim/beta</a>`,
		`<span class="label">decided by</span>`,
		`3 or more branches`,
		"No file is changed on three or more open branches.",
	)
	// Two branches: shown under the 2-branch chip, not as hot.
	if strings.Contains(branches, `<span class="chip chip-amber">hot · `) {
		t.Error("a file on two branches is not hot")
	}
	two := pageGet(t, ts.URL+"/p/mp/metrics?tab=branches&hot=2")
	assertHas(t, two, `data-testid="branches.hot.sim/tune_go"`, `<span class="chip chip-blue">2</span>`)
}

func TestMetricsJSON(t *testing.T) {
	r := setupMetricsRepo(t, metricsTestWorkflow)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{r.proj})
	defer ts.Close()

	_, body := getMetricsBody(t, ts.URL+"/p/mp/metrics.json")
	var data struct {
		Tab        string                `json:"tab"`
		Off        int                   `json:"off"`
		Open       int                   `json:"open_branches"`
		Metrics    tracker.MetricsReport `json:"metrics"`
		Knobs      []tracker.Knob        `json:"knobs"`
		HasMetrics bool                  `json:"has_metrics"`
		Sig        string                `json:"sig"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if data.Tab != "metrics" || data.Off != 1 || data.Open != 2 || len(data.Metrics.Metrics) != 3 || len(data.Knobs) != 1 || data.Sig == "" {
		t.Errorf("json = %+v", data)
	}
	_, sigOnly := getMetricsBody(t, ts.URL+"/p/mp/metrics.json?sig=1")
	if !strings.Contains(sigOnly, `"sig":"`+data.Sig+`"`) || strings.Contains(sigOnly, "metrics") {
		t.Errorf("sig = %s", sigOnly)
	}
	// A new run changes the signature.
	r.write(r.root, "probe/run.json", strings.ReplaceAll(metricsTestRun, "%COMMIT%", "abc")+"\n")
	_, again := getMetricsBody(t, ts.URL+"/p/mp/metrics.json?sig=1")
	if again == sigOnly {
		t.Error("sig did not change after a new run")
	}
}

func TestMetricsView_GitOnlyAndNoGit(t *testing.T) {
	r := setupMetricsRepo(t, "statuses:\n  - name: idea\n  - name: done\n")
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{r.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/mp/metrics")
	assertHas(t, page,
		`data-testid="nav.tab.metrics" aria-current="page">Branches`,
		`<h1 class="page-title">Branches</h1>`,
		`data-testid="metrics.tab.branches" aria-current="page"`,
	)
	if strings.Contains(page, `data-testid="metrics.tab.metrics"`) || strings.Contains(page, `data-testid="metrics.tab.knobs"`) {
		t.Error("metrics/knobs tabs without workbench config")
	}

	// Not a git checkout and no workbench block: no entry at all.
	plain := t.TempDir()
	r.write(plain, "issues/a.md", "---\ntitle: \"a\"\nstatus: \"idea\"\n---\n\nbody\n")
	r.write(plain, "workflow.yaml", "statuses:\n  - name: idea\n")
	proj := tracker.Project{Name: "Plain", Slug: "plain", IssueDir: filepath.Join(plain, "issues"), WorkflowFile: filepath.Join(plain, "workflow.yaml"), WorkDir: plain}
	ts2 := newTestServer(t, []tracker.Project{proj})
	defer ts2.Close()
	if list := pageGet(t, ts2.URL+"/p/plain/"); strings.Contains(list, `data-testid="nav.tab.metrics"`) {
		t.Error("nav lists Metrics for a project with neither git nor workbench")
	}

	// Metrics configured but no git: values render, git problem shown.
	r.write(plain, "workflow.yaml", metricsTestWorkflow)
	r.write(plain, "probe/run.json", strings.ReplaceAll(metricsTestRun, "%COMMIT%", ""))
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
	nogit := pageGet(t, ts2.URL+"/p/plain/metrics")
	assertHas(t, nogit, `data-testid="metrics.row.stuck"`, "git not available for this project")
}

func TestDetailPage_DependencyFields(t *testing.T) {
	r := setupMetricsRepo(t, metricsTestWorkflow)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{r.proj})
	defer ts.Close()

	alpha := pageGet(t, ts.URL+"/p/mp/issue/sim/alpha")
	assertHas(t, alpha,
		`data-testid="detail.field.depends_on"`,
		`<a href="/p/mp/issue/sim/beta" class="mono dep-target">sim/beta</a> <span class="chip chip-blue">in progress</span>`,
		`data-testid="detail.field.decided_by"`,
		`sim/beta#D-1</a> <span class="chip chip-green">active</span>`,
		`data-testid="detail.field.shared_files"`,
		"1 hot file with 1 other branch",
	)
	if strings.Contains(alpha, "<h3>Depends On</h3>") {
		t.Error("depends_on also listed as a custom field")
	}
	beta := pageGet(t, ts.URL+"/p/mp/issue/sim/beta")
	assertHas(t, beta, `data-testid="detail.field.depended_on_by"`, `sim/alpha</a>`)
	if strings.Contains(beta, `data-testid="detail.field.depends_on"`) {
		t.Error("beta has no depends_on row")
	}
}

func TestSystemMetricsPanels(t *testing.T) {
	r := setupMetricsRepo(t, metricsTestWorkflow)
	withMockTmuxSessions(t, nil)
	srv, err := NewServer([]tracker.Project{r.proj})
	if err != nil {
		t.Fatal(err)
	}
	issues, _ := tracker.LoadIssues(r.proj.IssueDir)
	mp, kp := systemMetricsPanels(&r.proj, issues, "/p/mp", "Sim", "")
	if mp == nil || kp == nil || mp.Total != 2 || mp.Off != 1 || mp.Rows[0].ID != "stuck" || kp.Total != 1 {
		t.Fatalf("panels = %+v %+v", mp, kp)
	}
	var buf bytes.Buffer
	if err := srv.tmpl.ExecuteTemplate(&buf, "system-guard-metrics", mp); err != nil {
		t.Fatal(err)
	}
	if err := srv.tmpl.ExecuteTemplate(&buf, "system-knobs", kp); err != nil {
		t.Fatal(err)
	}
	assertHas(t, buf.String(),
		`data-testid="panel.guard_metrics.row.stuck"`,
		"2 of 2 Sim metrics · 1 off",
		`href="/p/mp/metrics?system=Sim&amp;tab=metrics"`,
		`data-testid="panel.knobs.row.preptime"`,
		"also edited on 2 branches",
	)
	if mp, kp := systemMetricsPanels(&r.proj, issues, "/p/mp", "Economy", ""); mp != nil || kp != nil {
		t.Errorf("unlisted system = %+v %+v", mp, kp)
	}
}

func TestBalanceGroupShowsGuardMetric(t *testing.T) {
	proj := setupBalanceProject(t)
	root := filepath.Dir(proj.IssueDir)
	wf, _ := os.ReadFile(proj.WorkflowFile)
	wf = append(wf, []byte("  metrics:\n    probe_results: run.json\n")...)
	if err := os.WriteFile(proj.WorkflowFile, wf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run.json"), []byte(strings.ReplaceAll(metricsTestRun, "%COMMIT%", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	page := pageGet(t, ts.URL+"/p/wb/balance")
	assertHas(t, page, `data-testid="balance.group.full-holds.guard"`, `<span class="mono">≤ 20%</span> · master <span class="mono">7%</span>`)
	if strings.Contains(page, `data-testid="balance.group.dominant-plays.guard"`) {
		t.Error("a metric the files don't list has no guard line")
	}
}
