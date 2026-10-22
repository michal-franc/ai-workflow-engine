package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestRunner gives a runner over the repo's sessions and scenarios, with
// runs in a temp dir and a headless Chrome. It skips without Chrome.
func newTestRunner(t *testing.T) (*Runner, *Browser) {
	t.Helper()
	bins := buildTestBinaries(t)
	if _, err := ChromePath(); err != nil {
		t.Skip(err)
	}
	b, err := NewBrowser(false, 1440, 900)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(b.Close)
	repo := repoDir(t)
	return &Runner{
		Repo: repo, ScenarioDir: filepath.Join(repo, "playtest", "scenarios"),
		RunsRoot: t.TempDir(), Build: "test", Out: io.Discard, bins: bins,
	}, b
}

func loadRepoSession(t *testing.T, name string) *Session {
	t.Helper()
	s, errs := LoadSession(filepath.Join(repoDir(t), "playtest", "sessions", name+".yaml"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return s
}

// TestUXTestEndToEnd runs a real session headless at both viewports and
// checks the run's files.
func TestUXTestEndToEnd(t *testing.T) {
	r, b := newTestRunner(t)
	s := loadRepoSession(t, "board-basics")
	res, dir, err := r.UXTest(b, s, map[string]bool{"TP2": true, "TP5": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(res.Steps))
	}
	for _, st := range res.Steps {
		if st.Status != StatusPass {
			t.Errorf("%s: %s", st.ID, st.Status)
			for _, v := range st.Viewports {
				t.Logf("  %s %s %s %+v %+v %v", v.Viewport, v.Status, v.Error, v.Checks, v.Layout, v.Console)
			}
		}
		if len(st.Viewports) != 2 {
			t.Errorf("%s ran at %d viewports, want 2", st.ID, len(st.Viewports))
		}
		for _, v := range st.Viewports {
			if _, err := os.Stat(v.Screenshot); err != nil {
				t.Errorf("%s %s: no screenshot: %v", st.ID, v.Viewport, err)
			}
		}
	}
	back, err := ReadResults(dir)
	if err != nil || len(back.Steps) != 2 {
		t.Fatalf("results.yaml: %v", err)
	}
}

// TestUXTestReportsFailures: a wrong expectation fails the step with the
// check, the actual value and a log that lists the page's targets.
func TestUXTestReportsFailures(t *testing.T) {
	r, b := newTestRunner(t)
	p := filepath.Join(t.TempDir(), "wrong.yaml")
	os.WriteFile(p, []byte(`session: wrong
issue: x/y
scenario: basic
session_checks:
  viewports: [1440x900]
steps:
  - id: TP1
    title: expects three backlog cards
    setup:
      - open: /p/{project}/board
    checks:
      - text_equals: { target: board.column.backlog.count, text: "3" }
  - id: TP2
    title: clicks something that is not there
    setup:
      - open: /p/{project}/board
      - click: board.card.ui/no-such-card
    checks:
      - visible: board.search
`), 0o644)
	s, errs := LoadSession(p)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	res, _, err := r.UXTest(b, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := res.Steps[0].Viewports[0]
	if res.Steps[0].Status != StatusFail || len(v.Checks) != 1 || v.Checks[0].Actual != "2" {
		t.Errorf("TP1: want a failed check with actual 2, got %+v", v)
	}
	log, _ := os.ReadFile(v.Log)
	if !strings.Contains(string(log), "targets on the page") || !strings.Contains(string(log), "board.column.backlog.count") {
		t.Errorf("failure log has no target dump:\n%s", log)
	}
	v2 := res.Steps[1].Viewports[0]
	if !strings.Contains(v2.Error, "board.card.ui/no-such-card not found") {
		t.Errorf("TP2 error = %q", v2.Error)
	}
}

// pageEval runs a JS expression (returning a JSON string) on the judge's
// page.
func pageEval(t *testing.T, j *judge, expr string, out any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return j.page.Eval(ctx, expr, out)
}

// waitFor polls a JS condition on the judge's page.
func waitFor(t *testing.T, j *judge, what, cond string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if pageEval(t, j, "JSON.stringify(!!("+cond+"))", &ok) == nil && ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("timed out waiting for %s (%s)", what, cond)
}

// TestOverlayEndToEnd drives the owner's overlay the way the owner would:
// it renders the step, highlights targets, records an opinion, passes one
// step (running its machine checks), fails the next with a note and a
// screenshot, and the report turns that into ticks and comments.
func TestOverlayEndToEnd(t *testing.T) {
	r, b := newTestRunner(t)
	s := loadRepoSession(t, "playtest-self")
	auto, _, err := r.UXTest(b, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(j *judge, msg string) {
		var ok any
		if err := pageEval(t, j, fmt.Sprintf(`window.__ptSend(%s).then(function(){}), JSON.stringify(1)`, msg), &ok); err != nil {
			t.Errorf("send %s: %v", msg, err)
		}
	}
	r.judgeHook = func(j *judge) {
		// TP11: the dock shows DO/EXPECT, targets are highlighted.
		waitFor(t, j, "the dock", `document.querySelector('#__pt .dock') && document.querySelector('#__pt .dock').textContent.indexOf('EXPECT') >= 0`)
		waitFor(t, j, "highlights", `document.querySelectorAll('#__pt .hl').length >= 1`)
		waitFor(t, j, "a live check", `document.querySelector('#__pt .chk .pass')`)
		send(j, `{type:'opinion', target:'board.column.backlog', note:'columns could be wider'}`)
		time.Sleep(300 * time.Millisecond)
		send(j, `{type:'pass'}`)
		// TP12 starts on a fresh scenario; the owner does the act (drag).
		waitFor(t, j, "step TP12", `document.querySelector('#__pt .steps li.cur') && document.querySelector('#__pt .steps li.cur').textContent.indexOf('TP12') >= 0 && !document.querySelector('#__pt .busy')`)
		send(j, `{type:'fail', note:'the hover card hides the drop target'}`)
		waitFor(t, j, "done", `document.querySelector('#__pt .dock') && document.querySelector('#__pt .dock').textContent.indexOf('DONE') >= 0`)
		send(j, `{type:'quit'}`)
	}
	res, dir, err := r.Playtest(b, s, auto)
	if err != nil {
		t.Fatal(err)
	}
	tp11, tp12 := res.Step("TP11"), res.Step("TP12")
	if tp11.Status != StatusPass || tp11.Judge == nil || len(tp11.Judge.Checks) != 2 {
		t.Errorf("TP11 = %+v", tp11)
	} else if len(tp11.Judge.Opinions) != 1 || tp11.Judge.Opinions[0].Target != "board.column.backlog" {
		t.Errorf("TP11 opinions = %+v", tp11.Judge.Opinions)
	}
	if tp12.Status != StatusFail || tp12.Judge.Note != "the hover card hides the drop target" {
		t.Errorf("TP12 = %+v", tp12.Judge)
	} else if _, err := os.Stat(tp12.Judge.Screenshot); err != nil {
		t.Errorf("TP12 fail screenshot: %v", err)
	}
	if _, err := ReadResults(dir); err != nil {
		t.Fatal(err)
	}

	plan := BuildPlan([]*Session{s}, r.RunsRoot)
	if got := strings.Join(plan.Tick, " "); got != "TP11" {
		t.Errorf("tick = %q, want TP11", got)
	}
	comments := strings.Join(plan.Comments, "\n")
	for _, want := range []string{"TP11 opinion on board.column.backlog", "TP12 FAIL (owner, playtest-self): the hover card hides the drop target"} {
		if !strings.Contains(comments, want) {
			t.Errorf("missing comment %q in:\n%s", want, comments)
		}
	}
}

// TestStylesAppliedCatchesV048 replays the v0.48.0 break: a lost `}`
// left the rest of style.css inside a max-width: 900px block, so the status
// page rendered unstyled on desktop while every content check passed. With
// that stylesheet swapped in, styles_applied and the step's shape checks
// fail the status page at 1440x900 and its content checks still pass; with
// the current one everything passes.
func TestStylesAppliedCatchesV048(t *testing.T) {
	old, err := os.ReadFile(filepath.Join("testdata", "style-v0.48.0.css"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(swap bool) ([]LayoutViolation, []string) {
		r, b := newTestRunner(t)
		if swap {
			css, _ := json.Marshal(string(old))
			r.pageScripts = []string{`document.addEventListener('DOMContentLoaded', function () {
  var l = document.querySelector('link[href="/static/style.css"]');
  if (!l) return;
  var s = document.createElement('style');
  s.textContent = ` + string(css) + `;
  l.replaceWith(s);
});`}
		}
		s := loadRepoSession(t, "status-page")
		s.Checks.Viewports = []string{"1440x900"}
		s.Checks.Layout = []string{"styles_applied"}
		res, _, err := r.UXTest(b, s, map[string]bool{"TP5": true})
		if err != nil {
			t.Fatal(err)
		}
		st := res.Step("TP5")
		if st == nil || len(st.Viewports) != 1 {
			t.Fatalf("TP5 = %+v", st)
		}
		var shapes []string
		for _, c := range st.Viewports[0].Checks {
			if c.Pass {
				continue
			}
			kind, _, _ := strings.Cut(c.Check, " ")
			if !map[string]bool{"beside": true, "below": true, "css": true, "size": true}[kind] {
				t.Errorf("content check %s failed (%s): the swap should break only the styling", c.Check, c.Actual)
			}
			shapes = append(shapes, c.Check)
		}
		return st.Viewports[0].Layout, shapes
	}
	if v, shapes := run(false); len(v) != 0 || len(shapes) != 0 {
		t.Errorf("current style.css: want no violations, got %+v and failed checks %v", v, shapes)
	}
	v, shapes := run(true)
	if len(v) == 0 {
		t.Fatal("v0.48.0 style.css: styles_applied found nothing")
	}
	if !strings.Contains(strings.Join(shapes, "\n"), "beside [status.waiting.pop, status.working.pop]") {
		t.Errorf("v0.48.0 style.css: want the two-column check to fail, failed: %v", shapes)
	}
	if !strings.Contains(v[0].Detail, "has no CSS rule active at 1440x900") {
		t.Errorf("first violation = %+v", v[0])
	}
	t.Logf("v0.48.0: %d violations, first %s: %s; failed shape checks: %v", len(v), v[0].ID, v[0].Detail, shapes)
}

// TestOverlayMovesOnToTheNextSession: with another session queued, the last
// verdict ends this one without Alt+Q, so the next session's steps start.
func TestOverlayMovesOnToTheNextSession(t *testing.T) {
	r, b := newTestRunner(t)
	s := loadRepoSession(t, "playtest-self")
	auto, _, err := r.UXTest(b, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.moreSessions = true
	r.judgeHook = func(j *judge) {
		waitFor(t, j, "the dock", `document.querySelector('#__pt .dock') && document.querySelector('#__pt .dock').textContent.indexOf('EXPECT') >= 0`)
		var ok any
		pageEval(t, j, `window.__ptSend({type:'pass'}).then(function(){}), JSON.stringify(1)`, &ok)
		waitFor(t, j, "step TP12", `document.querySelector('#__pt .steps li.cur') && document.querySelector('#__pt .steps li.cur').textContent.indexOf('TP12') >= 0 && !document.querySelector('#__pt .busy')`)
		// TP12 wants the owner to drag first; skipping is a verdict too.
		pageEval(t, j, `window.__ptSend({type:'skip'}).then(function(){}), JSON.stringify(1)`, &ok)
	}
	done := make(chan *Results, 1)
	go func() {
		res, _, err := r.Playtest(b, s, auto)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	select {
	case res := <-done:
		if res == nil || res.Step("TP11").Status != StatusPass || res.Step("TP12").Status != StatusSkip {
			t.Errorf("TP11 = %+v, TP12 = %+v", res.Step("TP11"), res.Step("TP12"))
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("Playtest kept waiting for Alt+Q with another session queued")
	}
	if r.windowClosed {
		t.Error("the window was not closed")
	}
}

// TestOverlayTwoSessionsInTheOwnersWindow: make playtest over two sessions
// runs both in the owner's (visible) window. The second session gets a tab
// of its own: adding the overlay bridge to the first tab again fails.
func TestOverlayTwoSessionsInTheOwnersWindow(t *testing.T) {
	r, b := newTestRunner(t)
	s := loadRepoSession(t, "playtest-self")
	auto, _, err := r.UXTest(b, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.visible = true // the owner's window: sessions share one browser target
	skip := func(j *judge, step string) {
		waitFor(t, j, "step "+step, `document.querySelector('#__pt .steps li.cur') && document.querySelector('#__pt .steps li.cur').textContent.indexOf('`+step+`') >= 0 && !document.querySelector('#__pt .busy')`)
		var ok any
		pageEval(t, j, `window.__ptSend({type:'skip'}).then(function(){}), JSON.stringify(1)`, &ok)
	}
	r.moreSessions = true
	r.judgeHook = func(j *judge) { skip(j, "TP11"); skip(j, "TP12") }
	if _, _, err := r.Playtest(b, s, auto); err != nil {
		t.Fatal("first session:", err)
	}
	r.moreSessions = false
	r.judgeHook = func(j *judge) {
		skip(j, "TP11")
		skip(j, "TP12")
		pageEval(t, j, `window.__ptSend({type:'quit'}).then(function(){}), JSON.stringify(1)`, new(any))
	}
	res, _, err := r.Playtest(b, s, auto)
	if err != nil {
		t.Fatal("second session:", err)
	}
	if res.Step("TP11").Status != StatusSkip || res.Step("TP12").Status != StatusSkip {
		t.Errorf("second session: TP11 %s, TP12 %s, want both skipped", res.Step("TP11").Status, res.Step("TP12").Status)
	}
}
