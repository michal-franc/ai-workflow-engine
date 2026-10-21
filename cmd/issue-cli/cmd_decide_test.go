package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// makeDecisionFixture adds two Sim issues next to cli/sample: sim/phases
// records D-1, and sim/creeps (backlog, open) cites it.
func makeDecisionFixture(t *testing.T) (*tracker.Project, string, string) {
	t.Helper()
	proj, samplePath := makeGateFixture(t, "in design", "", true)
	simDir := filepath.Join(proj.IssueDir, "Sim")
	if err := os.MkdirAll(simDir, 0755); err != nil {
		t.Fatal(err)
	}
	phasesPath := filepath.Join(simDir, "phases.md")
	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(phasesPath, "---\ntitle: \"phases\"\nstatus: \"done\"\nsystem: \"Sim\"\n---\n\n## Decisions\n- **D-1** · 2026-09-27 · chat: Three phases of 20 minutes.\n")
	write(filepath.Join(simDir, "creeps.md"), "---\ntitle: \"creeps\"\nstatus: \"backlog\"\nsystem: \"Sim\"\n---\n\nCamps follow sim/phases#D-1.\n")
	return proj, samplePath, phasesPath
}

func TestDecideRecordsAndListsStaleCiters(t *testing.T) {
	proj, samplePath, _ := makeDecisionFixture(t)
	ctx, stdout, _ := newTestContext(proj, false)
	ctx.Now = func() time.Time { return time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local) }
	err := runDecide(ctx, []string{"cli/sample", "--text", "ET-style objectives, 6v6.", "--how", "chat",
		"--quote", "no readable tension", "--replaces", "#sim/phases#d-1"})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Recorded cli/sample#D-1")
	assertContains(t, out, "Replaced sim/phases#D-1. Open issues still citing it:\n  sim/creeps (backlog)")
	assertContains(t, out, "Next: revise those issues to cite cli/sample#D-1")
	assertContains(t, out, "/p/test/decisions")
	assertContains(t, readFile(t, samplePath),
		`- **D-1** · 2026-10-07 · chat: ET-style objectives, 6v6, "no readable tension" (replaces sim/phases#D-1)`)

	// show on the citing issue now flags the replaced decision.
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runShow(ctx, []string{"sim/creeps"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	assertContains(t, stdout.String(), "== Cites replaced decisions (1) ==\n  sim/phases#D-1 → replaced by cli/sample#D-1: ET-style objectives, 6v6")

	// The index reports it as stale.
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runDecisions(ctx, []string{"--state", "stale"}); err != nil {
		t.Fatalf("decisions: %v", err)
	}
	assertContains(t, stdout.String(), "sim/phases#D-1 · 2026-09-27 · replaced · stale · cited 1 open/0 closed")
	assertContains(t, stdout.String(), "still cited by: sim/creeps")
}

func TestDecideJSONAndSameIssueReplace(t *testing.T) {
	proj, _, phasesPath := makeDecisionFixture(t)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runDecide(ctx, []string{"sim/phases", "--text", "Two phases.", "--replaces", "D-1", "--deferred"}); err != nil {
		t.Fatalf("decide: %v", err)
	}
	var out decideOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if out.Decision.Ref != "sim/phases#D-2" || !out.Decision.Deferred || len(out.StaleCiters) != 1 || out.StaleCiters[0].Slug != "sim/creeps" {
		t.Fatalf("json output = %+v", out)
	}
	assertContains(t, readFile(t, phasesPath), "· deferred · recorded by agent: Two phases. (replaces sim/phases#D-1)")
}

func TestDecideRejectsBadInputUntouched(t *testing.T) {
	proj, samplePath, _ := makeDecisionFixture(t)
	before := readFile(t, samplePath)
	cases := map[string][]string{
		"no text":        {"cli/sample", "--how", "chat"},
		"colon in how":   {"cli/sample", "--text", "x", "--how", "chat: yes"},
		"unknown ref":    {"cli/sample", "--text", "x", "--replaces", "sim/phases#D-9"},
		"not a ref":      {"cli/sample", "--text", "x", "--replaces", "sim/phases"},
		"unknown issue":  {"cli/nope", "--text", "x"},
		"missing slug":   {},
		"unknown flag":   {"cli/sample", "--text", "x", "--bogus"},
		"positional arg": {"cli/sample", "extra"},
	}
	for name, args := range cases {
		ctx, _, _ := newTestContext(proj, false)
		err := runDecide(ctx, args)
		if err == nil && name != "positional arg" {
			t.Errorf("%s: want error, got nil", name)
		}
		if name == "unknown ref" && err != nil {
			assertContains(t, err.Error(), "issue-cli decisions --issue sim/phases")
		}
	}
	if readFile(t, samplePath) != before {
		t.Fatal("rejected decide modified the issue")
	}
}

func TestDecisionsFiltersAndEmpty(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runDecisions(ctx, nil); err != nil {
		t.Fatalf("decisions: %v", err)
	}
	assertContains(t, stdout.String(), "No decisions recorded yet")

	proj, _, _ = makeDecisionFixture(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{nil, 1},
		{[]string{"--system", "sim"}, 1},
		{[]string{"--system", "CLI"}, 0},
		{[]string{"--state", "active"}, 1},
		{[]string{"--state", "replaced"}, 0},
		{[]string{"--issue", "sim/phases"}, 1},
		{[]string{"--issue", "cli/sample"}, 0},
	} {
		ctx, stdout, _ := newTestContext(proj, true)
		if err := runDecisions(ctx, tc.args); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		var rows []decisionRow
		if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
			t.Fatalf("%v: json: %v", tc.args, err)
		}
		if len(rows) != tc.want {
			t.Errorf("%v: %d rows, want %d", tc.args, len(rows), tc.want)
		}
		if len(rows) == 1 && (rows[0].Ref != "sim/phases#D-1" || len(rows[0].CitedOpen) != 1 || rows[0].Status != "done") {
			t.Errorf("%v: row = %+v", tc.args, rows[0])
		}
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runDecisions(ctx, []string{"--system", "CLI"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "No decisions match these filters.")
	for _, bad := range [][]string{{"--state", "bogus"}, {"--issue", "nope/missing"}} {
		ctx, _, _ := newTestContext(proj, false)
		if err := runDecisions(ctx, bad); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("ąbcdef", 4); got != "ąbc…" {
		t.Errorf("truncateRunes = %q", got)
	}
	if got := truncateRunes("abc", 4); got != "abc" {
		t.Errorf("truncateRunes short = %q", got)
	}
	if !strings.HasSuffix(decisionsURL(nil), "/decisions") {
		t.Error("decisionsURL without project")
	}
}
