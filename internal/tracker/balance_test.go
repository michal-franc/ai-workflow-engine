package tracker

import (
	"strings"
	"testing"
)

func TestParseBalanceComment(t *testing.T) {
	text := "Balance: full-holds · Needs-Pieces\nhypothesis: Defenders revive too freely.\nchange: FinishChance ↑\n evidence: finish rate 0%: across 200\nverdict: Later.\npass: #3\nnote: extra line"
	n, typed, isBalance := ParseBalanceComment("balance:", text)
	if !isBalance || !typed {
		t.Fatalf("typed=%v isBalance=%v", typed, isBalance)
	}
	if n.Metric != "full-holds" || n.State != BalanceNeedsPieces || n.Hypothesis != "Defenders revive too freely." ||
		n.Change != "FinishChance ↑" || n.Evidence != "finish rate 0%: across 200" || n.Verdict != "Later." || n.Pass != 3 {
		t.Fatalf("note = %+v", n)
	}
	if len(n.Extra) != 1 || n.Extra[0] != "note: extra line" {
		t.Errorf("extra = %v", n.Extra)
	}

	for _, free := range []string{
		"balance: [#ai/x] full-holds is 38% after the fix",
		"balance: full-holds · maybe",
		"balance: full holds · open",
		"balance:",
	} {
		if _, typed, isBalance := ParseBalanceComment("", free); typed || !isBalance {
			t.Errorf("%q: typed=%v isBalance=%v, want free-text balance", free, typed, isBalance)
		}
	}
	if _, _, isBalance := ParseBalanceComment("", "progress: done"); isBalance {
		t.Error("non-balance comment matched")
	}
	if _, typed, _ := ParseBalanceComment("tuning:", "tuning: knob · kept"); !typed {
		t.Error("custom prefix not honoured")
	}
}

func TestRenderBalanceNote(t *testing.T) {
	text, err := RenderBalanceNote("", NewBalanceNote{Metric: "lanes.blocks.time_fighting", State: "not-this-branch", Change: "  Knob  ↑ ", Pass: 2})
	if err != nil {
		t.Fatal(err)
	}
	if text != "balance: lanes.blocks.time_fighting · not this branch\nchange: Knob ↑\npass: 2" {
		t.Fatalf("text = %q", text)
	}
	n, typed, _ := ParseBalanceComment("", text)
	if !typed || n.Pass != 2 || n.Change != "Knob ↑" {
		t.Fatalf("round trip = %+v", n)
	}
	for name, bad := range map[string]NewBalanceNote{
		"no metric":     {State: "open"},
		"spaced metric": {Metric: "full holds", State: "open"},
		"bad state":     {Metric: "m", State: "maybe"},
		"negative pass": {Metric: "m", State: "open", Pass: -1},
	} {
		if _, err := RenderBalanceNote("", bad); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func balanceIssue(slug, status string, extra ...ExtraField) *Issue {
	return &Issue{Slug: slug, Status: status, ExtraFields: extra, Created: "2026-09-20"}
}

func TestBuildBalanceBoard(t *testing.T) {
	finish := balanceIssue("combat/finish", "backlog")
	scout := balanceIssue("ai/scout", "done")
	coord := balanceIssue("sim/balance", "idea")
	pass2 := balanceIssue("sim/pass-2", "done", ExtraField{Key: "balance_pass", Value: "2"})
	pass2.DoneAt = "2026-09-30"
	pass3 := balanceIssue("sim/pass-3", "idea", ExtraField{Key: "balance_pass", Value: "3"})
	pass4 := balanceIssue("sim/pass-4", "idea", ExtraField{Key: "balance_pass", Value: "4"})
	issues := []*Issue{finish, scout, coord, pass2, pass3, pass4}

	comments := map[string][]Comment{
		"combat/finish": {
			{ID: 1, Date: "2026-10-06", Text: "balance: full-holds · open\nchange: FinishChance ↑"},
			{ID: 2, Date: "2026-10-05", Text: "progress: unrelated"},
		},
		"ai/scout": {
			{ID: 3, Date: "2026-09-30", Text: "balance: dominant-plays · kept\nchange: ScoutAccuracy 1.0 → 0.7\npass: 2"},
			{ID: 1, Date: "2026-09-29", Text: "balance: dominant-plays · open\nchange: scoutaccuracy  1.0 → 0.7"},
			{ID: 2, Date: "2026-09-29", Text: "balance: Dominant-Plays · reverted\nchange: Other"},
		},
		"sim/balance": {
			{ID: 1, Date: "2026-10-01", Text: "balance: [#ai/scout] scouting looks strong"},
			{ID: 2, Date: "2026-10-07", Text: "balance: [#nope/missing] unknown source stays in text"},
		},
	}
	closed := func(i *Issue) bool { return i.Status == "done" }
	b := BuildBalanceBoard(issues, func(i *Issue) []Comment { return comments[i.Slug] }, "", closed)

	if len(b.Groups) != 2 || b.Groups[0].Metric != "full-holds" || b.Groups[1].Metric != "dominant-plays" {
		t.Fatalf("groups = %+v", b.Groups)
	}
	dp := b.Groups[1]
	if len(dp.Notes) != 2 || dp.Notes[0].State != BalanceKept || dp.Notes[1].State != BalanceReverted {
		t.Fatalf("dominant-plays notes = %+v", dp.Notes)
	}
	if len(dp.Notes[0].History) != 1 || dp.Notes[0].History[0].State != BalanceOpen {
		t.Errorf("kept note history = %+v", dp.Notes[0].History)
	}
	if b.Counts[BalanceOpen] != 1 || b.Counts[BalanceKept] != 1 || b.Counts[BalanceReverted] != 1 || len(b.Notes) != 3 {
		t.Errorf("counts = %v, notes = %d", b.Counts, len(b.Notes))
	}

	if len(b.Unsorted) != 2 || b.Unsorted[0].From != "" || !strings.HasPrefix(b.Unsorted[0].Text, "[#nope/missing]") {
		t.Errorf("unsorted[0] = %+v", b.Unsorted[0])
	}
	if b.Unsorted[1].From != "ai/scout" || b.Unsorted[1].Text != "scouting looks strong" {
		t.Errorf("unsorted[1] = %+v", b.Unsorted[1])
	}

	if len(b.Passes) != 3 || b.Planned == nil || b.Planned.Number != 3 {
		t.Fatalf("passes = %+v planned=%v", b.Passes, b.Planned)
	}
	if p := b.Passes[0]; !p.Closed || p.Date != "2026-09-30" || p.Notes != 1 {
		t.Errorf("pass 2 = %+v", p)
	}
	if p := b.Passes[1]; !p.Planned || p.Notes != 1 {
		t.Errorf("pass 3 = %+v (collects the open pass-less note)", p)
	}
	if p := b.Passes[2]; p.Planned || p.Notes != 0 {
		t.Errorf("pass 4 = %+v", p)
	}
	if got := b.PlannedPassFor(b.Groups[0].Notes[0]); got != 3 {
		t.Errorf("PlannedPassFor open note = %d", got)
	}
	if got := b.PlannedPassFor(dp.Notes[1]); got != 0 {
		t.Errorf("PlannedPassFor reverted note = %d", got)
	}
}
