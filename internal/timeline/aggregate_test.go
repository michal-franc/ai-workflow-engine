package timeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// fixture is ../../testdata/timeline/local-ci: the local CI issue's real
// stats sidecar, per-agent clilog and playtest runs.
var fixture = filepath.Join("..", "..", "testdata", "timeline", "local-ci")

// localCIDone is when the fixture issue reached done.
var localCIDone = time.Date(2026, 10, 22, 19, 9, 0, 0, time.UTC)

// testProject lays out a project: the local CI issue from the fixture, a
// bugfix written here with a refused and a dry-run transition, one done
// long ago, one done without a stats sidecar and one in flight.
func testProject(t *testing.T) (*tracker.Project, []*tracker.Issue) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "issues", "Workflow")
	mustCopy(t, filepath.Join(fixture, "issue.md"), filepath.Join(dir, "local-ci.md"))
	mustCopy(t, filepath.Join(fixture, "issue.stats.json"), filepath.Join(dir, "local-ci.stats.json"))
	runs, _ := filepath.Glob(filepath.Join(fixture, ".playtest", "runs", "*", "*", "results.yaml"))
	for _, r := range runs {
		rel, _ := filepath.Rel(fixture, r)
		mustCopy(t, r, filepath.Join(root, rel))
	}

	t0 := localCIDone.Add(-2 * time.Hour)
	writeIssue(t, dir, "fix-thing", "Fix thing", "bugfix", "done", &tracker.StatsStore{
		Dispatches: []tracker.Dispatch{{At: t0}, {At: t0.Add(30 * time.Minute)}},
		Transitions: []tracker.TransitionStat{
			{From: "in progress", To: "testing", TS: t0.Add(20 * time.Minute)},
			{From: "testing", To: "in progress", TS: t0.Add(25 * time.Minute)},
			{From: "in progress", To: "testing", TS: t0.Add(40 * time.Minute)},
			{From: "testing", To: "shipping", TS: t0.Add(90 * time.Minute), ApprovedAt: ptrTime(t0.Add(85 * time.Minute))},
			{From: "shipping", To: "done", TS: t0.Add(100 * time.Minute)},
		},
	})
	writeIssue(t, dir, "old-thing", "Old thing", "bugfix", "done", &tracker.StatsStore{Transitions: []tracker.TransitionStat{
		{From: "in progress", To: "done", TS: localCIDone.Add(-60 * 24 * time.Hour)},
	}})
	writeIssue(t, dir, "no-stats", "No stats", "tweak", "done", nil)
	writeIssue(t, dir, "open-thing", "Open thing", "tweak", "in progress", nil)

	proj := &tracker.Project{Slug: "issue-viewer", IssueDir: filepath.Join(root, "issues"), WorkDir: root}
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issues {
		switch is.Title {
		case "Fix thing":
			p := clilog.AgentPath(root, tracker.AgentSessionName(is.Slug))
			exit1, exit0 := 1, 0
			dur := int64(1000)
			for _, r := range []clilog.Record{
				{TS: t0.Add(1 * time.Minute).Format(time.RFC3339), Args: []string{"start", is.Slug}, Exit: &exit0, DurMS: &dur},
				{TS: t0.Add(10 * time.Minute).Format(time.RFC3339), Args: []string{"transition", is.Slug, "--to", "testing", "--dry-run"}, Exit: &exit1, ErrClass: "validation", DurMS: &dur},
				{TS: t0.Add(19 * time.Minute).Format(time.RFC3339), Args: []string{"transition", is.Slug, "--to", "testing"}, Exit: &exit0, DurMS: &dur},
				{TS: t0.Add(30 * time.Minute).Format(time.RFC3339), Args: []string{"show", is.Slug}, Exit: &exit0, DurMS: &dur},
				{TS: t0.Add(39 * time.Minute).Format(time.RFC3339), Args: []string{"transition", is.Slug, "--to", "testing"}, Exit: &exit0, DurMS: &dur},
				{TS: t0.Add(41 * time.Minute).Format(time.RFC3339), Args: []string{"transition", is.Slug, "--to", "shipping"}, Exit: &exit1, ErrClass: "approval_missing", DurMS: &dur},
				{TS: t0.Add(92 * time.Minute).Format(time.RFC3339), Args: []string{"transition", is.Slug, "--to", "done"}, Exit: &exit0, DurMS: &dur},
			} {
				if err := clilog.Append(p, r); err != nil {
					t.Fatal(err)
				}
			}
		case "Local CI: validate integrated main after every merge and report failures":
			mustCopy(t, filepath.Join(fixture, "agent.clilog"), clilog.AgentPath(root, tracker.AgentSessionName(is.Slug)))
		}
	}
	return proj, issues
}

// AC1, AC3: the local CI issue from its recorded clilog, split per status,
// and the project tables over the issues done in the window.
func TestAggregate(t *testing.T) {
	proj, issues := testProject(t)
	now := localCIDone.Add(24 * time.Hour)
	rep := Aggregate(context.Background(), proj, tracker.DefaultWorkflow(), issues, now, Window)

	if len(rep.Issues) != 2 {
		t.Fatalf("issues in the window = %d, want 2 (old-thing is outside, no-stats is unread, open-thing in flight): %+v", len(rep.Issues), rep.Issues)
	}
	if len(rep.Unread) != 1 {
		t.Errorf("unread = %v, want no-stats", rep.Unread)
	}
	var ci, fix IssueStat
	for _, s := range rep.Issues {
		if s.Type == "bugfix" {
			fix = s
		} else {
			ci = s
		}
	}

	// The local CI issue: the same life the Timeline tab shows (5h 0m, 3h 48m
	// waiting on you; no commits here, so a little more of it is idle).
	if !ci.Known || ci.Type != NoType || ci.Cycle.Round(time.Minute) != 5*time.Hour {
		t.Errorf("local CI: known=%v type=%q cycle=%v", ci.Known, ci.Type, ci.Cycle)
	}
	var sum time.Duration
	for st, v := range ci.Statuses {
		if v.Working+v.Waiting+v.Idle != v.Total {
			t.Errorf("%s: working %v + waiting %v + idle %v != %v", st, v.Working, v.Waiting, v.Idle, v.Total)
		}
		sum += v.Total
	}
	if sum != ci.Cycle {
		t.Errorf("statuses sum to %v, cycle %v", sum, ci.Cycle)
	}
	if ht := ci.Statuses["human-testing"]; ht.Waiting < 2*time.Hour+50*time.Minute {
		t.Errorf("human-testing waiting = %v, want the 2h 53m wait for your approval", ht.Waiting)
	}
	if ci.Loops != 1 {
		t.Errorf("local CI loops = %d, want the TP5 playtest loop", ci.Loops)
	}

	// The bugfix: dispatched twice, sent back once, one refused transition.
	if fix.Dispatches != 2 || fix.Backward != 1 || fix.Loops != 1 {
		t.Errorf("bugfix: dispatches=%d backward=%d loops=%d", fix.Dispatches, fix.Backward, fix.Loops)
	}
	if len(fix.Failures) != 2 {
		t.Fatalf("bugfix failures = %+v", fix.Failures)
	}
	if f := fix.Failures[0]; f.From != "in progress" || f.To != "testing" || f.ErrClass != "validation" || !f.DryRun {
		t.Errorf("dry run failure = %+v", f)
	}
	if f := fix.Failures[1]; f.From != "testing" || f.To != "shipping" || f.ErrClass != "approval_missing" || f.DryRun {
		t.Errorf("refused transition = %+v", f)
	}
	if len(fix.Waits) != 1 || fix.Waits[0].Wait != 85*time.Minute-(41*time.Minute+time.Second) {
		t.Errorf("bugfix waits = %+v, want the agent's last call (41m) to the approval (85m)", fix.Waits)
	}

	// The tables.
	var all, bug TypeRow
	for _, r := range rep.Types {
		switch r.Type {
		case All:
			all = r
		case "bugfix":
			bug = r
		}
	}
	if all.N != 2 || bug.N != 1 || all.Cycle.P90 != ci.Cycle || all.Cycle.P50 != fix.Cycle {
		t.Errorf("type rows: all=%+v bugfix=%+v", all, bug)
	}
	if all.Dispatches != [2]float64{0, 2} {
		t.Errorf("dispatches p50/p90 = %v", all.Dispatches)
	}
	if rep.Types[0].Type != All {
		t.Errorf("first type row = %q, want all", rep.Types[0].Type)
	}
	found := false
	for _, s := range rep.Statuses {
		if s.Type == All && s.Status == "human-testing" {
			found = true
			if s.N != 1 || s.Split != 1 || s.Waiting.P50 != ci.Statuses["human-testing"].Waiting {
				t.Errorf("human-testing row = %+v", s)
			}
		}
	}
	if !found {
		t.Error("no human-testing row for all types")
	}
	// The local CI agent's own dry run to backlog, refused for the approval,
	// is in its recorded clilog.
	if len(rep.Failures) != 3 || rep.Failures[0].DryRun || rep.Failures[0].Transition != "testing→shipping" ||
		rep.Failures[1] != (FailureRow{Transition: "in design→backlog", ErrClass: "approval_missing", DryRun: true, N: 1}) {
		t.Errorf("failure rows = %+v, real refusals first", rep.Failures)
	}
	if len(rep.Approvals) == 0 || rep.Approvals[0].Transition != "in design→in progress" {
		t.Errorf("approval rows = %+v, in workflow order", rep.Approvals)
	}
}

// Issues without CLI calls count for cycle time and status totals only.
func TestAggregateUnsplit(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	writeIssue(t, dir, "by-hand", "By hand", "", "done", &tracker.StatsStore{Transitions: []tracker.TransitionStat{
		{From: "idea", To: "in progress", TS: t0},
		{From: "in progress", To: "done", TS: t0.Add(time.Hour)},
	}})
	proj := &tracker.Project{Slug: "p", IssueDir: dir, WorkDir: root}
	issues, _ := tracker.LoadIssues(dir)
	rep := Aggregate(context.Background(), proj, tracker.DefaultWorkflow(), issues, t0.Add(24*time.Hour), Window)
	if len(rep.Issues) != 1 || rep.Issues[0].Known || len(rep.Unsplit()) != 1 {
		t.Fatalf("issues = %+v", rep.Issues)
	}
	for _, s := range rep.Statuses {
		if s.Status == "in progress" && s.Type == All {
			if s.N != 1 || s.Split != 0 || s.Total.P50 != time.Hour || s.Working.P50 != 0 {
				t.Errorf("in progress row = %+v", s)
			}
		}
	}
	if rep.Types[0].Unsplit != 1 {
		t.Errorf("all row = %+v", rep.Types[0])
	}
}

func TestPercentile(t *testing.T) {
	vals := []float64{5, 1, 4, 2, 3, 10, 9, 8, 7, 6}
	if p := percentile(vals, 0.5); p != 5 {
		t.Errorf("p50 = %v", p)
	}
	if p := percentile(vals, 0.9); p != 9 {
		t.Errorf("p90 = %v", p)
	}
	if p := percentile(nil, 0.9); p != 0 {
		t.Errorf("empty = %v", p)
	}
	if p := percentile([]float64{7}, 0.9); p != 7 {
		t.Errorf("one = %v", p)
	}
}

// The family through health.Take: rows named workflow.*, with labels.
func TestWorkflowFamily(t *testing.T) {
	proj, _ := testProject(t)
	env := health.Env{Root: proj.WorkDir, Project: proj, Workflow: tracker.DefaultWorkflow(), Now: localCIDone.Add(24 * time.Hour)}
	snap, _, err := health.Take(context.Background(), env, health.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if fr := snap.Families["workflow"]; fr.Error != "" {
		t.Fatalf("workflow family error: %s", fr.Error)
	} else if fr.Hint != "tables: /stats" {
		t.Errorf("workflow hint = %q, want the pointer to /stats", fr.Hint)
	}
	checks := []struct {
		name   string
		labels map[string]string
		want   float64
	}{
		{"workflow.issues", map[string]string{"type": "all"}, 2},
		{"workflow.issues", map[string]string{"type": "bugfix"}, 1},
		{"workflow.unread", nil, 1},
		{"workflow.rework.backward", map[string]string{"type": "bugfix"}, 1},
		{"workflow.gate_failures", map[string]string{"transition": "testing→shipping", "err_class": "approval_missing", "dry_run": "false"}, 1},
		{"workflow.approval_wait.p50", map[string]string{"transition": "testing→shipping"}, (85*time.Minute - 41*time.Minute - time.Second).Seconds()},
	}
	for _, c := range checks {
		got, ok := snap.Value(c.name, c.labels)
		if !ok || got != c.want {
			t.Errorf("%s = %v (found %v), want %v", health.RowID(c.name, c.labels), got, ok, c.want)
		}
	}
	if _, ok := snap.Value("workflow.status.waiting.p90", map[string]string{"status": "human-testing", "type": "all"}); !ok {
		t.Error("no workflow.status.waiting.p90{status=human-testing,type=all} row")
	}
}

func writeIssue(t *testing.T, dir, file, title, typ, status string, stats *tracker.StatsStore) {
	t.Helper()
	body := "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\n"
	if typ != "" {
		body += "type: \"" + typ + "\"\n"
	}
	body += "---\n\nBody.\n"
	path := filepath.Join(dir, file+".md")
	mustWrite(t, path, []byte(body))
	if stats != nil {
		data, _ := json.Marshal(stats)
		mustWrite(t, tracker.StatsSidecarPath(path), data)
	}
}

func mustCopy(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, to, data)
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// The time sums and working percentiles per type add up from the issues,
// the family carries them, and Filter on a wide report gives the same
// numbers as an Aggregate over the narrow window.
func TestAggregateTimeSumsAndFilter(t *testing.T) {
	proj, issues := testProject(t)
	now := localCIDone.Add(24 * time.Hour)
	wf := tracker.DefaultWorkflow()
	rep := Aggregate(context.Background(), proj, wf, issues, now, 90*24*time.Hour)

	var all TypeRow
	for _, r := range rep.Types {
		if r.Type == All {
			all = r
		}
	}
	var working, waiting, idle time.Duration
	for _, s := range rep.Issues {
		if !s.Known {
			continue
		}
		working += s.Working
		for _, st := range s.Statuses {
			waiting += st.Waiting
			idle += st.Idle
		}
	}
	if all.Working != working || all.Waiting != waiting || all.Idle != idle || waiting == 0 {
		t.Fatalf("all row working/waiting/idle = %v/%v/%v, issues sum to %v/%v/%v", all.Working, all.Waiting, all.Idle, working, waiting, idle)
	}
	if want := float64(waiting) / float64(working+waiting+idle); all.WaitingShare() != want {
		t.Errorf("waiting share = %v, want %v", all.WaitingShare(), want)
	}
	if all.WorkingPer.P90 < all.WorkingPer.P50 || all.WorkingPer.P90 == 0 {
		t.Errorf("working per issue = %+v", all.WorkingPer)
	}

	narrow := Aggregate(context.Background(), proj, wf, issues, now, Window)
	got := rep.Filter(now.Add(-Window), now)
	if len(got.Issues) != len(narrow.Issues) || len(got.Types) != len(narrow.Types) {
		t.Fatalf("Filter: %d issues / %d types, Aggregate: %d / %d", len(got.Issues), len(got.Types), len(narrow.Issues), len(narrow.Types))
	}
	for i := range got.Types {
		if got.Types[i].Type != narrow.Types[i].Type || got.Types[i].Cycle != narrow.Types[i].Cycle || got.Types[i].Waiting != narrow.Types[i].Waiting {
			t.Errorf("type %d: Filter %+v, Aggregate %+v", i, got.Types[i], narrow.Types[i])
		}
	}
	if len(got.Statuses) != len(narrow.Statuses) || len(got.Approvals) != len(narrow.Approvals) {
		t.Errorf("Filter statuses/approvals = %d/%d, Aggregate %d/%d", len(got.Statuses), len(got.Approvals), len(narrow.Statuses), len(narrow.Approvals))
	}

	m := map[string]float64{}
	for _, r := range Metrics(rep) {
		m[health.RowID(r.Name, r.Labels)] = r.Value
	}
	for id, want := range map[string]float64{
		"workflow.time.waiting{type=all}":   waiting.Seconds(),
		"workflow.time.share{kind=waiting}": all.WaitingShare(),
		"workflow.working.p50{type=all}":    all.WorkingPer.P50.Seconds(),
		"workflow.working.p90{type=bugfix}": -1,
	} {
		got, ok := m[id]
		if !ok || (want >= 0 && got != want) {
			t.Errorf("%s = %v (found %v), want %v", id, got, ok, want)
		}
	}
}
