package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var showCommand = &Command{
	Name:      "context",
	ShortHelp: "Full context dump for an issue (alias: show)",
	LongHelp:  "Print the issue title, frontmatter, body, checklist, test-plan presence, and comments.",
	Run:       runShow,
}

func init() {
	registerCommand(showCommand)
	registerAlias("show", "context")
}

func runShow(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "context")
	if err != nil {
		return err
	}
	fs := newFlagSet("context", ctx)
	if err := fs.Parse(rest); err != nil {
		return err
	}

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}

	if ctx.JSONOutput {
		comments, _ := tracker.LoadComments(issue.FilePath)
		return writeJSON(ctx.Stdout, map[string]interface{}{
			"issue":    issue,
			"comments": comments,
		})
	}

	wf := ctx.Project.LoadWorkflowForIssue(issue)
	fmt.Fprintf(ctx.Stdout, "== %s ==\n", issue.Title)
	fmt.Fprintf(ctx.Stdout, "Status: %s | System: %s | Priority: %s | Assignee: %s\n",
		statusLabel(wf, issue.Status), issue.System, issue.Priority, issue.Assignee)
	fmt.Fprintf(ctx.Stdout, "File: %s\n\n", issue.FilePath)

	fmt.Fprintln(ctx.Stdout, "== Body ==")
	fmt.Fprintln(ctx.Stdout, issue.BodyRaw)
	fmt.Fprintln(ctx.Stdout)

	total, checked := tracker.CountCheckboxes(issue.BodyRaw)
	if total > 0 {
		fmt.Fprintf(ctx.Stdout, "== Checklist (%d/%d) ==\n", checked, total)
		printCheckboxes(ctx.Stdout, issue.BodyRaw)
		fmt.Fprintln(ctx.Stdout)
	}

	hasAuto, hasManual := tracker.HasTestPlan(issue.BodyRaw)
	if hasAuto || hasManual {
		fmt.Fprint(ctx.Stdout, "== Test Plan ==\n")
		if hasAuto {
			fmt.Fprintln(ctx.Stdout, "  ✓ ### Automated section present")
		}
		if hasManual {
			fmt.Fprintln(ctx.Stdout, "  ✓ ### Manual section present")
		}
		fmt.Fprintln(ctx.Stdout)
	}

	comments, _ := tracker.LoadComments(issue.FilePath)
	if len(comments) > 0 {
		fmt.Fprintf(ctx.Stdout, "== Comments (%d) ==\n", len(comments))
		for _, c := range comments {
			status := ""
			if c.Status == "done" {
				status = " [done]"
			}
			fmt.Fprintf(ctx.Stdout, "  [%s]%s %s\n", c.Date, status, c.Text)
		}
	}
	return nil
}
