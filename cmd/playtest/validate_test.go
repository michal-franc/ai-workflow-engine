package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoDir is the repo root as seen from this package's tests.
func repoDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestRepoSessionsValid is `make playtest-check` without the issue lookup:
// every committed session must validate.
func TestRepoSessionsValid(t *testing.T) {
	repo := repoDir(t)
	all, errs := LoadSessions(filepath.Join(repo, "playtest", "sessions"))
	if len(all) == 0 {
		t.Fatal("no sessions found in playtest/sessions")
	}
	v := &Validator{ScenarioDir: filepath.Join(repo, "playtest", "scenarios")}
	for _, s := range all {
		errs = append(errs, v.Validate(s)...)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

func writeSession(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateCatchesMistakes(t *testing.T) {
	p := writeSession(t, `session: bad
issue: some/issue
scenario: basic
session_checks:
  viewports: [1440x900, tiny]
  layout: [no_clipping, no_wobble]
steps:
  - id: TP1
    title: unknown things
    setup:
      - open: /p/{project}/board
      - teleport: somewhere
    checks:
      - visible: board.colum.backlog
      - glows: board.search
  - id: TP1
    title: duplicate id, and checks nothing
  - id: not-an-id
    title: bad id
    checks:
      - count: { pattern: board.card.* }
  - id: TP3
    title: judgement without do
    from: TP9
    expect: looks right
`)
	s, errs := LoadSession(p)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	v := &Validator{ScenarioDir: filepath.Join(repoDir(t), "playtest", "scenarios")}
	errs = v.Validate(s)
	var all []string
	for _, e := range errs {
		all = append(all, e.Error())
	}
	text := strings.Join(all, "\n")
	for _, want := range []string{
		`viewport "tiny"`,
		`unknown rule "no_wobble"`,
		`bad.yaml:12: unknown action "teleport"`,
		`unknown target "board.colum.backlog"`,
		`did you mean "board.column.*"`,
		`unknown check "glows"`,
		`duplicate step id "TP1"`,
		`step TP1 checks nothing`,
		`step id "not-an-id" is not a checkbox id`,
		`count: want {pattern, within?, equals|min|max}`,
		`judgement step TP3 needs do:`,
		`from: "TP9" must name an earlier step`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing error %q in:\n%s", want, text)
		}
	}
}

func TestValidateStepIDsAgainstIssue(t *testing.T) {
	p := writeSession(t, `session: bad
issue: some/issue
scenario: basic
steps:
  - id: TP2
    title: real box
    checks: [{ visible: board.search }]
  - id: TP7
    title: not in the issue
    checks: [{ visible: board.search }]
`)
	s, _ := LoadSession(p)
	v := &Validator{
		ScenarioDir: filepath.Join(repoDir(t), "playtest", "scenarios"),
		Checklist:   func(string) (map[string]string, bool) { return map[string]string{"TP2": "Test Plan"}, true },
	}
	errs := v.Validate(s)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), `step id "TP7" is not a checkbox in some/issue`) {
		t.Errorf("got %v", errs[1])
	}

	v.Checklist = func(string) (map[string]string, bool) { return nil, false }
	v.Notes = nil
	if errs := v.Validate(s); len(errs) != 0 {
		t.Errorf("an unresolvable issue should only check shape, got %v", errs)
	}
	if len(v.Notes) != 1 {
		t.Errorf("want a note about the unresolvable issue, got %v", v.Notes)
	}
}

func TestValidateProvesNamesCriteria(t *testing.T) {
	p := writeSession(t, `session: bad
issue: some/issue
scenario: basic
steps:
  - id: TP2
    title: real box
    proves: [AC1, TP2, AC5, "first one"]
    checks: [{ visible: board.search }]
`)
	s, _ := LoadSession(p)
	if strings.Join(s.Steps[0].Proves, ",") != "AC1,TP2,AC5,first one" {
		t.Fatalf("proves = %v", s.Steps[0].Proves)
	}
	v := &Validator{
		ScenarioDir: filepath.Join(repoDir(t), "playtest", "scenarios"),
		Checklist: func(string) (map[string]string, bool) {
			return map[string]string{"TP2": "Test Plan", "AC1": "Acceptance Criteria"}, true
		},
	}
	var text []string
	for _, e := range v.Validate(s) {
		text = append(text, e.Error())
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{
		`step TP2 proves "TP2", which is not an acceptance criterion of some/issue`,
		`step TP2 proves "AC5", which is not an acceptance criterion of some/issue`,
		`step TP2 proves "first one", which is not a criterion id`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if len(text) != 3 {
		t.Errorf("want 3 errors (AC1 is fine), got:\n%s", all)
	}
}

func TestWaitForCheck(t *testing.T) {
	p := writeSession(t, `session: bad
issue: x/y
scenario: basic
steps:
  - id: A1
    title: waits
    act:
      - wait_for: board.search
      - wait_for: { hidden: board.transition_modal, ms: 8000 }
      - wait_for: { url: { contains: status=backlog } }
    checks: [{ visible: board.search }]
`)
	s, errs := LoadSession(p)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	acts := s.Steps[0].act
	c, ms, err := waitForCheck(acts[0])
	if err != nil || c.Kind != "visible" || c.Arg["target"] != "board.search" || ms != 5000 {
		t.Errorf("shorthand: %v %v %v", c, ms, err)
	}
	c, ms, err = waitForCheck(acts[1])
	if err != nil || c.Kind != "hidden" || ms != 8000 {
		t.Errorf("map form: %v %v %v", c, ms, err)
	}
	c, _, err = waitForCheck(acts[2])
	if err != nil || c.Kind != "url" || c.Arg["contains"] != "status=backlog" {
		t.Errorf("url form: %v %v", c, err)
	}
}

func TestChainReplaysFromAncestors(t *testing.T) {
	p := writeSession(t, `session: bad
issue: x/y
scenario: basic
steps:
  - id: A1
    title: first
    setup: [{ open: /a }]
    act: [{ click: board.search }]
    checks: [{ visible: board.search }]
  - id: A2
    title: second
    from: A1
    setup: [{ open: /b }]
    checks: [{ visible: board.search }]
`)
	s, _ := LoadSession(p)
	var kinds []string
	for _, a := range chain(s, s.Steps[1]) {
		kinds = append(kinds, a.Kind+" "+argString(a.Arg))
	}
	want := "open /a|click board.search|open /b"
	if got := strings.Join(kinds, "|"); got != want {
		t.Errorf("chain = %q, want %q", got, want)
	}
}

// TestValidateShapeChecks: beside/below/css/size and styles_applied in
// their good forms pass; each bad form names the form it wants.
func TestValidateShapeChecks(t *testing.T) {
	good := writeSession(t, `session: bad
issue: x/y
scenario: basic
session_checks:
  layout: [no_overlap, styles_applied]
steps:
  - id: A1
    title: shapes
    act:
      - wait_for: { beside: [board.search, board.column.backlog] }
    checks:
      - beside: [board.column.backlog, board.column.done]
      - beside: { a: board.column.backlog, b: board.column.done, gap_max: 24 }
      - below: [board.search, board.column.backlog]
      - css: { target: board.column.backlog, property: display, equals: flex }
      - css: { target: board.column.backlog, property: display, in: [flex, grid] }
      - css: { target: board.column.backlog, property: display, not: none }
      - size: { target: board.column.backlog, min_width: 200, max_width: 400.5 }
      - size: { target: board.column.backlog, min_height: 1 }
`)
	s, errs := LoadSession(good)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	v := &Validator{ScenarioDir: filepath.Join(repoDir(t), "playtest", "scenarios")}
	if errs := v.Validate(s); len(errs) != 0 {
		t.Errorf("good shapes: %v", errs)
	}
	if got := s.Steps[0].checks[0].Label(); got != "beside [board.column.backlog, board.column.done]" {
		t.Errorf("label = %q", got)
	}

	bad := writeSession(t, `session: bad
issue: x/y
scenario: basic
steps:
  - id: A1
    title: shapes
    checks:
      - beside: [board.search]
      - below: { a: board.search }
      - beside: [board.search, board.colum.backlog]
      - below: { a: board.search, b: board.column.backlog, gap_max: 3 }
      - beside: { a: board.search, b: board.column.backlog, gap_max: wide }
      - css: { target: board.search, property: display }
      - css: { target: board.search, property: display, equals: flex, not: none }
      - css: { target: board.search, equals: flex }
      - css: { target: board.search, property: display, in: flex }
      - size: { target: board.search }
      - size: { target: board.search, min_width: wide }
      - size: { target: board.search, min_height: 50, max_height: 10 }
`)
	s, errs = LoadSession(bad)
	var all []string
	for _, e := range errs {
		all = append(all, e.Error())
	}
	for _, e := range v.Validate(s) {
		all = append(all, e.Error())
	}
	text := strings.Join(all, "\n")
	for _, want := range []string{
		`bad.yaml:8: beside: want two target ids, [a, b]`,
		`bad.yaml:9: below: want [a, b] or {a, b}`,
		`bad.yaml:10: unknown target "board.colum.backlog"`,
		`bad.yaml:11: below: want gap_max: a number of px (beside only)`,
		`bad.yaml:12: beside: want gap_max: a number of px (beside only)`,
		`bad.yaml:13: css: want {target, property, equals | in: [..] | not}`,
		`bad.yaml:14: css: want {target, property, equals | in: [..] | not}`,
		`bad.yaml:15: css: want {target, property, equals | in: [..] | not}`,
		`bad.yaml:16: css: want in: a list of values`,
		`bad.yaml:17: size: want {target, min_width?, max_width?, min_height?, max_height?} with at least one bound`,
		`bad.yaml:18: size: want min_width: a number of CSS px`,
		`bad.yaml:19: size: min_height 50 is above max_height 10`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing error %q in:\n%s", want, text)
		}
	}
}
