package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var commentCommand = &Command{
	Name:      "comment",
	ShortHelp: "Add a comment to an issue",
	LongHelp: `Append a comment block to an issue file.

Examples:
  issue-cli comment <slug> "your comment here"
  issue-cli comment <slug> --text "tests: 3 unit tests added"`,
	Run: runComment,
}

func init() {
	registerCommand(commentCommand)
}

func runComment(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "comment")
	if err != nil {
		return err
	}
	fs := newFlagSet("comment", ctx)
	textFlag := fs.String("text", "", "comment text")
	bodyFlag := fs.String("body", "", "alias for --text")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	text := normalizeEscapedText(*textFlag)
	if text == "" {
		text = normalizeEscapedText(*bodyFlag)
	}
	if text == "" {
		text = strings.Join(fs.Args(), " ")
	}
	if text == "" {
		return fmt.Errorf("text is required\n\nExample:\n  issue-cli comment %s \"your comment here\"\n  issue-cli comment %s --text \"your comment here\"", slug, slug)
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if err := tracker.AddComment(issue.FilePath, 0, text, "cli"); err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Comment added to %s\n", issue.Slug)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
