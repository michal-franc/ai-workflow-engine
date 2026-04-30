package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var setMetaCommand = &Command{
	Name:      "set-meta",
	ShortHelp: "Set/clear a frontmatter field",
	LongHelp: `Set or clear a frontmatter field on an issue.

Examples:
  issue-cli set-meta <slug> --key waiting --value "design review"
  issue-cli set-meta <slug> --key waiting --clear`,
	Run: runSetMeta,
}

func init() {
	registerCommand(setMetaCommand)
}

func runSetMeta(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "set-meta")
	if err != nil {
		return err
	}
	fs := newFlagSet("set-meta", ctx)
	keyFlag := fs.String("key", "", "frontmatter key")
	valueFlag := fs.String("value", "", "value to set")
	clearFlag := fs.Bool("clear", false, "clear the field")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	key := *keyFlag
	value := normalizeEscapedText(*valueFlag)
	clear := *clearFlag
	if key == "" {
		return fmt.Errorf("set-meta requires --key\n\nExamples:\n  issue-cli set-meta %s --key waiting --value \"waiting on design review\"\n  issue-cli set-meta %s --key waiting --clear", slug, slug)
	}
	if clear && flagWasSet(fs, "value") {
		return fmt.Errorf("set-meta: --value and --clear are mutually exclusive")
	}
	if !clear && value == "" {
		return fmt.Errorf("set-meta requires --value or --clear\n\nExamples:\n  issue-cli set-meta %s --key waiting --value \"...\"\n  issue-cli set-meta %s --key waiting --clear", slug, slug)
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if err := tracker.SetFrontmatterField(issue.FilePath, key, value, clear); err != nil {
		return fmt.Errorf("failed to set frontmatter: %w", err)
	}
	if clear {
		fmt.Fprintf(ctx.Stdout, "✓ Cleared %s on %s\n", key, issue.Slug)
	} else {
		fmt.Fprintf(ctx.Stdout, "✓ Set %s = %q on %s\n", key, value, issue.Slug)
	}
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}
