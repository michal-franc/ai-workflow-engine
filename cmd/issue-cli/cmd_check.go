package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var checkCommand = &Command{
	Name:      "check",
	ShortHelp: "Check off checkboxes by id (several at once), text, or section + index",
	LongHelp: `Mark checkboxes as checked. Address them by:

  ids            issue-cli check <slug> D3 D4 AC1      (several in one call)
  long id        issue-cli check <slug> "Design#3"
  whole section  issue-cli check <slug> --section "Design" --all
  text           issue-cli check <slug> "Code changes complete"
  index          issue-cli check <slug> --section "Design" --index 2
  position       issue-cli check <slug> --index 5

Run 'issue-cli checklist <slug>' to see each box's id. An id is the section's
initials plus the box's 1-based index in that section: D3 is the 3rd box under
"## Design", AC2 the 2nd under "## Acceptance Criteria". When two sections share
initials, the later one gets more letters of its first word (Documentation is
"Do" when Design holds "D"). Ids are case-insensitive and stable: indexes count
checked and unchecked boxes, and new sections are appended, so a box's id never
changes as work progresses.

Several ids in one call are all-or-nothing: if any id does not exist, nothing
is ticked. A box that is already checked is reported, not an error.

A text query matches a box whose label contains it (case-insensitive). If it
matches more than one unchecked box, check errors and lists the candidates
with their ids. Flags go before ids or text.`,
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
	allFlag := fs.Bool("all", false, "check every open box in --section")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}
	section := strings.TrimSpace(*sectionFlag)
	positional := fs.Args()
	query := strings.Join(positional, " ")

	if *allFlag {
		if section == "" {
			return fmt.Errorf("--all requires --section\n\nExample:\n  issue-cli check %s --section \"Design\" --all", slug)
		}
		if *indexFlag != 0 || query != "" {
			return fmt.Errorf("--all ticks a whole section; do not combine it with --index, ids or text")
		}
	}
	if !*allFlag && *indexFlag == 0 && query == "" {
		return fmt.Errorf("check requires checkbox ids, a text query, or --index\n\nExamples:\n  issue-cli check %s D1 D2          # ids from 'issue-cli checklist %s'\n  issue-cli check %s \"Code changes complete\"\n  issue-cli check %s --section \"Design\" --all", slug, slug, slug, slug)
	}
	if *indexFlag != 0 && query != "" {
		return fmt.Errorf("pass either a text query or --index, not both")
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}

	switch {
	case *allFlag:
		return checkSectionAll(ctx, issue, section)
	case *indexFlag != 0:
		return checkByIndex(ctx, issue, section, *indexFlag)
	case section == "" && looksLikeRefs(issue.BodyRaw, positional):
		return checkByRefs(ctx, issue, positional)
	}
	return checkByText(ctx, issue, section, query)
}

// looksLikeRefs reports whether args should be read as checkbox ids rather
// than a text query: every arg is shaped like an id and at least one names a
// section of this issue. A lone word such as "phase1" whose letters name no
// section stays a text query.
func looksLikeRefs(body string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	items := tracker.ListCheckboxes(body)
	known := false
	for _, a := range args {
		if !tracker.IsCheckboxRefShaped(a) {
			return false
		}
		if _, st := tracker.ResolveCheckboxRef(items, a); st != tracker.RefNotARef {
			known = true
		}
	}
	return known
}

// checkboxLabel renders a checkbox as "<id> [Section #index] text" (or
// "[#index] text" when the box sits before any section heading).
func checkboxLabel(it tracker.CheckboxItem) string {
	if it.Section == "" {
		return fmt.Sprintf("[#%d] %s", it.Index, it.Text)
	}
	label := fmt.Sprintf("[%s #%d] %s", it.Section, it.Index, it.Text)
	if it.ID != "" {
		label = it.ID + " " + label
	}
	return label
}

// printCheckResult reports what a check call changed: one line per ticked or
// already-checked box, overall progress plus progress of each touched section,
// and the issue file.
func printCheckResult(w io.Writer, issue *tracker.Issue, newBody string, ticked, already []tracker.CheckboxItem) {
	for _, it := range ticked {
		fmt.Fprintf(w, "✓ Checked: %s\n", checkboxLabel(it))
	}
	for _, it := range already {
		fmt.Fprintf(w, "  Already checked: %s\n", checkboxLabel(it))
	}
	total, checked := tracker.CountCheckboxes(newBody)
	var sections []string
	seen := map[string]bool{}
	for _, it := range append(append([]tracker.CheckboxItem{}, ticked...), already...) {
		if it.Section == "" || seen[it.Section] {
			continue
		}
		seen[it.Section] = true
		st, sc := tracker.CountCheckboxesInSection(newBody, it.Section)
		sections = append(sections, fmt.Sprintf("%s %d/%d", it.Section, sc, st))
	}
	if len(sections) > 0 {
		fmt.Fprintf(w, "  Progress: %d/%d (%s)\n", checked, total, strings.Join(sections, ", "))
	} else {
		fmt.Fprintf(w, "  Progress: %d/%d\n", checked, total)
	}
	fmt.Fprintf(w, "file: %s\n", issue.FilePath)
}

func checkByRefs(ctx *Context, issue *tracker.Issue, refs []string) error {
	var ticked, already []tracker.CheckboxItem
	var unknown []string
	newBody, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		items := tracker.ListCheckboxes(body)
		seen := map[int]bool{}
		var lines []int
		for _, ref := range refs {
			it, st := tracker.ResolveCheckboxRef(items, ref)
			if st != tracker.RefFound {
				unknown = append(unknown, ref)
				continue
			}
			if seen[it.Line] {
				continue
			}
			seen[it.Line] = true
			if it.Checked {
				already = append(already, it)
				continue
			}
			ticked = append(ticked, it)
			lines = append(lines, it.Line)
		}
		if len(unknown) > 0 || len(lines) == 0 {
			return body, false, nil
		}
		return tracker.CheckLines(body, lines), true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	if len(unknown) > 0 {
		fmt.Fprintf(ctx.Stdout, "No checkbox with id %s — nothing was ticked.\n\n", strings.Join(unknown, ", "))
		fmt.Fprintln(ctx.Stdout, "Checkboxes:")
		printCheckboxes(ctx.Stdout, newBody)
		return fmt.Errorf("unknown checkbox id(s): %s", strings.Join(unknown, ", "))
	}
	for i := range ticked {
		ticked[i].Checked = true
	}
	printCheckResult(ctx.Stdout, issue, newBody, ticked, already)
	return nil
}

func checkSectionAll(ctx *Context, issue *tracker.Issue, section string) error {
	var ticked, inSection []tracker.CheckboxItem
	newBody, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		var lines []int
		for _, it := range tracker.ListCheckboxes(body) {
			if !strings.EqualFold(it.Section, section) {
				continue
			}
			inSection = append(inSection, it)
			if !it.Checked {
				ticked = append(ticked, it)
				lines = append(lines, it.Line)
			}
		}
		if len(lines) == 0 {
			return body, false, nil
		}
		return tracker.CheckLines(body, lines), true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	if len(inSection) == 0 {
		fmt.Fprintf(ctx.Stdout, "No checkboxes in section %q\n\n", section)
		fmt.Fprintln(ctx.Stdout, "Checkboxes:")
		printCheckboxes(ctx.Stdout, newBody)
		return fmt.Errorf("no checkboxes in section %q", section)
	}
	name := inSection[0].Section
	if len(ticked) == 0 {
		fmt.Fprintf(ctx.Stdout, "Nothing to check: section %q already complete (%d/%d)\n", name, len(inSection), len(inSection))
		fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
		return nil
	}
	for i := range ticked {
		ticked[i].Checked = true
	}
	printCheckResult(ctx.Stdout, issue, newBody, ticked, nil)
	return nil
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
		printCheckResult(ctx.Stdout, issue, newBody, nil, []tracker.CheckboxItem{matched})
		return nil
	}
	matched.Checked = true
	printCheckResult(ctx.Stdout, issue, newBody, []tracker.CheckboxItem{matched}, nil)
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
		if first.ID != "" {
			fmt.Fprintf(ctx.Stdout, "\nRe-run with the box's id, e.g.:\n  issue-cli check %s %s\n", issue.Slug, first.ID)
		} else {
			fmt.Fprintf(ctx.Stdout, "\nRe-run with the exact box, e.g.:\n  issue-cli check %s --index %d\n", issue.Slug, first.Index)
		}
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
	target.Checked = true
	printCheckResult(ctx.Stdout, issue, newBody, []tracker.CheckboxItem{target}, nil)
	return nil
}

// printUncheckedItems lists every unchecked box with its id and
// [Section #index] label.
func printUncheckedItems(w io.Writer, body string) {
	for _, it := range tracker.ListCheckboxes(body) {
		if !it.Checked {
			fmt.Fprintf(w, "  %s\n", checkboxLabel(it))
		}
	}
}
