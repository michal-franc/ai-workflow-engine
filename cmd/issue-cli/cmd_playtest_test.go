package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/playtestruns"
)

// writePlaytestRun makes a run folder under dir's .playtest/runs with one
// 1 KB shot.
func writePlaytestRun(t *testing.T, dir, session, name, issue string) string {
	t.Helper()
	run := filepath.Join(playtestruns.RunsDir(dir), session, name)
	if err := os.MkdirAll(filepath.Join(run, "shots"), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := name[strings.LastIndex(name, "-")+1:]
	y := "session: " + session + "\nissue: " + issue + "\nmode: " + mode + "\nsteps: []\n"
	if err := os.WriteFile(filepath.Join(run, "results.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "shots", "TP1-1440x900.png"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestTransitionToDoneArchivesPlaytestRuns(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	root := projectRoot(proj)
	wt := playtestruns.WorktreeDir(root, "cli/sample")
	writePlaytestRun(t, wt, "sample", "20261023-100000.000-auto", "cli/sample")
	latest := writePlaytestRun(t, wt, "sample", "20261023-120000.000-auto", "cli/sample")
	other := writePlaytestRun(t, wt, "board-basics", "20261023-120000.000-auto", "ui/board")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "done"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ playtest: archived 1 of 2 runs, deleted 2; the latest of sample (")
	assertContains(t, out, ") moved to .playtest/runs/, ")
	if _, err := os.Stat(filepath.Join(playtestruns.RunsDir(root), "sample", "20261023-120000.000-auto", "results.yaml")); err != nil {
		t.Errorf("latest run not in the root: %v", err)
	}
	for _, gone := range []string{latest, other} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still in the worktree", gone)
		}
	}
}

// A game project's runner writes modes ux-test / playtest and its shots
// beside results.yaml: done archives them all the same.
func TestTransitionToDoneArchivesGameRuns(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	root := projectRoot(proj)
	wt := playtestruns.WorktreeDir(root, "cli/sample")
	write := func(name, mode string) string {
		run := filepath.Join(playtestruns.RunsDir(wt), "battle-ai-heat", name)
		os.MkdirAll(run, 0o755)
		os.WriteFile(filepath.Join(run, "MV7@1920x1080.png"), make([]byte, 1024), 0o644)
		y := "session: battle-ai-heat\nissue: cli/sample\nmode: " + mode + "\nstarted: 2026-10-24T11:05:08\nsteps:\n- id: MV7\n  status: pass\n"
		os.WriteFile(filepath.Join(run, "results.yaml"), []byte(y), 0o644)
		return run
	}
	write("20261024-090000-ux-test", "ux-test")
	write("20261024-110508-ux-test", "ux-test")
	write("20261024-133651-playtest", "playtest")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "done"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	assertContains(t, stdout.String(), "✓ playtest: archived 2 of 3 runs, deleted 1; the latest of battle-ai-heat (")
	for _, name := range []string{"20261024-110508-ux-test", "20261024-133651-playtest"} {
		if _, err := os.Stat(filepath.Join(playtestruns.RunsDir(root), "battle-ai-heat", name, "MV7@1920x1080.png")); err != nil {
			t.Errorf("%s not archived: %v", name, err)
		}
	}
}

func TestArchiveLine(t *testing.T) {
	line, warn := archiveLine(playtestruns.Report{Own: 3, Archived: 2, Moved: 2, Deleted: 4, Sessions: []string{"a", "b"}, MovedBytes: 1 << 20, DeletedBytes: 3 << 20})
	if warn || line != "archived 2 of 3 runs, deleted 4; the latest of a and b (1.0 MB) moved to .playtest/runs/, 3.0 MB deleted" {
		t.Errorf("line = %q, warn %v", line, warn)
	}
	line, warn = archiveLine(playtestruns.Report{Own: 59, Deleted: 59, DeletedBytes: 1 << 20})
	if !warn || !strings.HasPrefix(line, "archived 0 of 59 runs, deleted 59") || !strings.Contains(line, "none of this issue's runs") {
		t.Errorf("line = %q, warn %v; want a warning", line, warn)
	}
	if _, warn = archiveLine(playtestruns.Report{Deleted: 5}); warn {
		t.Error("a worktree with only other issues' runs is not a warning")
	}
}

func TestPlaytestPruneDoneIssueSaysArchived(t *testing.T) {
	proj := makeDoneParkedFixture(t, "done", "", "")
	root := projectRoot(proj)
	wt := playtestruns.WorktreeDir(root, "cli/sample")
	writePlaytestRun(t, wt, "sample", "20261023-100000.000-auto", "cli/sample")
	writePlaytestRun(t, wt, "sample", "20261023-120000.000-auto", "cli/sample")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runPlaytest(ctx, []string{"prune", "--all", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "  cli/sample: archived 1 of 2 runs, deleted 1;")
}

func TestTransitionToDoneWithoutRunsSaysNothing(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "done"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	assertNotContains(t, stdout.String(), "playtest:")
}

func TestPlaytestPruneDryRunAndAll(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	root := projectRoot(proj)
	// The done issue's worktree (sample is still shipping here: open).
	wt := playtestruns.WorktreeDir(root, "cli/sample")
	own := writePlaytestRun(t, wt, "sample", "20261023-120000.000-auto", "cli/sample")
	other := writePlaytestRun(t, wt, "board-basics", "20261023-120000.000-auto", "ui/board")
	orphan := writePlaytestRun(t, playtestruns.WorktreeDir(root, "ui/gone"), "board-basics", "20261023-120000.000-auto", "ui/board")
	ci := writePlaytestRun(t, filepath.Join(root, ".worktrees", ".ci"), "board-basics", "20261023-120000.000-auto", "ui/board")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runPlaytest(ctx, []string{"prune", "--all", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"== Playtest prune (dry run) ==", "1 worktree, ", "1 run, runs of other issues' sessions", "open issues' own runs stay", "Skipped 1 worktree with no issue: ui/gone", "Run without --dry-run to apply."} {
		assertContains(t, out, want)
	}
	for _, p := range []string{own, other, orphan, ci} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("dry run touched %s", p)
		}
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runPlaytest(ctx, []string{"prune", "cli/sample"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ Now ")
	if _, err := os.Stat(other); err == nil {
		t.Error("prune kept another issue's run")
	}
	if _, err := os.Stat(own); err != nil {
		t.Error("prune deleted an open issue's own run")
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Error("prune <slug> touched another worktree")
	}
}

func TestPlaytestPruneUsage(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	for _, args := range [][]string{nil, {"tidy"}, {"prune"}, {"prune", "cli/sample", "--all"}, {"prune", "a", "b"}} {
		ctx, _, _ := newTestContext(proj, false)
		err := runPlaytest(ctx, args)
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
	}
}

func TestJoinAndCountOf(t *testing.T) {
	if got := joinAnd([]string{"a", "b", "c"}); got != "a, b and c" {
		t.Errorf("joinAnd = %q", got)
	}
	if got := countOf(1, "run", "runs") + "/" + countOf(2, "run", "runs"); got != "1 run/2 runs" {
		t.Errorf("countOf = %q", got)
	}
}
