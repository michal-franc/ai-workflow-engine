package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// Config is where ConfigPath came from (findConfig), for `projects` and
	// report-bug's "why".
	Config configSource

	Now func() time.Time

	// Telemetry bookkeeping (see telemetry.go). flagSets collects every
	// FlagSet the command parsed so the event can list flag names without
	// values; issue is the issue findIssueOrErr resolved, if any.
	flagSets []*flag.FlagSet
	issue    *tracker.Issue
	// introspect makes parseFlags abort the command with the populated
	// FlagSet instead of parsing — used to enumerate a command's flags
	// without running it.
	introspect bool
}

func (c *Context) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Command is one entry in the registry. Run owns its own flag.FlagSet and
// returns errors instead of calling os.Exit.
type Command struct {
	Name      string
	ShortHelp string
	LongHelp  string
	Run       func(ctx *Context, args []string) error

	// Subcommands lists the first-positional subcommands (or topics) the
	// command dispatches on. Telemetry records the subcommand only when it is
	// one of these, and the usage report flags the ones never called.
	Subcommands []string
	// SubAliases maps an accepted alternative subcommand spelling to its
	// canonical entry in Subcommands (e.g. data "rm" → "remove").
	SubAliases map[string]string
	// ExtraFlags names flags the command parses by hand instead of through a
	// FlagSet (e.g. transition's repeatable --field), so telemetry and the
	// never-used report still see them.
	ExtraFlags []string
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
	// A flag where the slug belongs would otherwise be looked up as an issue
	// ("issue not found: --text").
	if isFlagToken(args[0]) {
		top, _, _ := strings.Cut(cmd, " ")
		return "", nil, &usageError{msg: fmt.Sprintf("%s takes <slug> first, then flags: issue-cli %s <slug> %s ...\n\nRun: issue-cli %s --help", cmd, cmd, args[0], top)}
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

// parseFlags is the one place every command parses its FlagSet. Flags may
// come anywhere in args (before or after positionals; "--" ends them), and
// any positional left over is an error: use parseFlagsArgs for commands that
// take positionals. It records the FlagSet for telemetry (flag names only)
// and, in introspection mode, aborts the command before any work by
// panicking with the fully defined FlagSet — see introspectFlags.
func parseFlags(ctx *Context, fs *flag.FlagSet, args []string) error {
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return unexpectedArgError(fs, fs.Arg(0))
	}
	return nil
}

func unexpectedArgError(fs *flag.FlagSet, arg string) *usageError {
	return &usageError{msg: fmt.Sprintf("unexpected argument %q for '%s'%s", arg, fs.Name(), helpHint(fs))}
}

// parseFlagsArgs is parseFlags for commands that read positionals from
// fs.Args(): flags still parse anywhere, positionals are left to the caller.
func parseFlagsArgs(ctx *Context, fs *flag.FlagSet, args []string) error {
	if ctx.introspect {
		panic(introspectedFlags{fs})
	}
	ctx.flagSets = append(ctx.flagSets, fs)
	// The global --json also works after the command (`list --all --json`),
	// unless the command defines its own.
	if fs.Lookup("json") == nil {
		fs.BoolVar(&ctx.JSONOutput, "json", ctx.JSONOutput, "output as JSON")
	}
	// The stdlib's message and full usage dump are replaced by usageError.
	fs.SetOutput(io.Discard)
	if err := fs.Parse(flagsFirst(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return newFlagUsageError(fs, err)
	}
	return nil
}

// parsePositionals parses args for a command that defines no flags, so any
// flag is unknown, and returns the positionals; more than max of them is an
// error (max < 0: no limit).
func parsePositionals(ctx *Context, name string, args []string, max int) ([]string, error) {
	fs := newFlagSet(name, ctx)
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return nil, err
	}
	if max >= 0 && fs.NArg() > max {
		return nil, unexpectedArgError(fs, fs.Arg(max))
	}
	return fs.Args(), nil
}

// flagsFirst reorders args so every flag (with its value) comes before the
// positionals, which follow a "--" in their original order. The stdlib
// FlagSet stops at the first positional, so without this a flag after one
// (`list done --all`) was silently dropped. A token is a flag when it starts
// with "-" and is not a bare "-" or a negative number; a flag the FlagSet
// knows as non-boolean takes the next token as its value unless written as
// --name=value. Unknown flags stay in the flag part so Parse rejects them.
func flagsFirst(fs *flag.FlagSet, args []string) []string {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !isFlagToken(a) {
			positionals = append(positionals, a)
			continue
		}
		flags = append(flags, a)
		name, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		f := fs.Lookup(name)
		if f == nil || hasValue || isBoolFlag(f) {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(append(flags, "--"), positionals...)
}

// flagTokenRe matches "-name", "--name" and "--name=value": a word that is
// meant as a flag. Text that merely starts with a dash ("- [ ] item",
// "--force is bad", "-5") is a positional.
var flagTokenRe = regexp.MustCompile(`^--?[A-Za-z][A-Za-z0-9_-]*(=|$)`)

// isFlagToken reports whether a looks like a flag rather than a value.
func isFlagToken(a string) bool {
	return flagTokenRe.MatchString(a)
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// usageError is a malformed invocation: an unknown flag, a bad flag value,
// a stray positional, or a flag where the slug belongs. main exits 2 on it
// and telemetry classifies it; flag is set for an unknown flag ("--all").
type usageError struct {
	msg  string
	flag string
	err  error
}

func (e *usageError) Error() string { return e.msg }
func (e *usageError) Unwrap() error { return e.err }

// newFlagUsageError turns a FlagSet parse error into a short usageError that
// names the flag the way agents type it and lists the valid ones.
func newFlagUsageError(fs *flag.FlagSet, err error) *usageError {
	const undefined = "flag provided but not defined: -"
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, undefined); ok {
		flagName := "--" + name
		hint := helpHint(fs)
		switch name {
		case "project", "config":
			hint = fmt.Sprintf("\n\n%s is a global flag: put it before the command (issue-cli %s <value> %s ...)", flagName, flagName, fs.Name())
		case "debug":
			hint = fmt.Sprintf("\n\n--debug is a global flag: put it before the command (issue-cli --debug %s ...)", fs.Name())
		}
		return &usageError{
			msg:  fmt.Sprintf("unknown flag %s for '%s'\nValid flags: %s%s", flagName, fs.Name(), validFlags(fs), hint),
			flag: flagName,
			err:  err,
		}
	}
	// "invalid value ... for flag -x", "flag needs an argument: -x": keep the
	// stdlib wording but name the flag with two dashes.
	msg = strings.Replace(msg, "flag -", "flag --", 1)
	msg = strings.Replace(msg, "argument: -", "argument: --", 1)
	return &usageError{msg: msg + helpHint(fs), err: err}
}

// validFlags lists fs's flags as "--a --b", or "(none)".
func validFlags(fs *flag.FlagSet) string {
	var names []string
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name != "json" || f.Usage != "output as JSON" {
			names = append(names, "--"+f.Name)
		}
	})
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, " ")
}

// helpHint points at the per-command help for fs's top-level command.
func helpHint(fs *flag.FlagSet) string {
	cmd, _, _ := strings.Cut(fs.Name(), " ")
	return fmt.Sprintf("\n\nRun: issue-cli %s --help", cmd)
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
// the active project's issue directory by exact slug (case-insensitive), then
// by a unique "/<slug>" suffix, then by a unique substring. A substring match
// is noted on stderr so a write never lands on a guessed issue silently. When
// a tier matches several issues it fails with the candidates listed instead of
// picking one, and when nothing matches it returns a not-found error.
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
			ctx.issue = issue
			return issue, issues, nil
		}
	}
	var suffix, contains []*tracker.Issue
	for _, issue := range issues {
		slugLower := strings.ToLower(issue.Slug)
		if strings.HasSuffix(slugLower, "/"+normalizedLower) {
			suffix = append(suffix, issue)
		}
		if strings.Contains(slugLower, normalizedLower) {
			contains = append(contains, issue)
		}
	}
	matches := suffix
	if len(matches) == 0 {
		matches = contains
	}
	switch {
	case len(matches) == 0:
		return nil, issues, notFoundError(ctx, slug)
	case len(matches) > 1:
		return nil, issues, ambiguousSlugError(slug, matches)
	}
	issue := matches[0]
	if len(suffix) == 0 {
		fmt.Fprintf(ctx.Stderr, "note: resolved %q to %s (partial match)\n", slug, issue.Slug)
	}
	ctx.issue = issue
	return issue, issues, nil
}

// ambiguousSlugListMax caps the candidates an ambiguity error lists; a system
// name can match dozens of issues.
const ambiguousSlugListMax = 10

// ambiguousSlugError lists the issues a partial slug matched, so the caller
// can retry with the full slug instead of acting on whichever loaded first.
func ambiguousSlugError(slug string, matches []*tracker.Issue) error {
	slugs := make([]string, len(matches))
	for i, m := range matches {
		slugs[i] = m.Slug
	}
	sort.Strings(slugs)
	shown := slugs
	more := ""
	if len(shown) > ambiguousSlugListMax {
		shown = shown[:ambiguousSlugListMax]
		more = fmt.Sprintf("\n  … and %d more (issue-cli list)", len(slugs)-ambiguousSlugListMax)
	}
	return fmt.Errorf("ambiguous slug %q: %d issues match:\n  %s%s\n\nRetry with the full slug.", slug, len(slugs), strings.Join(shown, "\n  "), more)
}

// issueNotFoundError is the "issue not found" error; typed so telemetry can
// classify it without matching on the message.
type issueNotFoundError struct{ msg string }

func (e *issueNotFoundError) Error() string { return e.msg }

// notFoundError builds the agent-facing "issue not found" message. In
// single-project setups the format is unchanged from before (regression
// guard for the byte-identical AC). In multi-project setups it appends the
// configured project list and a --project hint so a bot that ran against the
// wrong project can immediately retry with the right slug.
func notFoundError(ctx *Context, slug string) error {
	base := fmt.Sprintf("issue not found: %s\n\nRun: issue-cli list", slug)
	if ctx == nil || len(ctx.AllProjects) <= 1 {
		return &issueNotFoundError{msg: base}
	}
	defaultSlug := ""
	if ctx.ProjectSlug == "" && len(ctx.AllProjects) > 0 {
		defaultSlug = ctx.AllProjects[0].Slug
	}
	activeSlug := defaultSlug
	if ctx.Project != nil {
		activeSlug = ctx.Project.Slug
	}
	return &issueNotFoundError{msg: fmt.Sprintf("%s\n\nSearched project: %s\n%s", base, activeSlug, formatProjectList(ctx.AllProjects, defaultSlug))}
}
