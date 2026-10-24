package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestLintFamilyCountsFindings(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wf, []byte("template: custom\nstatuses:\n  - name: idea\n    description: raw\ntransitions: []\nbord: {}\n"), 0644)
	issues := filepath.Join(dir, "issues")
	os.MkdirAll(issues, 0755)
	os.WriteFile(filepath.Join(issues, "a.md"), []byte("---\ntitle: \"A\"\nstatus: idea\n---\n"), 0644)
	proj := &tracker.Project{Slug: "t", WorkflowFile: wf, IssueDir: issues}

	ms, err := lintFamily(context.Background(), Env{Root: dir, Project: proj, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, m := range ms {
		got[m.Name+" "+m.Labels["check"]] = m.Value
	}
	want := map[string]float64{
		"lint.warnings ":        2,
		"lint.findings schema":  1,
		"lint.findings undated": 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("metrics = %v, want only %v", got, want)
	}

	if _, err := lintFamily(context.Background(), Env{Root: dir, Project: proj, Now: time.Now(), Backfill: true}); !errors.Is(err, ErrNoPast) {
		t.Errorf("backfill: err = %v, want ErrNoPast", err)
	}
}

func TestLintScoreComponent(t *testing.T) {
	var lintComp Component
	for _, c := range DefaultComponents() {
		if c.Name == "lint" {
			lintComp = c
		}
	}
	if lintComp.Name == "" || lintComp.Weight != 10 || lintComp.Unit != UnitCount {
		t.Fatalf("lint component = %+v", lintComp)
	}
	cur := snapOf(map[string]float64{"lint.warnings": 5})
	got := scoreRows(Derive(cur, nil, []Component{lintComp}))
	if got["score.component{component=lint}"] != 75 || got["score.derived"] != 75 {
		t.Errorf("5 warnings (good 0, bad 20): rows = %v, want sub-score 75", got)
	}
}
