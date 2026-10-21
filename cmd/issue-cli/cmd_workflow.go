package main

import (
	"fmt"
)

const workflowUsage = `Usage:
  issue-cli workflow init [--template <name>] [--force]
  issue-cli workflow upgrade [--template <name>] [--all]
  issue-cli workflow scan [--dry-run] [--installed <ids>] [--declined <ids>] [--forget <ids>]`

var workflowCommand = &Command{
	Name:      "workflow",
	ShortHelp: "Bootstrap a project (init), propose newer features (upgrade), record installed features (scan)",
	LongHelp: `Bootstrap a new project, see what workflow features it could adopt, and keep its feature record.

Subcommands:
  init [--template <name>] [--force]   write workflow.yaml and scaffold issues/, docs/
  upgrade [--template <name>] [--all]  read-only: propose features workflow.yaml lacks, and the upgrade steps
                                       (agents: only when the operator asks; see issue-cli help upgrade)
  scan [--dry-run] [flags]             detect which catalog features workflow.yaml has and write its
                                       features: record (installed <id>@<version>, declined <id>)

Feature record (top level of workflow.yaml, next to template:):
  features:
    installed:
      - types-of-work@v0.36.0
    declined:
      - scoring

scan rewrites only that block; everything else in the file is left as is.
Detection is structural (parsed YAML paths, action types, validator rules);
the catalog is bundled in issue-cli. Ids recorded as installed stay installed
until --forget, and declined ids stay declined until --forget re-opens them.

` + scanUsage,
	Run:         runWorkflow,
	Subcommands: []string{"init", "upgrade", "scan"},
}

func init() {
	registerCommand(workflowCommand)
}

func runWorkflow(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("workflow requires a subcommand\n\n%s", workflowUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runWorkflowInit(ctx, rest)
	case "upgrade":
		return runWorkflowUpgrade(ctx, rest)
	case "scan":
		return runWorkflowScan(ctx, rest)
	default:
		return fmt.Errorf("unknown workflow subcommand: %s\n\n%s", sub, workflowUsage)
	}
}

func runWorkflowInit(ctx *Context, args []string) error {
	fs := newFlagSet("workflow init", ctx)
	templateFlag := fs.String("template", "", "template name")
	forceFlag := fs.Bool("force", false, "overwrite an existing workflow.yaml")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	return doWorkflowInit(*templateFlag, *forceFlag, ctx.Stdin, ctx.Stdout, contextStdinIsTTY(ctx))
}
