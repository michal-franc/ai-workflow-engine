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

Handoff statuses (backlog, human-testing) auto-advance to the next work status
when the matching approval is present — e.g. start on an approved backlog issue
moves it to "in progress" and consumes the approval. This advance is announced
with a prominent AUTO-ADVANCED banner so it is never silent. Without the
approval, start fails without changing anything. From any non-handoff status,
start only claims the issue and leaves the status unchanged.

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

	// A start that transitions is always a handoff auto-advance: StartIssueOnce
	// only sets a target status when the from-status is a handoff state. This is
	// the surprising part of `start`, so announce it with an unmissable banner
	// rather than a single quiet "✓ Status →" line.
	advanced := started.Transitioned && started.FromStatus != started.ToStatus
	if advanced {
		printAutoAdvanceBanner(ctx.Stdout, started.FromStatus, started.ToStatus, started.Result.ClearedApproval)
	}

	if started.Claimed {
		fmt.Fprintf(ctx.Stdout, "✓ Claimed (assignee: %s)\n", assignee)
	} else if issue.Assignee != "" {
		fmt.Fprintf(ctx.Stdout, "Already claimed by: %s\n", issue.Assignee)
	}

	if !advanced {
		fmt.Fprintf(ctx.Stdout, "Status unchanged (%s is a work status — ready to pick up)\n", started.ToStatus)
	}
	if started.Result.BodyAppended {
		fmt.Fprintln(ctx.Stdout, "✓ Workflow content appended to issue body")
	}
	// When advanced, the banner already states the approval was consumed.
	if started.Result.ClearedApproval && !advanced {
		fmt.Fprintln(ctx.Stdout, "✓ Approval consumed")
	}

	fmt.Fprintf(ctx.Stdout, "file: %s\n\n", issue.FilePath)

	printWorkflowNextSteps(ctx.Stdout, wf, issue)
	printStartWorkflowReminder(ctx.Stdout, wf)
	return nil
}

// printAutoAdvanceBanner renders the prominent notice shown when `start`
// auto-advances a handoff status (backlog, human-testing) to the next work
// status. The advance is the surprising part of `start`, so it gets an
// unmissable block naming from → to and the approval it consumed, instead of a
// single quiet "✓ Status →" line.
func printAutoAdvanceBanner(w io.Writer, from, to string, approvalConsumed bool) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "⚠ AUTO-ADVANCED  %s → %s\n", from, to)
	if approvalConsumed {
		fmt.Fprintf(w, "  A %q approval was present, so start moved this issue forward and consumed it.\n", to)
	} else {
		fmt.Fprintf(w, "  start moved this issue forward from the %q handoff status.\n", from)
	}
	fmt.Fprintln(w, "  start advances handoff statuses; to only claim, the issue must not be pre-approved.")
	fmt.Fprintln(w)
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
		requires, sideEffects := nextTransitionContract(wf, issue.Status, next)
		renderNextTransitionContract(w, requires, sideEffects)
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

// renderNextTransitionContract prints the "Requires:" and "Will:" sub-blocks
// under the next-transition command line. Each bucket is omitted when empty so
// transitions with no rules render unchanged.
func renderNextTransitionContract(w io.Writer, requires, sideEffects []string) {
	if len(requires) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Requires:")
		for _, r := range requires {
			fmt.Fprintf(w, "    - %s\n", r)
		}
	}
	if len(sideEffects) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Will:")
		for _, s := range sideEffects {
			fmt.Fprintf(w, "    - %s\n", s)
		}
	}
}
