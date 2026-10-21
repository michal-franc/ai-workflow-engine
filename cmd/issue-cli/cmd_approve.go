package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var approveCommand = &Command{
	Name:      "approve",
	ShortHelp: "Record a human approval for a gate, with the human's words (leads only; never workers)",
	LongHelp: `Record that the human approved an issue for a status. It writes an
approval: comment with the human's words, then sets human_approval (it never
toggles an existing approval off, unlike the viewer's button).

Who may run it:
  - the project lead, for a gate the human approved in its chat;
  - a sub-lead, only on its epic and the epic's children, and only for words
    the project lead relayed (say so with --how);
  - never a worker. Human Testing is always approved by the human in the
    viewer.

Examples:
  issue-cli approve <slug> --to backlog --quote "looks good, go"
  issue-cli approve <slug> --to "in progress" --quote "approved" --how "relayed by lead-issue-viewer"`,
	Run: runApprove,
}

func init() {
	registerCommand(approveCommand)
}

func runApprove(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "approve")
	if err != nil {
		return err
	}
	fs := newFlagSet("approve", ctx)
	toFlag := fs.String("to", "", "status the human approved (required)")
	quoteFlag := fs.String("quote", "", "the human's words (required)")
	howFlag := fs.String("how", "", "how the words reached you (default: \"in the lead chat\")")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}
	to := strings.ToLower(strings.TrimSpace(*toFlag))
	quote := strings.TrimSpace(normalizeEscapedText(*quoteFlag))
	how := strings.TrimSpace(*howFlag)
	if to == "" || quote == "" {
		return fmt.Errorf("approve requires --to and --quote (the human's words)\n\nExample:\n  issue-cli approve %s --to backlog --quote \"looks good, go\"", slug)
	}

	issue, issues, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	proj := ctx.Project
	wf := proj.LoadWorkflowForIssue(issue)
	if wf != nil && wf.GetStatus(to) == nil {
		return fmt.Errorf("unknown status %q; statuses: %s", to, strings.Join(wf.GetStatusOrder(), ", "))
	}
	if strings.ReplaceAll(to, " ", "-") == "human-testing" {
		return fmt.Errorf("Human Testing is approved by the human in the issue viewer, never by an agent")
	}

	me := whoAmI(proj, issues)
	switch me.Role {
	case tracker.RoleWorker:
		route := tracker.LeadFor(proj, issue, issues, sessionLive(proj))
		return fmt.Errorf("workers never tick gates. Ask your lead (%s, %s) on your issue trail:\n  %s", route.Lead, liveWord(route.Live),
			tracker.InjectProject(proj, fmt.Sprintf("issue-cli comment %s \"question: ready for %s?\"", issue.Slug, to)))
	case tracker.RoleSubLead:
		if !strings.EqualFold(issue.Slug, me.Scope) && !strings.EqualFold(tracker.ParentSlug(issue), me.Scope) {
			return fmt.Errorf("%s is outside your epic %s; sub-leads tick gates only on their epic and its children. Ask %s.",
				issue.Slug, me.Scope, tracker.LeadSessionName(proj, tracker.LeadScopeProject))
		}
		if how == "" {
			how = "relayed by " + tracker.LeadSessionName(proj, tracker.LeadScopeProject)
		}
	}
	if how == "" {
		how = "in the lead chat"
	}

	date := ctx.now().Format("2006-01-02")
	comment := fmt.Sprintf("approval: %q (%s, %s)", quote, how, date)
	if me.Name != "" {
		comment += " — recorded by " + me.Name
	}
	if err := tracker.AddComment(issue.FilePath, 0, comment, "cli"); err != nil {
		return fmt.Errorf("writing the approval comment: %w", err)
	}
	if !strings.EqualFold(issue.HumanApproval, to) {
		if err := tracker.UpdateIssueFrontmatter(issue.FilePath, tracker.IssueUpdate{HumanApproval: &to}); err != nil {
			return fmt.Errorf("setting human_approval: %w", err)
		}
		if err := tracker.RecordApproval(issue.FilePath, to, ctx.now()); err != nil {
			fmt.Fprintf(ctx.Stderr, "warning: recording approval time: %v\n", err)
		}
	}
	fmt.Fprintf(ctx.Stdout, "✓ Approved %s for %q (%s)\n", issue.Slug, to, how)
	fmt.Fprintf(ctx.Stdout, "  comment: %s\n", comment)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
