package main

import (
	"flag"
	"fmt"
	"io"
	"os"
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
	Run:         runHelp,
	Subcommands: processTopics,
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
		if _, err := parsePositionals(ctx, "help", args[1:], 0); err != nil {
			return err
		}
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
	printCommandFlags(w, cmd)
	return nil
}

// printCommandFlags lists the flags cmd parses (per subcommand when it has
// any), read from its FlagSet in introspection mode so the list can't drift
// from the code.
func printCommandFlags(w io.Writer, cmd *Command) {
	issueDir, err := os.MkdirTemp("", "issue-cli-help-")
	if err != nil {
		return
	}
	defer os.RemoveAll(issueDir)
	subs := []string{""}
	if hasFlagSubcommands(cmd) {
		subs = cmd.Subcommands
	}
	for _, sub := range subs {
		fs, _ := introspectFlagSet(cmd, sub, issueDir)
		var lines []string
		if fs != nil {
			fs.VisitAll(func(f *flag.Flag) {
				typ, usage := flag.UnquoteUsage(f)
				name := "--" + f.Name
				if typ != "" {
					name += " <" + typ + ">"
				}
				lines = append(lines, fmt.Sprintf("  %-24s %s", name, usage))
			})
		}
		for _, extra := range cmd.ExtraFlags {
			if sub == "" {
				lines = append(lines, fmt.Sprintf("  %-24s (repeatable)", "--"+extra+" <value>"))
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintln(w)
		if sub == "" {
			fmt.Fprintln(w, "Flags:")
		} else {
			fmt.Fprintf(w, "Flags (%s %s):\n", cmd.Name, sub)
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}
}

// hasFlagSubcommands reports whether cmd's Subcommands each parse their own
// flags (data, ci, lead…), as opposed to help/process topics.
func hasFlagSubcommands(cmd *Command) bool {
	return len(cmd.Subcommands) > 0 && cmd.Name != "help" && cmd.Name != "process"
}

// wantsHelp reports whether args ask for help: -h, -help or --help anywhere
// before a "--". It runs before project resolution, so `<cmd> --help` works
// without a project and never reads --help as a slug or as text.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "-h", "--h", "-help", "--help":
			return true
		}
	}
	return false
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
	case "transition", "start", "done", "set-type":
		return "transitions"
	case "create", "update", "append", "replace", "set-meta":
		return "format"
	case "comment":
		return "testing"
	}
	return ""
}
