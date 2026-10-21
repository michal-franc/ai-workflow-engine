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
	Metrics WorkbenchMetrics           `yaml:"metrics,omitempty" desc:"Read-only metric files (JSON, see docs/workbench-files.md): probe results and optional targets"`
	Tuning  []string                   `yaml:"tuning,omitempty" desc:"Knob export files (JSON, see docs/workbench-files.md), relative to the project root"`
	Balance WorkbenchBalance           `yaml:"balance,omitempty" desc:"Balance board config; enables the Balance view"`
}

// WorkbenchSystem describes one system page. Every key is optional: a system
// named only by the issues' `system` field still gets a page.
type WorkbenchSystem struct {
	Summary   string                     `yaml:"summary,omitempty" desc:"One-line summary shown in the system page header"`
	DesignDoc string                     `yaml:"design_doc,omitempty" desc:"Design doc path, relative to the project root"`
	Umbrella  string                     `yaml:"umbrella,omitempty" desc:"Slug of the umbrella issue tracking this system"`
	Metrics   []string                   `yaml:"metrics,omitempty" desc:"Metric ids whose guard rows show on the system page"`
	Knobs     []string                   `yaml:"knobs,omitempty" desc:"Tuning knob names (globs allowed) shown on the system page"`
	Families  map[string]WorkbenchFamily `yaml:"families,omitempty" desc:"Groups of mechanics inside the system, each an umbrella issue with its own page"`
}

// WorkbenchFamily is a group of mechanics inside a system (AI › Play calls):
// the umbrella's children in the system, with an optional design doc of its own.
type WorkbenchFamily struct {
	Summary   string   `yaml:"summary,omitempty" desc:"One-line summary shown in the family page header"`
	Umbrella  string   `yaml:"umbrella" desc:"Slug of the umbrella issue whose children make up the family (required)"`
	DesignDoc string   `yaml:"design_doc,omitempty" desc:"Design doc path, relative to the project root; the system's doc applies when empty"`
	Metrics   []string `yaml:"metrics,omitempty" desc:"Metric ids for the family page; the system's list applies when empty"`
	Knobs     []string `yaml:"knobs,omitempty" desc:"Tuning knob names (globs allowed) for the family page; the system's list applies when empty"`
}

// SystemConfig returns the configured entry matching name case-insensitively,
// with its key as written in workflow.yaml.
func (c WorkbenchConfig) SystemConfig(name string) (key string, sys WorkbenchSystem, ok bool) {
	if sys, ok := c.Systems[name]; ok {
		return name, sys, true
	}
	for k, v := range c.Systems {
		if strings.EqualFold(k, name) {
			return k, v, true
		}
	}
	return "", WorkbenchSystem{}, false
}

// FamilyNames lists the system's families in name order.
func (s WorkbenchSystem) FamilyNames() []string {
	names := make([]string, 0, len(s.Families))
	for name := range s.Families {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names
}

// Family returns the family matching name case-insensitively, with its key.
func (s WorkbenchSystem) Family(name string) (key string, fam WorkbenchFamily, ok bool) {
	if fam, ok := s.Families[name]; ok {
		return name, fam, true
	}
	for k, v := range s.Families {
		if strings.EqualFold(k, name) {
			return k, v, true
		}
	}
	return "", WorkbenchFamily{}, false
}

// WorkbenchMetrics points at the project's metric target and probe files.
type WorkbenchMetrics struct {
	Targets      string `yaml:"targets,omitempty" desc:"Metric targets JSON (id, name, group, band), relative to the project root; wins over bands in the results"`
	ProbeResults string `yaml:"probe_results,omitempty" desc:"Probe result JSON files (glob allowed; the newest run.date is the master run), relative to the project root"`
}

// WorkbenchBalance configures the balance board.
type WorkbenchBalance struct {
	CommentPrefix string `yaml:"comment_prefix,omitempty" desc:"Comment prefix that marks a balance note (e.g. 'balance:')"`
}

// IsZero reports whether the block was left out.
func (c WorkbenchConfig) IsZero() bool {
	return len(c.Systems) == 0 && c.Metrics == (WorkbenchMetrics{}) && len(c.Tuning) == 0 && c.Balance == (WorkbenchBalance{})
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
		sys := c.Systems[name]
		check(fmt.Sprintf("systems[%s].design_doc", name), sys.DesignDoc)
		seen := map[string]string{}
		for _, fname := range sys.FamilyNames() {
			fam := sys.Families[fname]
			key := fmt.Sprintf("systems[%s].families[%s]", name, fname)
			if prev, dup := seen[strings.ToLower(fname)]; dup {
				problems = append(problems, fmt.Sprintf("workbench.%s: duplicates family %q (names are case-insensitive)", key, prev))
			}
			seen[strings.ToLower(fname)] = fname
			if strings.TrimSpace(fam.Umbrella) == "" {
				problems = append(problems, fmt.Sprintf("workbench.%s: umbrella is required", key))
			}
			check(key+".design_doc", fam.DesignDoc)
		}
	}
	check("metrics.targets", c.Metrics.Targets)
	check("metrics.probe_results", c.Metrics.ProbeResults)
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
			v.Metrics = append([]string(nil), v.Metrics...)
			v.Knobs = append([]string(nil), v.Knobs...)
			if v.Families != nil {
				fams := make(map[string]WorkbenchFamily, len(v.Families))
				for fk, fv := range v.Families {
					fv.Metrics = append([]string(nil), fv.Metrics...)
					fv.Knobs = append([]string(nil), fv.Knobs...)
					fams[fk] = fv
				}
				v.Families = fams
			}
			out.Systems[k] = v
		}
	}
	out.Tuning = append([]string(nil), c.Tuning...)
	return out
}
