package main

import (
	"bufio"
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Results is a run's results.yaml, in the shared playtest shape: per step
// its id, mode, status, each check, notes and opinions, screenshots.
type Results struct {
	Session string        `yaml:"session"`
	Issue   string        `yaml:"issue"`
	Title   string        `yaml:"title,omitempty"`
	Mode    string        `yaml:"mode"` // auto (ux-test) or judge (playtest)
	Build   string        `yaml:"build"`
	Started string        `yaml:"started"`
	Steps   []*StepResult `yaml:"steps"`
}

// Step statuses.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusSkip    = "skip"
	StatusPending = "pending"
)

// StepResult is one step's outcome.
type StepResult struct {
	ID        string            `yaml:"id"`
	Title     string            `yaml:"title"`
	Judgement bool              `yaml:"judgement_step,omitempty"`
	HasChecks bool              `yaml:"has_checks,omitempty"`
	Status    string            `yaml:"status"`
	Viewports []*ViewportResult `yaml:"viewports,omitempty"`
	Judge     *JudgeResult      `yaml:"judge,omitempty"`
}

// ViewportResult is one step run at one viewport.
type ViewportResult struct {
	Viewport   string            `yaml:"viewport"`
	Status     string            `yaml:"status"`
	Error      string            `yaml:"error,omitempty"`
	Checks     []CheckResult     `yaml:"checks,omitempty"`
	Layout     []LayoutViolation `yaml:"layout,omitempty"`
	Console    []string          `yaml:"console,omitempty"`
	Screenshot string            `yaml:"screenshot,omitempty"`
	Log        string            `yaml:"log,omitempty"`
}

// LayoutViolation is one broken layout rule.
type LayoutViolation struct {
	Rule   string `yaml:"rule" json:"rule"`
	ID     string `yaml:"id" json:"id"`
	Detail string `yaml:"detail" json:"detail"`
}

// JudgeResult is the owner's verdict on a judgement step.
type JudgeResult struct {
	Status     string        `yaml:"status"`
	Note       string        `yaml:"note,omitempty"`
	Screenshot string        `yaml:"screenshot,omitempty"`
	Checks     []CheckResult `yaml:"checks,omitempty"`
	Opinions   []Opinion     `yaml:"opinions,omitempty"`
}

// Opinion is a note the owner left on an element (Alt+N).
type Opinion struct {
	Target     string `yaml:"target" json:"target"`
	Note       string `yaml:"note" json:"note"`
	Screenshot string `yaml:"screenshot,omitempty" json:"-"`
}

// Step returns the result for id, or nil.
func (r *Results) Step(id string) *StepResult {
	for _, s := range r.Steps {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// Write saves results.yaml into dir.
func (r *Results) Write(dir string) error {
	b, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "results.yaml"), b, 0o644)
}

// ReadResults loads dir/results.yaml.
func ReadResults(dir string) (*Results, error) {
	b, err := os.ReadFile(filepath.Join(dir, "results.yaml"))
	if err != nil {
		return nil, err
	}
	var r Results
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Join(dir, "results.yaml"), err)
	}
	return &r, nil
}

// LatestRun returns the newest run dir of a session in mode (auto|judge),
// optionally only for one build. Run dirs are named <time>-<mode>, so the
// newest sorts last.
func LatestRun(runsRoot, session, mode, build string) (string, *Results) {
	dirs, _ := filepath.Glob(filepath.Join(runsRoot, session, "*-"+mode))
	sort.Strings(dirs)
	for i := len(dirs) - 1; i >= 0; i-- {
		r, err := ReadResults(dirs[i])
		if err != nil {
			continue
		}
		if build != "" && r.Build != build {
			continue
		}
		return dirs[i], r
	}
	return "", nil
}

// Diff compares two auto runs on what must be the same every time
// (statuses, check results, layout, console) and returns the first
// difference, or "" when they match.
func Diff(a, b *Results) string {
	la, lb := canonical(a), canonical(b)
	n := max(len(la), len(lb))
	for i := range n {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n  run 1: %s\n  run 2: %s", i+1, x, y)
		}
	}
	return ""
}

func canonical(r *Results) []string {
	c := *r
	c.Started, c.Build = "", ""
	c.Steps = nil
	for _, s := range r.Steps {
		cs := *s
		cs.Viewports = nil
		for _, v := range s.Viewports {
			cv := *v
			cv.Screenshot, cv.Log = "", ""
			cs.Viewports = append(cs.Viewports, &cv)
		}
		c.Steps = append(c.Steps, &cs)
	}
	b, _ := yaml.Marshal(&c)
	var out []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// BuildID names the code a run tested: the commit, plus a hash of
// uncommitted changes. The overlay reuses an ux-test only from the same
// build.
func BuildID(repo string) string {
	rev, err := exec.Command("git", "-C", repo, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	id := strings.TrimSpace(string(rev))
	diff, _ := exec.Command("git", "-C", repo, "diff", "HEAD").Output()
	untracked, _ := exec.Command("git", "-C", repo, "status", "--porcelain", "--untracked-files=all").Output()
	if len(diff) > 0 || len(untracked) > 0 {
		h := sha1.New()
		h.Write(diff)
		h.Write(untracked)
		id += fmt.Sprintf("+%x", h.Sum(nil)[:4])
	}
	return id
}
