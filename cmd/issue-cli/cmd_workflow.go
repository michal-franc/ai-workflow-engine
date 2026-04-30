package main

import (
	"fmt"
)

var workflowCommand = &Command{
	Name:      "workflow",
	ShortHelp: "Bootstrap a new project: writes workflow.yaml + scaffolds issues/, docs/",
	LongHelp: `Bootstrap a new project.

Subcommands:
  init [--template <name>] [--force]   write workflow.yaml and scaffold issues/, docs/`,
	Run: runWorkflow,
}

func init() {
	registerCommand(workflowCommand)
}

func runWorkflow(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("workflow requires a subcommand\n\nUsage:\n  issue-cli workflow init [--template <name>] [--force]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runWorkflowInit(ctx, rest)
	default:
		return fmt.Errorf("unknown workflow subcommand: %s\n\nUsage:\n  issue-cli workflow init [--template <name>] [--force]", sub)
	}
}

func runWorkflowInit(ctx *Context, args []string) error {
	fs := newFlagSet("workflow init", ctx)
	templateFlag := fs.String("template", "", "template name")
	forceFlag := fs.Bool("force", false, "overwrite an existing workflow.yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return doWorkflowInit(*templateFlag, *forceFlag, ctx.Stdin, ctx.Stdout, contextStdinIsTTY(ctx))
}
