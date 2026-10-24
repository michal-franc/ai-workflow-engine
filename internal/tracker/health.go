package tracker

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// HealthConfig is the optional `health:` block in workflow.yaml: how the
// project health snapshots (internal/health, .health/snapshots/) measure the
// codebase. Every git project gets snapshots without it. It is project-wide:
// system and type overlays cannot override it.
type HealthConfig struct {
	Off          bool     `yaml:"off,omitempty" desc:"true turns health snapshots off for this project"`
	BigFileLines int      `yaml:"big_file_lines,omitempty" desc:"A file with more lines than this is a big file (default 1000)"`
	TestGlobs    []string `yaml:"test_globs,omitempty" desc:"Globs (** allowed) for test files; a set list replaces the defaults (*_test.go, *Tests.cs, *Test.cs, *.test.js, *.test.ts, test_*.py, *_test.py)"`
	Exclude      []string `yaml:"exclude,omitempty" desc:"Globs (** allowed) left out of the codebase counts, on top of the issues dir and .worktrees/"`
	Timeout      string   `yaml:"timeout,omitempty" desc:"Limit for each metric family, a Go duration (default 20s)"`
	// Limits are the thresholds the stats page's gauges draw (and that
	// trend alerts may use): waiting_share 0.25 (share of issue time waiting
	// on the owner), big_files 10. A key set to 0 turns that limit off.
	Limits map[string]float64 `yaml:"limits,omitempty" desc:"Thresholds for the stats page gauges: waiting_share (default 0.25), big_files (default 10); 0 turns one off"`
	// E2ESessions are the playtest session files the tests family counts.
	E2ESessions []string `yaml:"e2e_sessions,omitempty" desc:"Globs of the playtest session files the tests family counts (default playtest/sessions/*.yaml)"`
	// LongFuncLines: a function longer than this is long (shape family).
	LongFuncLines int `yaml:"long_func_lines,omitempty" desc:"A function with more lines than this is long (default 80)"`
	// Score overrides the derived health score's components, by name
	// (unit_wait, big_files, ...): good, bad and weight. Weight 0 turns one off.
	Score map[string]ScoreKnob `yaml:"score,omitempty" desc:"Derived health score components by name: {good, bad, weight}; weight 0 turns one off (docs/workbench-files.md, Scores)"`
	// ShrinkShotsAfterDays: the daily health pass shrinks the playtest shots
	// of issues done this many days ago (nil: the default 30; 0: never).
	ShrinkShotsAfterDays *int `yaml:"shrink_shots_after_days,omitempty" desc:"Days after done when the daily health pass shrinks an issue's playtest shots to small JPEGs (default 30; 0 turns it off)"`
}

// ScoreKnob overrides one derived-score component. Good and Bad take a
// number, a duration (60s, 10m: seconds) or a percentage (5%: 0.05).
type ScoreKnob struct {
	Good   *ScoreBound `yaml:"good,omitempty" desc:"Value that scores 100"`
	Bad    *ScoreBound `yaml:"bad,omitempty" desc:"Value that scores 0"`
	Weight *float64    `yaml:"weight,omitempty" desc:"Weight in the total; 0 turns the component off"`
}

// ScoreBound is a number read from a plain number, a Go duration (in
// seconds) or a percentage (as a share).
type ScoreBound float64

// UnmarshalYAML reads 60, 60s, 10m or 5%.
func (b *ScoreBound) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseScoreBound(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*b = ScoreBound(v)
	return nil
}

// ParseScoreBound reads 60, 60s, 10m or 5%.
func ParseScoreBound(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if p, ok := strings.CutSuffix(s, "%"); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a percentage like 5%%", s)
		}
		return f / 100, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d.Seconds(), nil
	}
	return 0, fmt.Errorf("%q is not a number, a duration like 60s or a percentage like 5%%", s)
}

// HealthLimitDefaults are the limits used when health.limits leaves a key
// out.
var HealthLimitDefaults = map[string]float64{
	"waiting_share": 0.25,
	"big_files":     10,
}

const (
	healthDefaultLongFuncLines = 80
	healthDefaultBigFileLines  = 1000
	healthDefaultTimeout       = 20 * time.Second
)

// HealthDefaultTestGlobs are the test globs used when test_globs is unset.
var HealthDefaultTestGlobs = []string{
	"**/*_test.go",
	"**/*Tests.cs",
	"**/*Test.cs",
	"**/*.test.js",
	"**/*.test.ts",
	"**/test_*.py",
	"**/*_test.py",
}

// Enabled reports whether snapshots are taken; a missing block means yes.
func (h *HealthConfig) Enabled() bool {
	return h == nil || !h.Off
}

// BigFileLimit is the big-file line count, the default when unset.
func (h *HealthConfig) BigFileLimit() int {
	if h != nil && h.BigFileLines > 0 {
		return h.BigFileLines
	}
	return healthDefaultBigFileLines
}

// LongFuncLimit is the long-function line count, the default when unset.
func (h *HealthConfig) LongFuncLimit() int {
	if h != nil && h.LongFuncLines > 0 {
		return h.LongFuncLines
	}
	return healthDefaultLongFuncLines
}

// HealthDefaultE2ESessions is where playtest sessions live by default.
var HealthDefaultE2ESessions = []string{"playtest/sessions/*.yaml"}

// E2ESessionGlobs is the e2e_sessions globs, the default when unset.
func (h *HealthConfig) E2ESessionGlobs() []string {
	if h != nil && len(h.E2ESessions) > 0 {
		return h.E2ESessions
	}
	return HealthDefaultE2ESessions
}

// Tests is the test globs, the defaults when unset.
func (h *HealthConfig) Tests() []string {
	if h != nil && len(h.TestGlobs) > 0 {
		return h.TestGlobs
	}
	return HealthDefaultTestGlobs
}

// Excluded is the exclude globs (nil-safe).
func (h *HealthConfig) Excluded() []string {
	if h == nil {
		return nil
	}
	return h.Exclude
}

// FamilyTimeout is each family's limit; an unset or bad value is the default.
func (h *HealthConfig) FamilyTimeout() time.Duration {
	if h != nil {
		if d, err := time.ParseDuration(strings.TrimSpace(h.Timeout)); err == nil && d > 0 {
			return d
		}
	}
	return healthDefaultTimeout
}

// healthDefaultShrinkDays is how long after done shots stay full size.
const healthDefaultShrinkDays = 30

// ShrinkShotsAfter is how long after done an issue's playtest shots are
// shrunk; ok is false when shrinking is off (0) or the value is negative.
func (h *HealthConfig) ShrinkShotsAfter() (time.Duration, bool) {
	days := healthDefaultShrinkDays
	if h != nil && h.ShrinkShotsAfterDays != nil {
		days = *h.ShrinkShotsAfterDays
	}
	if days <= 0 {
		return 0, false
	}
	return time.Duration(days) * 24 * time.Hour, true
}

// Limit is the named threshold: the configured value, else the default.
// ok is false when there is none or it is set to 0 (off).
func (h *HealthConfig) Limit(name string) (float64, bool) {
	v, set := 0.0, false
	if h != nil {
		v, set = h.Limits[name]
	}
	if !set {
		v, set = HealthLimitDefaults[name]
	}
	return v, set && v > 0
}

// Validate returns one problem per invalid entry. A missing block is valid.
func (h *HealthConfig) Validate() []string {
	if h == nil {
		return nil
	}
	var problems []string
	if h.BigFileLines < 0 {
		problems = append(problems, fmt.Sprintf("health.big_file_lines %d is negative (default 1000 is used)", h.BigFileLines))
	}
	if t := strings.TrimSpace(h.Timeout); t != "" {
		if d, err := time.ParseDuration(t); err != nil || d <= 0 {
			problems = append(problems, fmt.Sprintf("health.timeout %q is not a positive duration like 30s (default 20s is used)", h.Timeout))
		}
	}
	if h.LongFuncLines < 0 {
		problems = append(problems, fmt.Sprintf("health.long_func_lines %d is negative (default 80 is used)", h.LongFuncLines))
	}
	if h.ShrinkShotsAfterDays != nil && *h.ShrinkShotsAfterDays < 0 {
		problems = append(problems, fmt.Sprintf("health.shrink_shots_after_days %d is negative (use 0 to turn it off)", *h.ShrinkShotsAfterDays))
	}
	for k, v := range h.Score {
		if v.Weight != nil && *v.Weight < 0 {
			problems = append(problems, fmt.Sprintf("health.score.%s.weight %v is negative (use 0 to turn it off)", k, *v.Weight))
		}
		if v.Good != nil && v.Bad != nil && *v.Good == *v.Bad {
			problems = append(problems, fmt.Sprintf("health.score.%s: good and bad are equal, so the component can't score", k))
		}
	}
	for k, v := range h.Limits {
		if v < 0 {
			problems = append(problems, fmt.Sprintf("health.limits.%s %v is negative (use 0 to turn it off)", k, v))
		}
	}
	for _, list := range []struct {
		key   string
		globs []string
	}{{"test_globs", h.TestGlobs}, {"exclude", h.Exclude}, {"e2e_sessions", h.E2ESessions}} {
		key := list.key
		for i, g := range list.globs {
			// path.Match checks the syntax of each segment; ** is a plain *.
			if _, err := path.Match(strings.ReplaceAll(g, "**", "*"), ""); err != nil || strings.TrimSpace(g) == "" {
				problems = append(problems, fmt.Sprintf("health.%s[%d] %q is not a valid glob", key, i, g))
			}
		}
	}
	return problems
}

func cloneHealth(h *HealthConfig) *HealthConfig {
	if h == nil {
		return nil
	}
	out := *h
	out.TestGlobs = append([]string(nil), h.TestGlobs...)
	out.Exclude = append([]string(nil), h.Exclude...)
	out.E2ESessions = append([]string(nil), h.E2ESessions...)
	if h.ShrinkShotsAfterDays != nil {
		d := *h.ShrinkShotsAfterDays
		out.ShrinkShotsAfterDays = &d
	}
	if h.Score != nil {
		out.Score = make(map[string]ScoreKnob, len(h.Score))
		for k, v := range h.Score {
			out.Score[k] = v
		}
	}
	if h.Limits != nil {
		out.Limits = make(map[string]float64, len(h.Limits))
		for k, v := range h.Limits {
			out.Limits[k] = v
		}
	}
	return &out
}
