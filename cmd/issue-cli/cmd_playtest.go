package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/playtestruns"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var playtestCommand = &Command{
	Name:      "playtest",
	ShortHelp: "Tidy playtest runs: prune worktrees' .playtest/runs to what the Evidence tab needs",
	LongHelp: `Playtest runs (.playtest/runs/<session>/<time>-<mode>/: results.yaml and
shots/) pile up in every worktree, since ux-test runs every session. The
Evidence tab needs only the latest auto and judge run of each of an issue's
own sessions (results.yaml issue: is the issue; a game's modes ux-test and
playtest count as auto and judge).

At → done issue-cli keeps those runs, moves them to the project root's
.playtest/runs (so removing the worktree loses no evidence) and deletes the
rest, and says "archived N of M runs, deleted K" (N of the issue's own M
runs; 0 of a non-zero M is a ⚠ warning). An own run goes only when a newer
run of its session and mode is kept; a run whose shots a kept -cached run
reuses, an own run of another mode and a run it can't classify are kept.
prune does the same for worktrees that exist already:

  done issue   keep the latest own runs, move them to the root, delete the rest
  open issue   delete only the runs of other issues' sessions

Usage:
  issue-cli playtest prune <slug> [--dry-run]   one issue's worktree
  issue-cli playtest prune --all [--dry-run]    every worktree that belongs to an issue

--dry-run reports the MB it would delete, move and keep, and changes nothing.`,
	Subcommands: []string{"prune"},
	Run:         runPlaytest,
}

func init() {
	registerCommand(playtestCommand)
}

func runPlaytest(ctx *Context, args []string) error {
	if len(args) == 0 || args[0] != "prune" {
		return &usageError{msg: "playtest needs a subcommand\n\nUsage:\n  issue-cli playtest prune <slug> [--dry-run]\n  issue-cli playtest prune --all [--dry-run]"}
	}
	fs := newFlagSet("playtest prune", ctx)
	all := fs.Bool("all", false, "every worktree that belongs to an issue")
	dryRun := fs.Bool("dry-run", false, "report without changing anything")
	if err := parseFlagsArgs(ctx, fs, args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return unexpectedArgError(fs, fs.Arg(1))
	}
	if *all == (fs.NArg() == 1) {
		return &usageError{msg: "playtest prune takes a <slug> or --all\n\nExamples:\n  issue-cli playtest prune <slug> --dry-run\n  issue-cli playtest prune --all"}
	}
	root := projectRoot(ctx.Project)

	var targets []*tracker.Issue
	var orphans []string
	if *all {
		issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
		if err != nil {
			return fmt.Errorf("cannot load issues: %w", err)
		}
		bySlug := map[string]*tracker.Issue{}
		for _, is := range issues {
			bySlug[is.Slug] = is
		}
		for _, slug := range worktreesWithRuns(root) {
			if is, ok := bySlug[slug]; ok {
				targets = append(targets, is)
			} else {
				orphans = append(orphans, slug)
			}
		}
	} else {
		issue, _, err := findIssueOrErr(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		targets = append(targets, issue)
	}

	var total playtestruns.Report
	var openKept int64
	before := int64(0)
	var archived []string // one line per done issue
	for _, is := range targets {
		done := is.Status == "done"
		rep, err := playtestruns.Prune(root, is.Slug, done, *dryRun)
		if err != nil {
			return fmt.Errorf("%s: %w", is.Slug, err)
		}
		before += rep.KeptBytes + rep.MovedBytes + rep.DeletedBytes
		if !done {
			openKept += rep.KeptBytes
		} else if rep.Own+rep.Deleted > 0 {
			line, warn := archiveLine(rep)
			mark := " "
			if warn {
				mark = "⚠"
			}
			archived = append(archived, fmt.Sprintf("%s %s: %s", mark, is.Slug, line))
		}
		total.Add(rep)
	}

	title := "Playtest prune"
	if *dryRun {
		title += " (dry run)"
	}
	fmt.Fprintf(ctx.Stdout, "== %s ==\n", title)
	fmt.Fprintf(ctx.Stdout, "%s, %s in .playtest/runs\n", countOf(len(targets), "worktree", "worktrees"), playtestruns.MB(before))
	fmt.Fprintf(ctx.Stdout, "  delete %9s  %s, runs of other issues' sessions and older own runs", playtestruns.MB(total.DeletedBytes), countOf(total.Deleted, "run", "runs"))
	if total.CacheBytes > 0 {
		fmt.Fprintf(ctx.Stdout, "; done issues' runner binaries (runs/.bin, %s)", playtestruns.MB(total.CacheBytes))
	}
	fmt.Fprintln(ctx.Stdout)
	fmt.Fprintf(ctx.Stdout, "  move   %9s  %s, the latest own run per session of done issues → .playtest/runs/\n", playtestruns.MB(total.MovedBytes), countOf(total.Moved, "run", "runs"))
	fmt.Fprintf(ctx.Stdout, "  keep   %9s  open issues' own runs stay in their worktree", playtestruns.MB(total.KeptBytes))
	if total.KeptBytes > openKept {
		fmt.Fprint(ctx.Stdout, "; runs already in the root stay")
	}
	fmt.Fprintln(ctx.Stdout)
	for _, l := range archived {
		fmt.Fprintln(ctx.Stdout, l)
	}
	if len(orphans) > 0 {
		fmt.Fprintf(ctx.Stdout, "Skipped %s with no issue: %s\n", countOf(len(orphans), "worktree", "worktrees"), strings.Join(orphans, ", "))
	}
	after := total.MovedBytes + total.KeptBytes
	if *dryRun {
		fmt.Fprintf(ctx.Stdout, "After: %s. Run without --dry-run to apply.\n", playtestruns.MB(after))
	} else {
		fmt.Fprintf(ctx.Stdout, "✓ Now %s.\n", playtestruns.MB(after))
	}
	return nil
}

// worktreesWithRuns lists the slugs of root's worktrees that hold
// .playtest/runs, sorted. A worktree is .worktrees/<system>/<name> or
// .worktrees/<name>; hidden folders (.ci) are not issues' worktrees.
func worktreesWithRuns(root string) []string {
	base := filepath.Join(root, ".worktrees")
	var slugs []string
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == base {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if _, err := os.Stat(playtestruns.RunsDir(p)); err == nil {
			slugs = append(slugs, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		if strings.Count(filepath.ToSlash(rel), "/") >= 1 {
			return filepath.SkipDir // two levels at most
		}
		return nil
	})
	sort.Strings(slugs)
	return slugs
}

// archivePlaytestRuns runs at → done: the issue's latest own runs move to
// the project root, the rest of its worktree's runs go. It never fails the
// transition; a problem is one line.
func archivePlaytestRuns(ctx *Context, issue *tracker.Issue) {
	rep, err := playtestruns.Archive(projectRoot(ctx.Project), issue.Slug)
	if ctx.JSONOutput {
		return
	}
	if err != nil {
		fmt.Fprintf(ctx.Stdout, "playtest: %v\n", err)
		return
	}
	if rep.Moved+rep.Kept+rep.Deleted == 0 {
		return
	}
	line, warn := archiveLine(rep)
	mark := "✓"
	if warn {
		mark = "⚠"
	}
	fmt.Fprintf(ctx.Stdout, "%s playtest: %s\n", mark, line)
}

// archiveLine says what archiving a done issue's runs did: "archived N of
// M runs, deleted K", N of the issue's own M runs. Archiving none of a
// non-zero M is a warning (warn): the Evidence tab loses its playtest.
func archiveLine(rep playtestruns.Report) (string, bool) {
	line := fmt.Sprintf("archived %d of %s, deleted %d", rep.Archived, countOf(rep.Own, "run", "runs"), rep.Deleted)
	if rep.Archived == 0 && rep.Own > 0 {
		return line + ": none of this issue's runs reached .playtest/runs/, so its Evidence tab has no playtest; report it with issue-cli report-bug", true
	}
	var parts []string
	if rep.Moved+rep.Kept > 0 {
		kept := "the latest"
		if len(rep.Sessions) > 0 {
			kept += " of " + joinAnd(rep.Sessions)
		}
		parts = append(parts, fmt.Sprintf("%s (%s) moved to .playtest/runs/", kept, playtestruns.MB(rep.MovedBytes+rep.KeptBytes)))
	}
	if rep.Deleted > 0 {
		parts = append(parts, playtestruns.MB(rep.DeletedBytes)+" deleted")
	}
	if len(parts) == 0 {
		return line, false
	}
	return line + "; " + strings.Join(parts, ", "), false
}

// countOf is "1 run" / "3 runs".
func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func joinAnd(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
