package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var tl0 = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return tl0.Add(time.Duration(min) * time.Minute) }

func ptr(t time.Time) *time.Time { return &t }

func call(min int, args ...string) cliCall {
	return cliCall{At: at(min), Args: args, Agent: true}
}

// AC3: the local CI issue from its real stats sidecar, per-agent clilog and
// playtest runs: 5h 0m in all, 3h 48m waiting on you. The clilog is redacted
// as v0.54 writes it (clilog.Redact), except two legacy pre-v0.54 records.
func TestBuildTimelineLocalCI(t *testing.T) {
	dir := filepath.Join("testdata", "timeline", "local-ci")
	issuePath := filepath.Join(dir, "issue.md")
	stats, err := tracker.LoadStats(issuePath)
	if err != nil || len(stats.Transitions) == 0 {
		t.Fatalf("stats: %v", err)
	}
	comments, err := tracker.LoadComments(issuePath)
	if err != nil || len(comments) == 0 {
		t.Fatalf("comments: %v", err)
	}
	body, _ := os.ReadFile(issuePath)
	src := timelineSources{
		Comments:    comments,
		Decisions:   tracker.ParseDecisions("workflow/local-ci", string(body)),
		Transitions: stats.Transitions,
		Calls:       timeline.LoadCalls(nil, []string{filepath.Join(dir, "agent.clilog")}, "issue-viewer", "workflow/local-ci-validate-integrated-main-after-every-merge-and-report-failures"),
		Runs:        timeline.LoadPlaytestRuns(nil, "workflow/local-ci-validate-integrated-main-after-every-merge-and-report-failures", dir),
		Commits: []gitCommit{
			{SHA: "11826db", At: time.Date(2026, 10, 22, 15, 13, 38, 0, time.UTC), Subject: "ci: local CI runner"},
			{SHA: "af4e2da", At: time.Date(2026, 10, 22, 18, 31, 39, 0, time.UTC), Subject: "ci: Last runs boxes"},
			{SHA: "e545be0", At: time.Date(2026, 10, 22, 18, 50, 54, 0, time.UTC), Subject: "Merge branch 'main'", Merge: true},
		},
	}
	if len(src.Runs) != 5 {
		t.Fatalf("runs = %d, want 5", len(src.Runs))
	}
	tl := buildTimeline(src, timelineInput{Status: "done", Now: time.Now(), WF: tracker.DefaultWorkflow()})

	if got := fmtDur(tl.Total); got != "5h 0m" {
		t.Errorf("total = %s, want 5h 0m", got)
	}
	if got := fmtDur(tl.Waiting); got != "3h 48m" {
		t.Errorf("waiting = %s, want 3h 48m", got)
	}
	if tl.Waits != 3 {
		t.Errorf("waits = %d, want 3", tl.Waits)
	}
	if got := fmtDur(tl.Idle); got != "18m 55s" {
		t.Errorf("idle = %s, want 18m 55s (idea, before the first decide)", got)
	}
	if tl.Working+tl.Waiting+tl.Idle != tl.Total {
		t.Errorf("agent %v + waiting %v + idle %v != total %v", tl.Working, tl.Waiting, tl.Idle, tl.Total)
	}
	if len(tl.Loops) != 1 || tl.Loops[0].Label != "your playtest failed TP5" {
		t.Fatalf("loops = %+v, want the TP5 loop only (the 17:28 ux-test failure is before testing)", tl.Loops)
	}
	if got := fmtDur(tl.Rework); got != "11m 13s" {
		t.Errorf("rework = %s, want 11m 13s", got)
	}
	if tl.Commits != 2 || tl.Merges != 1 {
		t.Errorf("commits = %d, merges = %d", tl.Commits, tl.Merges)
	}

	var waits, decide, progress, refused string
	for _, b := range tl.Bands {
		for _, e := range b.Events {
			if e.Row == "wait" {
				waits += b.Status + ": " + e.Text + " · " + e.Note + "\n"
			}
			if e.Chip == "decide" && b.Status == "idea" {
				decide = e.Text
			}
			if e.Chip == "progress" && b.Status == "in design" {
				progress = e.Text
			}
			if e.Chip == "refused" {
				refused = e.Text
			}
		}
	}
	if !strings.HasPrefix(progress, "progress: design written") {
		t.Errorf("in design progress comment = %q; the redacted text is filled from the issue's comment", progress)
	}
	if refused != "transition → backlog refused: approval missing (dry run)" {
		t.Errorf("refused row = %q", refused)
	}
	if !strings.Contains(waits, "in design: Waiting on you · 45m 48s · approval to leave in design") ||
		!strings.Contains(waits, "human-testing: Waiting on you · 2h 53m") ||
		!strings.Contains(waits, "approval to leave human-testing") {
		t.Errorf("wait rows:\n%s", waits)
	}
	if !strings.Contains(decide, `4 decisions: D-7 "One issue, phased commits" · D-8 "Accept all three"`) {
		t.Errorf("decide row = %q; redacted decisions are matched by their placeholder", decide)
	}
	if n := legacyRecords(t, filepath.Join(dir, "agent.clilog")); n != 2 {
		t.Errorf("the fixture should keep 2 legacy records (no exit, no dur_ms), has %d", n)
	}
}

func legacyRecords(t *testing.T, path string) int {
	n := 0
	clilog.Read(path, func(r clilog.Record) {
		if r.Exit == nil && r.DurMS == nil {
			n++
		}
	})
	return n
}

func TestTimelineFilters(t *testing.T) {
	src := timelineSources{
		Transitions: []statRow{{From: "in design", To: "in progress", TS: at(30), ApprovedAt: ptr(at(29))}},
		Calls: []cliCall{
			call(0, "show", "x"),
			call(1, "comment", "x", "progress: designing"),
			call(2, "comment", "x", "--text", "tests: make validate green"),
			call(3, "checklist", "x"),
			call(4, "checklist", "x"),
			call(30, "transition", "x", "--to", "in progress"),
		},
		Commits: []gitCommit{{SHA: "abc1234def", At: at(40), Subject: "feat: thing"}},
	}
	count := func(show string) map[string]int {
		tl := buildTimeline(src, timelineInput{Status: "in progress", Now: at(50), Show: show, WF: tracker.DefaultWorkflow()})
		n := map[string]int{}
		for _, b := range tl.Bands {
			for _, e := range b.Events {
				n[e.Row+":"+e.Chip]++
			}
		}
		return n
	}
	all := count(showAll)
	if all["event:progress"] != 1 || all["event:tests"] != 1 || all["event:commit"] != 1 || all["event:you"] != 1 || all["event:show"] != 0 {
		t.Errorf("all = %v", all)
	}
	if c := count(showCommits); len(c) != 1 || c["event:commit"] != 1 {
		t.Errorf("commits = %v", c)
	}
	if tests := count(showTests); tests["event:tests"] != 1 || tests["event:progress"] != 0 {
		t.Errorf("tests = %v", tests)
	}
	if you := count(showYou); you["event:you"] != 1 || you["event:commit"] != 0 {
		t.Errorf("you = %v", you)
	}
	cli := count(showCLI)
	if cli["event:show"] != 1 || cli["fold:folded"] != 1 || cli["event:transition"] != 1 || cli["event:commit"] != 0 {
		t.Errorf("cli = %v; every call shows, two reads in a row fold", cli)
	}
}

func TestTimelineNoCLILog(t *testing.T) {
	src := timelineSources{
		Transitions: []statRow{
			{From: "idea", To: "in progress", TS: at(10)},
			{From: "in progress", To: "done", TS: at(70)},
		},
		Missing: []missingSource{{Name: "CLI log", Detail: "no calls"}},
	}
	tl := buildTimeline(src, timelineInput{Status: "done", Now: at(100), WF: tracker.DefaultWorkflow()})
	if tl.Known || tl.Total != time.Hour || len(tl.Missing) != 1 {
		t.Errorf("known=%v total=%v missing=%v", tl.Known, tl.Total, tl.Missing)
	}
	if tl.Waiting != 0 || tl.Idle != 0 || len(tl.Segments) != 0 {
		t.Errorf("without calls nothing is split: %+v", tl)
	}
}

func TestResolveRedactedText(t *testing.T) {
	text := "progress: design written"
	src := timelineSources{Comments: []tracker.Comment{{Text: text}}}
	if got := resolveText("comment x "+clilog.Placeholder(text), src); got != "comment x "+text {
		t.Errorf("resolveText = %q", got)
	}
	other := clilog.Placeholder("not in the issue")
	if got := resolveText(other, src); got != other {
		t.Errorf("unknown text = %q, want the placeholder kept", got)
	}
}

func TestDetailTabs(t *testing.T) {
	proj, tmp := setupTestProject(t)
	proj.WorkDir = tmp
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	base := ts.URL + "/p/test-project/issue/bug-in-login"

	_, issue := getBody(t, base)
	for _, want := range []string{`data-testid="detail.pagetab.issue" aria-current="true"`, `data-testid="detail.body"`, `data-testid="detail.sidebar"`} {
		if !strings.Contains(issue, want) {
			t.Errorf("issue tab: missing %s", want)
		}
	}
	if strings.Contains(issue, "Agent Timeline") || strings.Contains(issue, `data-testid="timeline.tab"`) {
		t.Error("issue tab still shows a timeline")
	}

	code, tab := getBody(t, base+"?tab=timeline")
	if code != 200 {
		t.Fatalf("timeline tab: %d", code)
	}
	for _, want := range []string{`data-testid="detail.pagetab.timeline" aria-current="true"`, `data-testid="timeline.tab"`, `data-testid="timeline.missing"`, "CLI log", `data-testid="timeline.filter.cli"`} {
		if !strings.Contains(tab, want) {
			t.Errorf("timeline tab: missing %s", want)
		}
	}
	if strings.Contains(tab, `data-testid="detail.sidebar"`) {
		t.Error("timeline tab shows the issue sidebar (D-3)")
	}

	_, ev := getBody(t, base+"?tab=evidence")
	if !strings.Contains(ev, `data-testid="evidence.coming"`) {
		t.Error("evidence tab: no coming panel")
	}
	_, odd := getBody(t, base+"?tab=nope")
	if !strings.Contains(odd, `data-testid="detail.pagetab.issue" aria-current="true"`) {
		t.Error("an unknown tab should open the Issue tab")
	}
}

func TestDetailTimelineWithLog(t *testing.T) {
	proj, tmp := setupTestProject(t)
	proj.WorkDir = tmp
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	stats := `{"transitions":[{"from":"in design","to":"in progress","ts":"2026-10-22T10:30:00Z","approved_at":"2026-10-22T10:29:00Z"}]}`
	if err := os.WriteFile(tracker.StatsSidecarPath(issuePath), []byte(stats), 0o644); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(tmp, ".agent-logs", "alice")
	os.MkdirAll(logDir, 0o755)
	comment := "progress: waiting on your approval"
	if err := tracker.AddComment(issuePath, 0, comment, "cli"); err != nil {
		t.Fatal(err)
	}
	lines := `{"args":["--project","test-project","show","bug-in-login"],"ts":"2026-10-22T10:00:00Z"}
{"args":["--project","test-project","comment","bug-in-login","--text","` + clilog.Placeholder(comment) + `"],"ts":"2026-10-22T10:05:00Z","pid":7,"ppid":6,"exit":0,"dur_ms":120}
{"args":["--project","test-project","transition","bug-in-login","--to","in progress"],"ts":"2026-10-22T10:30:00Z","pid":8,"ppid":6,"exit":0,"dur_ms":200}
{"args":["--project","test-project","transition","bug-in-login","--to","testing"],"ts":"2026-10-22T10:31:00Z","pid":9,"ppid":6,"exit":1,"err_class":"approval_missing","dur_ms":90}
`
	if err := os.WriteFile(filepath.Join(logDir, "alice.clilog"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	// The project log: the lead's call, with no --project flag.
	lead := `{"args":["set-meta","bug-in-login","--key","version"],"ts":"2026-10-22T10:20:00Z","pid":5,"ppid":4,"exit":0,"dur_ms":80}
`
	if err := os.WriteFile(clilog.ProjectPath(tmp), []byte(lead), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	_, page := getBody(t, ts.URL+"/p/test-project/issue/bug-in-login?tab=timeline&show=cli")
	if !strings.Contains(page, "set-meta bug-in-login --key version") {
		t.Error("CLI calls: the project log's call (no --project flag) is missing")
	}
	for _, want := range []string{`data-testid="timeline.cli.call"`, "Workflow actions the bot saw", "refused", "(failed: approval missing)", comment, `data-testid="timeline.wait"`} {
		if !strings.Contains(page, want) {
			t.Errorf("CLI calls: missing %q", want)
		}
	}
	if strings.Contains(page, "‹text") {
		t.Error("a redacted comment shows its placeholder instead of the issue's comment text")
	}
	_, all := getBody(t, ts.URL+"/p/test-project/issue/bug-in-login?tab=timeline")
	if !strings.Contains(all, comment) {
		t.Error("All filter: the redacted comment is not filled from the issue")
	}
	// D-4 of the workflow timeline issue: each band shows its agent time,
	// here up to the comment and from your approval to the transition.
	if !strings.Contains(all, `data-testid="timeline.band.working">6m agent</span>`) {
		t.Error("the in design band does not show its agent time")
	}
	if strings.Contains(page, `CLI log:</span>`) {
		t.Error("the clilog was found, yet the tab says it could not read it")
	}
}

// AC4: the Timeline tab and the project aggregate are one builder, so for a
// done issue the tab's time per status is the aggregate's.
func TestTimelineMatchesAggregate(t *testing.T) {
	proj := fixItProject(t)
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	wf := tracker.DefaultWorkflow()
	rep := timeline.Aggregate(context.Background(), &proj, wf, issues, time.Now(), timeline.Window)
	var agg timeline.IssueStat
	var issue *tracker.Issue
	for _, s := range rep.Issues {
		if s.Slug == "fix-it" {
			agg = s
		}
	}
	for _, is := range issues {
		if is.Slug == "fix-it" {
			issue = is
		}
	}
	if issue == nil || agg.Slug == "" {
		t.Fatalf("fix-it not found: aggregate %+v", rep.Issues)
	}
	tl := buildTimeline(loadTimelineSources(&proj, issue, wf), timelineInput{Status: "done", Now: time.Now(), WF: wf})
	if tl.Total != agg.Cycle || tl.Working != agg.Working || tl.Rework != agg.Rework {
		t.Errorf("tab total %v working %v rework %v; aggregate cycle %v working %v rework %v", tl.Total, tl.Working, tl.Rework, agg.Cycle, agg.Working, agg.Rework)
	}
	n := 0
	for _, b := range tl.Bands {
		st, ok := agg.Statuses[b.Status]
		if !ok {
			continue
		}
		n++
		if b.Working != st.Working || b.Waiting != st.Waiting || b.Idle != st.Idle || b.Rework != st.Rework {
			t.Errorf("%s: tab %v/%v/%v/%v, aggregate %+v", b.Status, b.Working, b.Waiting, b.Idle, b.Rework, st)
		}
	}
	if n != 2 || agg.Statuses["human-testing"].Working == 0 {
		t.Errorf("compared %d bands; human-testing = %+v", n, agg.Statuses["human-testing"])
	}
}

func TestApprovalWords_PairsCommentsWithApprovals(t *testing.T) {
	rfc := func(min int) string { return at(min).Format(time.RFC3339) }
	src := timelineSources{
		Transitions: []statRow{
			{From: "idea", To: "in design", TS: at(10)},
			{From: "in design", To: "backlog", TS: at(30), ApprovedAt: ptr(at(25))},
			{From: "backlog", To: "in progress", TS: at(60), ApprovedAt: ptr(at(55))},
			{From: "in progress", To: "testing", TS: at(90), ApprovedAt: ptr(at(85))},
		},
		Comments: []tracker.Comment{
			// Before the window of the backlog approval: ignored.
			{Date: rfc(5), Text: tracker.ApprovalComment("too early", "viewer", "2026-10-22", "backlog")},
			{Date: rfc(20), Text: tracker.ApprovalComment("first try", "viewer", "2026-10-22", "backlog")},
			{Date: rfc(24), Text: tracker.ApprovalComment("design reads right", "viewer", "2026-10-22", "backlog")},
			// Names another status: never pairs with in progress.
			{Date: rfc(50), Text: tracker.ApprovalComment("wrong gate", "viewer", "2026-10-22", "documentation")},
			// Older form, no → status: pairs by time.
			{Date: rfc(70), Text: `approval: "ship the tests" (in the lead chat, 2026-10-22) — recorded by lead-x`},
			{Date: rfc(71), Text: "question: anything else?"},
		},
	}
	got := approvalWords(src)
	if len(got) != 2 || got[1].Words != "design reads right" || got[3].Words != "ship the tests" || got[3].How != "in the lead chat" {
		t.Fatalf("approvalWords = %+v", got)
	}
	if _, ok := got[2]; ok {
		t.Fatalf("in progress paired with %+v, want no words", got[2])
	}

	items, words, approvals := wordItems(src)
	if words != 2 || approvals != 3 || len(items) != 3 {
		t.Fatalf("wordItems = %+v, %d, %d", items, words, approvals)
	}
	if items[0].Quote != "design reads right" || items[0].How != "approve → backlog · viewer" || items[0].Muted {
		t.Errorf("backlog item = %+v", items[0])
	}
	if items[1].Quote != "no words recorded" || !items[1].Muted {
		t.Errorf("in progress item = %+v", items[1])
	}

	tl := buildTimeline(src, timelineInput{Status: "testing", Now: at(100), Show: showAll, WF: tracker.DefaultWorkflow()})
	quotes := map[string]string{}
	for _, b := range tl.Bands {
		for _, e := range b.Events {
			if e.Chip == "you" {
				quotes[e.Text] = e.Quote + "|" + e.Note
			}
		}
	}
	if quotes["Approved → backlog"] != "design reads right|" || quotes["Approved → in progress"] != "|no words recorded" {
		t.Fatalf("approval events = %v", quotes)
	}
}

func TestApprovalWords_DayOnlyCommentPairsWithinItsDay(t *testing.T) {
	src := timelineSources{
		Transitions: []statRow{
			{From: "idea", To: "in design", TS: at(-600)},
			{From: "in design", To: "backlog", TS: at(30), ApprovedAt: ptr(at(25))},
		},
		// Pre-0.54 comments carry only the day; it overlaps the window.
		Comments: []tracker.Comment{{Date: tl0.Format("2006-01-02"), Text: `approval: "ok" (viewer, 2026-10-22)`}},
	}
	if got := approvalWords(src); got[1].Words != "ok" {
		t.Fatalf("approvalWords = %+v", got)
	}
}
