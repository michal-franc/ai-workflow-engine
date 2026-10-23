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

func TestRowIDSortsLabels(t *testing.T) {
	if got := RowID("codebase.lines", nil); got != "codebase.lines" {
		t.Fatalf("bare id = %q", got)
	}
	got := RowID("workflow.status.working.p50", map[string]string{"type": "all", "status": "testing"})
	if got != "workflow.status.working.p50{status=testing,type=all}" {
		t.Fatalf("id = %q", got)
	}
}

func TestRegisterTwicePanics(t *testing.T) {
	name := "test.dup"
	Register(name, func(context.Context, Env) ([]Metric, error) { return nil, nil })
	defer func() {
		registryMu.Lock()
		for i, r := range registry {
			if r.name == name {
				registry = append(registry[:i], registry[i+1:]...)
				break
			}
		}
		registryMu.Unlock()
		if recover() == nil {
			t.Fatal("second Register did not panic")
		}
	}()
	Register(name, func(context.Context, Env) ([]Metric, error) { return nil, nil })
}

// A failing, panicking or slow family is recorded; the others still land.
func TestTakeKeepsGoingPastBrokenFamilies(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 23, 14, 5, 0, 0, time.Local)
	env := Env{Root: root, Project: &tracker.Project{Slug: "p"}, Config: &tracker.HealthConfig{Timeout: "50ms"}, Branch: "main", Commit: "c55d815aaaa", Now: now}
	fams := []registered{
		{"bad", func(context.Context, Env) ([]Metric, error) { return nil, errors.New("boom") }},
		{"good", func(_ context.Context, e Env) ([]Metric, error) {
			return []Metric{
				{Name: "good.lines", Value: 10, Unit: UnitCount, Help: "Lines"},
				{Name: "good.lines", Labels: map[string]string{"lang": "go"}, Value: 7, Unit: UnitCount},
			}, nil
		}},
		{"panics", func(context.Context, Env) ([]Metric, error) { panic("oops") }},
		{"slow", func(ctx context.Context, _ Env) ([]Metric, error) { time.Sleep(time.Second); return nil, nil }},
	}
	Describe("good", "tables: /stats")
	defer Describe("good", "")
	snap, path, err := take(context.Background(), env, TriggerManual, fams)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".health", "snapshots", "2026-10-23T1405.json"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if snap.Run.Date != "2026-10-23T14:05" || snap.Run.Commit != "c55d815" || snap.Run.Trigger != "manual" || snap.Run.Project != "p" {
		t.Fatalf("run = %+v", snap.Run)
	}
	for name, want := range map[string]string{"bad": "boom", "panics": "panic: oops", "slow": "timed out after 50ms"} {
		if got := snap.Families[name].Error; got != want {
			t.Errorf("families[%s].error = %q, want %q", name, got, want)
		}
	}
	if snap.Families["good"].Metrics != 2 || snap.Families["good"].Error != "" || snap.Families["good"].Hint != "tables: /stats" {
		t.Fatalf("good = %+v", snap.Families["good"])
	}
	if v, ok := snap.Value("good.lines", map[string]string{"lang": "go"}); !ok || v != 7 {
		t.Fatalf("labelled value = %v %v", v, ok)
	}
	row := snap.Rows("good.lines")[1]
	if row.ID != "good.lines{lang=go}" || row.Name != "Lines" || row.Group != "good" || row.Band.Unit != UnitCount {
		t.Fatalf("row = %+v", row)
	}

	got, gotPath, err := Latest(root)
	if err != nil || gotPath != path || len(got.Metrics) != 2 {
		t.Fatalf("Latest = %v %s %v", got, gotPath, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, ".health", ".gitignore")); err != nil || string(data) == "" {
		t.Fatalf(".gitignore: %v", err)
	}
}

func TestBeforeAndPrune(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.Local)
	write := func(at time.Time) {
		if _, err := Write(root, &Snapshot{Run: RunInfo{Date: at.Format(dateLayout)}}, at); err != nil {
			t.Fatal(err)
		}
	}
	old := now.AddDate(0, 0, -40)
	write(old.Add(time.Hour))
	write(old.Add(3 * time.Hour)) // same old day: the earlier one goes
	write(now.AddDate(0, 0, -8))
	write(now.AddDate(0, 0, -8).Add(time.Hour)) // inside 35 days: both stay
	write(now)

	prune(root, now)
	if n := len(list(root)); n != 4 {
		t.Fatalf("after prune %d files, want 4", n)
	}
	if s := Before(root, now.AddDate(0, 0, -7)); s == nil || s.Run.Date != now.AddDate(0, 0, -8).Add(time.Hour).Format(dateLayout) {
		t.Fatalf("Before 7d = %+v", s)
	}
	if s := Before(root, old); s != nil {
		t.Fatalf("Before the first snapshot = %+v", s)
	}
}

func TestLatestWithoutSnapshots(t *testing.T) {
	s, p, err := Latest(t.TempDir())
	if s != nil || p != "" || err != nil {
		t.Fatalf("Latest on empty = %v %q %v", s, p, err)
	}
}
