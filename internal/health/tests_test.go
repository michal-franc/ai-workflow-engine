package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/testresults"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// writeRun stores a CI run record with results, as ci.Execute would.
func writeRun(t *testing.T, root, sha string, finished time.Time, kind string, sum testresults.Summary, tests []testresults.Test) {
	t.Helper()
	if tests != nil {
		list := filepath.Join(".ci", "results", sha, kind+".tests.json")
		writeJSON(t, filepath.Join(root, list), tests)
		sum.List = list
	}
	run := ci.Run{Sha: sha, Branch: "main", Tier: "default", Started: finished.Add(-time.Minute), Finished: finished, State: ci.Green,
		Results: map[string]testresults.Summary{kind: sum}}
	writeJSON(t, filepath.Join(ci.RunsDir(root), sha+".json"), run)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTestsFamily(t *testing.T) {
	now := time.Now()
	root, proj := healthRepo(t, now)
	writeT(t, root, "playtest/sessions/board.yaml", "session: board\nissue: ui/board\nsteps:\n  - id: TP1\n  - id: TP2\n")
	writeT(t, root, "playtest/sessions/notes.yaml", "# no steps: not a session\nfoo: 1\n")
	commitAt(t, root, now.AddDate(0, 0, -1), "sessions")
	// issues: UI has a session, Core has none; an old done issue is out of the window.
	writeT(t, root, "issues/UI/board.md", "---\ntitle: \"Board\"\nstatus: \"done\"\nsystem: \"UI\"\n---\n")
	writeT(t, root, "issues/Core/engine.md", "---\ntitle: \"Engine\"\nstatus: \"done\"\nsystem: \"Core\"\n---\n")
	writeT(t, root, "issues/Core/old.md", "---\ntitle: \"Old\"\nstatus: \"done\"\nsystem: \"Core\"\n---\n")
	for name, days := range map[string]int{"UI/board": 3, "Core/engine": 10, "Core/old": 200} {
		st := tracker.StatsStore{Transitions: []tracker.TransitionStat{{From: "shipping", To: "done", TS: now.AddDate(0, 0, -days)}}}
		if err := tracker.SaveStats(filepath.Join(root, "issues", name+".md"), st); err != nil {
			t.Fatal(err)
		}
	}
	writeRun(t, root, "aaa", now.Add(-48*time.Hour), "unit", testresults.Summary{Tests: 3, Passed: 2, Failed: 1, WallS: 30, SumS: 40, MeanMS: 13333, P50MS: 10, P90MS: 39000,
		Parts: []testresults.Part{{Name: "cmd/playtest", Seconds: 30}}},
		[]testresults.Test{{Name: "TestA", Part: "cmd/playtest", Area: "cmd", Seconds: 39, Outcome: "pass"}, {Name: "TestB", Part: ".", Area: ".", Seconds: 0.5, Outcome: "pass"}, {Name: "TestC", Part: ".", Area: ".", Seconds: 0.5, Outcome: "fail"}})
	writeRun(t, root, "bbb", now.Add(time.Hour), "unit", testresults.Summary{Tests: 99}, nil) // after Now: ignored

	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := take(context.Background(), env, TriggerManual, []registered{{"tests", testsFamily}})
	if err != nil {
		t.Fatal(err)
	}
	if e := snap.Families["tests"].Error; e != "" {
		t.Fatal(e)
	}
	want := map[string]float64{
		"tests.count{kind=unit,result=all}":                        3,
		"tests.count{kind=unit,result=fail}":                       1,
		"tests.wall_seconds{kind=unit}":                            30,
		"tests.per_test_ms{kind=unit,stat=mean}":                   13333,
		"tests.part.seconds{kind=unit,part=cmd/playtest}":          30,
		"tests.slow{kind=unit}":                                    1,
		"tests.count_by_area{area=.,kind=unit}":                    2,
		"tests.slowest.seconds{kind=unit,test=cmd/playtest.TestA}": 39,
		"tests.run_age_days{kind=unit}":                            2,
		"tests.e2e.sessions{system=all}":                           1,
		"tests.e2e.steps{system=all}":                              2,
		"tests.e2e.sessions{system=UI}":                            1,
		"tests.e2e.coverage":                                       0.5,
		"tests.e2e.uncovered{system=Core}":                         1,
	}
	for id, v := range want {
		if got, ok := value(snap, id); !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", id, got, ok, v)
		}
	}
	if _, ok := value(snap, "tests.count{kind=e2e,result=all}"); ok {
		t.Error("no e2e results were recorded, so there must be no e2e count row (not a zero)")
	}
}

func TestTestsFamilyWithoutResultsWritesNothing(t *testing.T) {
	now := time.Now()
	root, proj := healthRepo(t, now)
	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	m, err := testsFamily(context.Background(), env)
	if err != nil || len(m) != 0 {
		t.Errorf("rows = %v, err = %v", m, err)
	}
}

// A glob that matches no file is a warning on the family, not a silent zero:
// the default e2e_sessions in a repo with no playtest sessions, and
// test_globs set to a pattern no file has.
func TestFamiliesWarnWhenAGlobMatchesNothing(t *testing.T) {
	now := time.Now()
	root, proj := healthRepo(t, now)
	fams := []registered{{"codebase", codebaseFamily}, {"tests", testsFamily}}
	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := take(context.Background(), env, TriggerManual, fams)
	if err != nil {
		t.Fatal(err)
	}
	if w := strings.Join(snap.Families["tests"].Warnings, "\n"); !strings.Contains(w, "health.e2e_sessions") || !strings.Contains(w, "playtest/sessions/*.yaml") {
		t.Errorf("tests warnings = %q, want the default e2e_sessions glob named", w)
	}
	if w := snap.Families["codebase"].Warnings; len(w) != 0 {
		t.Errorf("codebase warnings = %q: cmd/app/app_test.go matches the default test globs", w)
	}

	writeT(t, root, "workflow.yaml", "statuses:\n  - name: idea\ntransitions: []\nhealth:\n  test_globs: [\"spec/**/*_spec.rb\"]\n  e2e_sessions: [\"e2e/*.yaml\"]\n")
	writeT(t, root, "e2e/board.yaml", "session: board\nissue: ui/board\nsteps:\n  - id: TP1\n")
	commitAt(t, root, now.Add(-time.Minute), "e2e")
	if env, err = NewEnv(proj, root, now); err != nil {
		t.Fatal(err)
	}
	snap, _, err = take(context.Background(), env, TriggerManual, fams)
	if err != nil {
		t.Fatal(err)
	}
	if w := strings.Join(snap.Families["codebase"].Warnings, "\n"); !strings.Contains(w, "health.test_globs") || !strings.Contains(w, "spec/**/*_spec.rb") {
		t.Errorf("codebase warnings = %q, want the configured test_globs named", w)
	}
	if w := snap.Families["tests"].Warnings; len(w) != 0 {
		t.Errorf("tests warnings = %q: e2e/board.yaml matches", w)
	}
}
