package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// workboardFixture is the workbench fixture plus issues for every marker and
// footer state.
func workboardFixture(t *testing.T, now time.Time) workbenchFixture {
	t.Helper()
	f := setupWorkbenchProject(t, now)
	dir := f.proj.IssueDir
	f.paths["epic"] = writeWorkbenchIssue(t, dir, "epic", "in progress", "type: \"epic\"\n", "The epic.")
	f.paths["child-done"] = writeWorkbenchIssue(t, dir, "child-done", "done", "parent: \"epic\"\n", "Shipped.")
	f.paths["waits"] = writeWorkbenchIssue(t, dir, "waits", "in progress", "parent: \"epic\"\nblocked_on: \"ready\"\nassignee: \"agent-waits\"\n", "- [ ] build")
	f.paths["freed"] = writeWorkbenchIssue(t, dir, "freed", "backlog", "blocked_on:\n  - child-done\n  - nope\n", "Free now.")
	f.paths["revise"] = writeWorkbenchIssue(t, dir, "revise", "backlog", "labels:\n  - needs-revision\n", "Pre-pivot design.")
	f.paths["revise-why"] = writeWorkbenchIssue(t, dir, "revise-why", "backlog", "needs_revision: \"D-5\"\n", "Pre-pivot design.")
	f.paths["other"] = writeWorkbenchIssue(t, dir, "other", "backlog", "version: \"0.2\"\n", "Other version.")
	f.paths["someday"] = writeWorkbenchIssue(t, dir, "someday", "idea", "", "An idea.")
	f.paths["someday-asks"] = writeWorkbenchIssue(t, dir, "someday-asks", "idea", "", "An idea with a question.")
	if err := tracker.AddComment(f.paths["someday-asks"], 0, "question: worth it?", "agent"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.AddComment(f.paths["asked"], 0, "question: carry gold over?", "agent"); err != nil {
		t.Fatal(err)
	}
	// "waits" entered in progress two days ago: its age turns amber.
	if err := tracker.AppendTransitionStat(f.paths["waits"], tracker.TransitionStat{From: "backlog", To: "in progress", TS: now.Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return f
}

// writeClilog records one issue-cli call by session at ts.
func writeClilog(t *testing.T, proj *tracker.Project, session string, ts time.Time, args string) {
	t.Helper()
	dir := filepath.Join(resolveProjectWorkDir(proj), ".agent-logs", session)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	line := `{"args":[` + args + `],"ts":"` + ts.UTC().Format(time.RFC3339) + `"}` + "\n"
	f, err := os.OpenFile(filepath.Join(dir, session+".clilog"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(line)
}

// cardHTML cuts one card's markup out of the page.
func cardHTML(t *testing.T, html, slug string) string {
	t.Helper()
	start := strings.Index(html, `data-testid="workboard.card.`+slug+`"`)
	if start < 0 {
		t.Fatalf("no card %s", slug)
	}
	end := strings.Index(html[start:], "</article>")
	return html[start : start+end]
}

func TestWorkboard_MarkersAndFooters(t *testing.T) {
	now := time.Now()
	f := workboardFixture(t, now)
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-ready"}, {Name: "agent-asked"}, {Name: "agent-waits"}, {Name: "agent-unready"}})
	writeClilog(t, &f.proj, "agent-waits", now.Add(-40*time.Minute), `"--project","wb","check","waits","Im1"`)
	// A test run in the agent's shell logs another project later; it is not the agent's call.
	writeClilog(t, &f.proj, "agent-waits", now.Add(-time.Minute), `"--project","test-project","list"`)
	writeClilog(t, &f.proj, "agent-unready", now.Add(-2*time.Minute), `"--project","wb","show","unready"`)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	code, html := getBody(t, ts.URL+"/p/wb/workboard")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Error("html/template rejected a value (ZgotmplZ)")
	}

	cases := []struct {
		slug string
		want []string
		not  []string
	}{
		{"ready", []string{`data-testid="marker.gate"`, "approve backlog · 3 h", `data-state="gate"`, "agent waiting on your approval"}, nil},
		// An unready gate is no marker, and the agent reads as working.
		{"unready", []string{`data-state="working"`, "agent working · last call 2 m ago"}, []string{"marker.gate"}},
		{"asked", []string{`data-testid="marker.question"`, `data-state="question"`, "agent asked you a question"}, nil},
		{"waits", []string{`data-testid="marker.blocked-on"`, "blocked on ready", `href="/p/wb/issue/ready"`,
			`card-age old`, "1 of 2 · in", `data-state="idle"`, "agent idle · last call 40 m ago"}, nil},
		{"freed", []string{`data-testid="marker.unblocked"`, "marker-struck", "child-done done", "blocked on nope (not found)"}, nil},
		{"revise", []string{`data-testid="marker.needs-revision"`, ">needs revision<"}, nil},
		{"revise-why", []string{"needs revision · D-5"}, nil},
		{"epic", []string{"approve done", "chip-blue", "1 of 2 children done", `width: 50%`, `data-state="stopped"`, "agent stopped · not running"}, nil},
	}
	for _, c := range cases {
		card := cardHTML(t, html, c.slug)
		for _, w := range c.want {
			if !strings.Contains(card, w) {
				t.Errorf("%s: want %q in\n%s", c.slug, w, card)
			}
		}
		for _, n := range c.not {
			if strings.Contains(card, n) {
				t.Errorf("%s: unexpected %q", c.slug, n)
			}
		}
	}
	// ready and epic (no boxes left) wait on an approval, asked and
	// someday-asks on an answer.
	if m := regexp.MustCompile(`Waiting on you · (\d+)`).FindStringSubmatch(html); m == nil || m[1] != "4" {
		t.Errorf("waiting count = %v, want 4", m)
	}
	// Quiet cards (idle, blocked, other, approved: no marker, agent or amber age) are
	// hidden until Show all.
	if strings.Contains(html, `data-testid="workboard.card.idle"`) || !strings.Contains(html, "Show all · 4 quiet") {
		t.Error("quiet cards hidden by default, counted on the Show all chip")
	}
	// backlog's quiet cards: idle, blocked, other.
	if !regexp.MustCompile(`data-testid="workboard.column.backlog.quiet"[^>]*>\+3 quiet<`).MatchString(html) {
		t.Error("backlog column links its 3 hidden quiet cards")
	}
	// The idea pile folds to a tile linking the list, with its waiting count.
	idea := html[strings.Index(html, `data-testid="workboard.column.idea"`):]
	idea = idea[:strings.Index(idea, "</section>")]
	if !strings.Contains(idea, `href="/p/wb/?status=idea"`) || !strings.Contains(idea, ">2<") || !strings.Contains(idea, "1 waiting on you") || strings.Contains(idea, "workboard.card.") {
		t.Errorf("idea column should be a folded tile:\n%s", idea)
	}
	// Done folds to a count tile too.
	if !strings.Contains(html, `data-testid="workboard.column.done.more"`) {
		t.Error("done column folded")
	}

	// Show all draws the quiet cards as one line.
	_, all := getBody(t, ts.URL+"/p/wb/workboard?all=1")
	if !strings.Contains(all, `class="workboard-card workboard-card-compact" data-testid="workboard.card.idle"`) || strings.Contains(cardHTML(t, all, "idle"), "marker.") {
		t.Error("quiet card under Show all should be one compact line")
	}
	if !strings.Contains(all, "Showing all") {
		t.Error("Show all chip shows its active state")
	}
	// The sidebar shows the legend and Board (workboard) is active.
	for _, w := range []string{`data-testid="nav.panel.legend"`, `data-testid="nav.tab.workboard" aria-current="page"`} {
		if !strings.Contains(html, w) {
			t.Errorf("missing %s", w)
		}
	}
}

func TestWorkboard_SortsWaitingThenBlockedFirst(t *testing.T) {
	f := workboardFixture(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	_, html := getBody(t, ts.URL+"/p/wb/workboard?all=1")
	col := html[strings.Index(html, `data-testid="workboard.column.backlog"`):]
	col = col[:strings.Index(col, "</section>")]
	var order []string
	for _, m := range regexp.MustCompile(`data-testid="workboard\.card\.([a-z-]+)"`).FindAllStringSubmatch(col, -1) {
		order = append(order, m[1])
	}
	got := strings.Join(order, ",")
	// freed, revise and revise-why carry red markers; idle, blocked and other don't.
	if !regexp.MustCompile(`^(freed|revise|revise-why)(,(freed|revise|revise-why)){2},`).MatchString(got) {
		t.Errorf("backlog order = %s, want the blocked cards first", got)
	}
}

func TestWorkboard_GroupsAndFilters(t *testing.T) {
	f := workboardFixture(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	_, html := getBody(t, ts.URL+"/p/wb/workboard?group=epic")
	if !strings.Contains(html, `data-testid="workboard.column.epic"`) || !strings.Contains(html, `data-testid="workboard.column.none"`) {
		t.Fatal("group=epic: want an epic column and No epic")
	}
	epicCol := html[strings.Index(html, `data-testid="workboard.column.epic"`):]
	epicCol = epicCol[:strings.Index(epicCol, "</section>")]
	if !strings.Contains(epicCol, "workboard.card.waits") || strings.Contains(epicCol, "workboard.card.child-done") || strings.Contains(epicCol, "workboard.card.epic\"") {
		t.Errorf("epic column holds its open children only")
	}
	if !strings.Contains(epicCol, "1 of 2 children done") {
		t.Error("epic column header shows the epic's progress")
	}

	_, html = getBody(t, ts.URL+"/p/wb/workboard?group=system")
	if !strings.Contains(html, `data-testid="workboard.column.economy"`) || strings.Contains(html, `data-testid="workboard.column.done"`) {
		t.Error("group=system: one column per system, done left out")
	}

	_, html = getBody(t, ts.URL+"/p/wb/workboard?waiting=1")
	for _, slug := range []string{"ready", "asked"} {
		if !strings.Contains(html, "workboard.card."+slug+`"`) {
			t.Errorf("waiting=1 keeps %s", slug)
		}
	}
	if strings.Contains(html, "workboard.card.idle\"") || strings.Contains(html, "workboard.card.waits\"") {
		t.Error("waiting=1 drops cards that don't wait on the owner")
	}

	_, html = getBody(t, ts.URL+"/p/wb/workboard?version=0.2&all=1")
	if !strings.Contains(html, "workboard.card.other\"") || strings.Contains(html, "workboard.card.idle\"") {
		t.Error("version filter keeps only that version")
	}
	if !strings.Contains(html, `data-testid="workboard.filter.version"`) {
		t.Error("the version select shows when an issue has a version")
	}
}

func TestWorkboard_ColumnCapAndNoVersionSelect(t *testing.T) {
	proj, _ := setupTestProject(t)
	for i := 0; i < workboardColumnCap+3; i++ {
		name := "extra-" + string(rune('a'+i))
		os.WriteFile(filepath.Join(proj.IssueDir, name+".md"), []byte("---\ntitle: \""+name+"\"\nstatus: \"backlog\"\n---\n"), 0644)
	}
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	_, html := getBody(t, ts.URL+"/p/test-project/workboard?all=1")
	col := html[strings.Index(html, `data-testid="workboard.column.backlog"`):]
	col = col[:strings.Index(col, "</section>")]
	if n := strings.Count(col, "data-search="); n != workboardColumnCap {
		t.Errorf("backlog shows %d cards, want the first %d", n, workboardColumnCap)
	}
	if !strings.Contains(col, `href="/p/test-project/?status=backlog"`) || !strings.Contains(col, "4 more in the list") {
		t.Errorf("past the cap the column links to the list:\n%s", col)
	}
}

func TestAgentsJSON_IdleAndLastCall(t *testing.T) {
	now := time.Now()
	f := workboardFixture(t, now)
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-waits"}, {Name: "agent-ready"}})
	writeClilog(t, &f.proj, "agent-waits", now.Add(-time.Hour), `"--project","wb","check","waits","Im1"`)
	srv, err := NewServer([]tracker.Project{f.proj})
	if err != nil {
		t.Fatal(err)
	}
	model, err := srv.buildAgentsModel(&f.proj, "/p/wb")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]AgentNode{}
	for _, w := range model.Workers {
		states[w.Name] = w
	}
	// waits' next transition needs an approval, so the canvas says at gate
	// (idle is only for agents with no gate); the last call is still shown.
	if w := states["agent-waits"]; w.State != "at-gate" || w.LastCmd != "check waits Im1" || w.LastCall == "" {
		t.Errorf("agent-waits = %s %q %q, want at-gate with its last call", w.State, w.LastCall, w.LastCmd)
	}
	// The canvas keeps "at gate" for any pending approval, with no clilog.
	if w := states["agent-ready"]; w.State != "at-gate" || w.Gate != "backlog" || w.LastCall != "" {
		t.Errorf("agent-ready = %s %s %q, want at-gate backlog", w.State, w.Gate, w.LastCall)
	}
}

func TestWorkerState(t *testing.T) {
	wf := &tracker.WorkflowConfig{}
	issue := &tracker.Issue{Status: "in progress"}
	now := time.Now()
	q := []tracker.Comment{{Text: "question: which?"}}
	for _, c := range []struct {
		comments []tracker.Comment
		call     lastCall
		known    bool
		want     string
	}{
		{q, lastCall{At: now.Add(-time.Hour)}, true, "question"},
		{nil, lastCall{At: now.Add(-agentIdleAfter)}, true, "idle"},
		{nil, lastCall{At: now.Add(-time.Minute)}, true, "working"},
		{nil, lastCall{}, false, "working"},
	} {
		if got, _ := workerState(wf, issue, c.comments, c.call, c.known, now); got != c.want {
			t.Errorf("workerState = %s, want %s", got, c.want)
		}
	}
}

func TestLastCliCall_ReadsTheNewestRecordOfTheProject(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.clilog")
	long := strings.Repeat("x", cliTailBytes) // the first record is cut off by the tail read
	os.WriteFile(p, []byte(`{"args":["--project","wb","append","s","--body","`+long+`"],"ts":"2026-10-21T10:00:00Z"}`+"\n"+
		`{"args":["--project","wb","check","s","D1"],"ts":"2026-10-21T11:00:00Z"}`+"\n"+
		`{"args":["--project","other","list"],"ts":"2026-10-21T12:00:00Z"}`+"\n"+
		"not json\n"), 0644)
	c, ok := lastCliCall("wb", filepath.Join(dir, "missing.clilog"), p)
	if !ok || c.Cmd != "check s D1" || c.At.Hour() != 11 {
		t.Errorf("lastCliCall = %+v %v", c, ok)
	}
	if _, ok := lastCliCall("wb", filepath.Join(dir, "missing.clilog")); ok {
		t.Error("no readable log: not known")
	}
}

func TestAgoText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "now", 5 * time.Minute: "5 m", 3 * time.Hour: "3 h", 47 * time.Hour: "47 h", 72 * time.Hour: "3 d",
	} {
		if got := agoText(d); got != want {
			t.Errorf("agoText(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestCardMarkers_Parked(t *testing.T) {
	issue := &tracker.Issue{Slug: "sim/creeps", Status: "parked"}
	ctx := &markerCtx{now: time.Now(), bySlug: map[string]*tracker.Issue{}, attention: map[string]Attention{
		"sim/creeps": {Parked: true, ParkReason: "deferred until the lanes settle"},
	}}
	got := cardMarkers(ctx, issue)
	if len(got) != 1 || got[0].Kind != "parked" || got[0].Tone != "grey" || got[0].Label != "parked · deferred until the lanes settle" {
		t.Errorf("parked marker = %+v", got)
	}
}
