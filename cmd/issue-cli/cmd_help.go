package main

import (
	"fmt"
	"sort"
	"strings"
)

var helpCommand = &Command{
	Name:      "help",
	ShortHelp: "Show CLI help (or details on a command/topic)",
	LongHelp: `Show top-level help, details on a single command, or a workflow topic.

Resolution order for the argument:
  1. command name or alias  → prints that command's long help
  2. workflow topic          → prints the topic (delegates to 'process')

Examples:
  issue-cli help                  # top-level command list
  issue-cli help transition       # help for the 'transition' command
  issue-cli help transitions      # workflow transition rules (topic)
  issue-cli help workflow         # status lifecycle (topic)
  issue-cli help start            # help for the 'start' command`,
	Run: runHelp,
}

func init() {
	registerCommand(helpCommand)
}

func runHelp(ctx *Context, args []string) error {
	if len(args) == 0 {
		return printHelp(ctx.Stdout, ctx.AllProjects, ctx.ProjectSlug)
	}
	name := args[0]
	// Names that historically meant "topic", and where a topic answer is far
	// more useful than the same-named command. `workflow` is the obvious one:
	// the bootstrap subcommand and the lifecycle topic share a name, but
	// `help workflow` is overwhelmingly a request for the lifecycle.
	if topicShadowsCommand(name) {
		return runProcess(ctx, args)
	}
	// Resolve as a command name or alias next so `help transition` describes
	// the command rather than rejecting the singular form of the topic.
	if cmd := lookupCommand(name); cmd != nil && name != "help" {
		return printCommandHelp(ctx, cmd, name)
	}
	return runProcess(ctx, args)
}

func topicShadowsCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "workflow", "workflows":
		return true
	}
	return false
}

// printCommandHelp renders a single command's documentation. Falls back to the
// short help when no LongHelp is set so every command produces meaningful
// output, and surfaces aliases so a bot that hit an alias learns the canonical
// name.
func printCommandHelp(ctx *Context, cmd *Command, requested string) error {
	w := ctx.Stdout
	header := cmd.Name
	if requested != cmd.Name {
		header = fmt.Sprintf("%s (alias: %s)", cmd.Name, requested)
	}
	fmt.Fprintf(w, "== issue-cli %s ==\n", header)
	fmt.Fprintln(w)
	fmt.Fprintln(w, cmd.ShortHelp)
	long := strings.TrimSpace(cmd.LongHelp)
	if long != "" && long != strings.TrimSpace(cmd.ShortHelp) {
		fmt.Fprintln(w)
		fmt.Fprintln(w, long)
	}
	if aliases := aliasesFor(cmd.Name); len(aliases) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Aliases: %s\n", strings.Join(aliases, ", "))
	}
	if related := relatedTopicFor(cmd.Name); related != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Related topic: issue-cli help %s\n", related)
	}
	return nil
}

// aliasesFor returns every alias that resolves to the given canonical command
// name, sorted for stable output.
func aliasesFor(canonical string) []string {
	var out []string
	for alias, target := range commandAliases {
		if target == canonical {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

// relatedTopicFor maps a command to the workflow topic that explains the rules
// it operates under. Only the commands whose behavior is configured by
// workflow.yaml have a related topic — for the rest the command-level help is
// authoritative.
func relatedTopicFor(name string) string {
	switch name {
	case "transition", "start", "done":
		return "transitions"
	case "create", "update", "append", "replace", "set-meta":
		return "format"
	case "comment":
		return "testing"
	}
	return ""
}
