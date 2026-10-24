package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func writeSized(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStorageFamilyMeasuresEachArea(t *testing.T) {
	root := t.TempDir()
	const mb = 1 << 20
	writeSized(t, filepath.Join(root, ".git", "objects", "pack"), 2*mb)
	writeSized(t, filepath.Join(root, ".worktrees", "ui", "a", ".git"), 100)
	writeSized(t, filepath.Join(root, ".worktrees", "ui", "a", "main.go"), 3*mb)
	writeSized(t, filepath.Join(root, ".worktrees", "ui", "a", ".playtest", "runs", "s", "r", "shots", "TP1.png"), 4*mb)
	writeSized(t, filepath.Join(root, ".worktrees", "cli", "b", ".git"), 100)
	writeSized(t, filepath.Join(root, ".playtest", "runs", "s", "r", "shots", "TP1.png"), 1*mb)
	writeSized(t, filepath.Join(root, ".agent-logs", "clilog.jsonl"), mb/2)
	writeSized(t, filepath.Join(root, "issues", "attachments", "x", "Main.png"), mb)
	writeSized(t, filepath.Join(root, "main.go"), mb)
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	env := Env{Root: root, Project: &tracker.Project{IssueDir: filepath.Join(root, "issues")}}
	metrics, err := storageFamily(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, m := range metrics {
		got[m.Name] = m.Value
		if m.Help == "" {
			t.Errorf("%s has no help", m.Name)
		}
	}
	want := map[string]float64{
		"storage.total_mb":       12.5,
		"storage.git_mb":         2,
		"storage.worktrees_mb":   3,
		"storage.worktrees":      2,
		"storage.playtest_mb":    5,
		"storage.agent_logs_mb":  0.5,
		"storage.attachments_mb": 1,
	}
	for name, v := range want {
		if got[name] != v {
			t.Errorf("%s = %v, want %v", name, got[name], v)
		}
	}
	for _, a := range StorageAreas {
		if _, ok := got[a.Metric]; !ok {
			t.Errorf("StorageAreas names %s, which the family doesn't record", a.Metric)
		}
	}
}

func TestStorageFamilyHasNoPast(t *testing.T) {
	if _, err := storageFamily(context.Background(), Env{Root: t.TempDir(), Backfill: true}); !errors.Is(err, ErrNoPast) {
		t.Errorf("backfill err = %v, want ErrNoPast", err)
	}
}

func TestStorageAttachmentsOutsideTheRoot(t *testing.T) {
	root, issues := t.TempDir(), t.TempDir()
	writeSized(t, filepath.Join(issues, "attachments", "a.png"), 1<<20)
	s, err := measureStorage(context.Background(), root, filepath.Join(issues, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	if s.attachments != 1<<20 || s.total != 0 {
		t.Errorf("sizes = %+v", s)
	}
}
