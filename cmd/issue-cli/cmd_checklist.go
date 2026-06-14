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

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	total, checked := tracker.CountCheckboxes(issue.BodyRaw)

	if ctx.JSONOutput {
		items := tracker.ListCheckboxes(issue.BodyRaw)
		boxes := make([]map[string]interface{}, 0, len(items))
		for _, it := range items {
			boxes = append(boxes, map[string]interface{}{
				"section": it.Section,
				"index":   it.Index,
				"text":    it.Text,
				"checked": it.Checked,
			})
		}
		return writeJSON(ctx.Stdout, map[string]interface{}{
			"total": total, "checked": checked, "items": boxes,
		})
	}

	fmt.Fprintf(ctx.Stdout, "== Checklist (%d/%d) ==\n", checked, total)
	printCheckboxes(ctx.Stdout, issue.BodyRaw)
	return nil
}
