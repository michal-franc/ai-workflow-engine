package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var replaceCommand = &Command{
	Name:      "replace",
	ShortHelp: "Replace content of an existing section",
	LongHelp: `Replace the body of an existing top-level section.

Example:
  issue-cli replace <slug> --section "Design" --body "updated approach"`,
	Run: runReplace,
}

func init() {
	registerCommand(replaceCommand)
}

func runReplace(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "replace")
	if err != nil {
		return err
	}
	fs := newFlagSet("replace", ctx)
	bodyFlag := fs.String("body", "", "new section content")
	textFlag := fs.String("text", "", "alias for --body")
	sectionFlag := fs.String("section", "", "section name to replace")
	forceFlag := fs.Bool("force", false, "force replace even when section is absent")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	text := normalizeEscapedText(*bodyFlag)
	if text == "" {
		text = normalizeEscapedText(*textFlag)
	}
	section := *sectionFlag
	force := *forceFlag
	if strings.TrimSpace(section) == "" {
		return fmt.Errorf("replace requires --section\n\nExample:\n  issue-cli replace %s --section \"Design\" --body \"updated approach\"", slug)
	}
	if text == "" {
		return fmt.Errorf("replace requires --body\n\nExample:\n  issue-cli replace %s --section \"Design\" --body \"updated approach\"", slug)
	}

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}

	_, _, err = tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		return tracker.ReplaceIssueBodySection(body, section, text, force)
	})
	if err != nil {
		return fmt.Errorf("failed to replace: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Replaced section %q in %s\n", section, issue.Slug)
	return nil
}
