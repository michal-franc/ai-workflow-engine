package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Context carries the shared state every Command.Run receives. Each subcommand
// reads JSON-mode and per-stream IO from here instead of the package globals
// the old switch-based dispatcher relied on, so two commands can run
// concurrently with different settings without racing.
type Context struct {
	JSONOutput bool
	Stdout     io.Writer
	Stderr     io.Writer
	Stdin      io.Reader
	Project    *tracker.Project

	// AllProjects is every project parsed from projects.yaml (or a
	// single-element slice in bootstrap/local-issues mode). Used by error
	// helpers to enumerate configured slugs when an issue can't be found in a
	// multi-project setup so dispatched bots get a clear "retry with --project"
	// pointer instead of a flat "not found".
	AllProjects []tracker.Project

	// ConfigPath / ProjectSlug capture the global flags for the few commands
	// that reload the project (e.g. process subcommands that re-resolve a
	// system-scoped workflow).
	ConfigPath  string
	ProjectSlug string

	Now func() time.Time
}

// Command is one entry in the registry. Run owns its own flag.FlagSet and
// returns errors instead of calling os.Exit.
type Command struct {
	Name      string
	ShortHelp string
	LongHelp  string
	Run       func(ctx *Context, args []string) error
}

// stdinIsTTY reports whether stdin is connected to a terminal, defined here
// (in addition to workflow_init.go) for use by any FlagSet that needs to
// disambiguate piped vs. interactive input.
func contextStdinIsTTY(ctx *Context) bool {
	if f, ok := ctx.Stdin.(*os.File); ok {
		info, err := f.Stat()
		if err == nil {
			return info.Mode()&os.ModeCharDevice != 0
		}
	}
	return false
}

// requireSlug pulls the slug positional argument from the front of args,
// returning an error formatted to match the historical "<cmd> requires <slug>"
// message that tests and bots rely on.
func requireSlug(args []string, cmd string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("%s requires <slug>\n\nExample:\n  issue-cli %s <slug>", cmd, cmd)
	}
	return args[0], args[1:], nil
}

// newFlagSet builds a FlagSet that writes its own usage/error output to
// ctx.Stderr and refuses to call os.Exit on parse error. This is the standard
// constructor every cmd_*.go file uses.
func newFlagSet(name string, ctx *Context) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	return fs
}

// flagWasSet reports whether a flag was explicitly set on the command line.
// Replaces the old "empty value means absent" pattern in flagValue.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

// findIssueOrErr is the error-returning replacement for findIssue. It searches
// the active project's issue directory by exact slug (case-insensitive),
// falling back to suffix/contains matching, and returns a not-found error
// when nothing matches.
//
// In multi-project setups the not-found error additionally enumerates the
// configured project slugs and points at --project, so a bot that ran the
// command against the wrong project can self-correct without a human editing
// the prompt.
func findIssueOrErr(ctx *Context, slug string) (*tracker.Issue, []*tracker.Issue, error) {
	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot load issues: %w", err)
	}

	normalized := strings.TrimSuffix(slug, ".md")
	if rel, relErr := filepath.Rel(proj.IssueDir, normalized); relErr == nil && !strings.HasPrefix(rel, "..") {
		normalized = rel
	}
	normalizedLower := strings.ToLower(normalized)

	for _, issue := range issues {
		if strings.ToLower(issue.Slug) == normalizedLower {
			return issue, issues, nil
		}
	}
	for _, issue := range issues {
		slugLower := strings.ToLower(issue.Slug)
		if strings.HasSuffix(slugLower, "/"+normalizedLower) || strings.Contains(slugLower, normalizedLower) {
			return issue, issues, nil
		}
	}
	return nil, issues, notFoundError(ctx, slug)
}

// notFoundError builds the agent-facing "issue not found" message. In
// single-project setups the format is unchanged from before (regression
// guard for the byte-identical AC). In multi-project setups it appends the
// configured project list and a --project hint so a bot that ran against the
// wrong project can immediately retry with the right slug.
func notFoundError(ctx *Context, slug string) error {
	base := fmt.Sprintf("issue not found: %s\n\nRun: issue-cli list", slug)
	if ctx == nil || len(ctx.AllProjects) <= 1 {
		return fmt.Errorf("%s", base)
	}
	defaultSlug := ""
	if ctx.ProjectSlug == "" && len(ctx.AllProjects) > 0 {
		defaultSlug = ctx.AllProjects[0].Slug
	}
	activeSlug := defaultSlug
	if ctx.Project != nil {
		activeSlug = ctx.Project.Slug
	}
	return fmt.Errorf("%s\n\nSearched project: %s\n%s", base, activeSlug, formatProjectList(ctx.AllProjects, defaultSlug))
}
