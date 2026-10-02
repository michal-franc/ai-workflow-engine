package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var goldArgs = []string{
	"cli/sample",
	"--question", "Does unspent gold carry over between stages?",
	"--context", "At PROBE_N=200 teams bank about 140 gold.",
	"--option", "A=Reset to the stage's starting gold|Simplest",
	"--option", "B=Carry over, capped at 300|Rewards saving",
	"--recommend", "b",
	"--link", "docs/economy.md",
}

func TestAskWritesQuestionAndHintsWait(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runAsk(ctx, goldArgs); err != nil {
		t.Fatalf("ask: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Asked Q-1 on cli/sample")
	assertContains(t, out, "/p/test/inbox?issue=cli/sample")
	assertContains(t, out, "Next: issue-cli ask cli/sample --wait-for Q-1 --timeout 9m")

	content := readFile(t, issuePath)
	assertContains(t, content, "## Questions")
	assertContains(t, content, "- **B** (recommended) Carry over, capped at 300 — Rewards saving")
	assertContains(t, content, "Links: docs/economy.md")
}

func TestAskRejectsBadInput(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	before := readFile(t, issuePath)
	cases := map[string][]string{
		"one option":        {"cli/sample", "--question", "Q?", "--option", "A=Only"},
		"unknown recommend": {"cli/sample", "--question", "Q?", "--option", "A=x", "--option", "B=y", "--recommend", "C"},
		"bad option spec":   {"cli/sample", "--question", "Q?", "--option", "first choice", "--option", "B=y"},
		"timeout no wait":   {"cli/sample", "--question", "Q?", "--option", "A=x", "--option", "B=y", "--timeout", "1m"},
		"no question":       {"cli/sample", "--option", "A=x", "--option", "B=y"},
	}
	for name, args := range cases {
		ctx, _, _ := newTestContext(proj, false)
		if err := runAsk(ctx, args); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
	if readFile(t, issuePath) != before {
		t.Fatal("rejected ask modified the issue")
	}
}

func TestAskWaitReturnsDecisionWhenAnswered(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	clock := &fakeClock{now: time.Date(2026, 10, 2, 14, 0, 0, 0, time.Local)}
	clock.onPoll = map[int]func(){
		2: func() {
			store, _ := tracker.LoadStats(issuePath)
			if _, ok := store.QuestionWaits["Q-1"]; !ok {
				t.Error("question wait not recorded while blocked")
			}
			if _, err := tracker.AnswerQuestion(issuePath, "Q-1", "B", "revisit later", clock.now); err != nil {
				t.Errorf("answer: %v", err)
			}
		},
	}
	ctx, stdout, stderr := newWaitContext(proj, clock)
	args := append(append([]string(nil), goldArgs...), "--wait", "--timeout", "1m", "--interval", "10s")
	if err := runAsk(ctx, args); err != nil {
		t.Fatalf("ask --wait: %v", err)
	}
	assertContains(t, stderr.String(), "Waiting for an answer to Q-1")
	out := stdout.String()
	assertContains(t, out, "✓ Q-1 answered: B (recorded as D-1 under ## Decisions)")
	assertContains(t, out, `- **D-1** · 2026-10-02 · answered in inbox (Q-1): Does unspent gold carry over between stages? → **B. Carry over, capped at 300**, "revisit later"`)
	assertContains(t, out, "✓ Waited: 20s")
}

func TestAskWaitForTimesOutWithExit3AndKeepsStart(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	ctx, _, _ := newTestContext(proj, false)
	if err := runAsk(ctx, goldArgs); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)}
	ctx, _, _ = newWaitContext(proj, clock)
	err := runAsk(ctx, []string{"cli/sample", "--wait-for", "q-1", "--timeout", "30s", "--interval", "10s"})
	if code := exitCode(t, err); code != exitWaitTimeout {
		t.Fatalf("exit code = %d, want %d", code, exitWaitTimeout)
	}
	assertContains(t, err.Error(), "issue-cli ask cli/sample --wait-for Q-1 --timeout 9m")

	ctx, _, _ = newWaitContext(proj, clock)
	_ = runAsk(ctx, []string{"cli/sample", "--wait-for", "Q-1", "--timeout", "10s", "--interval", "10s"})
	store, _ := tracker.LoadStats(issuePath)
	if got := store.QuestionWaits["Q-1"]; !got.Equal(time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("wait start after re-run = %v, want the first start", got)
	}
}

func TestAskWaitForUnknownQuestion(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, _, _ := newTestContext(proj, false)
	err := runAsk(ctx, []string{"cli/sample", "--wait-for", "Q-4"})
	if err == nil || !strings.Contains(err.Error(), "has no question Q-4") {
		t.Fatalf("err = %v", err)
	}
}

func TestAskJSONAndShowListsOpenQuestions(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runAsk(ctx, goldArgs); err != nil {
		t.Fatal(err)
	}
	var out askOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if out.Question != "Q-1" || out.State != "open" || out.Slug != "cli/sample" {
		t.Fatalf("json output = %+v", out)
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runShow(ctx, []string{"cli/sample"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "== Open questions (1) ==\n  Q-1  Does unspent gold carry over between stages?")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runChecklist(ctx, []string{"cli/sample"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Block on one: issue-cli ask cli/sample --wait-for Q-1 --timeout 9m")
}
