package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// healthSnap is a snapshot from "metric{k=v}" ids.
func healthSnap(at time.Time, rows map[string]float64) *health.Snapshot {
	s := &health.Snapshot{Run: health.RunInfo{Date: at.Format("2006-01-02T15:04"), Trigger: "daily"}, Families: map[string]health.FamilyResult{}}
	for id, v := range rows {
		name, rest, has := strings.Cut(id, "{")
		var labels map[string]string
		if has {
			labels = map[string]string{}
			for _, kv := range strings.Split(strings.TrimSuffix(rest, "}"), ",") {
				k, val, _ := strings.Cut(kv, "=")
				labels[k] = val
			}
		}
		s.Metrics = append(s.Metrics, health.Row{ID: health.RowID(name, labels), Metric: name, Labels: labels, Value: v})
	}
	return s
}

// healthProject is a git test project with two snapshots (8 days ago and
// now) and a review with two recommendations.
func healthProject(t *testing.T, now time.Time) (tracker.Project, string) {
	t.Helper()
	proj, root := setupTestProject(t)
	proj.WorkDir = root
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "h@t"}, {"config", "user.name", "h"}, {"config", "commit.gpgsign", "false"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	if out, err := exec.Command("sh", "-c", "cd "+root+" && git add main.go && git commit -q -m init").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
	old := healthSnap(now.AddDate(0, 0, -8), map[string]float64{
		"score.derived": 84, "score.coverage": 0.7,
		"tests.count{kind=unit,result=all}": 1000, "tests.wall_seconds{kind=unit}": 33, "tests.per_test_ms{kind=unit,stat=mean}": 33,
	})
	cur := healthSnap(now.Add(-time.Hour), map[string]float64{
		"score.derived": 86, "score.coverage": 0.7,
		"score.component{component=unit_wait}": 100, "score.component.value{component=unit_wait}": 30.3, "score.component.weight{component=unit_wait}": 20,
		"score.component{component=big_files}": 47, "score.component.value{component=big_files}": 0.182, "score.component.weight{component=big_files}": 15,
		"score.component.weight{component=flaky}": 10,
		"tests.count{kind=unit,result=all}":       1145, "tests.wall_seconds{kind=unit}": 30.3, "tests.per_test_ms{kind=unit,stat=mean}": 26.5,
		"shape.hotspot{path=static/style.css}":                          162513,
		"tests.slowest.seconds{kind=unit,test=cmd/playtest.TestUXTest}": 9,
		"lint.warnings": 2,
		"opinion.score": 78,
	})
	for _, s := range []*health.Snapshot{old, cur} {
		if _, err := health.Write(root, s, s.Time()); err != nil {
			t.Fatal(err)
		}
	}
	derived := 86.0
	op := &health.Opinion{Score: 78, Assessment: "Fast feedback; one package is the whole unit wall.", Reviewer: "agent-review", Derived: &derived,
		Recommendations: []health.Recommendation{
			{ID: "R1", Kind: "speed_up_tests", Title: "Move the Chrome tests to the e2e tier", Effort: "M", ExpectedEffect: "unit wall 30 s to 17 s",
				Evidence: []health.Evidence{{Metric: "tests.part.seconds{kind=unit,part=cmd/playtest}", Shows: "the whole wall"}}},
			{ID: "R2", Kind: "split_file", Title: "Split static/style.css", Effort: "M", ExpectedEffect: "big files down",
				Evidence: []health.Evidence{{Path: "static/style.css", Shows: "4,033 lines"}}},
		}}
	health.NewOpinion(op, nil, now.Add(-72*time.Hour), "agent-review")
	if _, err := health.WriteOpinion(root, op); err != nil {
		t.Fatal(err)
	}
	return proj, root
}

func TestBuildHealthPage(t *testing.T) {
	now := time.Now()
	proj, root := healthProject(t, now)
	p := buildHealthPage(healthIn{Proj: &proj, Prefix: "/p/test-project", Range: "30d", Snaps: health.Range(root, time.Time{}, now), Root: root, WF: proj.LoadWorkflow(), Now: now})
	if !p.Derived.Has || p.Derived.Value != "86" || p.Derived.Change != "▲ +2 in 7 days" {
		t.Errorf("derived = %+v", p.Derived)
	}
	if !p.Opinion.Has || p.Opinion.Value != "78" || p.Opinion.Change != "reviewed 3 days ago" || p.Opinion.Context != "derived was 86 then" {
		t.Errorf("opinion = %+v", p.Opinion)
	}
	if p.Coverage != "2 of 3 components, 70% of the weight" || len(p.Components) != 3 {
		t.Errorf("coverage %q, components %+v", p.Coverage, p.Components)
	}
	for _, c := range p.Components {
		if c.Name == "flaky" && (c.Has || !strings.Contains(c.Missing, "telemetry")) {
			t.Errorf("flaky has no data and must say why: %+v", c)
		}
		if c.Name == "big_files" && (c.Measured != "18.2%" || c.Sub != 47 || c.Range != "5.0% → 30.0%") {
			t.Errorf("big_files = %+v", c)
		}
	}
	if p.Review == nil || p.Review.Proposed != 2 || len(p.Review.Recs) != 2 || p.Review.Recs[0].Kind != "speed up tests" {
		t.Fatalf("review = %+v", p.Review)
	}
	unit := p.Pairs[0]
	if unit.Empty != "" || unit.Summary != "1k → 1.1k tests · 33s → 30s · per test 26.5 ms" || unit.Count.SVG == "" || unit.Runtime.SVG == "" {
		t.Errorf("unit pair = %+v", unit)
	}
	if e2e := p.Pairs[1]; !strings.Contains(e2e.Empty, "--tier slow") {
		t.Errorf("e2e without results must say how to get them: %q", e2e.Empty)
	}
	if p.Scores.Empty != "" || p.Scores.Points != 3 {
		t.Errorf("scores chart: %d points, empty %q", p.Scores.Points, p.Scores.Empty)
	}
	if len(p.Lists) != 2 || p.Lists[0].Title != "Hotspots" {
		t.Errorf("lists = %+v", p.Lists)
	}
	if p.Lint == nil || p.Lint.Err != "" || p.Lint.Command != "issue-cli --project test-project health lint" {
		t.Errorf("lint panel = %+v", p.Lint)
	}

	c := healthCard(&proj, now)
	if c == nil || c.Lint != "2 lint warnings" {
		t.Fatalf("health card lint line = %+v", c)
	}
	var md strings.Builder
	healthMarkdown(&md, c)
	if !strings.Contains(md.String(), "- Lint: 2 lint warnings · issue-cli --project test-project health lint\n") {
		t.Errorf("status.md health section:\n%s", md.String())
	}

	seven := buildHealthPage(healthIn{Proj: &proj, Prefix: "/p/x", Range: "7d", Snaps: health.Range(root, time.Time{}, now), Root: root, WF: proj.LoadWorkflow(), Now: now})
	if seven.Scores.Points != 2 || seven.RangeLabel != "7 days" {
		t.Errorf("7 days: %d points (the 8-day-old snapshot is out), label %q", seven.Scores.Points, seven.RangeLabel)
	}
}

func TestHealthPageAndActions(t *testing.T) {
	now := time.Now()
	proj, root := healthProject(t, now)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	get := func(path string) string {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d\n%s", path, resp.StatusCode, body)
		}
		return string(body)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(path string, form url.Values) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST %s: %d", path, resp.StatusCode)
		}
		return resp.Header.Get("Location")
	}

	page := get("/p/test-project/health")
	for _, want := range []string{`data-testid="health.page"`, `data-testid="health.score_gauge"`, `data-testid="health.component.big_files"`,
		`data-testid="health.file_recommendation"`, `data-testid="health.chart.unit_tests"`, `data-testid="nav.tab.health"`, "Move the Chrome tests to the e2e tier"} {
		if !strings.Contains(page, want) {
			t.Errorf("health page lacks %s", want)
		}
	}

	loc := post("/p/test-project/health/recommendation/R1/file", nil)
	if !strings.Contains(loc, "filed=move-the-chrome-tests-to-the-e2e-tier") {
		t.Fatalf("file redirect = %s", loc)
	}
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	found := false
	for _, is := range issues {
		if is.Slug == "move-the-chrome-tests-to-the-e2e-tier" {
			found = true
			body, _ := os.ReadFile(is.FilePath)
			if !strings.Contains(string(body), "viewer") || !strings.Contains(string(body), "File as issue (Health view)") {
				t.Errorf("the click must be recorded as the decision:\n%s", body)
			}
		}
	}
	if !found {
		t.Fatal("no issue filed")
	}
	if loc := post("/p/test-project/health/recommendation/R2/dismiss", url.Values{"reason": {"later"}}); !strings.Contains(loc, "dismissed=R2") {
		t.Fatalf("dismiss redirect = %s", loc)
	}
	if loc := post("/p/test-project/health/recommendation/R9/file", nil); !strings.Contains(loc, "error=") {
		t.Fatalf("unknown id must come back as an error: %s", loc)
	}
	page = get("/p/test-project/health?filed=x")
	if !strings.Contains(page, `data-testid="health.flash"`) || strings.Contains(page, `data-testid="health.file_recommendation"`) {
		t.Error("after filing and dismissing, nothing is proposed and the flash shows")
	}

	var spec agentrun.Spec
	orig := startReview
	startReview = func(_ *tracker.Project, s agentrun.Spec) agentrun.Response {
		spec = s
		return agentrun.Response{Status: "started", Session: s.Session}
	}
	t.Cleanup(func() { startReview = orig })
	if loc := post("/p/test-project/health/review", nil); !strings.Contains(loc, "review=review-test-project") {
		t.Fatalf("review redirect = %s", loc)
	}
	if spec.Session != "review-test-project" || !spec.Headless || !strings.Contains(spec.Prompt, filepath.Join(root, ".health", "reviews")) {
		t.Errorf("review spec = %+v", spec)
	}
}

func TestStatusHealthCard(t *testing.T) {
	now := time.Now()
	proj, _ := healthProject(t, now)
	c := healthCard(&proj, now)
	if c == nil || c.Derived != "86" || c.DerivedLine != "▲ +2 in 7 days" || c.Opinion != "78" || c.OpinionLine != "reviewed 3 days ago" || c.Proposed != 2 {
		t.Fatalf("card = %+v", c)
	}
	if c.Weakest != "big files 47 (18.2% source lines in big files)" {
		t.Errorf("weakest = %q", c.Weakest)
	}
	var b strings.Builder
	healthMarkdown(&b, c)
	for _, want := range []string{"## Health", "- Derived 86 (▲ +2 in 7 days) · weakest: big files 47", "- Opinion 78 (reviewed 3 days ago)", "2 recommendation(s) wait"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("status.md lacks %q:\n%s", want, b.String())
		}
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/p/test-project/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{`data-testid="status.health"`, `href="/p/test-project/health"`, "2 recommendations wait for you"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("status page lacks %s", want)
		}
	}

	empty, _ := setupTestProject(t)
	if healthCard(&empty, now) != nil {
		t.Error("no snapshot: no card")
	}
}

func TestLintPanelListsWarningsAndCaps(t *testing.T) {
	dir := t.TempDir()
	issues := filepath.Join(dir, "issues")
	os.MkdirAll(issues, 0755)
	for i := 0; i < lintPanelCap+5; i++ {
		os.WriteFile(filepath.Join(issues, fmt.Sprintf("i%d.md", i)), []byte(fmt.Sprintf("---\ntitle: \"Issue %d\"\nstatus: idea\n---\n", i)), 0644)
	}
	wf := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wf, []byte("template: custom\nstatuses:\n  - name: idea\n    description: raw\ntransitions: []\n"), 0644)
	p := lintPanel(&tracker.Project{Slug: "lp", IssueDir: issues, WorkflowFile: wf}, time.Now())
	if p.Err != "" || p.Warnings != lintPanelCap+5 || len(p.Findings) != lintPanelCap || p.More != 5 || p.Findings[0].Check != "undated" {
		t.Errorf("panel = warnings %d, listed %d, more %d, err %q", p.Warnings, len(p.Findings), p.More, p.Err)
	}
}

// The newest snapshot's family warnings (a glob that matched no file) show on
// the Health page under Derived health.
func TestHealthPageShowsFamilyWarnings(t *testing.T) {
	now := time.Now()
	proj, root := healthProject(t, now)
	s := healthSnap(now, map[string]float64{"score.derived": 86})
	s.Families["tests"] = health.FamilyResult{Warnings: []string{"health.e2e_sessions (default playtest/sessions/*.yaml) matched no file, so no e2e session rows"}}
	if _, err := health.Write(root, s, s.Time()); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	_, body := getBody(t, ts.URL+"/p/test-project/health")
	for _, want := range []string{`data-testid="health.warnings"`, `<span class="mono">tests</span>`, "health.e2e_sessions (default playtest/sessions/*.yaml) matched no file"} {
		if !strings.Contains(body, want) {
			t.Errorf("health page missing %q", want)
		}
	}
}
