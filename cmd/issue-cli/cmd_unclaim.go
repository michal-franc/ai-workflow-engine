package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var unclaimCommand = &Command{
	Name:      "unclaim",
	ShortHelp: "Remove assignee from an issue",
	LongHelp:  "Clear the assignee field on an issue. Does not change status.",
	Run:       runUnclaim,
}

func init() {
	registerCommand(unclaimCommand)
}

func runUnclaim(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "unclaim")
	if err != nil {
		return err
	}
	fs := newFlagSet("unclaim", ctx)
	if err := fs.Parse(rest); err != nil {
		return err
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	empty := ""
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, tracker.IssueUpdate{Assignee: &empty}); err != nil {
		return fmt.Errorf("failed to unclaim: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Unclaimed: %s\n", issue.Slug)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
