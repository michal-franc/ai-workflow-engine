package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var tl0 = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return tl0.Add(time.Duration(min) * time.Minute) }

func ptr(t time.Time) *time.Time { return &t }

func call(min int, args ...string) cliCall {
	return cliCall{At: at(min), Args: args, Agent: true}
}

func TestClassifyGaps(t *testing.T) {
	bands := []lifeBand{
		{Status: "idea", From: at(0), To: at(100)},
		{Status: "in design", From: at(100), To: at(300), Gated: true},
	}
	segs := classifyGaps(
		[]time.Time{at(5), at(30), at(40), at(110), at(150), at(160), at(200)},
		[]time.Time{at(190)},
		bands, at(0), at(210))
	want := []seg{
		{at(0), at(5), segAgent},
		{at(5), at(30), segIdle},       // 25m, not gated
		{at(30), at(40), segAgent},     // 10m < 15m
		{at(40), at(110), segIdle},     // starts in idea: not gated
		{at(110), at(150), segWaiting}, // 40m in a gated status
		{at(150), at(160), segAgent},
		{at(160), at(190), segWaiting}, // holds your approval
		{at(190), at(200), segAgent},
		{at(200), at(210), segAgent},
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if !segs[i].From.Equal(want[i].From) || !segs[i].To.Equal(want[i].To) || segs[i].Kind != want[i].Kind {
			t.Errorf("segment %d = %v→%v %s, want %v→%v %s", i, segs[i].From, segs[i].To, segs[i].Kind, want[i].From, want[i].To, want[i].Kind)
		}
	}
}

func TestReworkLoops(t *testing.T) {
	wf := tracker.DefaultWorkflow()
	src := timelineSources{
		Transitions: []statRow{
			{From: "in progress", To: "testing", TS: at(60)},
			{From: "testing", To: "in progress", TS: at(70)}, // backward
			{From: "in progress", To: "testing", TS: at(90)},
		},
		Runs: []playtestRun{
			{Mode: "auto", Started: at(20), Steps: []playtestStep{{"TP1", "fail"}}}, // before testing: not rework
			{Mode: "judge", Started: at(100), Steps: []playtestStep{{"TP1", "pass"}, {"TP2", "fail"}}},
			{Mode: "auto", Started: at(105), Steps: []playtestStep{{"TP2", "pass"}}}, // another mode
			{Mode: "judge", Started: at(120), Steps: []playtestStep{{"TP2", "pass"}}},
		},
	}
	loops := reworkLoops(src, wf, at(200))
	if len(loops) != 2 {
		t.Fatalf("loops = %+v, want 2", loops)
	}
	if !loops[0].From.Equal(at(70)) || !loops[0].To.Equal(at(90)) || !strings.Contains(loops[0].Label, "back to in progress") {
		t.Errorf("backward loop = %+v", loops[0])
	}
	if !loops[1].From.Equal(at(100)) || !loops[1].To.Equal(at(120)) || loops[1].Label != "your playtest failed TP2" {
		t.Errorf("playtest loop = %+v", loops[1])
	}

	segs := markRework([]seg{{at(60), at(200), segAgent}}, loops)
	if got := kindWithin(segs, segRework, at(0), at(200)); got != 40*time.Minute {
		t.Errorf("rework = %v, want 40m", got)
	}
}

// AC3: the local CI issue from its real stats sidecar, per-agent clilog and
// playtest runs: 5h 0m in all, 3h 48m waiting on you.
func TestBuildTimelineLocalCI(t *testing.T) {
	dir := filepath.Join("testdata", "timeline", "local-ci")
	stats, err := readStatFile(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	src := timelineSources{
		Transitions: stats.Transitions,
		Calls:       loadCliCalls([]string{filepath.Join(dir, "agent.clilog")}, "issue-viewer", "workflow/local-ci-validate-integrated-main-after-every-merge-and-report-failures"),
		Runs:        loadPlaytestRuns("workflow/local-ci-validate-integrated-main-after-every-merge-and-report-failures", dir),
		Commits: []gitCommit{
			{SHA: "11826db", At: time.Date(2026, 10, 22, 15, 13, 38, 0, time.UTC), Subject: "ci: local CI runner"},
			{SHA: "af4e2da", At: time.Date(2026, 10, 22, 18, 31, 39, 0, time.UTC), Subject: "ci: Last runs boxes"},
			{SHA: "e545be0", At: time.Date(2026, 10, 22, 18, 50, 54, 0, time.UTC), Subject: "Merge branch 'main'", Merge: true},
		},
	}
	src.Decisions = tracker.ParseDecisions("workflow/local-ci", "## Decisions\n\n"+
		"- **D-7** · 2026-10-22 · agent chat: One issue, built in phased commits in the lead's build order (run/status/reset, watcher + detached runs, reporting, enable here). Enabling CI in RogueTech stays with the RogueTech lead, \"One issue, phased commits\"\n")
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

	var waits, decide string
	for _, b := range tl.Bands {
		for _, e := range b.Events {
			if e.Row == "wait" {
				waits += b.Status + ": " + e.Text + " · " + e.Note + "\n"
			}
			if e.Chip == "decide" && b.Status == "idea" {
				decide = e.Text
			}
		}
	}
	if !strings.Contains(waits, "in design: Waiting on you · 45m 48s · approval to leave in design") ||
		!strings.Contains(waits, "human-testing: Waiting on you · 2h 53m") ||
		!strings.Contains(waits, "approval to leave human-testing") {
		t.Errorf("wait rows:\n%s", waits)
	}
	if !strings.Contains(decide, `4 decisions: D-7 "One issue, phased commits"`) {
		t.Errorf("decide row = %q; decisions are matched by their text", decide)
	}
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
	sum := sha256.Sum256([]byte(text))
	red := "‹text n=24 h=" + hex.EncodeToString(sum[:])[:6] + "›"
	src := timelineSources{Comments: []tracker.Comment{{Text: text}}}
	if got := resolveText("comment x "+red, src); got != "comment x "+text {
		t.Errorf("resolveText = %q", got)
	}
	if got := resolveText("‹text n=3 h=000000›", src); got != "[text not kept]" {
		t.Errorf("unknown hash = %q", got)
	}
}

func TestLoadCommitsFirstParent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "base")
	run("checkout", "-q", "-b", "work/ui/x")
	run("commit", "-q", "--allow-empty", "-m", "mine 1")
	run("checkout", "-q", "main")
	run("commit", "-q", "--allow-empty", "-m", "other issue")
	run("checkout", "-q", "work/ui/x")
	run("merge", "-q", "--no-edit", "main")
	run("commit", "-q", "--allow-empty", "-m", "mine 2")

	ref, name := timelineGitRef(dir, "ui/x", nil)
	if ref == "" || name != "work/ui/x" {
		t.Fatalf("ref = %q %q", ref, name)
	}
	commits, err := loadCommits(dir, ref, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range commits {
		s := c.Subject
		if c.Merge {
			s = "merge"
		}
		subjects = append(subjects, s)
	}
	if got := strings.Join(subjects, ","); got != "base,mine 1,merge,mine 2" {
		t.Errorf("commits = %s; the merge is one line and the other issue's commit never shows", got)
	}
	if ref, _ := timelineGitRef(dir, "ui/gone", nil); ref != "" {
		t.Errorf("a missing branch with no recorded head = %q", ref)
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
	lines := `{"args":["--project","test-project","show","bug-in-login"],"ts":"2026-10-22T10:00:00Z"}
{"args":["--project","test-project","transition","bug-in-login","--to","in progress"],"ts":"2026-10-22T10:30:00Z","exit":0}
{"args":["--project","test-project","transition","bug-in-login","--to","testing"],"ts":"2026-10-22T10:31:00Z","exit":1}
`
	if err := os.WriteFile(filepath.Join(logDir, "alice.clilog"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	_, page := getBody(t, ts.URL+"/p/test-project/issue/bug-in-login?tab=timeline&show=cli")
	for _, want := range []string{`data-testid="timeline.cli.call"`, "Workflow actions the bot saw", "refused", "exit 1", `data-testid="timeline.wait"`} {
		if !strings.Contains(page, want) {
			t.Errorf("CLI calls: missing %q", want)
		}
	}
	if strings.Contains(page, `CLI log:</span>`) {
		t.Error("the clilog was found, yet the tab says it could not read it")
	}
}
