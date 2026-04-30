package main

import (
	"fmt"
	"io"
	"sort"
)

// commandRegistry holds every top-level subcommand. Entries are appended via
// init() functions in each cmd_<name>.go file so the list of commands lives
// next to its implementation rather than in a giant switch.
var commandRegistry = map[string]*Command{}

// commandAliases maps an alias (e.g. "show") to its canonical command name
// (e.g. "context"). Looking up an alias resolves to the canonical command.
var commandAliases = map[string]string{}

// registerCommand adds a command to the registry. Panics on duplicate names so
// the conflict surfaces at process start rather than at runtime.
func registerCommand(c *Command) {
	if _, ok := commandRegistry[c.Name]; ok {
		panic("duplicate command: " + c.Name)
	}
	commandRegistry[c.Name] = c
}

// registerAlias registers an alternative name for an existing command.
func registerAlias(alias, canonical string) {
	if _, ok := commandAliases[alias]; ok {
		panic("duplicate alias: " + alias)
	}
	commandAliases[alias] = canonical
}

// lookupCommand resolves a name (or alias) to its Command, returning nil when
// no command matches.
func lookupCommand(name string) *Command {
	if c, ok := commandRegistry[name]; ok {
		return c
	}
	if canonical, ok := commandAliases[name]; ok {
		return commandRegistry[canonical]
	}
	return nil
}

// commandNames returns command names in display order.
func commandNames() []string {
	names := make([]string, 0, len(commandRegistry))
	for name := range commandRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// printHelp renders the top-level help text from the registry. Each command's
// ShortHelp is shown next to its name. The trailing block lists global flags
// and the recommended bootstrap commands so the output stays close to the old
// hand-rolled help blob.
func printHelp(w io.Writer) error {
	fmt.Fprintln(w, "== issue-cli — AI-Native Project Viewer CLI ==")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	width := 0
	for _, name := range commandNames() {
		if len(name) > width {
			width = len(name)
		}
	}
	for _, name := range commandNames() {
		c := commandRegistry[name]
		fmt.Fprintf(w, "  %-*s  %s\n", width, name, c.ShortHelp)
	}
	if len(commandAliases) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Aliases:")
		var aliasNames []string
		for a := range commandAliases {
			aliasNames = append(aliasNames, a)
		}
		sort.Strings(aliasNames)
		for _, a := range aliasNames {
			fmt.Fprintf(w, "  %-*s  → %s\n", width, a, commandAliases[a])
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --config <path>      Path to projects.yaml (default: projects.yaml)")
	fmt.Fprintln(w, "  --project <slug>     Select project (default: first in config)")
	fmt.Fprintln(w, "  --json               Output as JSON")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "First time? Run these:")
	fmt.Fprintln(w, "  1. issue-cli process")
	fmt.Fprintln(w, "  2. issue-cli next")
	fmt.Fprintln(w, "  3. issue-cli start <slug>   # works from any status — transitions from backlog/human-testing need approval")
	return nil
}
