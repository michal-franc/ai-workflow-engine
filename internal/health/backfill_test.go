package health

import (
	"context"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Backfill writes one end-of-day snapshot per past day at the commit the
// branch had then, skips days that have one or predate the first commit,
// and leaves out a family that cannot measure the past.
func TestBackfillWritesOnePastSnapshotPerDay(t *testing.T) {
	now := time.Date(2026, 10, 23, 14, 0, 0, 0, time.Local)
	root := t.TempDir()
	gitT(t, root, nil, "init", "-q", "-b", "main")
	gitT(t, root, nil, "config", "user.email", "h@test")
	gitT(t, root, nil, "config", "user.name", "h")
	gitT(t, root, nil, "config", "commit.gpgsign", "false")
	writeT(t, root, "a.go", lines(10))
	commitAt(t, root, now.AddDate(0, 0, -3).Add(-time.Hour), "first") // 20 Oct 13:00
	first := gitT(t, root, nil, "rev-parse", "HEAD")
	writeT(t, root, "b.go", lines(10))
	commitAt(t, root, now.AddDate(0, 0, -1).Add(-time.Hour), "second") // 22 Oct 13:00
	head := gitT(t, root, nil, "rev-parse", "HEAD")

	// 21 Oct already has a snapshot.
	if _, err := Write(root, &Snapshot{Run: RunInfo{Date: "2026-10-21T09:00", Trigger: TriggerDaily}}, time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}

	var commits []string
	fams := []registered{
		{"probe", func(_ context.Context, e Env) ([]Metric, error) {
			if !e.Backfill {
				t.Error("Env.Backfill not set")
			}
			commits = append(commits, e.Commit)
			return []Metric{{Name: "probe.day", Value: float64(e.Now.Day())}}, nil
		}},
		{"present", func(_ context.Context, e Env) ([]Metric, error) { return nil, ErrNoPast }},
	}
	env := Env{Root: root, Project: &tracker.Project{Slug: "p"}, Branch: "main", Commit: head, Now: now}
	var days []BackfillDay
	if err := backfill(context.Background(), env, 4, fams, func(d BackfillDay) { days = append(days, d) }); err != nil {
		t.Fatal(err)
	}
	if len(days) != 4 {
		t.Fatalf("days = %+v", days)
	}
	if !days[0].NoCommit || days[0].Path == "" || days[1].Path == "" || days[1].NoCommit || days[2].Skipped != "has a snapshot" || days[3].Path == "" {
		t.Fatalf("days = %+v", days)
	}
	if len(commits) != 3 || commits[0] != "" || commits[1] != first || commits[2] != head {
		t.Fatalf("measured commits = %v, want none, then %s, then %s", commits, first, head)
	}
	s, err := Read(days[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Run.Trigger != TriggerBackfill || s.Run.Date != "2026-10-20T23:59" {
		t.Fatalf("run = %+v", s.Run)
	}
	if _, ok := s.Families["present"]; ok {
		t.Fatal("a family that returned ErrNoPast was recorded")
	}
	if v, ok := s.Value("probe.day", nil); !ok || v != 20 {
		t.Fatalf("probe.day = %v, %v", v, ok)
	}
	// Before the first commit the codebase family sits the day out.
	if _, err := codebaseFamily(context.Background(), Env{Root: root, Backfill: true}); err != ErrNoPast {
		t.Fatalf("codebase before the first commit: %v, want ErrNoPast", err)
	}
	got := Range(root, time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local), now)
	if len(got) != 3 || got[0].Run.Date != "2026-10-20T23:59" || got[2].Run.Date != "2026-10-22T23:59" {
		t.Fatalf("Range = %d snapshots", len(got))
	}
	if !HasDay(root, time.Date(2026, 10, 22, 1, 0, 0, 0, time.Local)) || HasDay(root, now) {
		t.Fatal("HasDay wrong")
	}
}
