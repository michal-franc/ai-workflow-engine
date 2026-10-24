package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func words(n int) string { return strings.TrimSpace(strings.Repeat("word ", n)) }

func validOpinion() *Opinion {
	return &Opinion{Score: 70, Assessment: words(80), Recommendations: []Recommendation{
		{ID: "R1", Kind: "add_e2e", Title: "Add a session for MechLab", Effort: "M", ExpectedEffect: "e2e_coverage up",
			Evidence: []Evidence{{Metric: "tests.e2e.uncovered{system=MechLab}", Shows: "17 done, no session"}}},
		{ID: "R2", Kind: "split_file", Title: "Split main.go", Effort: "L", ExpectedEffect: "big_lines.share down",
			Evidence: []Evidence{{Path: "main.go", Lines: "10-40", Shows: "setup"}}},
	}}
}

func TestOpinionValidate(t *testing.T) {
	snap := snapOf(map[string]float64{"tests.e2e.uncovered{system=MechLab}": 17})
	exists := func(p string) bool { return p == "main.go" }
	if p := validOpinion().Validate(snap, exists); len(p) != 0 {
		t.Fatalf("a valid review was refused: %v", p)
	}
	bad := validOpinion()
	bad.Score = -1
	bad.Recommendations[0].Evidence[0].Metric = "tests.nope"
	bad.Recommendations[1].Evidence[0].Path = "gone.go"
	bad.Recommendations[1].Evidence[0].Lines = "x-y"
	bad.Recommendations[1].Title = "add a session for mechlab"
	bad.Recommendations[1].State = StateFiled
	got := strings.Join(bad.Validate(snap, exists), "\n")
	for _, want := range []string{"score: -1", `metric "tests.nope" is not in the snapshot`, `path "gone.go" does not exist`, `lines "x-y"`, "repeats another recommendation", "state: leave it out"} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
}

// opinionRepo is a project with one recorded opinion, its issues dir with a
// MechLab system folder.
func opinionRepo(t *testing.T, now time.Time) (string, *tracker.Project, string) {
	t.Helper()
	root, proj := healthRepo(t, now)
	if err := os.MkdirAll(filepath.Join(proj.IssueDir, "MechLab"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := validOpinion()
	NewOpinion(o, snapOf(map[string]float64{"score.derived": 84}), now.Add(-72*time.Hour), "agent-review")
	path, err := WriteOpinion(root, o)
	if err != nil {
		t.Fatal(err)
	}
	return root, proj, path
}

func TestOpinionFamilyAndStore(t *testing.T) {
	now := time.Now()
	root, proj, _ := opinionRepo(t, now)
	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := take(context.Background(), env, TriggerManual, []registered{{"opinion", opinionFamily}})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]float64{
		"opinion.score":    70,
		"opinion.age_days": 3,
		"opinion.recommendations{kind=add_e2e,state=proposed}": 1,
	} {
		if got, ok := value(snap, id); !ok || got != want {
			t.Errorf("%s = %v (present %v), want %v", id, got, ok, want)
		}
	}
	env.Backfill = true
	if _, err := opinionFamily(context.Background(), env); !errors.Is(err, ErrNoPast) {
		t.Errorf("backfill: err = %v, want ErrNoPast", err)
	}
	if o, _ := LatestOpinion(root, now.Add(-96*time.Hour)); o != nil {
		t.Error("an opinion newer than t must not count")
	}
}

func TestFileAndDismissNeedTheOwner(t *testing.T) {
	now := time.Now()
	root, proj, path := opinionRepo(t, now)
	wf := proj.LoadWorkflow()
	if _, err := FileRecommendation(proj, wf, root, "R1", Decision{How: "lead chat"}, now); !errors.Is(err, ErrNeedsOwner) {
		t.Fatalf("filing without the owner's words: err = %v", err)
	}
	if _, err := DismissRecommendation(root, "R2", "", Decision{}, now); !errors.Is(err, ErrNeedsOwner) {
		t.Fatalf("dismissing without the owner's words: err = %v", err)
	}
	slug, err := FileRecommendation(proj, wf, root, "r1", Decision{How: "viewer", Quote: "File as issue (Health view)"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "mechlab/add-a-session-for-mechlab" {
		t.Errorf("slug = %q: the system comes from the evidence's system label", slug)
	}
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	var filed *tracker.Issue
	for _, is := range issues {
		if is.Slug == slug {
			filed = is
		}
	}
	if filed == nil || filed.Status != "idea" || filed.System != "MechLab" {
		t.Fatalf("filed issue = %+v", filed)
	}
	body, _ := os.ReadFile(filed.FilePath)
	for _, want := range []string{"tests.e2e.uncovered{system=MechLab}", "17 done, no session", "File as issue (Health view)", "R1"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("issue body lacks %q:\n%s", want, body)
		}
	}
	if _, err := FileRecommendation(proj, wf, root, "R1", Decision{Quote: "again"}, now); err == nil || !strings.Contains(err.Error(), "already filed") {
		t.Errorf("filing twice: err = %v", err)
	}
	if _, err := DismissRecommendation(root, "R2", "not now", Decision{Quote: "not now"}, now); err != nil {
		t.Fatal(err)
	}
	o, _ := ReadOpinion(path)
	if o.Recommendations[0].State != StateFiled || o.Recommendations[0].Issue != slug || o.Recommendations[1].State != StateDismissed || o.Proposed() != 0 {
		t.Errorf("states after filing and dismissing = %+v", o.Recommendations)
	}
	if _, err := FileRecommendation(proj, wf, root, "R2", Decision{Quote: "x"}, now); err == nil || !strings.Contains(err.Error(), "dismissed") {
		t.Errorf("filing a dismissed one: err = %v", err)
	}
	if _, err := FileRecommendation(proj, wf, root, "R9", Decision{Quote: "x"}, now); err == nil || !strings.Contains(err.Error(), "R1, R2") {
		t.Errorf("unknown id: err = %v", err)
	}
}

func TestPrepareReviewWritesTheEvidencePack(t *testing.T) {
	now := time.Now()
	root, proj, _ := opinionRepo(t, now)
	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	snap := snapOf(map[string]float64{
		"score.derived": 81, "score.coverage": 0.7, "score.component{component=unit_wait}": 100,
		"score.component.weight{component=unit_wait}": 20, "score.component.value{component=unit_wait}": 30,
		"score.component.weight{component=flaky}": 10,
		"tests.count{kind=unit,result=all}":       1145, "tests.wall_seconds{kind=unit}": 30,
		"tests.part.seconds{kind=unit,part=cmd/playtest}": 30, "tests.slowest.seconds{kind=unit,test=cmd/playtest.TestA}": 9,
		"shape.hotspot{path=static/style.css}": 160000, "codebase.lines{lang=go}": 50000,
	})
	snap.Run.Date = "2026-10-23T18:00"
	retros := filepath.Join(root, "retros")
	writeT(t, root, "retros/2026-10-22-a.md", "---\ntitle: x\n---\nThe ux tests are slow and flaky today.\nUnrelated line.\n")
	rev, err := PrepareReview(ReviewInput{Env: env, Snap: snap, RetrosDir: retros})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"health review record", "(81)", rev.Pack, "(go)"} {
		if !strings.Contains(rev.Brief, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	pack, err := os.ReadFile(rev.Pack)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"`score.derived` = 81", "| flaky | no data:", "`tests.part.seconds{kind=unit,part=cmd/playtest}` 30.0 s",
		"`tests.slowest.seconds{kind=unit,test=cmd/playtest.TestA}`", "`shape.hotspot{path=static/style.css}`",
		"The ux tests are slow and flaky today.", "R1 [add_e2e, proposed] Add a session for MechLab", "No results from local CI for e2e tests.",
	} {
		if !strings.Contains(string(pack), want) {
			t.Errorf("evidence pack lacks %q:\n%s", want, pack)
		}
	}
	if strings.Contains(string(pack), "Unrelated line") {
		t.Error("retro lines that don't mention tests, speed or size are left out")
	}
}
