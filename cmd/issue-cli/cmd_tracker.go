package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/privacy"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var trackerCommand = &Command{
	Name:      "tracker",
	ShortHelp: "The private tracker repo for the work files (sub: init|sync)",
	LongHelp: `The private tracker repo keeps the project's work files (issues/, sessions/,
retros/, bugs/, .health/snapshots/) out of the code repo. Its git folder sits
outside the checkout (default <checkout's parent>/.trackers/<slug>.git, or
tracker_git_dir: in projects.yaml) and its work tree is the checkout, so every
path stays where it is. The viewer syncs it every 10 minutes.

Needs the owner's choice in workflow.yaml first:
  privacy:
    work_files: tracker
    decided: "2026-10-24, owner: \"…their words…\""

Subcommands:
  init --remote <url>
      The owner creates the private tracker repo and gives its URL; agents
      never create repos. Creates the git folder (or adopts an existing one),
      adds the URL as its origin, and ignores the work paths in the code repo's
      .gitignore. Work paths the code repo still tracks are listed with the git
      rm --cached to run; init never runs it. Then a first sync. --remote may be
      left out only when an adopted tracker already has an origin.
  sync [--quiet]
      Commit every change under the work paths ("tracker: N files"), pull with
      rebase, push. A conflict keeps the local commit and pushes nothing. Run it
      before a handoff and, on another machine or in a cloud session, before
      work (pull) and after (push). --quiet prints only errors.
See docs/workflow.md, "Private work and code".`,
	Run:         runTracker,
	Subcommands: []string{"init", "sync"},
}

func init() {
	registerCommand(trackerCommand)
}

const trackerNotChosen = `privacy.work_files is not tracker in workflow.yaml: the owner has not chosen a private tracker.
Ask them, then record it:
  privacy:
    work_files: tracker
    decided: "<date>, owner: \"<their words>\""
(issue-cli workflow upgrade shows the privacy entry and its steps)`

func runTracker(ctx *Context, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "tracker requires a subcommand\n\nUsage:\n  issue-cli tracker init --remote <url>\n  issue-cli tracker sync [--quiet]"}
	}
	switch args[0] {
	case "init":
		return runTrackerInit(ctx, args[1:])
	case "sync":
		return runTrackerSync(ctx, args[1:])
	}
	return &usageError{msg: fmt.Sprintf("unknown tracker subcommand %q (init, sync)", args[0])}
}

// projectTracker is the project's tracker, or an error when the owner has
// not chosen one.
func projectTracker(ctx *Context) (privacy.Tracker, *tracker.PrivacyConfig, error) {
	if ctx.Project == nil {
		return privacy.Tracker{}, nil, fmt.Errorf("tracker needs a project — pass --project <slug> or run from a project root")
	}
	cfg := ctx.Project.LoadWorkflow().Privacy
	if !cfg.Tracker() {
		return privacy.Tracker{}, cfg, fmt.Errorf("%s", trackerNotChosen)
	}
	return privacy.ForProject(ctx.Project, cfg, ctx.Now()).TrackerFor(), cfg, nil
}

func runTrackerInit(ctx *Context, args []string) error {
	fs := newFlagSet("tracker init", ctx)
	remote := fs.String("remote", "", "URL of the private tracker repo (the owner creates it)")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	t, _, err := projectTracker(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*remote) == "" && t.Origin() == "" {
		return &usageError{msg: "tracker init needs --remote <url>: the private repo the owner created for the work files (e.g. <name>-tracker). Ask the owner for it; agents never create repos.\n  issue-cli tracker init --remote <url>"}
	}
	res, err := t.Init(strings.TrimSpace(*remote))
	if err != nil {
		return err
	}
	out := ctx.Stdout
	if res.Adopted {
		fmt.Fprintf(out, "✓ Adopted the tracker git folder %s (work tree %s)\n", t.GitDir, t.Root)
	} else {
		fmt.Fprintf(out, "✓ Created the tracker git folder %s (work tree %s)\n", t.GitDir, t.Root)
	}
	if res.RemoteSet {
		fmt.Fprintf(out, "✓ Remote origin: %s\n", *remote)
	}
	if len(res.Ignored) > 0 {
		fmt.Fprintf(out, "✓ .gitignore now ignores %s (commit it in the code repo)\n", strings.Join(res.Ignored, ", "))
	}
	if len(res.Tracked) > 0 {
		fmt.Fprintf(out, "! The code repo still tracks %s. With the owner, untrack them (the files stay on disk and in the tracker):\n", strings.Join(res.Tracked, ", "))
		fmt.Fprintf(out, "    git rm -r -q --cached -- %s && git commit -m \"chore: work files move to the private tracker\"\n", strings.Join(res.Tracked, " "))
		fmt.Fprintln(out, "  Their old history stays in the code repo; cleaning it is a separate, one-time step (docs/workflow.md, \"Private work and code\").")
	}
	if err := installPrivacyHook(ctx, t.Root); err != nil {
		return err
	}
	sres, err := t.Sync(ctx.Now())
	if err != nil {
		return fmt.Errorf("first sync: %w", err)
	}
	printSync(ctx, sres, false)
	return nil
}

func runTrackerSync(ctx *Context, args []string) error {
	fs := newFlagSet("tracker sync", ctx)
	quiet := fs.Bool("quiet", false, "print only errors")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	t, _, err := projectTracker(ctx)
	if err != nil {
		return err
	}
	res, err := t.Sync(ctx.Now())
	if err != nil {
		return err
	}
	printSync(ctx, res, *quiet)
	return nil
}

func printSync(ctx *Context, res privacy.SyncResult, quiet bool) {
	if quiet {
		return
	}
	out := ctx.Stdout
	switch {
	case res.Committed > 0:
		fmt.Fprintf(out, "✓ Committed tracker: %d %s\n", res.Committed, pluralWord(res.Committed, "file", "files"))
	default:
		fmt.Fprintln(out, "✓ Nothing changed under the work paths")
	}
	if res.Pulled {
		fmt.Fprintln(out, "✓ Pulled changes from the tracker's remote")
	}
	switch {
	case res.NoRemote:
		fmt.Fprintln(out, "! The tracker has no remote: its history is on this disk only (issue-cli tracker init --remote <url>)")
	case res.Pushed:
		fmt.Fprintf(out, "✓ Pushed %s to origin\n", res.Branch)
	default:
		fmt.Fprintln(out, "✓ Up to date with origin")
	}
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
