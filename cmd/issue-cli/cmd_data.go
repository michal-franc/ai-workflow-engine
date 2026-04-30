package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var dataCommand = &Command{
	Name:      "data",
	ShortHelp: "Per-issue structured data store (sub: add|list|set-status|set-comment|remove)",
	LongHelp: `Manage the per-issue sidecar data store.

Subcommands:
  add <slug> --description "..." [--status <s>]
  list <slug> [--json]
  set-status <slug> <id> <status>
  set-comment <slug> <id> --text "..."
  remove <slug> <id>`,
	Run: runData,
}

func init() {
	registerCommand(dataCommand)
}

func runData(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("data requires a subcommand\n\nUsage:\n  issue-cli data add <slug> --description \"...\" [--status <s>]\n  issue-cli data list <slug> [--json]\n  issue-cli data set-status <slug> <id> <status>\n  issue-cli data set-comment <slug> <id> --text \"...\"\n  issue-cli data remove <slug> <id>")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return runDataAdd(ctx, rest)
	case "list":
		return runDataList(ctx, rest)
	case "set-status":
		return runDataSetStatus(ctx, rest)
	case "set-comment":
		return runDataSetComment(ctx, rest)
	case "remove", "rm":
		return runDataRemove(ctx, rest)
	default:
		return fmt.Errorf("unknown data subcommand: %s\n\nValid: add, list, set-status, set-comment, remove", sub)
	}
}

func parseDataID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q: must be a positive integer", s)
	}
	return id, nil
}

func runDataAdd(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "data add")
	if err != nil {
		return err
	}
	fs := newFlagSet("data add", ctx)
	descFlag := fs.String("description", "", "entry description (required)")
	statusFlag := fs.String("status", "", "entry status")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	desc := normalizeEscapedText(*descFlag)
	if desc == "" {
		return fmt.Errorf("data add requires --description\n\nExample:\n  issue-cli data add %s --description \"finding\" --status \"open\"", slug)
	}
	status := normalizeEscapedText(*statusFlag)

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}
	id, err := tracker.AddEntry(issue.FilePath, desc, status)
	if err != nil {
		return fmt.Errorf("failed to add entry: %w", err)
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]interface{}{"id": id, "slug": issue.Slug})
	}
	fmt.Fprintln(ctx.Stdout, id)
	fmt.Fprintf(ctx.Stderr, "✓ Added entry #%d to %s\n", id, issue.Slug)
	return nil
}

func runDataList(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "data list")
	if err != nil {
		return err
	}
	fs := newFlagSet("data list", ctx)
	if err := fs.Parse(rest); err != nil {
		return err
	}

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}
	store, err := tracker.LoadData(issue.FilePath)
	if err != nil {
		return fmt.Errorf("failed to load data: %w", err)
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, store.Entries)
	}
	if len(store.Entries) == 0 {
		fmt.Fprintf(ctx.Stdout, "== %s — data ==\n(no entries)\n", issue.Slug)
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "== %s — data (%d) ==\n", issue.Slug, len(store.Entries))
	for _, e := range store.Entries {
		fmt.Fprintf(ctx.Stdout, "  #%d  [%s]  %s\n", e.ID, e.Status, e.Description)
		if e.Comment != "" {
			fmt.Fprintf(ctx.Stdout, "        comment: %s\n", e.Comment)
		}
	}
	return nil
}

func runDataSetStatus(ctx *Context, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("data set-status requires <slug> <id> <status>\n\nExample:\n  issue-cli data set-status my-issue 1 resolved")
	}
	slug := args[0]
	id, err := parseDataID(args[1])
	if err != nil {
		return err
	}
	status := args[2]

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}
	if err := tracker.SetEntryStatus(issue.FilePath, id, status); err != nil {
		return fmt.Errorf("failed to set status: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d status → %s\n", issue.Slug, id, status)
	return nil
}

func runDataSetComment(ctx *Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("data set-comment requires <slug> <id> --text \"...\"")
	}
	slug := args[0]
	id, err := parseDataID(args[1])
	if err != nil {
		return err
	}
	rest := args[2:]
	fs := newFlagSet("data set-comment", ctx)
	textFlag := fs.String("text", "", "comment text")
	bodyFlag := fs.String("body", "", "alias for --text")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	text := normalizeEscapedText(*textFlag)
	if text == "" {
		text = normalizeEscapedText(*bodyFlag)
	}
	if text == "" {
		text = strings.Join(fs.Args(), " ")
	}

	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}
	if err := tracker.SetEntryComment(issue.FilePath, id, text); err != nil {
		return fmt.Errorf("failed to set comment: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d comment updated\n", issue.Slug, id)
	return nil
}

func runDataRemove(ctx *Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("data remove requires <slug> <id>")
	}
	slug := args[0]
	id, err := parseDataID(args[1])
	if err != nil {
		return err
	}
	issue, _, err := findIssueOrErr(ctx.Project, slug)
	if err != nil {
		return err
	}
	if err := tracker.RemoveEntry(issue.FilePath, id); err != nil {
		return fmt.Errorf("failed to remove entry: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d removed\n", issue.Slug, id)
	return nil
}
