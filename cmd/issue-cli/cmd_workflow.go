package main

import (
	"fmt"
)

var workflowCommand = &Command{
	Name:      "workflow",
	ShortHelp: "Bootstrap a project (init) or propose newer template features (upgrade)",
	LongHelp: `Bootstrap a new project, or see what newer template features it could adopt.

Subcommands:
  init [--template <name>] [--force]   write workflow.yaml and scaffold issues/, docs/
  upgrade [--template <name>]          read-only: propose features workflow.yaml lacks, and the upgrade steps
                                       (agents: only when the operator asks; see issue-cli help upgrade)`,
	Run:         runWorkflow,
	Subcommands: []string{"init", "upgrade"},
}

func init() {
	registerCommand(workflowCommand)
}

func runWorkflow(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("workflow requires a subcommand\n\nUsage:\n  issue-cli workflow init [--template <name>] [--force]\n  issue-cli workflow upgrade [--template <name>]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runWorkflowInit(ctx, rest)
	case "upgrade":
		return runWorkflowUpgrade(ctx, rest)
	default:
		return fmt.Errorf("unknown workflow subcommand: %s\n\nUsage:\n  issue-cli workflow init [--template <name>] [--force]\n  issue-cli workflow upgrade [--template <name>]", sub)
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
