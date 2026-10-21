package main

import (
	"os"
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
