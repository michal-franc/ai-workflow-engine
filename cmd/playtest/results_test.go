package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffIgnoresPathsAndTimes(t *testing.T) {
	a := &Results{Session: "s", Started: "t1", Build: "b1", Steps: []*StepResult{{ID: "TP1", Status: StatusPass,
		Viewports: []*ViewportResult{{Viewport: "1440x900", Status: StatusPass, Screenshot: "/a.png", Log: "/a.log"}}}}}
	b := &Results{Session: "s", Started: "t2", Build: "b2", Steps: []*StepResult{{ID: "TP1", Status: StatusPass,
		Viewports: []*ViewportResult{{Viewport: "1440x900", Status: StatusPass, Screenshot: "/b.png", Log: "/b.log"}}}}}
	if d := Diff(a, b); d != "" {
		t.Errorf("runs that only differ in paths and times differ:\n%s", d)
	}
	a.Steps[0].DurationMS, b.Steps[0].DurationMS = 1200, 1350
	if d := Diff(a, b); d != "" {
		t.Errorf("runs that only differ in step times differ:\n%s", d)
	}
	a.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "http /x.json", Pass: true, Actual: `200 {"listed_at":"2026-10-21T12:47:43Z","start_time":"2026-10-21 12:47:43"} in /tmp/pt-1844542027/proj`}}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "http /x.json", Pass: true, Actual: `200 {"listed_at":"2026-10-21T12:47:58Z","start_time":"2026-10-21 12:47:58"} in /tmp/pt-3864846610/proj`}}
	if d := Diff(a, b); d != "" {
		t.Errorf("runs that only differ in timestamps and the scenario dir inside a check's actual differ:\n%s", d)
	}
	a.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "http /status.md", Pass: true, Actual: `200 # Atlas status, 2026-10-22 13:39`}}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "http /status.md", Pass: true, Actual: `200 # Atlas status, 2026-10-22 13:40`}}
	if d := Diff(a, b); d != "" {
		t.Errorf("runs a minute apart differ in a minute-only stamp:\n%s", d)
	}
	a.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "http /x.json", Pass: true, Actual: `200 {"dir":"/tmp/pt-1/tmux","next":"abc…`}}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "http /x.json", Pass: true, Actual: `200 {"dir":"/tmp/pt-3864846610/tmux","ne…`}}
	if d := Diff(a, b); d != "" {
		t.Errorf("one body cut at different points after masking differs:\n%s", d)
	}
	a.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "cli dispatch", Pass: true, Actual: `A human approves at: http://127.0.0.1:36755/p/crew/issue/x`}}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "cli dispatch", Pass: true, Actual: `A human approves at: http://127.0.0.1:45875/p/crew/issue/x`}}
	if d := Diff(a, b); d != "" {
		t.Errorf("runs that only differ in the board's port differ:\n%s", d)
	}
	// "exit 0: {" makes YAML quote the value, so the cut ends in …'
	a.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "cli status --json", Pass: true, Actual: `exit 0: {"as_of":"2026-10-23T15:41:12.1+02:00","slug":"ui/child"…`}}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "cli status --json", Pass: true, Actual: `exit 0: {"as_of":"2026-10-23T15:41:12.123+02:00","slug":"ui/chi…`}}
	if d := Diff(a, b); d != "" {
		t.Errorf("one quoted body cut at different points after masking differs:\n%s", d)
	}
	// A newline makes YAML double-quote the value.
	a.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "cli show", Pass: true, Actual: "exit 0: == Child ==\nTimeline: http://127.0.0.1:36755/p/crew\nBrief: none. The Issue ta…"}}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "cli show", Pass: true, Actual: "exit 0: == Child ==\nTimeline: http://127.0.0.1:45875/p/crew\nBrief: none. The Issue …"}}
	if d := Diff(a, b); d != "" {
		t.Errorf("one double-quoted body cut at different points after masking differs:\n%s", d)
	}
	b.Steps[0].Viewports[0].Checks = []CheckResult{{Check: "visible x", Pass: false, Actual: "hidden"}}
	if d := Diff(a, b); !strings.Contains(d, "run 2:") {
		t.Errorf("a different check result was not reported: %q", d)
	}
}

func writeRun(t *testing.T, root, session, name string, r *Results) string {
	t.Helper()
	dir := filepath.Join(root, session, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.Write(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLatestRun(t *testing.T) {
	root := t.TempDir()
	writeRun(t, root, "s", "20260101-100000.000-auto", &Results{Session: "s", Mode: "auto", Build: "b1"})
	writeRun(t, root, "s", "20260102-100000.000-auto", &Results{Session: "s", Mode: "auto", Build: "b2"})
	writeRun(t, root, "s", "20260103-100000.000-judge", &Results{Session: "s", Mode: "judge", Build: "b2"})
	if dir, r := LatestRun(root, "s", "auto", ""); r == nil || !strings.HasSuffix(dir, "20260102-100000.000-auto") {
		t.Errorf("latest auto = %s", dir)
	}
	if _, r := LatestRun(root, "s", "auto", "b1"); r == nil || r.Build != "b1" {
		t.Errorf("latest auto of build b1 = %+v", r)
	}
	if _, r := LatestRun(root, "s", "auto", "b9"); r != nil {
		t.Errorf("no run of build b9 expected, got %+v", r)
	}
}

func TestBuildPlan(t *testing.T) {
	root := t.TempDir()
	p := writeSession(t, `session: plan
issue: x/y
scenario: basic
steps:
  - id: TP1
    title: machine pass
    checks: [{ visible: board.search }]
  - id: TP2
    title: machine fail
    checks: [{ visible: board.search }]
  - id: TP3
    title: judged pass, machine pass
    checks: [{ visible: board.search }]
    do: look
    expect: fine
  - id: TP4
    title: judged fail
    do: look
    expect: fine
  - id: TP5
    title: judged pass but ux-test failed
    checks: [{ visible: board.search }]
    do: look
    expect: fine
  - id: TP6
    title: never judged
    do: look
    expect: fine
`)
	s, errs := LoadSession(p)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s.Name = "plan"
	fail := &ViewportResult{Viewport: "390x844", Status: StatusFail, Checks: []CheckResult{{Check: "visible board.search", Line: 9, Actual: "hidden"}}}
	writeRun(t, root, "plan", "20260101-100000.000-auto", &Results{Session: "plan", Mode: "auto", Build: "b", Steps: []*StepResult{
		{ID: "TP1", Status: StatusPass},
		{ID: "TP2", Status: StatusFail, Viewports: []*ViewportResult{fail}},
		{ID: "TP3", Status: StatusPass},
		{ID: "TP4", Status: StatusPending},
		{ID: "TP5", Status: StatusFail, Viewports: []*ViewportResult{fail}},
		{ID: "TP6", Status: StatusPending},
	}})
	writeRun(t, root, "plan", "20260101-110000.000-judge", &Results{Session: "plan", Mode: "judge", Build: "b", Steps: []*StepResult{
		{ID: "TP3", Status: StatusPass, Judge: &JudgeResult{Status: StatusPass, Opinions: []Opinion{{Target: "board.search", Note: "too small"}}}},
		{ID: "TP4", Status: StatusFail, Judge: &JudgeResult{Status: StatusFail, Note: "cards overlap"}},
		{ID: "TP5", Status: StatusPass, Judge: &JudgeResult{Status: StatusPass}},
		{ID: "TP6", Status: StatusPending},
	}})
	plan := BuildPlan([]*Session{s}, root)
	if got := strings.Join(plan.Tick, " "); got != "TP1 TP3" {
		t.Errorf("tick = %q, want TP1 TP3", got)
	}
	comments := strings.Join(plan.Comments, "\n")
	for _, want := range []string{
		"playtest: TP2 FAIL in ux-test (plan, b): 390x844: visible board.search (line 9), actual \"hidden\"",
		"playtest: TP3 opinion on board.search (plan): too small",
		"playtest: TP4 FAIL (owner, plan): cards overlap",
	} {
		if !strings.Contains(comments, want) {
			t.Errorf("missing comment %q in:\n%s", want, comments)
		}
	}
	missing := strings.Join(plan.Missing, "\n")
	for _, want := range []string{"TP5 (plan): the owner passed it, but its ux-test did not", "TP6 (plan): no verdict"} {
		if !strings.Contains(missing, want) {
			t.Errorf("missing %q in:\n%s", want, missing)
		}
	}
}

func TestOwnIssueFromFlagOrWorkBranch(t *testing.T) {
	if got := OwnIssue(t.TempDir(), "ui/flag"); got != "ui/flag" {
		t.Errorf("flag: %q", got)
	}
	if got := OwnIssue(t.TempDir(), ""); got != "" {
		t.Errorf("not a repo: %q", got)
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "x")
	if got := OwnIssue(repo, ""); got != "" {
		t.Errorf("main: %q", got)
	}
	run("checkout", "-q", "-b", "work/ui/evidence-tab")
	if got := OwnIssue(repo, ""); got != "ui/evidence-tab" {
		t.Errorf("work branch: %q", got)
	}
}
