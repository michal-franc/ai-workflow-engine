package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// shrinkProject writes a project with one done issue (ui/old, done at
// doneAt) and one playtest run of it with a 1440x900 PNG in the root.
func shrinkProject(t *testing.T, healthBlock string, doneAt time.Time) (tracker.Project, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "issues", "UI")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(root, "workflow.yaml"), []byte("statuses:\n  - name: \"shipping\"\n  - name: \"done\"\ntransitions: []\n"+healthBlock), 0o644)
	os.WriteFile(filepath.Join(dir, "old.md"), []byte("---\ntitle: \"old\"\nstatus: \"done\"\nsystem: \"UI\"\n---\n\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "old.stats.json"), []byte(`{"transitions":[{"from":"shipping","to":"done","ts":"`+doneAt.Format(time.RFC3339)+`"}]}`), 0o644)
	run := filepath.Join(root, ".playtest", "runs", "old-tab", "20261001-100000.000-auto")
	os.MkdirAll(filepath.Join(run, "shots"), 0o755)
	os.WriteFile(filepath.Join(run, "results.yaml"), []byte("session: old-tab\nissue: ui/old\nmode: auto\nsteps:\n  - id: TP1\n    viewports:\n      - viewport: 1440x900\n        screenshot: shots/TP1-1440x900.png\n"), 0o644)
	f, err := os.Create(filepath.Join(run, "shots", "TP1-1440x900.png"))
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 1440, 900)))
	f.Close()
	return tracker.Project{Name: "p", Slug: "p", IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root}, run
}

func TestShrinkOldShotsAfterThirtyDays(t *testing.T) {
	now := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	proj, run := shrinkProject(t, "", now.AddDate(0, 0, -31))
	line := shrinkOldShots(&proj, now)
	if !strings.Contains(line, "health: p: shrank shots of 1 issues done before 2026-11-01:") {
		t.Fatalf("line = %q", line)
	}
	if _, err := os.Stat(filepath.Join(run, "shots", "TP1-1440x900.jpg")); err != nil {
		t.Errorf("no JPEG: %v", err)
	}
	if again := shrinkOldShots(&proj, now.Add(24*time.Hour)); again != "" {
		t.Errorf("a reduced run was shrunk again: %q", again)
	}
}

func TestShrinkOldShotsRespectsTheSetting(t *testing.T) {
	now := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	recent, _ := shrinkProject(t, "", now.AddDate(0, 0, -10))
	if line := shrinkOldShots(&recent, now); line != "" {
		t.Errorf("done 10 days ago was shrunk: %q", line)
	}
	off, run := shrinkProject(t, "health:\n  shrink_shots_after_days: 0\n", now.AddDate(0, 0, -300))
	if line := shrinkOldShots(&off, now); line != "" {
		t.Errorf("shrink_shots_after_days: 0 shrank: %q", line)
	}
	if _, err := os.Stat(filepath.Join(run, "shots", "TP1-1440x900.png")); err != nil {
		t.Error("the PNG went with shrinking off")
	}
	week, _ := shrinkProject(t, "health:\n  shrink_shots_after_days: 7\n", now.AddDate(0, 0, -10))
	if line := shrinkOldShots(&week, now); line == "" {
		t.Error("shrink_shots_after_days: 7 left a 10-day-old issue alone")
	}
}
