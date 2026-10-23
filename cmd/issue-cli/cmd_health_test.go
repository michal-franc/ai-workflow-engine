package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHealthSnapshotAndLatest(t *testing.T) {
	proj, root := makeCIFixture(t, "")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), healthNoSnapshot)
	ctx, stdout, _ = newTestContext(proj, true)
	if err := runHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), `"hint": "No health snapshot yet`)

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"snapshot"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Snapshot written: "+filepath.Join(root, ".health", "snapshots"))
	assertContains(t, out, "· main at ")
	assertContains(t, out, "· manual ·")
	assertContains(t, out, "Codebase (lines by kind")
	assertContains(t, out, "  data         1         7      n/a      n/a")
	assertContains(t, out, "Areas (top 10 by lines, of 1):")
	assertContains(t, out, "Big files: 0\n")
	assertContains(t, out, "  codebase   ok, ")

	ctx, stdout, _ = newTestContext(proj, true)
	if err := runHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var snap health.Snapshot
	if err := json.Unmarshal(stdout.Bytes(), &snap); err != nil {
		t.Fatalf("--json: %v\n%s", err, stdout.String())
	}
	if snap.Run.Trigger != health.TriggerManual {
		t.Fatalf("trigger = %q", snap.Run.Trigger)
	}
	if _, ok := snap.Value("codebase.lines", map[string]string{"kind": "data"}); !ok {
		t.Fatal("no codebase.lines{kind=data} row")
	}

	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"bogus"}); err == nil || !strings.Contains(err.Error(), `unexpected argument "bogus"`) {
		t.Fatalf("bogus subcommand: %v", err)
	}
}

func TestHealthSnapshotOffAndNoGit(t *testing.T) {
	proj, _ := makeCIFixture(t, "health:\n  off: true\n")
	ctx, _, _ := newTestContext(proj, false)
	if err := runHealth(ctx, []string{"snapshot"}); err == nil || !strings.Contains(err.Error(), "health.off: true") {
		t.Fatalf("off: %v", err)
	}
	noGit := &tracker.Project{Slug: "x", WorkDir: t.TempDir()}
	ctx, _, _ = newTestContext(noGit, false)
	if err := runHealth(ctx, []string{"snapshot"}); err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Fatalf("no git: %v", err)
	}
}

func TestCIRunTakesHealthSnapshot(t *testing.T) {
	proj, root := makeCIFixture(t, ciBlockFor("mark"))
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCI(ctx, []string{"run"}); err != nil {
		t.Fatalf("ci run: %v\n%s", err, stdout.String())
	}
	assertContains(t, stdout.String(), "health: snapshot "+filepath.Join(root, ".health", "snapshots"))
	snap, _, err := health.Latest(root)
	if err != nil || snap == nil || snap.Run.Trigger != health.TriggerCI {
		t.Fatalf("latest after ci run = %+v, %v", snap, err)
	}
}

func TestPrintHealthShowsFamilyErrors(t *testing.T) {
	snap := &health.Snapshot{
		Run:      health.RunInfo{Date: "2026-10-23T14:05", Branch: "main", Commit: "abc1234", Trigger: "daily", DurationMS: 5},
		Families: map[string]health.FamilyResult{"workflow": {MS: 20000, Error: "timed out after 20s"}, "codebase": {MS: 3, Error: "git ls-tree: boom"}, "other": {MS: 1, Metrics: 4, Hint: "tables: /stats"}},
	}
	var buf bytes.Buffer
	printHealth(&buf, snap, "/x.json")
	out := buf.String()
	assertContains(t, out, "Health snapshot 2026-10-23T14:05 · main at abc1234 · daily · 5 ms")
	assertContains(t, out, "  codebase   error: git ls-tree: boom (3 ms)")
	assertContains(t, out, "  workflow   error: timed out after 20s (20000 ms)")
	assertContains(t, out, "  other      ok, 4 metrics (1 ms) · tables: /stats")
	if strings.Contains(out, "Codebase (") {
		t.Fatal("printed a codebase summary for a failed family")
	}
}
