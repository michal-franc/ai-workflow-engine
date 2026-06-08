package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var checkCommand = &Command{
	Name:      "check",
	ShortHelp: "Check off a checkbox item by text or by section + index",
	LongHelp: `Mark a checkbox as checked. Address it three ways:

  by text      issue-cli check <slug> "Code changes complete"
  by index     issue-cli check <slug> --section "Design" --index 2
  by position  issue-cli check <slug> --index 5

Indexes are 1-based and stable: they count every box (checked and unchecked)
in document order, so an index never shifts as boxes get ticked. Run
'issue-cli checklist <slug>' to see each box's [Section #index].

A text query matches a box whose label contains it (case-insensitive). If it
matches more than one unchecked box, check errors and lists the candidates so
you can re-run with --section/--index.`,
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
	sectionFlag := fs.String("section", "", "section to scope the match to (\"## <name>\")")
	indexFlag := fs.Int("index", 0, "1-based stable index of the checkbox within the section (or whole body)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	section := strings.TrimSpace(*sectionFlag)
	query := strings.Join(fs.Args(), " ")

	if *indexFlag == 0 && query == "" {
		return fmt.Errorf("check requires a text query or --index\n\nExamples:\n  issue-cli check <slug> \"Code changes complete\"\n  issue-cli check <slug> --section \"Design\" --index 2")
	}
	if *indexFlag != 0 && query != "" {
		return fmt.Errorf("pass either a text query or --index, not both")
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}

	if *indexFlag != 0 {
		return checkByIndex(ctx, issue, section, *indexFlag)
	}
	return checkByText(ctx, issue, section, query)
}

// checkboxLabel renders a checkbox as "[Section #index] text" (or "[#index]"
// when the box sits before any section heading).
func checkboxLabel(it tracker.CheckboxItem) string {
	if it.Section == "" {
		return fmt.Sprintf("[#%d] %s", it.Index, it.Text)
	}
	return fmt.Sprintf("[%s #%d] %s", it.Section, it.Index, it.Text)
}

func checkByIndex(ctx *Context, issue *tracker.Issue, section string, index int) error {
	var matched tracker.CheckboxItem
	var already, ok bool
	newBody, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		updated, item, was, found := tracker.CheckByIndex(body, section, index)
		matched, already, ok = item, was, found
		return updated, found && !was, nil
	})
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	if !ok {
		scope := "the body"
		if section != "" {
			scope = fmt.Sprintf("section %q", section)
		}
		fmt.Fprintf(ctx.Stdout, "No checkbox at index %d in %s\n\n", index, scope)
		fmt.Fprintln(ctx.Stdout, "Checkboxes:")
		printCheckboxes(ctx.Stdout, newBody)
		return fmt.Errorf("no checkbox at index %d in %s", index, scope)
	}
	if already {
		fmt.Fprintf(ctx.Stdout, "Already checked: %s\n", checkboxLabel(matched))
		total, checked := tracker.CountCheckboxes(newBody)
		fmt.Fprintf(ctx.Stdout, "  Progress: %d/%d\n", checked, total)
		fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
		return nil
	}
	total, checked := tracker.CountCheckboxes(newBody)
	fmt.Fprintf(ctx.Stdout, "✓ Checked: %s\n", checkboxLabel(matched))
	fmt.Fprintf(ctx.Stdout, "  Progress: %d/%d\n", checked, total)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}

func checkByText(ctx *Context, issue *tracker.Issue, section, query string) error {
	matches := tracker.MatchUncheckedByText(issue.BodyRaw, section, query)
	switch len(matches) {
	case 0:
		scope := ""
		if section != "" {
			scope = fmt.Sprintf(" in section %q", section)
		}
		fmt.Fprintf(ctx.Stdout, "No unchecked item matching \"%s\"%s\n\n", query, scope)
		fmt.Fprintln(ctx.Stdout, "Unchecked items:")
		printUncheckedItems(ctx.Stdout, issue.BodyRaw)
		return fmt.Errorf("no unchecked item matched %q", query)
	case 1:
		// fall through to the single-match check below
	default:
		fmt.Fprintf(ctx.Stdout, "Ambiguous: %d unchecked boxes match \"%s\":\n", len(matches), query)
		for _, it := range matches {
			fmt.Fprintf(ctx.Stdout, "  %s\n", checkboxLabel(it))
		}
		first := matches[0]
		fmt.Fprintf(ctx.Stdout, "\nRe-run with the exact box, e.g.:\n  issue-cli check %s --section %q --index %d\n", issue.Slug, first.Section, first.Index)
		return fmt.Errorf("%q is ambiguous — %d unchecked boxes match", query, len(matches))
	}

	target := matches[0]
	newBody, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		updated, _, _, found := tracker.CheckByIndex(body, target.Section, target.Index)
		return updated, found, nil
	})
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	total, checked := tracker.CountCheckboxes(newBody)
	fmt.Fprintf(ctx.Stdout, "✓ Checked: %s\n", checkboxLabel(target))
	fmt.Fprintf(ctx.Stdout, "  Progress: %d/%d\n", checked, total)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}

// printUncheckedItems lists every unchecked box with its [Section #index] label.
func printUncheckedItems(w io.Writer, body string) {
	for _, it := range tracker.ListCheckboxes(body) {
		if !it.Checked {
			fmt.Fprintf(w, "  %s\n", checkboxLabel(it))
		}
	}
}
