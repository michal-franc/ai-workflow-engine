package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func snapshotFiles(t *testing.T, root string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(root, ".health", "snapshots", "*.json"))
	return files
}

func TestHealthDailyTakesOneSnapshotPerDay(t *testing.T) {
	proj, root := ciRepo(t, "h", "")
	twin := proj // a second project on the same repo
	twin.Slug = "h2"
	off, offRoot := ciRepo(t, "off", "health:\n  off: true\n")
	noGit := tracker.Project{Slug: "nogit", WorkDir: t.TempDir()}
	projects := []tracker.Project{proj, twin, off, noGit}

	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.Local)
	lastErr := map[string]string{}
	healthDaily(projects, now, lastErr)
	if files := snapshotFiles(t, root); len(files) != 1 || filepath.Base(files[0]) != "2026-10-23T0900.json" {
		t.Fatalf("snapshots after the first check = %v", files)
	}
	if len(lastErr) != 0 {
		t.Fatalf("errors logged: %v", lastErr)
	}
	if files := snapshotFiles(t, offRoot); len(files) != 0 {
		t.Fatalf("health.off project got %v", files)
	}

	healthDaily(projects, now.Add(3*time.Hour), lastErr)
	if files := snapshotFiles(t, root); len(files) != 1 {
		t.Fatalf("second check the same day took another: %v", files)
	}
	healthDaily(projects, now.Add(24*time.Hour), lastErr)
	if files := snapshotFiles(t, root); len(files) != 2 {
		t.Fatalf("next day: %v", files)
	}
	if _, err := os.Stat(filepath.Join(root, ".health", ".gitignore")); err != nil {
		t.Fatal(err)
	}
}
