package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var updateCommand = &Command{
	Name:      "update",
	ShortHelp: `Change an issue's title (keeps its slug) and/or replace its body`,
	LongHelp: `Update issue title and/or body. At least one of --title / --body is required.

A new title keeps the issue's slug, so links, parent:/depends_on: references
and branch names stay valid: the old slug is pinned with a slug: field. Pass
--rename to let the slug follow the new title instead; the file is renamed to
match and issues that still reference the old slug are listed.

Examples:
  issue-cli update <slug> --title "new title"
  issue-cli update <slug> --title "new title" --rename
  issue-cli update <slug> --body "new body content"`,
	Run: runUpdate,
}

func init() {
	registerCommand(updateCommand)
}

func runUpdate(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "update")
	if err != nil {
		return err
	}
	fs := newFlagSet("update", ctx)
	titleFlag := fs.String("title", "", "new issue title (the slug is kept)")
	bodyFlag := fs.String("body", "", "new issue body")
	renameFlag := fs.Bool("rename", false, "let the slug follow the title: rename the file and drop the slug: pin")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}

	titleSet := flagWasSet(fs, "title")
	bodySet := flagWasSet(fs, "body")
	if !titleSet && !bodySet && !*renameFlag {
		return fmt.Errorf("update requires --title and/or --body\n\nExample:\n  issue-cli update %s --title \"new title\" --body \"new body content\"", slug)
	}

	issue, issues, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	update := tracker.IssueUpdate{}
	if titleSet || *renameFlag {
		title := issue.Title
		if titleSet {
			title = *titleFlag
		}
		update = tracker.RetitleUpdate(issue, title, *renameFlag)
	}
	if bodySet {
		b := normalizeEscapedText(*bodyFlag)
		update.Body = &b
	}
	newPath := issue.FilePath
	if *renameFlag {
		newPath = filepath.Join(filepath.Dir(issue.FilePath), tracker.Slugify(*update.Title)+".md")
		if _, err := os.Stat(newPath); err == nil && newPath != issue.FilePath {
			return fmt.Errorf("cannot rename: %s already exists\n\nPick another title, or keep the slug by dropping --rename", newPath)
		}
	}
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, update); err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}
	if newPath != issue.FilePath {
		if err := os.Rename(issue.FilePath, newPath); err != nil {
			return fmt.Errorf("updated, but renaming the file failed: %w", err)
		}
	}

	switch {
	case *renameFlag:
		newSlug := strings.TrimSuffix(issue.Slug, tracker.BaseSlug(issue)) + tracker.Slugify(*update.Title)
		fmt.Fprintf(ctx.Stdout, "✓ Renamed: %s → %s\n", issue.Slug, newSlug)
		printStaleReferences(ctx, issues, issue.Slug)
	case update.Slug != nil:
		fmt.Fprintf(ctx.Stdout, "✓ Retitled: %s (slug kept: pinned with slug: in frontmatter)\n", issue.Slug)
		fmt.Fprintf(ctx.Stdout, "  To let the slug follow the title: issue-cli update %s --title %q --rename\n", issue.Slug, *update.Title)
	default:
		fmt.Fprintf(ctx.Stdout, "✓ Updated: %s\n", issue.Slug)
	}
	fmt.Fprintf(ctx.Stdout, "file: %s\n", newPath)
	return nil
}

// printStaleReferences lists the issues whose parent: or depends_on: still
// name old after a rename; they are reported, not rewritten.
func printStaleReferences(ctx *Context, issues []*tracker.Issue, old string) {
	var stale []string
	for _, it := range issues {
		if it.Slug == old {
			continue
		}
		refs := tracker.DependsOn(it)
		if p := tracker.ParentSlug(it); p != "" {
			refs = append(refs, strings.ToLower(p))
		}
		for _, r := range refs {
			if r == old {
				stale = append(stale, it.Slug)
				break
			}
		}
	}
	if len(stale) == 0 {
		return
	}
	fmt.Fprintf(ctx.Stdout, "⚠ %d issue(s) still reference %s in parent:/depends_on: — update them:\n", len(stale), old)
	for _, s := range stale {
		fmt.Fprintf(ctx.Stdout, "  %s\n", s)
	}
}
