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

Use --body-file <path> (or --body-file - for stdin) to supply the comment as raw
bytes, which avoids shell mangling of backticks and parentheses.

Examples:
  issue-cli comment <slug> "your comment here"
  issue-cli comment <slug> --text "tests: 3 unit tests added"
  issue-cli comment <slug> --body-file note.md
  cat note.md | issue-cli comment <slug> --body-file -`,
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
	bodyFileFlag := fs.String("body-file", "", "read comment from file (or - for stdin); avoids shell mangling")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	inline := normalizeEscapedText(*textFlag)
	if inline == "" {
		inline = normalizeEscapedText(*bodyFlag)
	}
	if inline == "" {
		inline = strings.Join(fs.Args(), " ")
	}
	inlineSet := flagWasSet(fs, "text") || flagWasSet(fs, "body") || len(fs.Args()) > 0
	text, err := resolveBodyInput(ctx, inline, inlineSet, *bodyFileFlag, flagWasSet(fs, "body-file"))
	if err != nil {
		return err
	}
	if text == "" {
		return fmt.Errorf("text is required\n\nExample:\n  issue-cli comment %s \"your comment here\"\n  issue-cli comment %s --text \"your comment here\"\n  issue-cli comment %s --body-file note.md", slug, slug, slug)
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
