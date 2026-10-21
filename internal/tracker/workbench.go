package tracker

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// WorkbenchConfig is the optional `workbench:` block in workflow.yaml. It
// points the workbench views (system pages, balance board, metric views) at
// files the project already keeps. Every path is relative to the project root
// and read-only: the files stay the source of truth.
type WorkbenchConfig struct {
	Systems map[string]WorkbenchSystem `yaml:"systems,omitempty" desc:"Per-system page config keyed by the issue 'system' value; enables the Systems view"`
	Metrics WorkbenchMetrics           `yaml:"metrics,omitempty" desc:"Read-only metric sources (target bands, probe results)"`
	Tuning  []string                   `yaml:"tuning,omitempty" desc:"Files holding tuning knobs, relative to the project root"`
	Balance WorkbenchBalance           `yaml:"balance,omitempty" desc:"Balance board config; enables the Balance view"`
	// Changelog is the release history the Status page lists recent releases
	// from; empty means CHANGELOG.md when the project root has one.
	Changelog string `yaml:"changelog,omitempty" desc:"Changelog file the Status page reads recent releases from (default CHANGELOG.md), relative to the project root"`
}

// WorkbenchSystem describes one system page.
type WorkbenchSystem struct {
	Summary   string `yaml:"summary,omitempty" desc:"One-line summary shown in the system page header"`
	DesignDoc string `yaml:"design_doc,omitempty" desc:"Design doc path, relative to the project root"`
	Umbrella  string `yaml:"umbrella,omitempty" desc:"Slug of the umbrella issue tracking this system"`
}

// WorkbenchMetrics points at the project's metric target and probe files.
type WorkbenchMetrics struct {
	Targets      string `yaml:"targets,omitempty" desc:"Metric targets file, relative to the project root"`
	ProbeResults string `yaml:"probe_results,omitempty" desc:"Probe result files (glob allowed), relative to the project root"`
}

// WorkbenchBalance configures the balance board.
type WorkbenchBalance struct {
	CommentPrefix string `yaml:"comment_prefix,omitempty" desc:"Comment prefix that marks a balance note (e.g. 'balance:')"`
}

// IsZero reports whether the block was left out.
func (c WorkbenchConfig) IsZero() bool {
	return len(c.Systems) == 0 && c.Metrics == (WorkbenchMetrics{}) && len(c.Tuning) == 0 && c.Balance == (WorkbenchBalance{}) && c.Changelog == ""
}

// HasSystems reports whether the Systems view should be shown.
func (c WorkbenchConfig) HasSystems() bool { return len(c.Systems) > 0 }

// HasBalance reports whether the Balance view should be shown.
func (c WorkbenchConfig) HasBalance() bool {
	return c.Balance.CommentPrefix != "" || c.Metrics != (WorkbenchMetrics{})
}

// Validate returns one problem per invalid entry: paths must be relative and
// stay inside the project root. An empty block is valid.
func (c WorkbenchConfig) Validate() []string {
	var problems []string
	check := func(key, path string) {
		if path == "" {
			return
		}
		if err := checkProjectRelativePath(path); err != nil {
			problems = append(problems, fmt.Sprintf("workbench.%s: %v", key, err))
		}
	}
	names := make([]string, 0, len(c.Systems))
	for name := range c.Systems {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		check(fmt.Sprintf("systems[%s].design_doc", name), c.Systems[name].DesignDoc)
	}
	check("metrics.targets", c.Metrics.Targets)
	check("metrics.probe_results", c.Metrics.ProbeResults)
	check("changelog", c.Changelog)
	for i, path := range c.Tuning {
		check(fmt.Sprintf("tuning[%d]", i), path)
	}
	return problems
}

func checkProjectRelativePath(path string) error {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return fmt.Errorf("%q must be relative to the project root", path)
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%q points outside the project root", path)
	}
	return nil
}

func cloneWorkbench(c WorkbenchConfig) WorkbenchConfig {
	out := c
	if c.Systems != nil {
		out.Systems = make(map[string]WorkbenchSystem, len(c.Systems))
		for k, v := range c.Systems {
			out.Systems[k] = v
		}
	}
	out.Tuning = append([]string(nil), c.Tuning...)
	return out
}
