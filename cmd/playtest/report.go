package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Plan is what the last runs mean for an issue: boxes to tick, comments to
// post, and steps with no usable result.
type Plan struct {
	Tick     []string
	Comments []string
	Missing  []string
	// Runs lists the runs the plan read, one line each.
	Runs []string
	// sources are the run dirs the plan read; applying it marks them.
	sources []string
}

// BuildPlan reads the newest auto and judge runs of each session and decides
// per step:
//   - a machine step is ticked when its ux-test passed;
//   - a judgement step is ticked when the owner passed it, and its machine
//     checks (if any) passed in ux-test and in the playtest;
//   - every failure and every opinion becomes a `playtest:` comment.
func BuildPlan(sessions []*Session, runsRoot string) *Plan {
	p := &Plan{}
	for _, s := range sessions {
		autoDir, auto := LatestRun(runsRoot, s.Name, "auto", "")
		judgeDir, judge := LatestRun(runsRoot, s.Name, "judge", "")
		if autoDir != "" {
			p.sources = append(p.sources, autoDir)
			p.Runs = append(p.Runs, fmt.Sprintf("%s ux-test  %s (build %s)", s.Name, filepath.Base(autoDir), auto.Build))
		}
		if judgeDir != "" {
			p.sources = append(p.sources, judgeDir)
			p.Runs = append(p.Runs, fmt.Sprintf("%s playtest %s (build %s)", s.Name, filepath.Base(judgeDir), judge.Build))
		}
		for _, st := range s.Steps {
			var ar, jr *StepResult
			if auto != nil {
				ar = auto.Step(st.ID)
			}
			if judge != nil {
				jr = judge.Step(st.ID)
			}
			if jr != nil && jr.Judge != nil {
				for _, op := range jr.Judge.Opinions {
					p.Comments = append(p.Comments, fmt.Sprintf("playtest: %s opinion on %s (%s): %s", st.ID, op.Target, s.Name, op.Note))
				}
			}
			autoOK := ar != nil && (ar.Status == StatusPass || (ar.Status == StatusPending && len(st.checks) == 0))
			if ar != nil && ar.Status == StatusFail {
				p.Comments = append(p.Comments, fmt.Sprintf("playtest: %s FAIL in ux-test (%s, %s): %s", st.ID, s.Name, auto.Build, firstFailure(ar)))
			}
			if !st.Judgement() {
				switch {
				case ar == nil:
					p.Missing = append(p.Missing, fmt.Sprintf("%s (%s): no ux-test run", st.ID, s.Name))
				case autoOK:
					p.Tick = append(p.Tick, st.ID)
				}
				continue
			}
			switch {
			case jr == nil || jr.Judge == nil || jr.Judge.Status == StatusPending:
				p.Missing = append(p.Missing, fmt.Sprintf("%s (%s): no verdict from the owner (make playtest)", st.ID, s.Name))
			case jr.Status == StatusFail:
				p.Comments = append(p.Comments, fmt.Sprintf("playtest: %s FAIL (owner, %s): %s", st.ID, s.Name, orDash(jr.Judge.Note)))
			case jr.Status == StatusSkip:
				p.Missing = append(p.Missing, fmt.Sprintf("%s (%s): skipped by the owner", st.ID, s.Name))
			case jr.Status == StatusPass && (len(st.checks) == 0 || autoOK):
				p.Tick = append(p.Tick, st.ID)
			case jr.Status == StatusPass:
				p.Missing = append(p.Missing, fmt.Sprintf("%s (%s): the owner passed it, but its ux-test did not", st.ID, s.Name))
			}
		}
	}
	return p
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no note)"
	}
	return s
}

func firstFailure(sr *StepResult) string {
	for _, v := range sr.Viewports {
		if v.Status != StatusFail {
			continue
		}
		if v.Error != "" {
			return v.Viewport + ": " + v.Error
		}
		for _, c := range v.Checks {
			if !c.Pass {
				return fmt.Sprintf("%s: %s (line %d), actual %q", v.Viewport, c.Check, c.Line, c.Actual)
			}
		}
		for _, l := range v.Layout {
			return fmt.Sprintf("%s: layout %s on %s: %s", v.Viewport, l.Rule, l.ID, l.Detail)
		}
		for _, c := range v.Console {
			return v.Viewport + ": console error: " + c
		}
	}
	return "failed"
}

// reportedFile records, per run dir, the comments already posted, so a
// second APPLY doesn't post them again.
const reportedFile = "reported.yaml"

func alreadyPosted(dirs []string) map[string]bool {
	seen := map[string]bool{}
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(d, reportedFile))
		if err != nil {
			continue
		}
		var l []string
		if yaml.Unmarshal(b, &l) == nil {
			for _, c := range l {
				seen[c] = true
			}
		}
	}
	return seen
}

func cmdReport(o opts, out, errw io.Writer) int {
	if o.issue == "" {
		fmt.Fprintln(errw, "playtest report: -issue SLUG is required (make playtest-report ISSUE=<slug>)")
		return 2
	}
	all := loadAll(o, out, errw, false)
	if all == nil {
		return 1
	}
	sel := selectOrFail(o, all, errw)
	if len(sel) == 0 {
		return 1
	}
	p := BuildPlan(sel, o.runs)
	posted := alreadyPosted(p.sources)
	var fresh []string
	for _, c := range p.Comments {
		if !posted[c] {
			fresh = append(fresh, c)
		}
	}
	fmt.Fprintf(out, "== playtest report: %s ==\n", o.issue)
	if repo, err := repoRoot(); err == nil {
		build := BuildID(repo)
		for _, r := range p.Runs {
			stale := ""
			if !strings.HasSuffix(r, "(build "+build+")") {
				stale = "  ← not the current build " + build + "; re-run make ux-test before APPLY"
			}
			fmt.Fprintf(out, "runs: %s%s\n", r, stale)
		}
	}
	fmt.Fprintf(out, "tick (%d): %s\n", len(p.Tick), strings.Join(p.Tick, " "))
	fmt.Fprintf(out, "comments (%d):\n", len(fresh))
	for _, c := range fresh {
		fmt.Fprintln(out, "  "+c)
	}
	if n := len(p.Comments) - len(fresh); n > 0 {
		fmt.Fprintf(out, "  (%d already posted)\n", n)
	}
	if len(p.Missing) > 0 {
		fmt.Fprintf(out, "no result (%d):\n", len(p.Missing))
		for _, m := range p.Missing {
			fmt.Fprintln(out, "  "+m)
		}
	}
	if !o.apply {
		fmt.Fprintln(out, "dry run: add -apply (APPLY=1) to tick and comment through issue-cli")
		return 0
	}
	bin := issueCLI()
	run := func(args ...string) error {
		full := append([]string{"--project", o.project}, args...)
		cmd := exec.Command(bin, full...)
		cmd.Stdout, cmd.Stderr = out, errw
		return cmd.Run()
	}
	if len(p.Tick) > 0 {
		if err := run(append([]string{"check", o.issue}, p.Tick...)...); err != nil {
			fmt.Fprintf(errw, "issue-cli check: %v\n", err)
			return 1
		}
	}
	var done []string
	for _, c := range fresh {
		if err := run("comment", o.issue, "--text", c); err != nil {
			fmt.Fprintf(errw, "issue-cli comment: %v\n", err)
			return 1
		}
		done = append(done, c)
	}
	if len(done) > 0 {
		for _, d := range p.sources {
			prev := alreadyPosted([]string{d})
			list := make([]string, 0, len(prev)+len(done))
			for c := range prev {
				list = append(list, c)
			}
			list = append(list, done...)
			b, _ := yaml.Marshal(list)
			_ = os.WriteFile(filepath.Join(d, reportedFile), b, 0o644)
		}
	}
	fmt.Fprintf(out, "✓ ticked %d, commented %d\n", len(p.Tick), len(done))
	return 0
}
