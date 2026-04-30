package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var startCommand = &Command{
	Name:      "start",
	ShortHelp: "*** USE THIS TO BEGIN WORK *** Picks up an issue from any status — claims, advances handoff states, shows checklist + next steps",
	LongHelp: `Pick up an issue at any status: claim it, advance through handoff statuses
when human-approved, and print the checklist + next steps.

Examples:
  issue-cli start <slug>
  issue-cli start <slug> --assignee my-bot`,
	Run: runStart,
}

func init() {
	registerCommand(startCommand)
}

func runStart(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "start")
	if err != nil {
		return err
	}
	fs := newFlagSet("start", ctx)
	assigneeFlag := fs.String("assignee", "", "assignee name (default: derived from slug or AGENT_NAME)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	assignee := *assigneeFlag

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	wf := ctx.Project.LoadWorkflowForIssue(issue)
	if assignee == "" {
		assignee = agentNameForSlug(slug)
	}

	started, err := wf.StartIssueOnce(issue.FilePath, slug, assignee)
	if err != nil {
		return err
	}
	issue = started.Issue

	fmt.Fprintf(ctx.Stdout, "== Starting work on: %s ==\n", issue.Title)
	fmt.Fprintf(ctx.Stdout, "Status: %s\n", statusLabel(wf, started.FromStatus))

	if started.Claimed {
		fmt.Fprintf(ctx.Stdout, "✓ Claimed (assignee: %s)\n", assignee)
	} else if issue.Assignee != "" {
		fmt.Fprintf(ctx.Stdout, "Already claimed by: %s\n", issue.Assignee)
	}

	if started.Transitioned && started.FromStatus != started.ToStatus {
		fmt.Fprintf(ctx.Stdout, "✓ Status → %s\n", started.ToStatus)
	} else {
		fmt.Fprintf(ctx.Stdout, "Status unchanged (%s is a work status — ready to pick up)\n", started.ToStatus)
	}
	if started.Result.BodyAppended {
		fmt.Fprintln(ctx.Stdout, "✓ Workflow content appended to issue body")
	}
	if started.Result.ClearedApproval {
		fmt.Fprintln(ctx.Stdout, "✓ Approval consumed")
	}

	fmt.Fprintf(ctx.Stdout, "file: %s\n\n", issue.FilePath)

	printWorkflowNextSteps(ctx.Stdout, wf, issue)
	printStartWorkflowReminder(ctx.Stdout, wf)
	return nil
}

func printStartWorkflowReminder(w io.Writer, wf *tracker.WorkflowConfig) {
	order := wf.GetStatusOrder()
	if len(order) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Workflow lifecycle ==")
	fmt.Fprintf(w, "  %s\n", strings.Join(order, " → "))
	fmt.Fprintln(w, "Run 'issue-cli process workflow' or 'issue-cli process transitions' for details.")
}

func printWorkflowNextSteps(w io.Writer, wf *tracker.WorkflowConfig, issue *tracker.Issue) {
	total, checked := tracker.CountCheckboxes(issue.BodyRaw)
	if total > 0 {
		fmt.Fprintf(w, "== Checklist (%d/%d) ==\n", checked, total)
		printCheckboxes(w, issue.BodyRaw)
		fmt.Fprintln(w)
	}

	if prompt := wf.StatusPrompt(issue.Status); prompt != "" {
		fmt.Fprintln(w, "== Current Status Guidance ==")
		fmt.Fprintf(w, "- %s\n", prompt)
		fmt.Fprintln(w)
	}

	tmpl := wf.TemplateForStatus(issue.Status)
	if tmpl != "" {
		firstLine := strings.SplitN(tmpl, "\n", 2)[0]
		if !strings.Contains(issue.BodyRaw, firstLine) {
			fmt.Fprintln(w, "== Current status template ==")
			fmt.Fprintln(w, tmpl)
			fmt.Fprintln(w)
		}
	}

	required, optionals := wf.DefaultNextStatus(issue.Status)
	next := required
	allOptional := false
	if next == "" && len(optionals) > 0 {
		next = optionals[0]
		optionals = optionals[1:]
		allOptional = true
	}
	if next != "" {
		fmt.Fprintln(w, "== Next ==")
		suffix := ""
		if allOptional {
			suffix = "   (optional — every remaining status is optional)"
		}
		fmt.Fprintf(w, "  issue-cli transition %s --to \"%s\"%s\n", issue.Slug, next, suffix)
		if len(optionals) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Optional side-paths:")
			for _, opt := range optionals {
				fmt.Fprintf(w, "  issue-cli transition %s --to \"%s\"\n", issue.Slug, opt)
			}
		}
		prompts := wf.EntryPrompts(issue.Status, next)
		if len(prompts) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "== Entry Guidance ==")
			for _, prompt := range prompts {
				fmt.Fprintf(w, "- %s\n", prompt)
			}
		}
	}
}
