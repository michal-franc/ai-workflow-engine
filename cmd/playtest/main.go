// Command playtest is issue-viewer's own playtest system: an issue's manual
// checks as YAML sessions that drive the board in Chrome (chromedp) against
// a throwaway scenario project.
//
//	playtest check                       validate every session (no browser)
//	playtest ux-test [-issue S] [-s N]   machine checks, headless, every viewport
//	playtest playtest [-issue S] [-s N]  the owner's judgement steps, in an overlay
//	playtest report -issue S [-apply]    tick passed boxes, comment failures
//
// The make targets (playtest-check, ux-test, playtest, playtest-report) wrap
// these. The shared contract behind the format: `issue-cli process playtest`.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/testresults"
)

const usage = `playtest — run issue-viewer playtest sessions (playtest/sessions/*.yaml)

Commands:
  check                        validate every session: schema, actions, checks,
                               targets (internal/testids), scenarios, step ids
  ux-test  [-issue SLUG] [-s SESSION] [-step ID,…] [-repeat N] [-keep] [-headed] [-results FILE]
                               run the machine checks headless at every viewport;
                               exit 1 on any failure; -repeat N diffs N runs;
                               -results writes sessions, steps and times as JSON
                               (default $CI_RESULTS_DIR/e2e.json in local CI)
  playtest [-issue SLUG] [-s SESSION]
                               walk the owner through the judgement steps
                               (expect:) in a Chrome window with the overlay
  report   -issue SLUG [-apply]
                               show what the last runs mean for the issue;
                               -apply ticks passed boxes and comments failures
                               and opinions through issue-cli

Common flags: -sessions DIR, -scenarios DIR, -runs DIR, -project NAME
Runs are kept in .playtest/runs/<session>/<time>-<mode>/ (gitignored).
Contract and guides: issue-cli process playtest
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type opts struct {
	sessions, scenarios, runs string
	issue, session, steps     string
	project                   string
	results                   string
	repeat                    int
	keep, headed, apply       bool
}

func run(args []string, out, errw io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, usage)
		return 0
	}
	cmd := args[0]
	repo, err := repoRoot()
	if err != nil {
		fmt.Fprintln(errw, "playtest:", err)
		return 2
	}
	var o opts
	fs := flag.NewFlagSet("playtest "+cmd, flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.StringVar(&o.sessions, "sessions", filepath.Join(repo, "playtest", "sessions"), "session files")
	fs.StringVar(&o.scenarios, "scenarios", filepath.Join(repo, "playtest", "scenarios"), "scenario fixtures")
	fs.StringVar(&o.runs, "runs", filepath.Join(repo, ".playtest", "runs"), "where runs are kept")
	fs.StringVar(&o.issue, "issue", "", "only sessions for this issue slug")
	fs.StringVar(&o.session, "s", "", "only this session")
	fs.StringVar(&o.steps, "step", "", "only these step ids (comma-separated)")
	fs.StringVar(&o.project, "project", envOr("PLAYTEST_PROJECT", "issue-viewer"), "issue-cli --project for the real issue")
	fs.StringVar(&o.results, "results", resultsDefault(), "ux-test: also write the e2e results (sessions, steps, times) here; default $CI_RESULTS_DIR/e2e.json in local CI")
	fs.IntVar(&o.repeat, "repeat", 1, "run N times and fail if results differ")
	fs.BoolVar(&o.keep, "keep", false, "keep scenario temp dirs")
	fs.BoolVar(&o.headed, "headed", false, "show the browser during ux-test")
	fs.BoolVar(&o.apply, "apply", false, "report: tick and comment through issue-cli")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	switch cmd {
	case "check":
		return cmdCheck(o, out, errw)
	case "ux-test":
		return cmdUXTest(repo, o, out, errw)
	case "playtest":
		return cmdPlaytest(repo, o, out, errw)
	case "report":
		return cmdReport(o, out, errw)
	}
	fmt.Fprintf(errw, "playtest: unknown command %q\n\n%s", cmd, usage)
	return 2
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// repoRoot walks up from the working directory to the go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run from inside the issue-viewer repo (no go.mod found)")
		}
		dir = parent
	}
}

// loadAll loads and validates every session; it prints problems and returns
// nil when there are any.
func loadAll(o opts, out, errw io.Writer, resolve bool) []*Session {
	all, errs := LoadSessions(o.sessions)
	v := &Validator{ScenarioDir: o.scenarios}
	if resolve {
		v.Checklist = issueChecklist(o.project)
	}
	for _, s := range all {
		errs = append(errs, v.Validate(s)...)
	}
	for _, n := range v.Notes {
		fmt.Fprintln(out, "note:", n)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(errw, e)
		}
		fmt.Fprintf(errw, "✗ %d problem(s) in %s\n", len(errs), o.sessions)
		return nil
	}
	return all
}

func cmdCheck(o opts, out, errw io.Writer) int {
	all := loadAll(o, out, errw, true)
	if all == nil {
		return 1
	}
	steps := 0
	for _, s := range all {
		steps += len(s.Steps)
	}
	fmt.Fprintf(out, "✓ %d session(s), %d step(s) valid\n", len(all), steps)
	return 0
}

func selectOrFail(o opts, all []*Session, errw io.Writer) []*Session {
	sel := SelectSessions(all, o.session, o.issue)
	if len(sel) == 0 {
		fmt.Fprintf(errw, "playtest: no session matches (issue=%q session=%q) in %s\n", o.issue, o.session, o.sessions)
	}
	return sel
}

func stepFilter(s string) map[string]bool {
	if s == "" {
		return nil
	}
	m := map[string]bool{}
	for _, id := range strings.Split(s, ",") {
		m[strings.TrimSpace(id)] = true
	}
	return m
}

func cmdUXTest(repo string, o opts, out, errw io.Writer) int {
	all := loadAll(o, out, errw, false)
	if all == nil {
		return 1
	}
	sel := selectOrFail(o, all, errw)
	if len(sel) == 0 {
		return 1
	}
	b, err := NewBrowser(o.headed, 1440, 900)
	if err != nil {
		fmt.Fprintln(errw, "playtest:", err)
		return 2
	}
	defer b.Close()
	r := &Runner{Repo: repo, ScenarioDir: o.scenarios, RunsRoot: o.runs, Build: BuildID(repo), Keep: o.keep, Out: out, OwnIssue: OwnIssue(repo, o.issue)}
	code := 0
	e2e := testresults.Playtest{Started: time.Now()}
	defer func() {
		if note := r.ShotsNote(); note != "" {
			fmt.Fprintln(out, note)
		}
	}()
	defer func() {
		if o.results == "" {
			return
		}
		e2e.Finished = time.Now()
		if err := writeE2E(o.results, e2e); err != nil {
			fmt.Fprintln(errw, "playtest: e2e results:", err)
		}
	}()
	for _, s := range sel {
		var first *Results
		for i := range max(o.repeat, 1) {
			start := time.Now()
			res, dir, err := r.UXTest(b, s, stepFilter(o.steps))
			if err != nil {
				fmt.Fprintln(errw, "playtest:", err)
				return 2
			}
			e2e.Sessions = append(e2e.Sessions, e2eSession(res, time.Since(start)))
			fmt.Fprintf(out, "results: %s\n", filepath.Join(dir, "results.yaml"))
			for _, st := range res.Steps {
				if st.Status == StatusFail {
					code = 1
				}
			}
			if i == 0 {
				first = res
			} else if d := Diff(first, res); d != "" {
				fmt.Fprintf(errw, "✗ %s: run %d differs from run 1\n%s\n", s.Name, i+1, d)
				code = 1
			} else {
				fmt.Fprintf(out, "✓ %s: run %d matches run 1\n", s.Name, i+1)
			}
		}
	}
	if code == 0 {
		fmt.Fprintln(out, "✓ ux-test passed")
	} else {
		fmt.Fprintln(errw, "✗ ux-test failed")
	}
	return code
}

// issueCLI is the real issue-cli, used to read and update the real issue.
func issueCLI() string { return envOr("PLAYTEST_ISSUE_CLI", "issue-cli") }

// issueChecklist reads an issue's checkbox ids through the real issue-cli.
// It returns ok=false when the issue can't be resolved here.
func issueChecklist(project string) func(string) (map[string]string, bool) {
	cache := map[string]map[string]string{}
	return func(slug string) (map[string]string, bool) {
		if ids, ok := cache[slug]; ok {
			return ids, ids != nil
		}
		cache[slug] = nil
		bin, err := exec.LookPath(issueCLI())
		if err != nil {
			return nil, false
		}
		cmd := exec.Command(bin, "--project", project, "--json", "checklist", slug)
		raw, err := cmd.Output()
		if err != nil {
			return nil, false
		}
		var doc struct {
			Items []struct {
				ID      string `json:"id"`
				Section string `json:"section"`
			} `json:"items"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			return nil, false
		}
		ids := map[string]string{}
		for _, it := range doc.Items {
			if it.ID != "" {
				ids[strings.ToUpper(it.ID)] = it.Section
			}
		}
		cache[slug] = ids
		return ids, true
	}
}
