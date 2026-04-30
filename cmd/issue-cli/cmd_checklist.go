package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var checklistCommand = &Command{
	Name:      "checklist",
	ShortHelp: "Show checkbox status for an issue",
	LongHelp:  "Print every `- [ ]` and `- [x]` line in the issue body and the overall checked-vs-total count.",
	Run:       runChecklist,
}

func init() {
	registerCommand(checklistCommand)
}

func runChecklist(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "checklist")
	if err != nil {
		return err
	}
	fs := newFlagSet("checklist", ctx)
	if err := fs.Parse(rest); err != nil {
		return err
	}

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}
	total, checked := tracker.CountCheckboxes(issue.BodyRaw)

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]interface{}{
			"total": total, "checked": checked,
		})
	}

	fmt.Fprintf(ctx.Stdout, "== Checklist (%d/%d) ==\n", checked, total)
	printCheckboxes(ctx.Stdout, issue.BodyRaw)
	return nil
}
