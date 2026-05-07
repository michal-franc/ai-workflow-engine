package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var dataCommand = &Command{
	Name:      "data",
	ShortHelp: "Per-issue structured data store (sub: add|list|set-status|set-tier|set-comment|remove)",
	LongHelp: `Manage the per-issue sidecar data store.

Subcommands:
  add <slug> --description "..." [--status <s>] [--tier <t>]
  list <slug> [--json]
  set-status <slug> <id> <status>
  set-tier <slug> <id> <tier>
  set-comment <slug> <id> --text "..."
  remove <slug> <id>

Tier is the optional second axis (e.g. critical/nice, S1/S2/S3) configured per
workflow via a "<!-- data tiers=... -->" marker.`,
	Run: runData,
}

func init() {
	registerCommand(dataCommand)
}

func runData(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("data requires a subcommand\n\nUsage:\n  issue-cli data add <slug> --description \"...\" [--status <s>] [--tier <t>]\n  issue-cli data list <slug> [--json]\n  issue-cli data set-status <slug> <id> <status>\n  issue-cli data set-tier <slug> <id> <tier>\n  issue-cli data set-comment <slug> <id> --text \"...\"\n  issue-cli data remove <slug> <id>")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return runDataAdd(ctx, rest)
	case "list":
		return runDataList(ctx, rest)
	case "set-status":
		return runDataSetStatus(ctx, rest)
	case "set-tier":
		return runDataSetTier(ctx, rest)
	case "set-comment":
		return runDataSetComment(ctx, rest)
	case "remove", "rm":
		return runDataRemove(ctx, rest)
	default:
		return fmt.Errorf("unknown data subcommand: %s\n\nValid: add, list, set-status, set-tier, set-comment, remove", sub)
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
	tierFlag := fs.String("tier", "", "entry tier (must match workflow's tiers= marker if set)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	desc := normalizeEscapedText(*descFlag)
	if desc == "" {
		return fmt.Errorf("data add requires --description\n\nExample:\n  issue-cli data add %s --description \"finding\" --status \"open\" --tier \"🔴 critical\"", slug)
	}
	status := normalizeEscapedText(*statusFlag)
	tier := normalizeEscapedText(*tierFlag)

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if tier != "" {
		if err := validateTier(issue, tier); err != nil {
			return err
		}
	}
	id, err := tracker.AddEntryWithTier(issue.FilePath, desc, status, tier)
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

// validateTier rejects --tier values that don't match the issue body's
// declared tiers= marker. When the marker has no tiers (or is missing) we
// still allow any value — the workflow may rely on the agent to pick from a
// list it knows about, and we don't want to second-guess unstructured use.
func validateTier(issue *tracker.Issue, tier string) error {
	marker := tracker.ParseDataMarker(issue.BodyRaw)
	if len(marker.Tiers) == 0 {
		return nil
	}
	for _, t := range marker.Tiers {
		if t == tier {
			return nil
		}
	}
	return fmt.Errorf("tier %q is not in the workflow's declared tiers: %s", tier, strings.Join(marker.Tiers, ", "))
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

	issue, _, err := findIssueOrErr(ctx, slug)
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
	marker := tracker.ParseDataMarker(issue.BodyRaw)
	hasTier := len(marker.Tiers) > 0 || anyEntryHasTier(store.Entries)
	fmt.Fprintf(ctx.Stdout, "== %s — data (%d) ==\n", issue.Slug, len(store.Entries))
	for _, e := range store.Entries {
		if hasTier {
			fmt.Fprintf(ctx.Stdout, "  #%d  [%s]  <%s>  %s\n", e.ID, e.Status, e.Tier, e.Description)
		} else {
			fmt.Fprintf(ctx.Stdout, "  #%d  [%s]  %s\n", e.ID, e.Status, e.Description)
		}
		if e.Comment != "" {
			fmt.Fprintf(ctx.Stdout, "        comment: %s\n", e.Comment)
		}
	}
	return nil
}

func anyEntryHasTier(entries []tracker.DataEntry) bool {
	for _, e := range entries {
		if e.Tier != "" {
			return true
		}
	}
	return false
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

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if err := tracker.SetEntryStatus(issue.FilePath, id, status); err != nil {
		return fmt.Errorf("failed to set status: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d status → %s\n", issue.Slug, id, status)
	return nil
}

func runDataSetTier(ctx *Context, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("data set-tier requires <slug> <id> <tier>\n\nExample:\n  issue-cli data set-tier my-issue 1 \"🔴 critical\"\n\nPass an empty tier to clear: issue-cli data set-tier my-issue 1 \"\"")
	}
	slug := args[0]
	id, err := parseDataID(args[1])
	if err != nil {
		return err
	}
	tier := args[2]

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if tier != "" {
		if err := validateTier(issue, tier); err != nil {
			return err
		}
	}
	if err := tracker.SetEntryTier(issue.FilePath, id, tier); err != nil {
		return fmt.Errorf("failed to set tier: %w", err)
	}
	if tier == "" {
		fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d tier cleared\n", issue.Slug, id)
	} else {
		fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d tier → %s\n", issue.Slug, id, tier)
	}
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

	issue, _, err := findIssueOrErr(ctx, slug)
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
	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if err := tracker.RemoveEntry(issue.FilePath, id); err != nil {
		return fmt.Errorf("failed to remove entry: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s entry #%d removed\n", issue.Slug, id)
	return nil
}
