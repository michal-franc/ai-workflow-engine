package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDecisions_InboxAndDecideLines(t *testing.T) {
	body := strings.Join([]string{
		"Intro mentions **D-9** but is not a decision.",
		"",
		"## Decisions",
		"- Free-form legacy note: **A** was picked.",
		`- **D-1** · 2026-10-02 · answered in inbox (Q-1): Does gold carry over? → **B. Carry over, capped at 300**, "B, but \"revisit\" later" (also considered: A Reset; C Carry over in full)`,
		`- **D-2** · 2026-10-04 · deferred · chat, relayed by the lead: Bows stay behind the front for now.`,
		`- **D-3** · 2026-10-06 · project rule: Phases: none. (replaces D-1, sim/old#d-4)`,
		"- **D-2** · 2026-10-07 · chat: duplicate id, ignored",
		"",
		"```",
		"- **D-8** · 2026-10-02 · chat: fenced example",
		"```",
		"",
		"## Design",
		"- **D-5** · 2026-10-02 · chat: wrong section",
	}, "\n")

	got := ParseDecisions("economy/gold", body)
	if len(got) != 3 {
		t.Fatalf("got %d decisions, want 3: %+v", len(got), got)
	}
	d1 := got[0]
	if d1.Ref != "economy/gold#D-1" || d1.How != "answered in inbox (Q-1)" {
		t.Errorf("d1 ref/how = %q / %q", d1.Ref, d1.How)
	}
	if d1.Text != "Does gold carry over? → **B. Carry over, capped at 300**" {
		t.Errorf("d1 text = %q", d1.Text)
	}
	if d1.Quote != `B, but "revisit" later` || d1.AlsoConsidered != "A Reset; C Carry over in full" {
		t.Errorf("d1 quote/also = %q / %q", d1.Quote, d1.AlsoConsidered)
	}
	if !d1.Date.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.Local)) {
		t.Errorf("d1 date = %v", d1.Date)
	}
	if d2 := got[1]; !d2.Deferred || d2.How != "chat, relayed by the lead" || d2.Text != "Bows stay behind the front for now." {
		t.Errorf("d2 = %+v", d2)
	}
	d3 := got[2]
	if d3.Text != "Phases: none." {
		t.Errorf("d3 text = %q (how/text split must use the first ': ')", d3.Text)
	}
	if strings.Join(d3.Replaces, ",") != "economy/gold#D-1,sim/old#D-4" {
		t.Errorf("d3 replaces = %v", d3.Replaces)
	}
}

func TestNormalizeDecisionRef(t *testing.T) {
	for in, want := range map[string]string{
		"D-2":            "a/b#D-2",
		" d-2 ":          "a/b#D-2",
		"#sim/x#D-7":     "sim/x#D-7",
		"Sim/X.md#d-7":   "sim/x#D-7",
		"sim/x":          "",
		"sim/x#Q-1":      "",
		"#D-1":           "",
		"sim/x#D-banana": "",
	} {
		if got := NormalizeDecisionRef("a/b", in); got != want {
			t.Errorf("NormalizeDecisionRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddDecision_AppendsTightAndRoundTrips(t *testing.T) {
	fp := writeDecisionIssue(t, "Body.\n\n## Decisions\n- **D-1** · 2026-10-02 · chat: First.\n\n## Design\nText.")
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	d, err := AddDecision(fp, "economy/gold", NewDecision{
		Text: "Gold resets each stage.", How: "playtest", Quote: `"reset it"`, Deferred: true,
		Replaces: []string{"economy/gold#D-1"},
	}, now)
	if err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	if d.Ref != "economy/gold#D-2" || !d.Deferred || d.Quote != `"reset it"` || d.Text != "Gold resets each stage" {
		t.Fatalf("decision = %+v", d)
	}
	body := readBody(t, fp)
	want := "## Decisions\n- **D-1** · 2026-10-02 · chat: First.\n" +
		`- **D-2** · 2026-10-07 · deferred · playtest: Gold resets each stage, "\"reset it\"" (replaces economy/gold#D-1)` +
		"\n\n## Design"
	if !strings.Contains(body, want) {
		t.Fatalf("body missing tight decision list:\n%s", body)
	}

	// Missing section is created; default how.
	fp2 := writeDecisionIssue(t, "Just a body.")
	d, err = AddDecision(fp2, "x", NewDecision{Text: "Ship it."}, now)
	if err != nil || d.ID != "D-1" || d.How != defaultDecisionHow {
		t.Fatalf("AddDecision new section = %+v, %v", d, err)
	}
	if !strings.Contains(readBody(t, fp2), "## Decisions\n\n- **D-1** · 2026-10-07 · recorded by agent: Ship it.") {
		t.Fatalf("section not created:\n%s", readBody(t, fp2))
	}
}

func TestValidateNewDecision(t *testing.T) {
	if err := ValidateNewDecision(NewDecision{Text: "  "}); err == nil {
		t.Error("empty text accepted")
	}
	if err := ValidateNewDecision(NewDecision{Text: "x", How: "chat: yes"}); err == nil {
		t.Error("how with ':' accepted")
	}
	if err := ValidateNewDecision(NewDecision{Text: "x", How: "chat"}); err != nil {
		t.Errorf("valid decision rejected: %v", err)
	}
}

func decisionIssue(slug, status, body string) *Issue {
	return &Issue{Slug: slug, Status: status, System: "Sim", BodyRaw: body}
}

func TestBuildDecisionIndex_ReplacedStaleAndCitations(t *testing.T) {
	pivot := decisionIssue("sim/pivot", "done", "## Decisions\n- **D-1** · 2026-09-28 · chat: ET-style objectives. (replaces sim/phases#D-1, sim/format#D-2)")
	phases := decisionIssue("sim/phases", "obsolete", "## Decisions\n- **D-1** · 2026-09-27 · chat: Three phases of 20 minutes.\n- **D-2** · 2026-09-27 · deferred · chat: Respawn timers later.")
	format := decisionIssue("sim/format", "obsolete", "## Decisions\n- **D-2** · 2026-09-26 · project rule: One continuous raid.\n\nSelf-cite sim/format#D-2 does not count.")
	creeps := decisionIssue("sim/creeps", "backlog", "Built on sim/phases#D-1 and #sim/pivot#D-1.\nAgain sim/phases#D-1 (counted once).\n```\nsim/format#D-2 in a fence\n```")
	events := decisionIssue("sim/events", "idea", "See http://localhost:8080/p/rm/issue/sim/phases#D-1 and unknown/x#D-1.")
	closed := decisionIssue("ui/gdd", "done", "Cites sim/phases#D-1 and sim/format#D-2.")
	issues := []*Issue{pivot, phases, format, creeps, events, closed}

	closedFn := func(i *Issue) bool { return i.Status == "done" || i.Status == "obsolete" }
	x := BuildDecisionIndex(issues, closedFn)

	var refs []string
	for _, e := range x.Entries {
		refs = append(refs, e.Ref)
	}
	if got := strings.Join(refs, " "); got != "sim/pivot#D-1 sim/phases#D-2 sim/phases#D-1 sim/format#D-2" {
		t.Fatalf("order = %s", got)
	}

	ph := x.Lookup("sim/phases#D-1")
	if ph.ReplacedBy != x.Lookup("sim/pivot#D-1") || ph.State != DecisionStale || !ph.Stale() {
		t.Errorf("phases D-1 state = %s, replaced by %v", ph.State, ph.ReplacedBy)
	}
	if len(ph.CitedOpen) != 2 || ph.CitedOpen[0].Slug != "sim/creeps" || ph.CitedOpen[1].Slug != "sim/events" || len(ph.CitedClosed) != 1 {
		t.Errorf("phases D-1 cited open=%v closed=%v", slugsOf(ph.CitedOpen), slugsOf(ph.CitedClosed))
	}

	fm := x.Lookup("sim/format#D-2")
	if fm.State != DecisionReplaced || len(fm.CitedOpen) != 0 || len(fm.CitedClosed) != 1 {
		t.Errorf("format D-2 state=%s open=%v closed=%v (fenced and self citations must not count)", fm.State, slugsOf(fm.CitedOpen), slugsOf(fm.CitedClosed))
	}
	if p := x.Lookup("sim/pivot#D-1"); p.State != DecisionActive || len(p.CitedOpen) != 1 {
		t.Errorf("pivot state=%s open=%v (replaces refs are not citations)", p.State, slugsOf(p.CitedOpen))
	}
	if d := x.Lookup("sim/phases#D-2"); d.State != DecisionDeferred {
		t.Errorf("deferred state = %s", d.State)
	}

	cited := x.Cited("sim/creeps")
	if len(cited) != 2 || cited[0].Ref != "sim/phases#D-1" || cited[1].Ref != "sim/pivot#D-1" {
		t.Errorf("Cited(creeps) = %v", cited)
	}
	if x.Lookup("unknown/x#D-1") != nil {
		t.Error("unknown ref resolved")
	}
}

func TestBuildDecisionIndex_NewestReplacerWins(t *testing.T) {
	a := decisionIssue("a", "idea", "## Decisions\n- **D-1** · 2026-09-01 · chat: Old.")
	b := decisionIssue("b", "idea", "## Decisions\n- **D-1** · 2026-09-02 · chat: Newer. (replaces a#D-1)")
	c := decisionIssue("c", "idea", "## Decisions\n- **D-1** · 2026-09-03 · chat: Newest. (replaces a#D-1, b#D-1)")
	x := BuildDecisionIndex([]*Issue{a, b, c}, nil)
	if got := x.Lookup("a#D-1").ReplacedBy.Ref; got != "c#D-1" {
		t.Errorf("a#D-1 replaced by %s, want c#D-1", got)
	}
	if got := x.Lookup("b#D-1").ReplacedBy.Ref; got != "c#D-1" {
		t.Errorf("b#D-1 replaced by %s", got)
	}
}

func slugsOf(issues []*Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Slug)
	}
	return out
}

func TestClosedIssueFunc(t *testing.T) {
	proj := &Project{}
	closed := ClosedIssueFunc(proj)
	if !closed(&Issue{Status: "done"}) || closed(&Issue{Status: "backlog"}) {
		t.Error("default workflow: done should be closed, backlog open")
	}
}

func writeDecisionIssue(t *testing.T, body string) string {
	t.Helper()
	fp := filepath.Join(t.TempDir(), "gold.md")
	content := "---\ntitle: \"Gold shop\"\nstatus: \"in design\"\n---\n\n" + body + "\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}
	return fp
}

func readBody(t *testing.T, fp string) string {
	t.Helper()
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	issue, err := ParseIssue(filepath.Base(fp), data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return issue.BodyRaw
}
