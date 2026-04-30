package main

import (
	"fmt"
)

var doneCommand = &Command{
	Name:      "done",
	ShortHelp: "Mark issue as done (validates and auto-unclaims)",
	LongHelp:  "Run shipping → done validations and finalize the issue. Refuses to run twice.",
	Run:       runDone,
}

func init() {
	registerCommand(doneCommand)
}

func runDone(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "done")
	if err != nil {
		return err
	}
	fs := newFlagSet("done", ctx)
	if err := fs.Parse(rest); err != nil {
		return err
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	wf := ctx.Project.LoadWorkflowForIssue(issue)
	updated, err := wf.MarkIssueDoneOnce(issue.FilePath, slug)
	if err != nil {
		return err
	}

	fmt.Fprintln(ctx.Stdout, "== Validation ==")
	fmt.Fprintln(ctx.Stdout, "✓ done: all checks passed")
	fmt.Fprintf(ctx.Stdout, "\n✓ Status → %s\n", statusLabel(wf, updated.Status))
	fmt.Fprintln(ctx.Stdout, "✓ Assignee cleared")
	fmt.Fprintf(ctx.Stdout, "file: %s\n", updated.FilePath)
	return nil
}
