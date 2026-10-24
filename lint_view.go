package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/lint"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// lintPanelCap is how many warnings the Health view lists before pointing
// at issue-cli health lint for the rest.
const lintPanelCap = 50

// LintPanel is the Health view's Lint section: issue-cli health lint run
// live over workflow.yaml and the issues (cheap: one parse plus the
// issues' frontmatter). Every finding is a warning.
type LintPanel struct {
	Warnings int
	Findings []lint.Finding
	// More is how many warnings the list leaves out.
	More int
	// Command is what the owner or an agent runs for the full list.
	Command string
	Err     string
}

func lintPanel(proj *tracker.Project, now time.Time) *LintPanel {
	p := &LintPanel{Command: lintCommandFor(proj)}
	env, err := lint.Load(proj, now)
	if err != nil {
		p.Err = err.Error()
		return p
	}
	rep, err := lint.Run(env)
	if err != nil {
		p.Err = err.Error()
		return p
	}
	p.Warnings = rep.Warnings
	p.Findings = rep.Findings
	if len(p.Findings) > lintPanelCap {
		p.More = len(p.Findings) - lintPanelCap
		p.Findings = p.Findings[:lintPanelCap]
	}
	return p
}

func lintCommandFor(proj *tracker.Project) string {
	if proj.Slug != "" {
		return "issue-cli --project " + proj.Slug + " health lint"
	}
	return "issue-cli health lint"
}

// lintLine is the Status page Health card's lint line, from the latest
// snapshot (as its scores are): "2 lint warnings", "no lint warnings", or
// "" when the snapshot predates the lint family.
func lintLine(s *health.Snapshot) string {
	v, ok := s.Value("lint.warnings", nil)
	if !ok {
		return ""
	}
	switch n := int(v); n {
	case 0:
		return "no lint warnings"
	case 1:
		return "1 lint warning"
	default:
		return fmt.Sprintf("%d lint warnings", n)
	}
}

func lintMarkdown(b *strings.Builder, line, command string) {
	if line == "" {
		return
	}
	fmt.Fprintf(b, "- Lint: %s · %s\n", line, command)
}
