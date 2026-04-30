package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var statsCommand = &Command{
	Name:      "stats",
	ShortHelp: "Project health overview",
	LongHelp:  "Print issue counts grouped by status, system, and assignee. Pass --json for machine-readable output.",
	Run:       runStats,
}

func init() {
	registerCommand(statsCommand)
}

func runStats(ctx *Context, args []string) error {
	fs := newFlagSet("stats", ctx)
	if err := fs.Parse(args); err != nil {
		return err
	}

	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}

	byStatus := map[string]int{}
	bySystem := map[string]int{}
	byAssignee := map[string]int{}
	for _, issue := range issues {
		byStatus[issue.Status]++
		if issue.System != "" {
			bySystem[issue.System]++
		}
		if issue.Assignee != "" {
			byAssignee[issue.Assignee]++
		}
	}

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]interface{}{
			"total":       len(issues),
			"by_status":   byStatus,
			"by_system":   bySystem,
			"by_assignee": byAssignee,
		})
	}

	fmt.Fprintf(ctx.Stdout, "== Project Stats (%d issues) ==\n\n", len(issues))

	wf := proj.LoadWorkflow()
	fmt.Fprintln(ctx.Stdout, "By status:")
	for _, s := range wf.GetStatusOrder() {
		if n, ok := byStatus[s]; ok {
			label := s
			if st := wf.GetStatus(s); st != nil && st.Optional {
				label = s + " (optional)"
			}
			fmt.Fprintf(ctx.Stdout, "  %-24s %d\n", label, n)
		}
	}

	fmt.Fprintln(ctx.Stdout, "\nBy system:")
	for sys, n := range bySystem {
		fmt.Fprintf(ctx.Stdout, "  %-15s %d\n", sys, n)
	}

	if len(byAssignee) > 0 {
		fmt.Fprintln(ctx.Stdout, "\nBy assignee:")
		for a, n := range byAssignee {
			fmt.Fprintf(ctx.Stdout, "  %-15s %d\n", a, n)
		}
	}
	return nil
}
