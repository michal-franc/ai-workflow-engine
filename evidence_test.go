package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/playtestruns"
	"github.com/michal-franc/issue-viewer/internal/proof"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var evNow = time.Date(2026, 10, 23, 18, 0, 0, 0, time.Local)

func crit(id string, checked bool, links ...proof.CheckedLink) proof.Criterion {
	return proof.Criterion{ID: id, Text: "criterion " + id, Checked: checked, Links: append([]proof.CheckedLink{}, links...)}
}

func passing(kind, ref, source, from string) proof.CheckedLink {
	return proof.CheckedLink{Link: proof.Link{Kind: kind, Ref: ref, Source: source, From: from}, Check: proof.Check{OK: true, State: "passed"}}
}

func failing(kind, ref, source, from, state, fix string) proof.CheckedLink {
	return proof.CheckedLink{Link: proof.Link{Kind: kind, Ref: ref, Source: source, From: from}, Check: proof.Check{State: state, Fix: fix}}
}

func TestEvidenceLabel(t *testing.T) {
	step := passing(tracker.ProofStep, "TP1", proof.SourcePlaytest, "s")
	bad := failing(tracker.ProofTest, "TestX", proof.SourceTrailer, "abc", "not found", "")
	for _, tc := range []struct {
		name     string
		r        proof.Report
		required bool
		want     EvidenceLabel
	}{
		{"no criteria", proof.Report{}, true, EvidenceLabel{}},
		{"all proven", proof.Report{Criteria: []proof.Criterion{crit("AC1", true, step), crit("AC2", true, step)}}, false, EvidenceLabel{"2 / 2 proven", "green"}},
		{"short, covered", proof.Report{Criteria: []proof.Criterion{crit("AC1", true, step), crit("AC2", false)}}, true, EvidenceLabel{"1 / 2 proven", "amber"}},
		{"short, not covered but linked", proof.Report{Criteria: []proof.Criterion{crit("AC1", true, bad), crit("AC2", false)}}, false, EvidenceLabel{"0 / 2 proven", "amber"}},
		{"unlinked, not covered", proof.Report{Criteria: []proof.Criterion{crit("AC1", true), crit("AC2", true), crit("AC3", false)}}, false, EvidenceLabel{"2 ticked · 0 linked", "gray"}},
		{"unlinked, covered", proof.Report{Criteria: []proof.Criterion{crit("AC1", false), crit("AC2", false)}}, true, EvidenceLabel{"0 / 2 proven", "amber"}},
	} {
		if got := evidenceLabel(tc.r, tc.required); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func evRun(session, mode string, started time.Time, steps ...timeline.PlaytestStep) timeline.PlaytestRun {
	return timeline.PlaytestRun{Session: session, Mode: mode, Started: started, Steps: steps, Dir: "/repo/.worktrees/ui/x/.playtest/runs/" + session + "/" + started.Format("150405") + "-" + mode, Build: "abc1234+dirty"}
}

func vp(name, status, shot string, checks ...timeline.PlaytestCheck) timeline.PlaytestViewport {
	return timeline.PlaytestViewport{Viewport: name, Status: status, Screenshot: shot, Checks: checks}
}

func TestEvidenceShots(t *testing.T) {
	r := proof.Report{Slug: "ui/x", Criteria: []proof.Criterion{
		crit("AC1", true, passing(tracker.ProofStep, "TP4", proof.SourcePlaytest, "tab")),
		crit("AC2", true, passing(tracker.ProofStep, "TP4", proof.SourcePlaytest, "tab"), failing(tracker.ProofStep, "TP5", proof.SourcePlaytest, "tab", "failed", "fix it")),
		crit("AC3", false, failing(tracker.ProofStep, "TP9", proof.SourcePlaytest, "tab", "not run", "run it")),
	}}
	old := evRun("tab", "auto", evNow.Add(-3*time.Hour), timeline.PlaytestStep{ID: "TP4", Status: "fail"})
	latest := evRun("tab", "auto", evNow.Add(-time.Hour),
		timeline.PlaytestStep{ID: "TP4", Title: "The band", Status: "pass", Viewports: []timeline.PlaytestViewport{
			vp("390x844", "pass", "/old/abs/path/shots/TP4-390x844.png"), vp("1440x900", "pass", "shots/TP4-1440x900.png")}},
		timeline.PlaytestStep{ID: "TP5", Title: "Approving", Status: "fail", Viewports: []timeline.PlaytestViewport{
			vp("1440x900", "pass", ""), vp("390x844", "fail", "", timeline.PlaytestCheck{Check: "no_horizontal_scroll", Actual: "530 > 390"})}},
		timeline.PlaytestStep{ID: "TP6", Status: "pass"},
	)
	reduced := evRun("other", "auto", evNow.Add(-2*time.Hour), timeline.PlaytestStep{ID: "TP1", Status: "pass"})
	reduced.Reduced = "2026-11-22"
	on := map[string]bool{}
	tab := buildEvidence(evidenceIn{
		Prefix: "/p/iv", Root: "/repo", Report: r, Required: true, PastDesign: true, Now: evNow,
		Runs:   []timeline.PlaytestRun{old, reduced, latest},
		Exists: func(p string) bool { on[p] = true; return !strings.Contains(p, "TP5-1440x900") },
	})

	if len(tab.Shots) != 3 {
		t.Fatalf("shots = %+v, want TP4, TP5, TP9", tab.Shots)
	}
	tp4 := tab.Shots[0]
	if tp4.Step != "TP4" || !tp4.Large || tp4.ProvesText != "AC1 AC2" || tp4.State != "passed" || tp4.Title != "The band" {
		t.Errorf("TP4 = %+v", tp4)
	}
	if len(tp4.Views) != 2 || tp4.Views[0].Name != "1440x900" || !tp4.Views[0].On || tp4.Views[1].On {
		t.Errorf("TP4 views = %+v, want the widest first and on", tp4.Views)
	}
	wantSrc := "/p/iv/files/.worktrees/ui/x/.playtest/runs/tab/170000-auto/shots/TP4-1440x900.png"
	if tp4.Src != wantSrc {
		t.Errorf("TP4 src = %q, want %q (looked up next to results.yaml, not by the recorded path)", tp4.Src, wantSrc)
	}
	if got := tp4.Views[1].Src; !strings.HasSuffix(got, "/170000-auto/shots/TP4-390x844.png") {
		t.Errorf("TP4 phone src = %q: an old absolute screenshot path must resolve in the run folder", got)
	}
	tp5 := tab.Shots[1]
	if tp5.State != "failed" || tp5.StateClass != "red" || tp5.FailLine != "no_horizontal_scroll: 530 > 390" {
		t.Errorf("TP5 = %+v, want failed with its failing check", tp5)
	}
	if len(tp5.Views) != 2 || !tp5.Views[1].On || !tp5.Views[1].Failed || tp5.Views[0].Src != "" {
		t.Errorf("TP5 views = %+v, want the failed phone view on and the missing desktop shot empty", tp5.Views)
	}
	if !tab.Shots[2].NoRun || tab.Shots[2].State != "not run" {
		t.Errorf("TP9 = %+v, want a no-run card", tab.Shots[2])
	}
	if len(tab.Other) != 2 || tab.Other[0].Session != "other" || tab.Other[1].StepsText != "TP6" {
		t.Errorf("other = %+v, want other: TP1 and tab: TP6", tab.Other)
	}
	if tab.ShotsRun != "latest ux-test run, 17:00 at abc1234" {
		t.Errorf("run = %q", tab.ShotsRun)
	}
}

func TestEvidenceReducedShotResolvesToJPEG(t *testing.T) {
	r := proof.Report{Criteria: []proof.Criterion{crit("AC1", true, passing(tracker.ProofStep, "TP1", proof.SourcePlaytest, "s"))}}
	run := evRun("s", "auto", evNow, timeline.PlaytestStep{ID: "TP1", Status: "pass", Viewports: []timeline.PlaytestViewport{vp("1440x900", "pass", "shots/TP1-1440x900.png")}})
	run.Dir = "/repo/.playtest/runs/s/1-auto"
	run.Reduced = "2026-11-22"
	tab := buildEvidence(evidenceIn{Prefix: "/p/iv", Root: "/repo", Report: r, Now: evNow, Runs: []timeline.PlaytestRun{run},
		Exists: func(p string) bool { return strings.HasSuffix(p, ".jpg") }})
	if s := tab.Shots[0]; !s.Reduced || s.Src != "/p/iv/files/.playtest/runs/s/1-auto/shots/TP1-1440x900.jpg" {
		t.Errorf("reduced shot = %+v, want the .jpg from the project root's runs", s)
	}
}

func TestEvidenceStepsAndWords(t *testing.T) {
	auto := evRun("tab", "auto", evNow.Add(-2*time.Hour),
		timeline.PlaytestStep{ID: "TP4", Status: "pass", Viewports: []timeline.PlaytestViewport{vp("1440x900", "pass", ""), vp("390x844", "pass", "")}},
		timeline.PlaytestStep{ID: "TP10", Status: "pending", Judgement: true, Viewports: []timeline.PlaytestViewport{vp("1440x900", "pass", "")}},
	)
	judge := evRun("tab", "judge", evNow.Add(-time.Hour), timeline.PlaytestStep{ID: "TP10", Status: "pass", Judgement: true, Judge: &timeline.PlaytestJudge{Status: "pass"}})
	comments := []tracker.Comment{
		{Date: evNow.Add(-50 * time.Minute).Format(time.RFC3339), Text: `playtest: TP10 pass, owner in chat: "All good"`},
		{Date: evNow.Add(-40 * time.Minute).Format(time.RFC3339), Text: "progress: TP4 mentioned but not a verdict"},
	}
	steps, judged, words := evidenceSteps(evidenceIn{Runs: []timeline.PlaytestRun{auto, judge}, Comments: comments, Now: evNow})
	if len(steps) != 2 {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[0].ID != "TP4" || steps[0].Machine != "passed" || steps[0].Screens != "desktop · phone" || steps[0].You != "—" {
		t.Errorf("TP4 = %+v", steps[0])
	}
	if steps[1].Machine != "judgement step" || steps[1].You != "pass, 17:00" || steps[1].YouCls != "ok" {
		t.Errorf("TP10 = %+v, want your judge-run verdict", steps[1])
	}
	if judged != "judged in the viewer" {
		t.Errorf("judged = %q", judged)
	}
	if len(words) != 1 || words[0].Step != "TP10" || words[0].Source != "playtest: comment" || !strings.Contains(words[0].Text, "All good") {
		t.Errorf("words = %+v, want the playtest: comment naming TP10 only", words)
	}

	// No judge run: the comment is your verdict.
	steps, judged, _ = evidenceSteps(evidenceIn{Runs: []timeline.PlaytestRun{auto}, Comments: comments, Now: evNow})
	if steps[1].You != "said in a comment" || judged != "judged in chat" {
		t.Errorf("TP10 without a judge run = %+v, %q", steps[1], judged)
	}
}

func TestEvidenceVerdictAndWatch(t *testing.T) {
	r := proof.Report{Ref: "abc", RefName: "the merge abc", TestsAt: evNow.Add(-4 * time.Hour), LastChange: evNow.Add(-5 * time.Hour), LastChangeSHA: "0ff4c621f153",
		Criteria: []proof.Criterion{crit("AC1", true, passing(tracker.ProofTest, "TestA", proof.SourceTrailer, "0ff4c62")), crit("AC2", false)}}
	comments := []tracker.Comment{
		{Date: evNow.Add(-6 * time.Hour).Format(time.RFC3339), Text: "tests: old run"},
		{Date: evNow.Add(-4 * time.Hour).Format(time.RFC3339), Text: "tests: make validate green at 0ff4c62"},
		{Date: evNow.Add(-time.Hour).Format(time.RFC3339), Text: "risk: time to kill sits at the low edge"},
		{Date: evNow.Add(-time.Minute).Format(time.RFC3339), Text: "risk: none."},
	}
	tab := buildEvidence(evidenceIn{
		Report: r, Required: true, PastDesign: true, Comments: comments, Now: evNow, Status: "done",
		Transitions: []tracker.TransitionStat{{To: "done", TS: evNow.Add(-2 * time.Hour), MergeSHA: "105fe86181ad"}},
		Total:       "2h 9m", Waiting: 83 * time.Minute, WaitKnown: true, CI: &ciSummary{State: "green"},
	})
	v := tab.Verdict
	if v.Merge != "105fe86" || v.MergeAt != "16:00" || v.CI != "local CI green after the merge" {
		t.Errorf("merge = %+v", v)
	}
	if !v.HasTests || !v.TestsAfter || v.Tests != "make validate green at 0ff4c62" || v.LastChangeSHA != "0ff4c62" {
		t.Errorf("tests = %+v, want the newest tests: after the last code commit", v)
	}
	if v.FromDispatch != "2h 9m" || v.Waiting != "1h 23m" {
		t.Errorf("time = %q, %q", v.FromDispatch, v.Waiting)
	}
	if v.Short != "AC2 (no link)" {
		t.Errorf("short = %q", v.Short)
	}
	if len(tab.Watch) != 2 || tab.Watch[0].None || !tab.Watch[1].None {
		t.Errorf("watch = %+v, want a risk then a none", tab.Watch)
	}
	if !strings.Contains(strings.Join(tab.Ways, "\n"), "issue-cli prove") || tab.WaysFor != "AC2" {
		t.Errorf("ways = %q for %q", tab.Ways, tab.WaysFor)
	}

	r.TestsAt = evNow.Add(-6 * time.Hour)
	if v := buildEvidence(evidenceIn{Report: r, Now: evNow}).Verdict; v.TestsAfter {
		t.Error("a tests: report older than the last code commit must read as before it")
	}
}

func TestEvidenceUnlinkedAndNotStarted(t *testing.T) {
	r := proof.Report{Criteria: []proof.Criterion{crit("AC1", true), crit("AC2", true)}}
	tab := buildEvidence(evidenceIn{Report: r, PastDesign: true, Now: evNow})
	if !tab.Unlinked || tab.Verdict.Short != "" || tab.Ways != nil || tab.ShotsEmpty == "" {
		t.Errorf("unlinked = %+v", tab)
	}
	tab = buildEvidence(evidenceIn{Report: r, Required: true, PastDesign: true, Now: evNow})
	if tab.Unlinked || tab.Verdict.Short != "AC1 (no link), AC2 (no link)" {
		t.Errorf("covered, unlinked = unlinked %v short %q", tab.Unlinked, tab.Verdict.Short)
	}
	if tab := buildEvidence(evidenceIn{Report: r, Now: evNow}); !tab.NotStarted || tab.StaleRun {
		t.Errorf("not started = %+v", tab)
	}
}

func TestPastDesign(t *testing.T) {
	wf := &tracker.WorkflowConfig{Statuses: []tracker.WorkflowStatus{{Name: "idea"}, {Name: "in design"}, {Name: "backlog"}, {Name: "in progress"}, {Name: "parked", Optional: true}}}
	for status, want := range map[string]bool{"idea": false, "in design": false, "backlog": true, "in progress": true, "parked": true} {
		if got := pastDesign(wf, status); got != want {
			t.Errorf("pastDesign(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestParseEvidenceChange(t *testing.T) {
	out := "\x1eaaaaaaaaaaaa\x1fp1\x1f1761231000\x1fdocs: the tab\n\n10\t2\tdocs/UI/overview.md\n1\t0\tCHANGELOG.md\n" +
		"\x1ebbbbbbbbbbbb\x1fp1 p2\x1f1761230000\x1fMerge branch 'main'\n" +
		"\x1ecccccccccccc\x1fp0\x1f1761229000\x1ffeat(ui): the tab\n\n100\t5\tevidence.go\n50\t0\tevidence_test.go\n20\t1\tinternal/proof/check.go\n-\t-\tstatic/x.png\n3\t3\t{old => templates}/_evidence.html\n"
	c := parseEvidenceChange(out)
	if len(c.Commits) != 2 || c.Merges != 1 || c.Commits[1].Files != 5 {
		t.Fatalf("commits = %+v merges %d", c.Commits, c.Merges)
	}
	if c.Files != 7 || c.Add != 184 || c.Del != 11 || c.TestAdd != 50 {
		t.Errorf("totals = files %d +%d −%d tests %d", c.Files, c.Add, c.Del, c.TestAdd)
	}
	got := map[string]evArea{}
	for _, a := range c.Areas {
		got[a.Name] = a
	}
	if got["(root)"].Add != 151 || got["(root)"].Files != 3 || got["internal/proof"].Add != 20 || got["templates"].Files != 1 || got["docs"].Del != 2 {
		t.Errorf("areas = %+v", c.Areas)
	}
	if c.Areas[0].Name != "(root)" || c.Areas[0].Pct != 100 {
		t.Errorf("first area = %+v, want the largest at 100%%", c.Areas[0])
	}
}

func TestStaleLines(t *testing.T) {
	body := "Intro mentions a pending approval in passing.\n\n" +
		"## Decisions\n\n- **D-1** · 2026-10-08 · chat: Upgrade covers new features such as types of work, \"yes\"\n\n" +
		"## Idea\n\n### Open questions\n\n" +
		"- Where does the version live: a top-level `template:` field or a header?\n" +
		"- Should the upgrade also cover per-system overlays and `types:` paths?\n" +
		"- ~~Struck through, so answered~~\n" +
		"- Answered by D-1 already.\n" +
		"- What format should changelog entries use?\n\n" +
		"### Clarifications\n\n" +
		"- **Version:** a top-level `template:` field holds the version; no header, the field lives in the file.\n\n" +
		"## Design\n\nOpen question: block until nvim exits?\n\nOwner pick: pending\n\n```\nstatus: pending\n```\n\n" +
		"- [ ] a box that says pending\n\n" +
		"## Test Plan\n\nOpen question: not here\n"
	lines := staleLines(body, tracker.ParseDecisions("x/y", body))
	var got []string
	for _, l := range lines {
		got = append(got, l.Section+" | "+l.Text+" -> "+l.Answer)
	}
	want := []string{
		"Open questions | Where does the version live: a top-level `template:` field or a header? -> Clarifications: Version",
		"Open questions | Should the upgrade also cover per-system overlays and `types:` paths? -> D-1",
		"Open questions | What format should changelog entries use? -> ",
		"Design | Open question: block until nvim exits? -> ",
		"Design | Owner pick: pending -> ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stale lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if lines[1].Quote == "" || lines[0].Anchor != "clarifications" {
		t.Errorf("answers = %+v", lines[:2])
	}
}

// A game's runs (RogueTech's real results.yaml): no step proves a criterion,
// shots sit beside results.yaml, and a -cached run has none.
func TestEvidenceShotsGameRuns(t *testing.T) {
	const slug = "api/reproducible-scenarios-and-playtest-cases-for-battles-and-screens"
	root, _ := filepath.Abs(filepath.Join("internal", "timeline", "testdata", "roguetech"))
	runs, _ := timeline.LoadPlaytestRuns(nil, slug, root)
	for i := range runs {
		if strings.HasSuffix(runs[i].Dir, "-ux-test-cached") {
			runs[i].Started = evNow // the newest auto run, with no shot
		}
	}
	shot := filepath.Join(root, ".playtest", "runs", "battle-ai-heat", "20261024-110508-ux-test", "MV7@1920x1080.png")
	tab := buildEvidence(evidenceIn{
		Prefix: "/p/roguetech", Root: root, Now: evNow, PastDesign: true, Runs: runs,
		Report: proof.Report{Slug: slug, Criteria: []proof.Criterion{crit("AC1", true), crit("AC2", false)}},
		Exists: func(p string) bool { return p == shot },
	})
	if len(tab.Shots) != 1 {
		t.Fatalf("shots = %+v, want MV7 from battle-ai-heat", tab.Shots)
	}
	s := tab.Shots[0]
	if s.Step != "MV7" || s.Session != "battle-ai-heat" || s.Proves != nil || s.State != "judged pass" {
		t.Errorf("MV7 = %+v, want battle-ai-heat's shot, judged pass in the playtest run", s)
	}
	if want := "/p/roguetech/files/.playtest/runs/battle-ai-heat/20261024-110508-ux-test/MV7@1920x1080.png"; s.Src != want {
		t.Errorf("src = %q, want %q (beside results.yaml, the newest run that has the shot)", s.Src, want)
	}
	if want := "latest ux-test run, " + evWhen(time.Date(2026, 10, 24, 11, 5, 8, 0, time.Local), evNow) + " at b5de0554"; tab.ShotsRun != want {
		t.Errorf("run = %q, want %q: the run the shot is from, not the newer -cached one", tab.ShotsRun, want)
	}
	if len(s.Views) != 1 || s.Views[0].Label != "1920×1080" {
		t.Errorf("views = %+v", s.Views)
	}
	if tab.Other != nil || tab.ShotsEmpty != "" || tab.ShotsBy != "each session's latest ux-test run; no step proves a criterion yet" {
		t.Errorf("other %+v, empty %q, by %q", tab.Other, tab.ShotsEmpty, tab.ShotsBy)
	}
}

// A done game-project issue: at → done the archive moves its latest runs to
// the root and the worktree goes; the Evidence tab still shows the shot,
// found through the -cached run's older ux-test run.
func TestEvidenceShowsArchivedGameRunsAfterWorktreeIsGone(t *testing.T) {
	const slug = "api/battles"
	root := t.TempDir()
	wt := playtestruns.WorktreeDir(root, slug)
	write := func(name, body string, shot bool) {
		t.Helper()
		dir := filepath.Join(playtestruns.RunsDir(wt), "battle-ai-heat", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if shot {
			os.WriteFile(filepath.Join(dir, "MV7@1920x1080.png"), []byte("png"), 0o644)
		}
		y := "session: battle-ai-heat\nissue: " + slug + "\nbuild: b5de0554\n" + body
		if err := os.WriteFile(filepath.Join(dir, "results.yaml"), []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	step := "steps:\n- id: MV7\n  title: The overheated Sniper holds fire\n  judgement: true\n  status: pass\n  screenshots:\n  - MV7@1920x1080.png\n"
	write("20261024-090000-ux-test", "mode: ux-test\nstarted: 2026-10-24T09:00:00\n"+step, true)
	write("20261024-110508-ux-test", "mode: ux-test\nstarted: 2026-10-24T11:05:08\n"+step, true)
	write("20261024-120000-ux-test-cached", "mode: ux-test\nstarted: 2026-10-24T12:00:00\n"+step+"  cached: .playtest/runs/battle-ai-heat/20261024-110508-ux-test\n", false)
	write("20261024-133313-playtest", "mode: playtest\nstarted: 2026-10-24T13:33:17\n", false)
	write("20261024-133651-playtest", "mode: playtest\nstarted: 2026-10-24T13:37:17\nsteps:\n- id: MV7\n  status: pass\n  verdict: pass\n", false)

	if _, err := playtestruns.Archive(root, slug); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	runs, problems := timeline.LoadPlaytestRuns(nil, slug, root, wt)
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	tab := buildEvidence(evidenceIn{
		Prefix: "/p/roguetech", Root: root, Now: evNow, PastDesign: true, Runs: runs,
		Report: proof.Report{Slug: slug, Criteria: []proof.Criterion{crit("AC1", true)}},
		Exists: fileExists,
	})
	if len(tab.Shots) != 1 {
		t.Fatalf("shots = %+v (runs %d), want MV7 from the archived runs", tab.Shots, len(runs))
	}
	s := tab.Shots[0]
	if want := "/p/roguetech/files/.playtest/runs/battle-ai-heat/20261024-110508-ux-test/MV7@1920x1080.png"; s.Src != want {
		t.Errorf("src = %q, want %q", s.Src, want)
	}
	if s.State != "judged pass" {
		t.Errorf("state = %q, want the archived playtest's verdict", s.State)
	}
}
