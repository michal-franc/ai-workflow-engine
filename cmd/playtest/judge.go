package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

//go:embed assets/overlay.js
var overlayJS string

// judgeMsg is what the overlay sends through window.__ptSend.
type judgeMsg struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Note   string `json:"note"`
	Target string `json:"target"`
}

// judge runs one session's judgement steps in the visible window.
type judge struct {
	r     *Runner
	s     *Session
	auto  *Results
	res   *Results
	dir   string
	page  *Page
	log   io.Writer
	steps []*Step // judgement steps, in order

	mu    sync.Mutex
	cur   int // index into steps; -1 when done
	busy  bool
	err   string
	snaps map[string]string
	sc    *Scenario
}

// view is the state the overlay renders.
func (j *judge) view() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	var steps []map[string]any
	judged, passedAuto, autoSteps := 0, 0, 0
	for _, st := range j.s.Steps {
		row := map[string]any{"id": st.ID, "title": st.Title, "you": st.Judgement()}
		if j.auto != nil {
			if ar := j.auto.Step(st.ID); ar != nil && ar.Status != StatusPending {
				row["auto"] = ar.Status
				if !st.Judgement() {
					autoSteps++
					if ar.Status == StatusPass {
						passedAuto++
					}
				}
			}
		}
		if jr := j.res.Step(st.ID); jr != nil && jr.Judge != nil {
			row["judged"] = jr.Judge.Status
			judged++
		}
		steps = append(steps, row)
	}
	v := map[string]any{
		"session":  j.s.Name,
		"title":    j.s.Title,
		"steps":    steps,
		"busy":     j.busy,
		"error":    j.err,
		"current":  j.cur,
		"done":     j.cur < 0,
		"results":  filepath.Join(j.dir, "results.yaml"),
		"snaps":    j.snaps,
		"progress": fmt.Sprintf("judged %d/%d · auto %d/%d passed (ux-test %s)", judged, len(j.steps), passedAuto, autoSteps, j.autoName()),
	}
	if j.cur >= 0 {
		st := j.steps[j.cur]
		var dom []map[string]any
		var runner []string
		for _, c := range st.checks {
			if domChecks[c.Kind] {
				dom = append(dom, map[string]any{"kind": c.Kind, "arg": c.Arg, "label": c.Label()})
			} else {
				runner = append(runner, c.Label())
			}
		}
		v["step"] = map[string]any{
			"id": st.ID, "title": st.Title, "do": st.Do, "expect": st.Expect, "why": st.Why,
			"targets": st.targets, "checks": dom, "runner_checks": runner,
		}
	}
	return v
}

func (j *judge) autoName() string {
	if j.auto == nil {
		return "none"
	}
	return j.auto.Started
}

func (j *judge) logf(format string, a ...any) {
	fmt.Fprintf(j.log, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, a...)...)
}

// refresh asks the overlay to fetch the state again (best effort: a page
// that is navigating re-reads it on load anyway).
func (j *judge) refresh(ctx context.Context) {
	_ = j.page.Eval(ctx, `JSON.stringify(window.__ptOverlay ? (window.__ptOverlay.refresh(), 1) : 0)`, nil)
}

// start resets the scenario and brings step i to its starting state: the
// runner replays the setup (and any `from` chain); the owner does the act.
func (j *judge) start(ctx context.Context, i int) {
	j.mu.Lock()
	j.cur, j.busy, j.err = i, true, ""
	j.snaps = map[string]string{}
	old := j.sc
	j.sc = nil
	j.mu.Unlock()
	j.refresh(ctx)
	if old != nil {
		old.Stop(false)
	}
	st := j.steps[i]
	j.logf("step %s: starting scenario %s", st.ID, j.s.Scenario)
	bins, err := j.r.binaries()
	var sc *Scenario
	if err == nil {
		sc, err = StartScenario(j.r.ScenarioDir, j.s.Scenario, bins, j.log)
	}
	if err != nil {
		j.setErr("scenario: %v", err)
		return
	}
	j.mu.Lock()
	j.sc = sc
	j.mu.Unlock()
	env := &stepEnv{sc: sc, page: j.page, snaps: j.snaps, log: j.log}
	acts := chain(j.s, st)
	opened := false
	for _, a := range acts {
		opened = opened || a.Kind == "open"
	}
	if !opened {
		acts = append([]Action{{Kind: "open", Arg: scalarNode("/p/{project}/"), Line: st.Line}}, acts...)
	}
	for _, a := range acts {
		if err := env.do(ctx, a); err != nil {
			j.setErr("setup failed — %s at line %d: %v\nFix the session, or Skip this step.", a.Kind, a.Line, err)
			return
		}
	}
	j.mu.Lock()
	j.busy = false
	j.mu.Unlock()
	j.refresh(ctx)
}

func (j *judge) setErr(format string, a ...any) {
	j.mu.Lock()
	j.busy, j.err = false, fmt.Sprintf(format, a...)
	j.mu.Unlock()
	j.logf("error: %s", j.err)
	j.refresh(context.Background())
}

// shot saves a screenshot of the page without the overlay.
func (j *judge) shot(ctx context.Context, name string) string {
	_ = j.page.Eval(ctx, `JSON.stringify(window.__ptOverlay && (window.__ptOverlay.hide(), 1))`, nil)
	defer func() {
		_ = j.page.Eval(ctx, `JSON.stringify(window.__ptOverlay && (window.__ptOverlay.show(), 1))`, nil)
	}()
	png, err := j.page.Screenshot(ctx, false)
	if err != nil {
		return ""
	}
	rel := filepath.Join("shots", name+".png")
	if os.WriteFile(filepath.Join(j.dir, rel), png, 0o644) != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// record stores the owner's verdict and the step's machine checks.
func (j *judge) record(ctx context.Context, status, note string) {
	st := j.steps[j.cur]
	jr := &JudgeResult{Status: status, Note: note}
	if status == StatusFail {
		jr.Screenshot = j.shot(ctx, st.ID+"-fail")
	}
	if status != StatusSkip && len(st.checks) > 0 && j.sc != nil {
		env := &stepEnv{sc: j.sc, page: j.page, snaps: j.snaps, log: j.log}
		jr.Checks = env.settle(ctx, st.checks, 3*time.Second)
	}
	sr := j.res.Step(st.ID)
	if sr.Judge != nil {
		jr.Opinions = sr.Judge.Opinions
	}
	sr.Judge = jr
	sr.Status = status
	if status == StatusPass {
		for _, c := range jr.Checks {
			if !c.Pass {
				sr.Status = StatusFail
				jr.Note = strings.TrimSpace(jr.Note + " (owner passed it, but machine check failed: " + c.Check + ", actual " + c.Actual + ")")
			}
		}
	}
	j.logf("step %s: %s %s", st.ID, sr.Status, note)
	_ = j.res.Write(j.dir)
}

// next returns the next step without a verdict after cur, or -1.
func (j *judge) next() int {
	for k := 1; k <= len(j.steps); k++ {
		i := (j.cur + k) % len(j.steps)
		if sr := j.res.Step(j.steps[i].ID); sr.Judge == nil {
			return i
		}
	}
	return -1
}

// Playtest walks the owner through one session's judgement steps. It
// returns when the owner quits, closes the window, or presses Ctrl+C.
func (r *Runner) Playtest(b *Browser, s *Session, auto *Results) (*Results, string, error) {
	j := &judge{r: r, s: s, auto: auto, snaps: map[string]string{}}
	for _, st := range s.Steps {
		if st.Judgement() {
			j.steps = append(j.steps, st)
		}
	}
	if len(j.steps) == 0 {
		fmt.Fprintf(r.Out, "%s: no judgement steps (expect:), nothing for the owner\n", s.Name)
		return nil, "", nil
	}
	dir, err := r.newRunDir(s.Name, "judge")
	if err != nil {
		return nil, "", err
	}
	j.dir = dir
	logf, err := os.Create(filepath.Join(dir, "logs", "playtest.log"))
	if err != nil {
		return nil, "", err
	}
	defer logf.Close()
	j.log = logf
	j.res = &Results{Session: s.Name, Issue: s.Issue, Title: s.Title, Mode: "judge", Build: r.Build, Started: time.Now().Format(time.RFC3339)}
	for _, st := range j.steps {
		j.res.Steps = append(j.res.Steps, &StepResult{ID: st.ID, Title: st.Title, Judgement: true, HasChecks: len(st.checks) > 0, Status: StatusPending, Proves: st.Proves})
	}
	_ = j.res.Write(dir)

	pg, err := b.NewPage(nil, checksJS, overlayJS)
	if err != nil {
		return nil, "", err
	}
	j.page = pg
	cmds := make(chan judgeMsg, 16)
	expose := chromedp.ExposeFunc("__ptSend", func(ctx context.Context, m judgeMsg) (map[string]any, error) {
		if m.Type == "hello" {
			return j.view(), nil
		}
		select {
		case cmds <- m:
		default:
		}
		return map[string]any{"ok": true}, nil
	})
	ctx := context.Background()
	if err := pg.Do(ctx, expose); err != nil {
		return nil, "", fmt.Errorf("overlay bridge: %v", err)
	}
	defer func() {
		if j.sc != nil {
			j.sc.Stop(r.Keep)
		}
	}()

	fmt.Fprintf(r.Out, "\n== playtest %s: %d judgement step(s) in the Chrome window ==\n", s.Name, len(j.steps))
	fmt.Fprintln(r.Out, "   Alt+P pass · Alt+F fail · Alt+S skip · Alt+R replay · Alt+N opinion · F10 dock · Alt+Q quit")
	j.start(ctx, 0)
	if r.judgeHook != nil {
		go r.judgeHook(j)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	alive := time.NewTicker(time.Second)
	defer alive.Stop()
	for {
		select {
		case m := <-cmds:
			switch m.Type {
			case StatusPass, StatusFail, StatusSkip:
				if j.cur < 0 {
					continue
				}
				j.record(ctx, m.Type, m.Note)
				if n := j.next(); n >= 0 {
					j.start(ctx, n)
				} else if r.moreSessions {
					fmt.Fprintf(r.Out, "every judgement step of %s has a verdict; on to the next session\n", s.Name)
					return j.finish()
				} else {
					j.mu.Lock()
					j.cur = -1
					j.mu.Unlock()
					j.refresh(ctx)
					fmt.Fprintf(r.Out, "every judgement step of %s has a verdict; Alt+Q (or close the window) to finish\n", s.Name)
				}
			case "replay":
				if j.cur >= 0 {
					j.start(ctx, j.cur)
				}
			case "goto":
				for i, st := range j.steps {
					if st.ID == m.ID {
						j.start(ctx, i)
					}
				}
			case "opinion":
				if strings.TrimSpace(m.Note) == "" {
					continue
				}
				id := ""
				if j.cur >= 0 {
					id = j.steps[j.cur].ID
				} else {
					id = j.steps[len(j.steps)-1].ID
				}
				sr := j.res.Step(id)
				if sr.Judge == nil {
					sr.Judge = &JudgeResult{Status: StatusPending}
				}
				op := Opinion{Target: m.Target, Note: strings.TrimSpace(m.Note), Screenshot: j.shot(ctx, fmt.Sprintf("%s-opinion-%d", id, len(sr.Judge.Opinions)+1))}
				sr.Judge.Opinions = append(sr.Judge.Opinions, op)
				j.logf("opinion on %s (%s): %s", op.Target, id, op.Note)
				_ = j.res.Write(dir)
			case "quit":
				return j.finish()
			}
		case <-sig:
			fmt.Fprintln(r.Out, "interrupted; saving what was judged")
			return j.finish()
		case <-alive.C:
			if _, err := chromedp.Targets(b.ctx); err != nil || pg.ctx.Err() != nil {
				fmt.Fprintln(r.Out, "the window was closed; saving what was judged")
				r.windowClosed = true
				return j.finish()
			}
		}
	}
}

func (j *judge) finish() (*Results, string, error) {
	// A step that only got opinions keeps them, but has no verdict.
	for _, sr := range j.res.Steps {
		if sr.Judge != nil && sr.Judge.Status == StatusPending {
			sr.Status = StatusPending
		}
	}
	if err := j.res.Write(j.dir); err != nil {
		return nil, "", err
	}
	return j.res, j.dir, nil
}

func cmdPlaytest(repo string, o opts, out, errw io.Writer) int {
	all := loadAll(o, out, errw, false)
	if all == nil {
		return 1
	}
	sel := selectOrFail(o, all, errw)
	if len(sel) == 0 {
		return 1
	}
	r := &Runner{Repo: repo, ScenarioDir: o.scenarios, RunsRoot: o.runs, Build: BuildID(repo), Keep: o.keep, Out: out}
	// The owner sees each step's machine result: reuse the last ux-test of
	// this build, or run it now.
	autos := map[string]*Results{}
	var headless *Browser
	for _, s := range sel {
		if _, res := LatestRun(o.runs, s.Name, "auto", r.Build); res != nil {
			autos[s.Name] = res
			continue
		}
		if headless == nil {
			var err error
			if headless, err = NewBrowser(false, 1440, 900); err != nil {
				fmt.Fprintln(errw, "playtest:", err)
				return 2
			}
		}
		fmt.Fprintf(out, "no ux-test of this build for %s; running it first\n", s.Name)
		res, _, err := r.UXTest(headless, s, nil)
		if err != nil {
			fmt.Fprintln(errw, "playtest:", err)
			return 2
		}
		autos[s.Name] = res
	}
	if headless != nil {
		headless.Close()
	}
	b, err := NewBrowser(true, 1440, 900)
	if err != nil {
		fmt.Fprintln(errw, "playtest:", err)
		return 2
	}
	defer b.Close()
	for i, s := range sel {
		if r.windowClosed {
			var left []string
			for _, rest := range sel[i:] {
				if hasJudgement(rest) {
					left = append(left, rest.Name)
				}
			}
			if len(left) > 0 {
				fmt.Fprintf(out, "the window was closed before %s; run make playtest again to judge them\n", strings.Join(left, ", "))
			}
			break
		}
		r.moreSessions = false
		for _, rest := range sel[i+1:] {
			r.moreSessions = r.moreSessions || hasJudgement(rest)
		}
		res, dir, err := r.Playtest(b, s, autos[s.Name])
		if err != nil {
			fmt.Fprintln(errw, "playtest:", err)
			return 2
		}
		if res == nil {
			continue
		}
		fmt.Fprintf(out, "results: %s\n", filepath.Join(dir, "results.yaml"))
		for _, st := range res.Steps {
			note := ""
			if st.Judge != nil && st.Judge.Note != "" {
				note = " — " + st.Judge.Note
			}
			fmt.Fprintf(out, "  %s %s%s\n", st.ID, st.Status, note)
		}
	}
	fmt.Fprintln(out, "next: make playtest-report ISSUE=<slug> (add APPLY=1 to tick and comment)")
	return 0
}

// hasJudgement reports whether the session has a step for the owner.
func hasJudgement(s *Session) bool {
	for _, st := range s.Steps {
		if st.Judgement() {
			return true
		}
	}
	return false
}
