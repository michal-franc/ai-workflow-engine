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
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}

	issue, issues, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}

	wf := ctx.Project.LoadWorkflowForIssue(issue)
	if ctx.JSONOutput {
		comments, _ := tracker.LoadComments(issue.FilePath)
		out := map[string]interface{}{
			"issue":         issue,
			"comments":      comments,
			"workflow_file": workflowFileLabel(ctx.Project),
		}
		if wf.ActiveType != "" {
			out["type"] = wf.ActiveType
			out["type_path"] = typePath(wf)
		}
		return writeJSON(ctx.Stdout, out)
	}

	fmt.Fprintf(ctx.Stdout, "== %s ==\n", issue.Title)
	fmt.Fprintf(ctx.Stdout, "Status: %s%s | System: %s | Priority: %s | Assignee: %s\n",
		statusLabel(wf, issue.Status), typeLabel(wf), issue.System, issue.Priority, issue.Assignee)
	fmt.Fprintf(ctx.Stdout, "File: %s\n", issue.FilePath)
	fmt.Fprintf(ctx.Stdout, "Workflow file: %s\n", workflowFileLabel(ctx.Project))
	if wf.ActiveType != "" {
		fmt.Fprintf(ctx.Stdout, "Path: %s\n", typePath(wf))
	}
	printTypeWarnings(ctx.Stdout, wf, issue)
	printLeadLines(ctx, issue)
	printDepLines(ctx, issue, issues)
	// Where the issue's time went, to hand the owner a link.
	fmt.Fprintf(ctx.Stdout, "Timeline: %s?tab=timeline\n", issueURL(ctx.Project, issue.Slug))
	fmt.Fprintf(ctx.Stdout, "Evidence: %s?tab=evidence\n", issueURL(ctx.Project, issue.Slug))
	// The sentence the owner reads first on the Issue tab.
	if brief := tracker.Brief(issue.BodyRaw); brief != "" {
		fmt.Fprintf(ctx.Stdout, "Brief: %s\n", brief)
	} else {
		fmt.Fprintln(ctx.Stdout, "Brief: none. The Issue tab shows the title alone; start the body with a lead paragraph or **In short:**")
	}
	fmt.Fprintln(ctx.Stdout)

	fmt.Fprintln(ctx.Stdout, "== Body ==")
	fmt.Fprintln(ctx.Stdout, issue.BodyRaw)
	fmt.Fprintln(ctx.Stdout)

	total, checked := tracker.CountCheckboxes(issue.BodyRaw)
	if total > 0 {
		fmt.Fprintf(ctx.Stdout, "== Checklist (%d/%d) ==\n", checked, total)
		printCheckboxes(ctx.Stdout, issue.BodyRaw)
		printTickHint(ctx.Stdout, issue.Slug, issue.BodyRaw)
		fmt.Fprintln(ctx.Stdout)
	}

	printReplacedCitations(ctx, issue, issues)
	printSharedFiles(ctx.Stdout, ctx.Project, issue, issues)

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
	printCheckScriptHint(ctx.Stdout, ctx.Project, wf, issue)

	comments, _ := tracker.LoadComments(issue.FilePath)
	if len(comments) > 0 {
		fmt.Fprintf(ctx.Stdout, "== Comments (%d) ==\n", len(comments))
		for _, c := range comments {
			status := ""
			if c.Status == "done" {
				status = " [done]"
			}
			fmt.Fprintf(ctx.Stdout, "  [%s]%s %s\n", tracker.CommentDay(c.Date), status, c.Text)
		}
	}
	return nil
}
