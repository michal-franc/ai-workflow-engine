package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var setTypeCommand = &Command{
	Name:      "set-type",
	ShortHelp: "Change an issue's type of work (only at its first status)",
	LongHelp: `Change an issue's type of work (feature, tweak, bugfix, … as defined under
types: in workflow.yaml). The type picks the issue's path through the statuses.

Agents may change the type only while the issue is still at its type's first
status (triage). After that the type is the human's call: they change it in
the issue viewer (detail sidebar → Type).

If the current status is not on the new type's path, the issue moves to the
new type's first status.

Example:
  issue-cli set-type ui/bigger-play-cards tweak`,
	Run: runSetType,
}

func init() {
	registerCommand(setTypeCommand)
}

func runSetType(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "set-type")
	if err != nil {
		return err
	}
	fs := newFlagSet("set-type", ctx)
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("set-type needs exactly one type\n\nUsage:\n  issue-cli set-type %s <type>", slug)
	}
	newType := strings.TrimSpace(fs.Arg(0))

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	wf := ctx.Project.LoadWorkflow()
	if !wf.HasTypes() {
		return fmt.Errorf("this project defines no types: add a types: block to workflow.yaml (see docs/Workflow/types.md)")
	}
	if _, ok := wf.Types[newType]; !ok {
		return fmt.Errorf("unknown type %q (types: %s)", newType, strings.Join(wf.TypeNames(), ", "))
	}
	current, _ := wf.ResolveType(issue.Type)
	if current == newType && issue.Type == newType {
		fmt.Fprintf(ctx.Stdout, "✓ %s is already type %s\n", issue.Slug, newType)
		return nil
	}
	first := wf.FirstStatus(issue.Type)
	if issue.Status != first {
		return fmt.Errorf("%s is at %q: type changes after %q are the human's call.\nAsk them to change Type in the issue viewer (detail sidebar → Type):\n  %s",
			issue.Slug, issue.Status, first, issueURL(ctx.Project, issue.Slug))
	}

	pick := ""
	if wf.ForType(newType).GetStatusIndex(issue.Status) == -1 {
		pick = wf.FirstStatus(newType)
	}
	status, err := wf.RetypeStatus(issue, newType, pick)
	if err != nil {
		return err
	}
	move := ""
	if status != issue.Status {
		move = status
	}
	if err := tracker.SetIssueType(issue.FilePath, newType, move); err != nil {
		return fmt.Errorf("failed to set type: %w", err)
	}

	from := issue.Type
	if from == "" {
		from = current + " (default)"
	}
	fmt.Fprintf(ctx.Stdout, "✓ Type: %s → %s on %s\n", from, newType, issue.Slug)
	if move != "" {
		fmt.Fprintf(ctx.Stdout, "✓ Status: %s → %s (not on %s's path)\n", issue.Status, move, newType)
	}
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	fmt.Fprintf(ctx.Stdout, "  Path: %s\n", wf.ForType(newType).PathLine())
	fmt.Fprintf(ctx.Stdout, "Run 'issue-cli process transitions %s' for this type's rules.\n", issue.Slug)
	return nil
}
