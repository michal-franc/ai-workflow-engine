package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/lint"
)

// lintWarnCap is how many findings per check the text output shows
// without --all.
const lintWarnCap = 20

// runHealthLint is issue-cli health lint: the lint checks over workflow.yaml
// and the issues, each finding with its fix. Every finding is a warning, so
// it exits 0 unless the usage is wrong.
func runHealthLint(ctx *Context, args []string) error {
	fs := newFlagSet("health lint", ctx)
	checkFlag := fs.String("check", "", "comma-separated checks to run (default: all; see --list)")
	listFlag := fs.Bool("list", false, "list the checks and exit")
	allFlag := fs.Bool("all", false, "print every finding (default: the first 20 per check)")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	if *listFlag {
		for _, c := range lint.Checks() {
			fmt.Fprintf(ctx.Stdout, "  %-13s %s\n", c.Name, c.Help)
		}
		return nil
	}
	var names []string
	for _, n := range strings.Split(*checkFlag, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	for _, n := range names {
		if !slices.Contains(lint.Names(), n) {
			return &usageError{msg: fmt.Sprintf("unknown check %q (checks: %s)", n, strings.Join(lint.Names(), ", "))}
		}
	}

	env, err := lint.Load(ctx.Project, ctx.now())
	if err != nil {
		return err
	}
	rep, err := lint.Run(env, names...)
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, rep)
	}
	printLintReport(ctx.Stdout, rep, *allFlag)
	return nil
}

func printLintReport(w io.Writer, rep *lint.Report, all bool) {
	fmt.Fprintf(w, "== health lint: %s (%s) ==\n", rep.Project, strings.Join(rep.Checks, ", "))
	if len(rep.Findings) == 0 {
		fmt.Fprintln(w, "✓ no warnings")
		return
	}
	shown := map[string]int{}
	hidden := map[string]int{}
	for _, f := range rep.Findings {
		shown[f.Check]++
		if !all && shown[f.Check] > lintWarnCap {
			hidden[f.Check]++
			continue
		}
		fmt.Fprintf(w, "%s %s: %s\n", f.Check, f.Where, f.Message)
		if f.Fix != "" {
			fmt.Fprintf(w, "  fix: %s\n", f.Fix)
		}
	}
	for _, c := range rep.Checks {
		if n := hidden[c]; n > 0 {
			fmt.Fprintf(w, "… %d more %s warnings: issue-cli health lint --check %s --all\n", n, c, c)
		}
	}
	fmt.Fprintf(w, "\n%s; the health score's lint component counts them (/health lists them)\n", lintCount(rep.Warnings, "warning"))
}

func lintCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
