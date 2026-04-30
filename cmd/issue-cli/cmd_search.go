package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var searchCommand = &Command{
	Name:      "search",
	ShortHelp: "Search issues by regex/text",
	LongHelp: `Search issue title, body, and status with case-insensitive regex.

Examples:
  issue-cli search "heat"
  issue-cli search "foo|bar"`,
	Run: runSearch,
}

func init() {
	registerCommand(searchCommand)
}

func runSearch(ctx *Context, args []string) error {
	fs := newFlagSet("search", ctx)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("search requires <query>\n\nExample:\n  issue-cli search \"heat\"")
	}
	query := strings.Join(rest, " ")

	issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}

	normalized := strings.ReplaceAll(query, `\|`, "|")
	re, err := regexp.Compile("(?i)" + normalized)
	if err != nil {
		re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
	}
	var matches []*tracker.Issue
	for _, issue := range issues {
		if re.MatchString(issue.Title) ||
			re.MatchString(issue.BodyRaw) ||
			re.MatchString(issue.Status) {
			matches = append(matches, issue)
		}
	}

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, matches)
	}
	if len(matches) == 0 {
		fmt.Fprintf(ctx.Stdout, "No issues matching \"%s\"\n", query)
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "== Search: \"%s\" (%d results) ==\n", query, len(matches))
	for _, issue := range matches {
		fmt.Fprintf(ctx.Stdout, "  [%-13s] %-45s %s\n", issue.Status, issue.Slug, issue.System)
	}
	return nil
}
