package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestExecuteParsesDeclaredResults(t *testing.T) {
	root := newRepo(t)
	write := `printf '%s\n' '{"Time":"2026-10-23T10:00:00Z","Action":"pass","Package":"m","Test":"TestA","Elapsed":0.5}' '{"Time":"2026-10-23T10:00:02Z","Action":"pass","Package":"m","Elapsed":2}' > "$CI_RESULTS_DIR/unit.json"`
	unit := tracker.CICommand{Cmd: write, Results: []tracker.CIResult{{Kind: "unit", Format: "go-test-json", File: "unit.json"}}}
	missing := tracker.CICommand{Cmd: "true", Results: []tracker.CIResult{{Kind: "e2e", Format: "playtest", File: "e2e.json"}}}
	var out strings.Builder
	runs, _, err := Execute(Options{Root: root, Config: cfg(unit, missing), Out: &out})
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
	r := runs[0]
	if r.State != Green {
		t.Fatalf("a results file must never change the verdict: %+v", r)
	}
	u := r.Results["unit"]
	if u.Tests != 1 || u.Passed != 1 || u.WallS != 2 || u.Error != "" || u.List == "" {
		t.Fatalf("unit summary = %+v", u)
	}
	if e := r.Results["e2e"]; !strings.Contains(e.Error, "CI_RESULTS_DIR/e2e.json") {
		t.Errorf("missing file should say what to write: %+v", e)
	}
	if !strings.Contains(out.String(), "results: unit 1 tests") {
		t.Errorf("log lacks the results line: %q", out.String())
	}
	if tests := ReadTests(root, u); len(tests) != 1 || tests[0].Name != "TestA" {
		t.Errorf("per-test list = %+v", tests)
	}
	got, sum, ok := LatestResults(root, "unit", time.Time{})
	if !ok || got.Sha != r.Sha || sum.Tests != 1 {
		t.Errorf("LatestResults = %v %+v %v", got, sum, ok)
	}
	if _, _, ok := LatestResults(root, "e2e", time.Time{}); ok {
		t.Error("a summary with an error must not count as the latest results")
	}
	if _, _, ok := LatestResults(root, "unit", r.Started.Add(-time.Hour)); ok {
		t.Error("a run after t must not count")
	}
}

func TestResultsDirStartsEmpty(t *testing.T) {
	root := newRepo(t)
	head := run(t, root, "git", "rev-parse", "HEAD")
	dir := ResultsDir(root, head, "")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "unit.json"), []byte("stale"), 0o644)
	c := tracker.CICommand{Cmd: "true", Results: []tracker.CIResult{{Kind: "unit", Format: "go-test-json", File: "unit.json"}}}
	runs, _, err := Execute(Options{Root: root, Config: cfg(c)})
	if err != nil {
		t.Fatal(err)
	}
	if e := runs[0].Results["unit"].Error; !strings.Contains(e, "not written") {
		t.Errorf("a file from an earlier run counted: %q", e)
	}
}

func TestValidateResults(t *testing.T) {
	c := cfg(tracker.CICommand{Cmd: "make", Results: []tracker.CIResult{
		{Kind: "unit", Format: "go-test-json", File: "unit.json"},
		{Kind: "smoke", Format: "tap", File: "../x"},
		{Kind: "e2e", Format: "playtest", File: "unit.json"},
		{Kind: "e2e", Format: "junit"},
	}})
	got := strings.Join(c.Validate(), "\n")
	for _, want := range []string{"results[1].kind \"smoke\"", "results[1].format \"tap\"", "results[1].file \"../x\"", "results[2].file \"unit.json\" is listed twice", "results[3].file is empty"} {
		if !strings.Contains(got, want) {
			t.Errorf("validate lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "results[0]") {
		t.Errorf("a valid entry was flagged:\n%s", got)
	}
}
