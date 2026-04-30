package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var nextCommand = &Command{
	Name:      "next",
	ShortHelp: "Find work for a version (use --design for ideas-needing-design)",
	LongHelp: `List actionable issues for the current version.

Examples:
  issue-cli next --version 0.1
  issue-cli next --design --version 0.1`,
	Run: runNext,
}

func init() {
	registerCommand(nextCommand)
}

func runNext(ctx *Context, args []string) error {
	fs := newFlagSet("next", ctx)
	designFlag := fs.Bool("design", false, "show ideas/in-design issues needing design")
	versionFlag := fs.String("version", "", "version filter (defaults to project version)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	design := *designFlag
	version := *versionFlag

	proj := ctx.Project
	if version == "" {
		version = proj.Version
	}
	if version == "" && !design {
		return fmt.Errorf("--version is required (or set version in project.yaml)\n\nExample:\n  issue-cli next --version 0.1\n  issue-cli next --design --version 0.1")
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	if version != "" {
		var filtered []*tracker.Issue
		for _, issue := range issues {
			if issue.Version == version {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	priorityRank := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "": 4}

	if design {
		var matches []*tracker.Issue
		for _, issue := range issues {
			if issue.Status == "idea" || issue.Status == "in design" {
				matches = append(matches, issue)
			}
		}
		sortByPriority(matches, priorityRank)

		if ctx.JSONOutput {
			return writeJSON(ctx.Stdout, matches)
		}
		if len(matches) == 0 {
			fmt.Fprintf(ctx.Stdout, "No issues needing design work for version %s.\n", version)
			return nil
		}
		fmt.Fprintf(ctx.Stdout, "== Issues needing design work (v%s) ==\n", version)
		for _, issue := range matches {
			p := issue.Priority
			if p == "" {
				p = "none"
			}
			fmt.Fprintf(ctx.Stdout, "  [%-8s] %-45s %s\n", p, issue.Slug, issue.System)
		}
		fmt.Fprintln(ctx.Stdout, "\nPick one: issue-cli claim <slug>")
		return nil
	}

	var backlog, inProgress, testing []*tracker.Issue
	for _, issue := range issues {
		switch issue.Status {
		case "backlog":
			if issue.Assignee == "" {
				backlog = append(backlog, issue)
			}
		case "in progress":
			inProgress = append(inProgress, issue)
		case "testing":
			testing = append(testing, issue)
		}
	}

	sortByPriority(backlog, priorityRank)
	sortByPriority(inProgress, priorityRank)
	sortByPriority(testing, priorityRank)

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]interface{}{
			"backlog":     backlog,
			"in_progress": inProgress,
			"testing":     testing,
		})
	}

	if len(backlog) == 0 && len(inProgress) == 0 && len(testing) == 0 {
		fmt.Fprintf(ctx.Stdout, "No work available for version %s. Try: issue-cli next --design --version %s\n", version, version)
		return nil
	}

	fmt.Fprintf(ctx.Stdout, "== Work for v%s ==\n", version)
	if len(inProgress) > 0 {
		fmt.Fprintf(ctx.Stdout, "\nIn Progress (%d):\n", len(inProgress))
		for _, issue := range inProgress {
			a := ""
			if issue.Assignee != "" {
				a = " @" + issue.Assignee
			}
			fmt.Fprintf(ctx.Stdout, "  [%-8s] %-45s %s%s\n", issue.Priority, issue.Slug, issue.System, a)
		}
	}
	if len(testing) > 0 {
		fmt.Fprintf(ctx.Stdout, "\nTesting (%d):\n", len(testing))
		for _, issue := range testing {
			a := ""
			if issue.Assignee != "" {
				a = " @" + issue.Assignee
			}
			fmt.Fprintf(ctx.Stdout, "  [%-8s] %-45s %s%s\n", issue.Priority, issue.Slug, issue.System, a)
		}
	}
	if len(backlog) > 0 {
		fmt.Fprintf(ctx.Stdout, "\nBacklog — unclaimed (%d):\n", len(backlog))
		for _, issue := range backlog {
			p := issue.Priority
			if p == "" {
				p = "none"
			}
			fmt.Fprintf(ctx.Stdout, "  [%-8s] %-45s %s\n", p, issue.Slug, issue.System)
		}
	}
	fmt.Fprintln(ctx.Stdout, "\nPick one: issue-cli start <slug>")
	return nil
}
