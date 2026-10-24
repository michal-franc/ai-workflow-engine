package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner holds what every mode shares: where things live, the built
// binaries, and the browser.
type Runner struct {
	Repo        string
	ScenarioDir string
	RunsRoot    string
	Build       string
	Keep        bool
	Out         io.Writer
	// OwnIssue is the issue this run is for (-issue, else the work/<slug>
	// branch). An auto run of another issue's session keeps no shots except
	// failing ones; "" keeps every shot. See OwnIssue().
	OwnIssue string

	// shotsSkipped: a shot was left out for another issue's session, said
	// once per invocation.
	shotsSkipped bool

	bins *Binaries
	// judgeHook lets a test drive the overlay the way the owner would.
	judgeHook func(*judge)
	// pageScripts run in every ux-test document after the runner's own; a
	// test swaps the stylesheet with one.
	pageScripts []string
	// moreSessions: another session with judgement steps follows, so Playtest
	// moves on once every step has a verdict instead of waiting for Alt+Q.
	moreSessions bool
	// windowClosed: the owner closed the Chrome window; later sessions can't
	// run in it.
	windowClosed bool
}

// keepsShots: an auto run keeps the shots of the run's own issue's
// sessions, and every shot when the run has no own issue.
func (r *Runner) keepsShots(s *Session) bool {
	return r.OwnIssue == "" || s.Issue == r.OwnIssue
}

// ShotsNote is the one line ux-test prints when it left shots out.
func (r *Runner) ShotsNote() string {
	if !r.shotsSkipped {
		return ""
	}
	return fmt.Sprintf("shots kept for %s only (other sessions keep results.yaml and failing shots)", r.OwnIssue)
}

// OwnIssue is the issue a run is for: the -issue flag, else the slug of the
// repo's work/<slug> branch, else "".
func OwnIssue(repo, flag string) string {
	if flag != "" {
		return flag
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	if slug, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "work/"); ok {
		return slug
	}
	return ""
}

// stepTimeout bounds one step at one viewport.
const stepTimeout = 90 * time.Second

// newRunDir creates .playtest/runs/<session>/<time>-<mode>/.
func (r *Runner) newRunDir(session, mode string) (string, error) {
	dir := filepath.Join(r.RunsRoot, session, time.Now().Format("20060102-150405.000")+"-"+mode)
	for _, d := range []string{"logs", "shots"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// binaries builds issue-viewer and issue-cli once per invocation.
func (r *Runner) binaries() (*Binaries, error) {
	if r.bins != nil {
		return r.bins, nil
	}
	dir := filepath.Join(r.RunsRoot, ".bin")
	fmt.Fprintf(r.Out, "building issue-viewer and issue-cli from %s …\n", r.Repo)
	b, err := BuildBinaries(r.Repo, dir, r.Out)
	if err != nil {
		return nil, err
	}
	r.bins = b
	return b, nil
}

// chain returns the actions that bring a step to its state: the setup and
// act of every `from` ancestor (not their checks), then its own setup.
func chain(s *Session, st *Step) []Action {
	var acts []Action
	if st.From != "" {
		if parent := s.Step(st.From); parent != nil {
			acts = append(acts, chain(s, parent)...)
			acts = append(acts, parent.act...)
		}
	}
	return append(acts, st.setup...)
}

// UXTest runs every step's machine checks with no human, at every viewport,
// each in a fresh scenario and browser context. only limits it to some step
// ids.
func (r *Runner) UXTest(b *Browser, s *Session, only map[string]bool) (*Results, string, error) {
	bins, err := r.binaries()
	if err != nil {
		return nil, "", err
	}
	dir, err := r.newRunDir(s.Name, "auto")
	if err != nil {
		return nil, "", err
	}
	res := &Results{Session: s.Name, Issue: s.Issue, Title: s.Title, Mode: "auto", Build: r.Build, Started: time.Now().Format(time.RFC3339)}
	fmt.Fprintf(r.Out, "\n== ux-test %s (%s) ==\n", s.Name, s.Issue)
	for _, st := range s.Steps {
		if len(only) > 0 && !only[st.ID] {
			continue
		}
		sr := &StepResult{ID: st.ID, Title: st.Title, Judgement: st.Judgement(), HasChecks: len(st.checks) > 0, Status: StatusPass, Proves: st.Proves}
		res.Steps = append(res.Steps, sr)
		if len(st.checks) == 0 {
			// A judgement-only step: nothing for the machine to check, but
			// its setup must still run cleanly.
			sr.Status = StatusPending
		}
		stepStart := time.Now()
		for _, vp := range s.Viewports() {
			vr := r.runStep(b, bins, s, st, vp, dir)
			sr.Viewports = append(sr.Viewports, vr)
			if vr.Status == StatusFail {
				sr.Status = StatusFail
			}
		}
		sr.DurationMS = time.Since(stepStart).Milliseconds()
		r.printStep(sr, dir)
	}
	if err := res.Write(dir); err != nil {
		return nil, "", err
	}
	return res, dir, nil
}

func (r *Runner) printStep(sr *StepResult, dir string) {
	mark := map[string]string{StatusPass: "✓", StatusFail: "✗", StatusPending: "·"}[sr.Status]
	note := ""
	if sr.Status == StatusPending {
		note = "  (judgement only: setup ran, the owner checks it)"
	}
	fmt.Fprintf(r.Out, "%s %s %s%s\n", mark, sr.ID, sr.Title, note)
	for _, vr := range sr.Viewports {
		if vr.Status != StatusFail {
			continue
		}
		fmt.Fprintf(r.Out, "    at %s:\n", vr.Viewport)
		if vr.Error != "" {
			fmt.Fprintf(r.Out, "      error: %s\n", vr.Error)
		}
		for _, c := range vr.Checks {
			if !c.Pass {
				fmt.Fprintf(r.Out, "      check (line %d) %s — actual: %q %s\n", c.Line, c.Check, c.Actual, c.Detail)
			}
		}
		for _, l := range vr.Layout {
			fmt.Fprintf(r.Out, "      layout %s on %s: %s\n", l.Rule, l.ID, l.Detail)
		}
		for _, c := range vr.Console {
			fmt.Fprintf(r.Out, "      console error: %s\n", c)
		}
		if vr.Screenshot != "" {
			fmt.Fprintf(r.Out, "      screenshot: %s\n", filepath.Join(dir, vr.Screenshot))
		}
		if vr.Log != "" {
			fmt.Fprintf(r.Out, "      log: %s\n", vr.Log)
		}
	}
}

// runStep runs one step at one viewport in a fresh scenario.
func (r *Runner) runStep(b *Browser, bins *Binaries, s *Session, st *Step, vp Viewport, dir string) *ViewportResult {
	vr := &ViewportResult{Viewport: vp.String(), Status: StatusPass}
	name := fmt.Sprintf("%s-%s", st.ID, vp)
	logPath := filepath.Join(dir, "logs", name+".log")
	vr.Log = logPath
	logf, err := os.Create(logPath)
	if err != nil {
		vr.Status, vr.Error = StatusFail, err.Error()
		return vr
	}
	defer logf.Close()
	fail := func(format string, a ...any) *ViewportResult {
		vr.Status, vr.Error = StatusFail, fmt.Sprintf(format, a...)
		fmt.Fprintf(logf, "FAIL: %s\n", vr.Error)
		return vr
	}

	sc, err := StartScenario(r.ScenarioDir, s.Scenario, bins, logf)
	if err != nil {
		return fail("scenario %s: %v", s.Scenario, err)
	}
	defer sc.Stop(r.Keep)
	fmt.Fprintf(logf, "scenario %s at %s (board %s)\n", s.Scenario, sc.Root, sc.Base)

	pg, err := b.NewPage(&vp, append([]string{checksJS, noMotionJS}, r.pageScripts...)...)
	if err != nil {
		return fail("browser: %v", err)
	}
	defer pg.Close()

	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()
	env := &stepEnv{sc: sc, page: pg, snaps: map[string]string{}, log: logf}

	// shoot saves the page; screenshot: is relative to the run folder, so
	// a moved run still finds it.
	shoot := func() {
		if s.Checks.Screenshots == "none" {
			return
		}
		if !r.keepsShots(s) && vr.Status != StatusFail {
			r.shotsSkipped = true
			return
		}
		if png, err := pg.Screenshot(ctx, true); err == nil {
			rel := filepath.Join("shots", name+".png")
			if os.WriteFile(filepath.Join(dir, rel), png, 0o644) == nil {
				vr.Screenshot = filepath.ToSlash(rel)
			}
		}
	}
	for _, a := range append(chain(s, st), st.act...) {
		if err := env.do(ctx, a); err != nil {
			vr.Status = StatusFail
			shoot()
			dumpTargets(ctx, pg, logf)
			return fail("%s at %s:%d: %v", a.Kind, s.File, a.Line, err)
		}
	}
	vr.Checks = env.settle(ctx, st.checks, time.Duration(s.Settle())*time.Millisecond)
	for _, c := range vr.Checks {
		if !c.Pass {
			vr.Status = StatusFail
		}
	}
	if len(s.Checks.Layout) > 0 {
		var v []LayoutViolation
		if err := pg.Call(ctx, &v, "layout", s.Checks.Layout, s.Checks.LayoutIgnore); err != nil {
			return fail("layout: %v", err)
		}
		vr.Layout = v
		if len(v) > 0 {
			vr.Status = StatusFail
		}
	}
	if s.Checks.Console != "off" {
		vr.Console = consoleErrors(pg.Console(), s.Checks.ConsoleIgnore)
		if len(vr.Console) > 0 {
			vr.Status = StatusFail
		}
	}
	shoot()
	if vr.Status == StatusFail {
		dumpTargets(ctx, pg, logf)
	}
	if vr.Status == StatusFail && vr.Error == "" {
		var failed []string
		for _, c := range vr.Checks {
			if !c.Pass {
				failed = append(failed, c.Check)
			}
		}
		fmt.Fprintf(logf, "FAIL: %s\n", strings.Join(failed, "; "))
	}
	return vr
}

// dumpTargets writes every test id on the page (rect, visible, enabled,
// text) to a failing step's log, so a wrong or missing id is easy to see.
func dumpTargets(ctx context.Context, pg *Page, w io.Writer) {
	var ts []struct {
		ID      string `json:"id"`
		Rect    []int  `json:"rect"`
		Visible bool   `json:"visible"`
		Enabled bool   `json:"enabled"`
		Text    string `json:"text"`
	}
	if err := pg.Call(ctx, &ts, "targets"); err != nil {
		fmt.Fprintf(w, "targets: %v\n", err)
		return
	}
	fmt.Fprintf(w, "targets on the page (%d):\n", len(ts))
	for _, t := range ts {
		fmt.Fprintf(w, "  %-50s rect=%v visible=%v enabled=%v %q\n", t.ID, t.Rect, t.Visible, t.Enabled, t.Text)
	}
}
