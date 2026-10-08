package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func enableBalance(t *testing.T, proj *tracker.Project) {
	t.Helper()
	data, err := os.ReadFile(proj.WorkflowFile)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nworkbench:\n  balance:\n    comment_prefix: \"tuning:\"\n")...)
	if err := os.WriteFile(proj.WorkflowFile, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBalanceWritesTypedNoteAndUpdates(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in progress", "", true)
	enableBalance(t, proj)
	args := []string{"cli/sample", "--metric", "full-holds", "--state", "open", "--hypothesis", "Defenders revive too freely.", "--change", "FinishChance ↑"}

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runBalance(ctx, args); err != nil {
		t.Fatalf("balance: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Balance note on cli/sample: full-holds · open")
	assertContains(t, stdout.String(), "/p/test/balance")
	if strings.Contains(stdout.String(), "Updates the note") || strings.Contains(stdout.String(), "tab is hidden") {
		t.Errorf("unexpected output:\n%s", stdout.String())
	}
	comments, _ := tracker.LoadComments(issuePath)
	if len(comments) != 1 || comments[0].Text != "tuning: full-holds · open\nhypothesis: Defenders revive too freely.\nchange: FinishChance ↑" {
		t.Fatalf("comments = %+v", comments)
	}

	ctx, stdout, _ = newTestContext(proj, true)
	if err := runBalance(ctx, []string{"cli/sample", "--metric", "full-holds", "--state", "needs-pieces", "--change", "finishchance ↑", "--pass", "3"}); err != nil {
		t.Fatalf("balance update: %v", err)
	}
	var out balanceOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if out.Note.State != tracker.BalanceNeedsPieces || out.Note.Pass != 3 || out.Replaces == nil || out.Replaces.State != tracker.BalanceOpen || !out.Enabled {
		t.Fatalf("json = %+v", out)
	}

	// The list shows only the current note, with one earlier note.
	ctx, stdout, _ = newTestContext(proj, true)
	if err := runBalance(ctx, []string{"--list", "--metric", "FULL-HOLDS"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	var list balanceListOutput
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Notes) != 1 || list.Notes[0].Earlier != 1 || list.Notes[0].State != tracker.BalanceNeedsPieces {
		t.Fatalf("list = %+v", list)
	}
}

func TestBalanceRejectsBadInputUntouched(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in progress", "", true)
	before := readFile(t, issuePath)
	for name, args := range map[string][]string{
		"no slug":       {"--metric", "m", "--state", "open"},
		"no metric":     {"cli/sample", "--state", "open"},
		"spaced metric": {"cli/sample", "--metric", "full holds", "--state", "open"},
		"bad state":     {"cli/sample", "--metric", "m", "--state", "maybe"},
		"bad pass":      {"cli/sample", "--metric", "m", "--state", "open", "--pass", "x"},
		"unknown issue": {"cli/nope", "--metric", "m", "--state", "open"},
		"list + slug":   {"cli/sample", "--list"},
		"list bad st":   {"--list", "--state", "maybe"},
	} {
		ctx, _, _ := newTestContext(proj, false)
		if err := runBalance(ctx, args); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if readFile(t, issuePath) != before {
		t.Fatal("rejected balance modified the issue")
	}
}

func TestBalanceWithoutWorkbenchWarnsAndListsUnsorted(t *testing.T) {
	proj, _ := makeGateFixture(t, "in progress", "", true)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runBalance(ctx, []string{"cli/sample", "--metric", "m", "--state", "kept"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Balance tab is hidden until workflow.yaml has a workbench.balance block")

	// A free-text balance comment gets the tip and lands in unsorted.
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runComment(ctx, []string{"cli/sample", "--text", "balance: [#cli/sample] holds look high"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Tip: issue-cli balance cli/sample --metric <id> --state <state>")
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runComment(ctx, []string{"cli/sample", "--text", "progress: no tip here"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "Tip:") {
		t.Error("tip printed for a non-balance comment")
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runBalance(ctx, []string{"--list"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "== Balance notes (1) ==\nm\n  [kept] cli/sample")
	assertContains(t, stdout.String(), "Unsorted (free-text) balance comments: 1")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runBalance(ctx, []string{"--list", "--state", "open"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "No typed balance notes match.")
	if !strings.HasSuffix(boardURL(nil), "/balance") {
		t.Error("boardURL without project")
	}
}
