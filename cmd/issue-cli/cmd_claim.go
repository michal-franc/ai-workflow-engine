package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var claimCommand = &Command{
	Name:      "claim",
	ShortHelp: "Only set assignee (does NOT start work — use 'start' instead)",
	LongHelp: `Claim an issue by setting its assignee field. Use --force to reassign
an issue that is already claimed by someone else.`,
	Run: runClaim,
}

func init() {
	registerCommand(claimCommand)
}

func runClaim(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "claim")
	if err != nil {
		return err
	}
	fs := newFlagSet("claim", ctx)
	assigneeFlag := fs.String("assignee", "", "assignee name (default: derived from slug)")
	force := fs.Bool("force", false, "reassign even when already claimed by someone else")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	assignee := *assigneeFlag
	if assignee == "" {
		assignee = agentNameForSlug(slug)
	}

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}

	if issue.Assignee != "" && issue.Assignee != assignee && !*force {
		return fmt.Errorf("already claimed by %q\n\nUse --force to reassign:\n  issue-cli claim %s --assignee %s --force",
			issue.Assignee, slug, assignee)
	}
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, tracker.IssueUpdate{Assignee: &assignee}); err != nil {
		return fmt.Errorf("failed to claim: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Claimed: %s (assignee: %s)\n", issue.Slug, assignee)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
