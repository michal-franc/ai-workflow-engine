package tracker

import (
	"fmt"
	"path"
	"strings"
	"time"
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
}

// HealthLimitDefaults are the limits used when health.limits leaves a key
// out.
var HealthLimitDefaults = map[string]float64{
	"waiting_share": 0.25,
	"big_files":     10,
}

const (
	healthDefaultBigFileLines = 1000
	healthDefaultTimeout      = 20 * time.Second
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
	for k, v := range h.Limits {
		if v < 0 {
			problems = append(problems, fmt.Sprintf("health.limits.%s %v is negative (use 0 to turn it off)", k, v))
		}
	}
	for _, list := range []struct {
		key   string
		globs []string
	}{{"test_globs", h.TestGlobs}, {"exclude", h.Exclude}} {
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
	if h.Limits != nil {
		out.Limits = make(map[string]float64, len(h.Limits))
		for k, v := range h.Limits {
			out.Limits[k] = v
		}
	}
	return &out
}
