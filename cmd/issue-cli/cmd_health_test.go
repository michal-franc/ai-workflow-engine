package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// A family's warnings print under its line: a glob that matched nothing is
// said, not counted as zero silently.
func TestPrintHealthShowsFamilyWarnings(t *testing.T) {
	snap := &health.Snapshot{
		Run:      health.RunInfo{Date: "2026-10-23T14:05", Branch: "main", Commit: "abc1234", Trigger: "daily"},
		Families: map[string]health.FamilyResult{"tests": {MS: 2, Warnings: []string{"health.e2e_sessions (default playtest/sessions/*.yaml) matched no file, so no e2e session rows"}}},
	}
	var buf bytes.Buffer
	printHealth(&buf, snap, "/x.json")
	assertContains(t, buf.String(), "  tests      ok, 0 metrics (2 ms)\n             ⚠ health.e2e_sessions (default playtest/sessions/*.yaml) matched no file")
}

// Backfill fills each past day after the first commit, skips days that
// have a snapshot, and names the stats page; health then points at it.
func TestHealthBackfill(t *testing.T) {
	proj, root := makeCIFixture(t, "")
	later := time.Now().AddDate(0, 0, 3)

	ctx, stdout, _ := newTestContext(proj, false)
	ctx.Now = func() time.Time { return later }
	if err := runHealth(ctx, []string{"backfill", "--days", "3"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Backfilled 3 of 3 days (0 skipped, 0 failed) · charts: ")
	assertContains(t, out, "/p/test/stats")
	snaps := health.Range(root, time.Time{}, later)
	if len(snaps) != 3 || snaps[0].Run.Trigger != health.TriggerBackfill || !strings.HasSuffix(snaps[0].Run.Date, "T23:59") {
		t.Fatalf("snapshots = %d, first %+v", len(snaps), snaps[0].Run)
	}
	if _, ok := snaps[0].Families["milestone"]; ok {
		t.Error("backfill recorded the milestone family")
	}

	ctx, stdout, _ = newTestContext(proj, true)
	ctx.Now = func() time.Time { return later }
	if err := runHealth(ctx, []string{"backfill", "--days", "3"}); err != nil {
		t.Fatal(err)
	}
	var res struct {
		Written, Skipped int
		Charts           string
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil || res.Written != 0 || res.Skipped != 3 || !strings.HasSuffix(res.Charts, "/p/test/stats") {
		t.Fatalf("second run --json = %+v, %v\n%s", res, err, stdout.String())
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Charts: ")
	if strings.Contains(stdout.String(), "Trends need history") {
		t.Error("history hint shown with 3 snapshots")
	}

	ctx, _, _ = newTestContext(proj, false)
	var ue *usageError
	if err := runHealth(ctx, []string{"backfill", "--days", "0"}); !errors.As(err, &ue) {
		t.Fatalf("--days 0: %v, want a usage error", err)
	}
}

func TestHealthHintsBackfillWithOneSnapshot(t *testing.T) {
	proj, _ := makeCIFixture(t, "")
	ctx, _, _ := newTestContext(proj, false)
	if err := runHealth(ctx, []string{"snapshot"}); err != nil {
		t.Fatal(err)
	}
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Trends need history (1 snapshot so far): issue-cli health backfill")
}
