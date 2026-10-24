// Package lint checks a project's workflow.yaml and its board for problems
// the engine would otherwise meet silently: undated issues, unknown keys,
// an old template stamp, placeholders nothing fills, gates on sections no
// template writes. Every finding is a warning. issue-cli health lint prints
// them, the health family lint records their count (the lint score
// component) and /health lists them live. See docs/workbench-files.md,
// "The lint family".
package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Finding is one problem a check found. Every finding is a warning: lint
// lowers the health score, it never fails a run.
type Finding struct {
	Check string `json:"check"`
	// Where is "workflow.yaml:<line>", "workflow.yaml" or an issue slug.
	Where   string `json:"where"`
	Message string `json:"message"`
	// Fix is the command to run or the edit to make; empty when the
	// message says it.
	Fix string `json:"fix,omitempty"`
}

// Check is one named check.
type Check struct {
	Name string
	Help string
	Run  func(env *Env) []Finding
}

// checks is every check, in the order lint runs and prints them. A new
// check is one entry here.
var checks = []Check{
	{"undated", "issues without a created: date (the criteria_proven gate counts them as new)", checkUndated},
	{"workflow", "types:, workbench:, ci: and health: problems (the warnings process workflow prints)", checkWorkflow},
	{"schema", "workflow.yaml keys the parser doesn't know, missing statuses:/transitions:, deprecated keys", checkSchema},
	{"template", "the template: stamp against the template bundled in this issue-cli", checkTemplate},
	{"placeholders", "{{name}} placeholders the field never fills, so the agent sees them as written", checkPlaceholders},
	{"sections", "checkbox and test-plan gates on sections no template writes the way the gate needs", checkSections},
}

// Checks lists every check in run order.
func Checks() []Check { return append([]Check(nil), checks...) }

// Env is what the checks read.
type Env struct {
	Project  *tracker.Project
	Workflow *tracker.WorkflowConfig
	// File is the workflow.yaml the project loads, "" for the built-in
	// default; Raw is its content.
	File   string
	Raw    []byte
	Issues []*tracker.Issue
	Now    time.Time
}

// Load reads the project's workflow.yaml and issues.
func Load(proj *tracker.Project, now time.Time) (*Env, error) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil, fmt.Errorf("loading issues: %w", err)
	}
	return NewEnv(proj, issues, now), nil
}

// NewEnv is Load for a caller that already has the issues (the Status page).
func NewEnv(proj *tracker.Project, issues []*tracker.Issue, now time.Time) *Env {
	env := &Env{Project: proj, Workflow: proj.LoadWorkflow(), Issues: issues, Now: now}
	for _, f := range []string{proj.WorkflowFile, "workflow.yaml"} {
		if f == "" {
			continue
		}
		if raw, err := os.ReadFile(f); err == nil {
			env.File, env.Raw = f, raw
			break
		}
	}
	return env
}

// Report is a lint run's findings and their counts.
type Report struct {
	Project  string    `json:"project"`
	Checks   []string  `json:"checks"`
	Findings []Finding `json:"findings"`
	Warnings int       `json:"warnings"`
}

// Run runs the named checks (every check when names is empty).
func Run(env *Env, names ...string) (*Report, error) {
	run := checks
	if len(names) > 0 {
		run = nil
		for _, n := range names {
			c, ok := find(n)
			if !ok {
				return nil, fmt.Errorf("unknown check %q (checks: %s)", n, strings.Join(Names(), ", "))
			}
			run = append(run, c)
		}
	}
	rep := &Report{Project: env.Project.Slug, Findings: []Finding{}}
	for _, c := range run {
		rep.Checks = append(rep.Checks, c.Name)
		for _, f := range c.Run(env) {
			f.Check = c.Name
			rep.Findings = append(rep.Findings, f)
			rep.Warnings++
		}
	}
	return rep, nil
}

// Names lists every check name in run order.
func Names() []string {
	var out []string
	for _, c := range checks {
		out = append(out, c.Name)
	}
	return out
}

func find(name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

// where names a workflow.yaml line, or the file alone for line 0.
func (env *Env) where(line int) string {
	name := "workflow.yaml"
	if env.File != "" {
		name = filepath.Base(env.File)
	}
	if line <= 0 {
		return name
	}
	return fmt.Sprintf("%s:%d", name, line)
}

// lineOf is the first non-comment line of workflow.yaml containing needle,
// or 0.
func (env *Env) lineOf(needle string) int {
	if needle == "" {
		return 0
	}
	for i, line := range strings.Split(string(env.Raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 0
}
