package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var checkCommand = &Command{
	Name:      "check",
	ShortHelp: "Check off a checkbox item by text match",
	LongHelp: `Mark the first matching unchecked item as checked.

Example:
  issue-cli check <slug> "Code changes complete"`,
	Run: runCheck,
}

func init() {
	registerCommand(checkCommand)
}

func runCheck(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "check")
	if err != nil {
		return err
	}
	fs := newFlagSet("check", ctx)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	queryParts := fs.Args()
	if len(queryParts) == 0 {
		return fmt.Errorf("check requires a query\n\nExample:\n  issue-cli check <slug> \"Code changes complete\"")
	}
	query := strings.Join(queryParts, " ")

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}

	newBody, found, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		updated, ok := tracker.CheckCheckbox(body, query)
		return updated, ok, nil
	})
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	if !found {
		fmt.Fprintf(ctx.Stdout, "No unchecked item matching \"%s\"\n\n", query)
		fmt.Fprintln(ctx.Stdout, "Unchecked items:")
		printCheckboxes(ctx.Stdout, newBody)
		return fmt.Errorf("no unchecked item matched %q", query)
	}

	total, checked := tracker.CountCheckboxes(newBody)
	fmt.Fprintf(ctx.Stdout, "✓ Checked: \"%s\"\n", query)
	fmt.Fprintf(ctx.Stdout, "  Progress: %d/%d\n", checked, total)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
