package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var updateCommand = &Command{
	Name:      "update",
	ShortHelp: `Replace issue body (--body "content"), preserves frontmatter`,
	LongHelp: `Update issue title and/or body. At least one of --title / --body is required.

Examples:
  issue-cli update <slug> --title "new title"
  issue-cli update <slug> --body "new body content"`,
	Run: runUpdate,
}

func init() {
	registerCommand(updateCommand)
}

func runUpdate(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "update")
	if err != nil {
		return err
	}
	fs := newFlagSet("update", ctx)
	titleFlag := fs.String("title", "", "new issue title")
	bodyFlag := fs.String("body", "", "new issue body")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	titleSet := flagWasSet(fs, "title")
	bodySet := flagWasSet(fs, "body")
	if !titleSet && !bodySet {
		return fmt.Errorf("update requires --title and/or --body\n\nExample:\n  issue-cli update %s --title \"new title\" --body \"new body content\"", slug)
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	update := tracker.IssueUpdate{}
	if titleSet {
		t := *titleFlag
		update.Title = &t
	}
	if bodySet {
		b := normalizeEscapedText(*bodyFlag)
		update.Body = &b
	}
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, update); err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Updated: %s\n", issue.Slug)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
