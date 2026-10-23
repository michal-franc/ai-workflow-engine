package main

import (
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func tabInput(issue *tracker.Issue) issueTabInput {
	return issueTabInput{
		Issue:  issue,
		WF:     tracker.DefaultWorkflow(),
		Now:    at(120),
		Prefix: "/p/x",
		Att:    Attention{Since: at(60)},
	}
}

func TestNeedsYouQuestionWinsOverGate(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "ui/a", Status: "in design"})
	in.Att.OpenQuestion = true
	in.Att.Gate = &GateInfo{Target: "backlog", Ready: true}
	in.Targets = []string{"backlog", "in progress"}
	in.Src.Comments = []tracker.Comment{{Text: "question: keep refused calls in the list?", Date: at(100).Format(time.RFC3339)}}

	n := buildIssueTab(in).Needs
	if n.State != "question" || !n.Amber || n.Label != "Needs you" {
		t.Fatalf("want an amber question, got %+v", n)
	}
	if n.Question != "keep refused calls in the list?" || !n.AskedAt.Equal(at(100)) {
		t.Errorf("question = %q at %v", n.Question, n.AskedAt)
	}
	if len(n.Also) != 1 || n.Also[0] != "approve → in progress or backlog" {
		t.Errorf("also = %v", n.Also)
	}
	if n.Wait != "1h" {
		t.Errorf("wait = %q, want the time in status (Status page's wording)", n.Wait)
	}
}

func TestNeedsYouGate(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "ui/a", Status: "in design"})
	in.Att.Gate = &GateInfo{Target: "backlog", Ready: true}
	in.Targets = []string{"backlog", "in progress"}
	in.Src.Decisions = []tracker.Decision{{ID: "D-1", Text: "x"}}

	n := buildIssueTab(in).Needs
	if n.State != "gate" {
		t.Fatalf("state = %q", n.State)
	}
	if strings.Join(n.Targets, ",") != "in progress,backlog" {
		t.Errorf("targets = %v, want the handoff status last", n.Targets)
	}
	if n.Title != "Approve the design: start it now, or queue it in backlog." {
		t.Errorf("title = %q", n.Title)
	}
	if n.Text != "Check the 1 decision below. The agent has stopped at the gate until you approve." {
		t.Errorf("text = %q", n.Text)
	}
	if n.Command != `issue-cli approve ui/a --to "in progress" --quote "…"` {
		t.Errorf("command = %q", n.Command)
	}
}

func TestNeedsYouGateNotReadyIsQuiet(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "ui/a", Status: "in design"})
	in.Att.Gate = &GateInfo{Target: "backlog"}
	in.Footer = &CardAgent{State: "working"}
	in.Call, in.Known = lastCall{At: at(117), Cmd: "check ui/a D3"}, true

	n := buildIssueTab(in).Needs
	if n.Amber || n.State != "working" || n.Label != "With the agent" {
		t.Fatalf("want a quiet working state, got %+v", n)
	}
	want := "Heading for the backlog gate; its checklist is still open. Last call 3 m ago · check ui/a D3."
	if n.Text != want {
		t.Errorf("text = %q, want %q", n.Text, want)
	}
}

func TestNeedsYouPlaytest(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "ui/a", Status: "human-testing"})
	in.Att.PlaytestDue, in.Att.Playtests = true, []string{"timeline-tab"}
	n := buildIssueTab(in).Needs
	if n.State != "playtest" || n.Title != "Run the playtest: 1 session." || n.Command != "make playtest ISSUE=ui/a" {
		t.Errorf("got %+v", n)
	}

	// You play it before you approve it, as on the board cards.
	in.Att.Gate = &GateInfo{Target: "documentation", Ready: true}
	in.Targets = []string{"documentation"}
	n = buildIssueTab(in).Needs
	if n.State != "playtest" || len(n.Also) != 1 || n.Also[0] != "approve → documentation" {
		t.Errorf("playtest should win over the gate: %+v", n)
	}
}

func TestNeedsYouQuietStates(t *testing.T) {
	cases := []struct {
		name   string
		status string
		att    func(*Attention)
		footer *CardAgent
		state  string
		text   string
	}{
		{"parked", "parked", func(a *Attention) { a.Parked, a.ParkReason = true, "Delivered in v0.53.0." }, nil, "parked", "Delivered in v0.53.0."},
		{"done", "done", nil, nil, "done", ""},
		{"idle", "in progress", nil, &CardAgent{State: "idle"}, "idle", "No issue-cli call since"},
		{"stopped", "in progress", nil, &CardAgent{State: "stopped"}, "stopped", "tmux session is not running"},
		{"queued", "backlog", nil, nil, "queued", "Queued in backlog."},
		{"nobody", "idea", nil, nil, "none", "No agent is working on it."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := tabInput(&tracker.Issue{Slug: "ui/a", Status: c.status})
			if c.att != nil {
				c.att(&in.Att)
			}
			in.Footer = c.footer
			n := buildIssueTab(in).Needs
			if n.Amber || n.State != c.state || !strings.Contains(n.Text, c.text) {
				t.Errorf("got %+v", n)
			}
			if c.state == "idle" && n.OpenTmux != "agent-ui-a" {
				t.Errorf("idle should offer Open in tmux, got %q", n.OpenTmux)
			}
		})
	}
}

func TestNeedsYouApprovedWaitsForTheAgent(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "ui/a", Status: "in design", HumanApproval: "in progress"})
	in.Footer = &CardAgent{State: "working"}
	n := buildIssueTab(in).Needs
	if n.State != "working" || !strings.HasPrefix(n.Text, "You approved → in progress; the agent continues.") {
		t.Errorf("got %+v", n)
	}
}

func TestApprovingSinceLastGate(t *testing.T) {
	exit1 := 1
	refused := call(95, "transition", "ui/a", "--to", "backlog", "--dry-run")
	refused.Exit = &exit1
	src := timelineSources{
		Transitions: []statRow{
			{From: "idea", To: "in design", TS: at(10)},
			{From: "in design", To: "in progress", TS: at(60), ApprovedAt: ptr(at(55))},
		},
		Calls: []cliCall{
			call(20, "append", "ui/a", "--section", "Design", "--body", "old"),
			call(70, "append", "ui/a", "--section", "Implementation", "--body", "x"),
			call(71, "replace", "ui/a", "--section", "Implementation", "--line", "1", "--text", "y"),
			call(72, "append", "ui/a", "--body", "z"),
			call(80, "check", "ui/a", "Im1", "Im2"),
			call(81, "check", "ui/a", "Im2", "TP1"),
			call(85, "decide", "ui/a", "--text", "keep it", "--quote", "go"),
			call(90, "comment", "ui/a", "progress: half way"),
			call(91, "comment", "ui/a", "tests: make validate green"),
			refused,
		},
		Commits: []gitCommit{{At: at(30)}, {At: at(75)}, {At: at(76), Merge: true}},
	}
	a := approvingSince(src, Timeline{})
	if !a.Since.Equal(at(60)) || a.From != "in design → in progress" {
		t.Fatalf("since %v from %q", a.Since, a.From)
	}
	got := map[string]approvingCount{}
	for _, c := range a.Counts {
		got[c.Key] = c
	}
	if c := got["sections"]; c.N != 2 || c.Detail != "Implementation, body" {
		t.Errorf("sections = %+v", c)
	}
	if c := got["boxes"]; c.N != 3 || c.Detail != "Im1 Im2 TP1" || c.Label != "boxes ticked" {
		t.Errorf("boxes = %+v", c)
	}
	if c := got["decisions"]; c.N != 1 {
		t.Errorf("decisions = %+v", c)
	}
	if c := got["comments"]; c.N != 2 || c.Detail != "progress, tests" {
		t.Errorf("comments = %+v", c)
	}
	if c := got["commits"]; c.N != 1 {
		t.Errorf("commits = %+v, want the one non-merge commit after the gate", c)
	}
	if len(a.Events) != 1 || a.Events[0].Chip != "status" {
		t.Errorf("events = %+v, want the transition (no bands given)", a.Events)
	}
}

func TestApprovingSkipsDryRunRefusals(t *testing.T) {
	tl := Timeline{Bands: []timelineBand{{Status: "in design", Events: []tlEvent{
		{At: at(5), Row: "event", Chip: "refused", Text: "transition → backlog refused: validation (dry run)"},
		{At: at(6), Row: "event", Chip: "refused", Text: "transition → backlog refused: validation"},
		{At: at(7), Row: "event", Chip: "section", Text: "Design written"},
	}}}}
	a := approvingSince(timelineSources{Dispatches: []time.Time{at(0)}}, tl)
	if a.From != "dispatch" || len(a.Events) != 2 {
		t.Errorf("from %q, events %+v", a.From, a.Events)
	}
}

func TestYourWords(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "ui/a"})
	in.Src.Decisions = []tracker.Decision{
		{ID: "D-1", How: "recorded by agent", Quote: `"Fold it"`},
		{ID: "D-2", How: "lead chat", Quote: `"Drop it (Recommended)"`},
	}
	if w := yourWords(in); w != nil {
		t.Fatalf("agent records and recommended picks are not your words: %+v", w)
	}
	in.Src.Comments = []tracker.Comment{{Text: tracker.ApprovalComment("go, keep the guard", "viewer", "2026-10-23", "in progress")}}
	if w := yourWords(in); w == nil || w.Quote != "go, keep the guard" || w.Who != "You, viewer, 2026-10-23" {
		t.Fatalf("approval words: %+v", w)
	}
	in.Src.Comments = nil
	in.Parent = &tracker.Issue{Slug: "ui/epic", BodyRaw: "## Decisions\n\n- **D-1** · 2026-10-23 · lead chat: epic first, \"lets do it\"\n"}
	w := yourWords(in)
	if w == nil || w.Quote != "lets do it" || w.Ref != "epic D-1" || w.Who != "You, lead chat, 23 Oct" || w.Href != "/p/x/issue/ui/epic" {
		t.Fatalf("epic words: %+v", w)
	}
	in.Src.Decisions = append(in.Src.Decisions, tracker.Decision{ID: "D-3", How: "lead chat", Quote: `"i want it now"`})
	if w := yourWords(in); w == nil || w.Ref != "D-3" {
		t.Errorf("the issue's own words win over the epic's: %+v", w)
	}
}

func TestIssuePath(t *testing.T) {
	tl := Timeline{Bands: []timelineBand{
		{Status: "idea", Start: at(0), End: at(30)},
		{Status: "in design", Start: at(30), End: at(50)},
		{Status: "in design", Start: at(70), End: at(80)}, // back again after rework
	}}
	steps := issuePath(tracker.DefaultWorkflow(), &tracker.Issue{Status: "in design"}, tl)
	byName := map[string]pathStep{}
	for _, s := range steps {
		byName[s.Status] = s
	}
	if s := byName["idea"]; s.State != "done" || s.Dur != 30*time.Minute || s.Gate {
		t.Errorf("idea = %+v", s)
	}
	if s := byName["in design"]; s.State != "current" || s.Dur != 30*time.Minute {
		t.Errorf("in design = %+v", s)
	}
	if s := byName["backlog"]; s.State != "next" || !s.Gate {
		t.Errorf("backlog = %+v", s)
	}
	if s := byName["human-testing"]; !s.Playtest || s.Gate {
		t.Errorf("human-testing = %+v", s)
	}
}

func TestFoldSections(t *testing.T) {
	body := "Lead para.\n\n## Idea\n- [x] a\n- [ ] b\n\nWhy it **matters**.\n\n### Edge cases\n\n### Not included\n\n## `Design` notes\nText.\n\n## Decisions\n- **D-1** · 2026-10-23 · lead chat: x\n"
	boxes := tracker.ListCheckboxes(body)
	f := foldSections(body, boxes, 1)
	if len(f) != 4 {
		t.Fatalf("want intro + 3 sections, got %+v", f)
	}
	if f[0].Title != "Intro" || f[0].ID != "intro" || f[0].Lead != "Lead para." {
		t.Errorf("intro = %+v", f[0])
	}
	if f[1].Done != 1 || f[1].Total != 2 || f[1].Lead != "Why it matters." || strings.Join(f[1].Subs, ",") != "Edge cases,Not included" {
		t.Errorf("idea = %+v", f[1])
	}
	if f[2].Key != "design notes" || f[2].ID != "design_notes" {
		t.Errorf("design = %+v, want the key the rendered <h2> reads", f[2])
	}
	if f[3].Lead != "1 decision, shown above" {
		t.Errorf("decisions = %+v", f[3])
	}
}

func TestNotStartedSections(t *testing.T) {
	issue := &tracker.Issue{Status: "in progress", BodyRaw: "## Idea\n\n## Implementation\n"}
	got := notStartedSections(tracker.DefaultWorkflow(), issue)
	if strings.Join(got, ",") != "Testing,Human Testing,Documentation,Shipping" {
		t.Errorf("got %v", got)
	}
}

func TestMockupThumbs(t *testing.T) {
	body := "## Mockups\n\nIntro.\n\n### Gate\n\n![a](../att/a.png)\n![a2](../att/a2.png)\n\n### Phone\n\n![b](../att/b.png)\n"
	got := mockupThumbs(body, func(p string) string { return "/files/" + strings.TrimPrefix(p, "../") })
	if len(got) != 2 || got[0].Title != "Gate" || got[0].Src != "/files/att/a.png" || got[1].Title != "Phone" {
		t.Errorf("got %+v", got)
	}
}

func TestBuildIssueTabDecisionsAndCriteria(t *testing.T) {
	body := "Brief here.\n\n## Acceptance Criteria\n- [x] one\n- [ ] two\n"
	in := tabInput(&tracker.Issue{Slug: "ui/a", Status: "in progress", BodyRaw: body})
	in.Src.Boxes = tracker.ListCheckboxes(body)
	in.Src.Decisions = []tracker.Decision{{ID: "D-1", Text: "a", Quote: `"Fold it (Recommended)"`}, {ID: "D-2", Text: "b"}}
	tab := buildIssueTab(in)
	if tab.Brief != "Brief here." {
		t.Errorf("brief = %q", tab.Brief)
	}
	if tab.Recommended != 1 || !tab.Decisions[0].Recommended || tab.Decisions[0].Quote != "Fold it (Recommended)" {
		t.Errorf("decisions = %+v", tab.Decisions)
	}
	if len(tab.Criteria) != 2 || tab.CriteriaDone != 1 || tab.Criteria[0].ID != "AC1" {
		t.Errorf("criteria = %+v", tab.Criteria)
	}
	if tab.Agent != nil {
		t.Errorf("no assignee and no agent: no card, got %+v", tab.Agent)
	}
}
