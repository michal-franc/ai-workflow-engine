package main

import (
	"encoding/json"
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/privacy"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var privacyCommand = &Command{
	Name:      "privacy",
	ShortHelp: "Where the work files and the dev history go (sub: check|hook|pre-push)",
	LongHelp: `The project's privacy choice (privacy: in workflow.yaml) and its guard.

Subcommands:
  check
      One line per badge the Status page shows (work files; code), plus the
      pre-push guard, each with its fix. Exit 1 when a line is red. --json
      (global flag) prints the same as JSON.
  hook
      Install the pre-push guard in this repository (shared by every
      worktree). A pre-push hook that is not ours is kept as pre-push.local and
      still runs first.
  pre-push <remote> <url>
      What the hook runs, with git's ref lines on stdin. Refuses a push that
      adds work files to the code repo (privacy.work_files: tracker).
See docs/workflow.md, "Private work and code".`,
	Run:         runPrivacy,
	Subcommands: []string{"check", "hook", "pre-push"},
}

func init() {
	registerCommand(privacyCommand)
}

func runPrivacy(ctx *Context, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "privacy requires a subcommand\n\nUsage:\n  issue-cli privacy check\n  issue-cli privacy hook\n  issue-cli privacy pre-push <remote> <url> < refs"}
	}
	if ctx.Project == nil {
		return fmt.Errorf("privacy needs a project — pass --project <slug> or run from a project root")
	}
	switch args[0] {
	case "check":
		return runPrivacyCheck(ctx, args[1:])
	case "hook":
		return runPrivacyHook(ctx, args[1:])
	case "pre-push":
		return runPrivacyPrePush(ctx, args[1:])
	}
	return &usageError{msg: fmt.Sprintf("unknown privacy subcommand %q (check, hook, pre-push)", args[0])}
}

// privacyCheckLine is one line of privacy check.
type privacyCheckLine struct {
	Kind     string `json:"kind"`
	Level    string `json:"level"`
	Label    string `json:"label"`
	Sentence string `json:"sentence"`
	Fix      string `json:"fix,omitempty"`
}

func runPrivacyCheck(ctx *Context, args []string) error {
	fs := newFlagSet("privacy check", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	cfg := ctx.Project.LoadWorkflow().Privacy
	in := privacy.ForProject(ctx.Project, cfg, ctx.Now())
	var out []privacyCheckLine
	add := func(b privacy.Badge) {
		out = append(out, privacyCheckLine{Kind: b.Kind, Level: string(b.Level), Label: b.Label, Sentence: b.Sentence, Fix: b.Fix})
	}
	add(privacy.WorkBadge(in))
	add(privacy.CodeBadge(in))
	if cfg.Tracker() && !cfg.Snapshots() && cfg.Code != tracker.CodePublic {
		g := privacyCheckLine{Kind: "guard", Level: "green", Label: "Guard on", Sentence: "The pre-push guard is installed in " + in.Root}
		if !privacy.HookInstalled(in.Root) {
			g = privacyCheckLine{Kind: "guard", Level: "amber", Label: "Guard off", Sentence: "This clone has no pre-push guard, so a push is not checked", Fix: "issue-cli privacy hook"}
		}
		out = append(out, g)
	}
	red := false
	for _, l := range out {
		red = red || l.Level == "red"
	}
	if ctx.JSONOutput {
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"project": ctx.Project.Slug, "lines": out, "red": red}); err != nil {
			return err
		}
	} else {
		for _, l := range out {
			fmt.Fprintf(ctx.Stdout, "%-6s %-6s %s: %s\n", l.Kind, l.Level, l.Label, l.Sentence)
			if l.Fix != "" {
				fmt.Fprintf(ctx.Stdout, "%13s fix: %s\n", "", l.Fix)
			}
		}
	}
	if red {
		return &exitCodeError{Code: 1}
	}
	return nil
}

// hookCLIArgs are the global flags the hook passes back to issue-cli, so
// it loads this project's workflow from any folder.
func hookCLIArgs(ctx *Context) string {
	args := "--project " + shellQuote(ctx.Project.Slug)
	if p := ctx.Project.ConfigPath; p != "" {
		args = "--config " + shellQuote(p) + " " + args
	}
	return args
}

func installPrivacyHook(ctx *Context, root string) error {
	path, kept, err := privacy.InstallHook(root, hookCLIArgs(ctx))
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "✓ Pre-push guard installed: %s\n", path)
	if kept {
		fmt.Fprintln(ctx.Stdout, "✓ The existing pre-push hook is kept as pre-push.local and runs first")
	}
	return nil
}

func runPrivacyHook(ctx *Context, args []string) error {
	fs := newFlagSet("privacy hook", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	in := privacy.ForProject(ctx.Project, ctx.Project.LoadWorkflow().Privacy, ctx.Now())
	return installPrivacyHook(ctx, in.Root)
}

func runPrivacyPrePush(ctx *Context, args []string) error {
	fs := newFlagSet("privacy pre-push", ctx)
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return &usageError{msg: "privacy pre-push needs the remote name and url git passes to the hook"}
	}
	remote, url := fs.Arg(0), fs.Arg(1)
	cfg := ctx.Project.LoadWorkflow().Privacy
	in := privacy.ForProject(ctx.Project, cfg, ctx.Now())
	refs := privacy.ReadPushRefs(ctx.Stdin)
	var quotes []string
	if cfg != nil && cfg.Code == tracker.CodePublic {
		issues, _ := tracker.LoadIssues(ctx.Project.IssueDir)
		quotes = privacy.DecisionQuotes(issues)
	}
	if err := privacy.PrePush(in.Root, cfg, remote, url, refs, quotes); err != nil {
		return &exitCodeError{Code: 1, Msg: err.Error()}
	}
	return nil
}
