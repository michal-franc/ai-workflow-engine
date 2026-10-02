package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// invocation is what run() learns about a call, handed back to main so the
// telemetry event can be built after the command finishes. Telemetry is a
// separate sink from logAction: logAction keeps feeding the agent timeline
// and retry hint with raw args; this records names-only usage data.
type invocation struct {
	global  []string // global flag tokens as split off by splitGlobalAndCommand
	command string   // command name as typed ("" = bare `issue-cli`)
	args    []string // args after the command name
	cmd     *Command // resolved command, nil when unknown
	ctx     *Context // nil when run() failed before building one
	phase   string   // where run() failed before the command ran, if it did
}

const (
	phaseSplit          = "split"
	phaseUnknownCommand = "unknown_command"
	phaseProject        = "project"
)

// globalFlagNames are issue-cli's global flags, recorded like any other flag.
var globalFlagNames = []string{"config", "project", "json"}

// recordTelemetry builds and writes the event for one invocation. It must
// never affect the CLI: every failure (including a panic) is swallowed.
func recordTelemetry(inv *invocation, rawArgs []string, runErr error, start time.Time, panicked bool) {
	defer func() { _ = recover() }()
	if telemetry.EnvDisabled() || os.Getenv(telemetry.SkipEnv) != "" {
		return
	}
	path, ok := telemetryPath(inv)
	if !ok {
		return
	}
	ev := buildEvent(inv, rawArgs, runErr, panicked)
	ev.TS = start.UTC().Format(time.RFC3339Nano)
	ev.DurMS = time.Since(start).Milliseconds()
	_ = telemetry.FileSink{Path: path}.Write(ev)
}

// telemetryPath picks the per-project file when a project resolved, else the
// global fallback. ok is false when telemetry is off for the project or no
// path can be determined.
func telemetryPath(inv *invocation) (string, bool) {
	if inv.ctx != nil && inv.ctx.Project != nil {
		if !inv.ctx.Project.TelemetryEnabled() {
			return "", false
		}
		if root := inv.ctx.Project.TelemetryRoot(); root != "" {
			return telemetry.ProjectPath(root), true
		}
	}
	p := telemetry.GlobalPath()
	return p, p != ""
}

func buildEvent(inv *invocation, rawArgs []string, runErr error, panicked bool) telemetry.Event {
	ev := telemetry.Event{
		V:        telemetry.SchemaVersion,
		PID:      os.Getpid(),
		PPID:     os.Getppid(),
		ArgsHash: telemetry.HashArgs(rawArgs),
	}
	switch {
	case inv.cmd != nil:
		ev.Cmd = inv.cmd.Name
		if inv.command != inv.cmd.Name && inv.command != "" && !strings.HasPrefix(inv.command, "-") {
			ev.Alias = inv.command
		}
		ev.Sub = commandSub(inv.cmd, inv.args)
	case inv.command == "" && inv.phase == "":
		ev.Cmd = "help" // bare `issue-cli` prints top-level help
	}

	ev.Flags = eventFlags(inv)

	if inv.ctx != nil {
		if inv.ctx.Project != nil {
			ev.Project = inv.ctx.Project.Slug
		}
		if inv.ctx.issue != nil {
			ev.Issue = inv.ctx.issue.Slug
			ev.Assignee = inv.ctx.issue.Assignee
		}
		if safe := safeValues(inv.ctx); !safe.Empty() {
			ev.Safe = safe
		}
	}
	if ev.Project == "" {
		ev.Project = telemetry.Truncate(extractCLIFlag(inv.global, "--project"), telemetry.MaxUnknownLen)
	}

	if panicked {
		ev.Exit = 2
		ev.ErrClass = telemetry.ErrPanic
	} else if runErr != nil {
		ev.Exit = 1
		var codeErr *exitCodeError
		if errors.As(runErr, &codeErr) {
			ev.Exit = codeErr.Code
		}
		ev.ErrClass, ev.Unknown = classifyError(inv, runErr)
	}

	ev.Caller, ev.Agent, ev.Tmux = detectCaller()
	return ev
}

// commandSub returns the canonical subcommand/topic for commands that
// dispatch on their first positional, or "" when there is none or it is not
// a known one (unknown tokens are recorded via the error path instead).
func commandSub(cmd *Command, args []string) string {
	if len(cmd.Subcommands) == 0 || len(args) == 0 {
		return ""
	}
	first := args[0]
	switch cmd.Name {
	case "help", "process":
		// Same resolution order as runHelp: shadowing topics, then command
		// names (`help transition` documents the command), then topics.
		if cmd.Name == "help" && !topicShadowsCommand(first) {
			if c := lookupCommand(first); c != nil && first != "help" {
				return c.Name
			}
		}
		topic := normalizeTopic(first)
		for _, t := range processTopics {
			if t == topic {
				return t
			}
		}
		return ""
	}
	if canon, ok := cmd.SubAliases[first]; ok {
		return canon
	}
	for _, s := range cmd.Subcommands {
		if s == first {
			return s
		}
	}
	return ""
}

// eventFlags lists the flag names used in this call: global flags, every
// flag a parsed FlagSet saw, and hand-parsed ExtraFlags. Names only.
func eventFlags(inv *invocation) []string {
	seen := map[string]bool{}
	for _, g := range inv.global {
		if strings.HasPrefix(g, "--") {
			seen[strings.TrimPrefix(g, "--")] = true
		}
	}
	if inv.ctx != nil {
		for _, fs := range inv.ctx.flagSets {
			fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
		}
	}
	if inv.cmd != nil {
		for _, name := range inv.cmd.ExtraFlags {
			for _, a := range inv.args {
				if a == "--"+name || strings.HasPrefix(a, "--"+name+"=") {
					seen[name] = true
				}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// safeValues extracts the whitelisted structural values (--to, --section)
// from the parsed FlagSets. No other value ever reaches telemetry.
func safeValues(ctx *Context) *telemetry.Safe {
	s := &telemetry.Safe{}
	for _, fs := range ctx.flagSets {
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "to":
				s.To = telemetry.Truncate(f.Value.String(), telemetry.MaxSafeLen)
			case "section":
				s.Section = telemetry.Truncate(f.Value.String(), telemetry.MaxSafeLen)
			}
		})
	}
	return s
}

// classifyError maps an error to a telemetry class plus, for "unknown X"
// errors, the offending token. Typed errors are checked first; the few
// stdlib/dispatcher messages that have no type are matched by prefix.
func classifyError(inv *invocation, err error) (class, unknown string) {
	trunc := func(s string) string { return telemetry.Truncate(s, telemetry.MaxUnknownLen) }
	switch inv.phase {
	case phaseUnknownCommand:
		return telemetry.ErrUnknownCommand, trunc(inv.command)
	case phaseProject:
		return telemetry.ErrProjectResolution, ""
	case phaseSplit:
		return telemetry.ErrUsage, ""
	}
	var approval *tracker.ApprovalMissingError
	if errors.As(err, &approval) || errors.Is(err, tracker.ErrApprovalMissing) {
		return telemetry.ErrApprovalMissing, ""
	}
	if errors.Is(err, tracker.ErrInvalidTransition) {
		return telemetry.ErrInvalidTransition, ""
	}
	if errors.Is(err, tracker.ErrTransitionValidation) {
		return telemetry.ErrValidation, ""
	}
	// --wait / --dry-run outcomes carry their own exit code: 3 means the
	// approval wait timed out; 1 means unmet requirements were reported.
	var codeErr *exitCodeError
	if errors.As(err, &codeErr) {
		if codeErr.Code == exitWaitTimeout {
			return telemetry.ErrWaitTimeout, ""
		}
		return telemetry.ErrValidation, ""
	}
	var notFound *issueNotFoundError
	if errors.As(err, &notFound) {
		return telemetry.ErrIssueNotFound, ""
	}
	msg := err.Error()
	firstLine := strings.SplitN(msg, "\n", 2)[0]
	var fpe *flagParseError
	if errors.As(err, &fpe) {
		const undefined = "flag provided but not defined: -"
		if strings.HasPrefix(firstLine, undefined) {
			return telemetry.ErrUnknownFlag, trunc("--" + strings.TrimPrefix(firstLine, undefined))
		}
		return telemetry.ErrFlagParse, ""
	}
	if rest, ok := strings.CutPrefix(firstLine, "unknown topic: "); ok {
		return telemetry.ErrUnknownTopic, trunc(rest)
	}
	if strings.HasPrefix(firstLine, "unknown ") {
		if _, tok, ok := strings.Cut(firstLine, " subcommand: "); ok {
			return telemetry.ErrUnknownSubcommand, trunc(tok)
		}
	}
	if strings.Contains(firstLine, " requires ") || strings.Contains(firstLine, " is required") {
		return telemetry.ErrUsage, ""
	}
	return telemetry.ErrOther, ""
}

// detectCaller guesses who is calling from the environment alone (no
// subprocesses — it runs on every invocation).
func detectCaller() (caller, agent string, tmux bool) {
	tmux = os.Getenv("TMUX") != ""
	switch {
	case os.Getenv("CLAUDECODE") == "1":
		agent = "claude-code"
	case os.Getenv("AI_AGENT") != "":
		// e.g. "claude-code_2-1-287_agent" → "claude-code"
		agent = telemetry.Truncate(strings.SplitN(os.Getenv("AI_AGENT"), "_", 2)[0], 32)
	default:
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "CODEX_") {
				agent = "codex"
				break
			}
		}
	}
	switch {
	case os.Getenv("ISSUE_VIEWER_ISSUE_SLUG") != "":
		caller = telemetry.CallerDispatched
	case agent != "":
		caller = telemetry.CallerAgent
	case stdinIsTerminal():
		caller = telemetry.CallerTTY
	default:
		caller = telemetry.CallerScript
	}
	return caller, agent, tmux
}

// stdinIsTerminal is a var so tests can pin it regardless of how the test
// binary's stdin is wired.
var stdinIsTerminal = func() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// introspectedFlags is the panic payload parseFlags uses in introspection
// mode to hand back a command's fully defined FlagSet.
type introspectedFlags struct{ fs *flag.FlagSet }

// introspectArgs fill every positional slot a command might demand before
// parsing flags (slug, numeric id, value) so it reaches parseFlags.
var introspectArgs = []string{"_introspect_", "1", "_introspect_"}

// introspectFlags returns the flag names cmd (or cmd sub) defines, by running
// it in introspection mode: parseFlags panics with the FlagSet before the
// command does any work. The context points at an empty issues dir, discards
// all output, and reads empty stdin, so a command that never reaches
// parseFlags still cannot touch real data. ok is false when the command
// panicked for another reason.
func introspectFlags(cmd *Command, sub, issueDir string) (flags []string, ok bool) {
	ctx := &Context{
		Stdout:     io.Discard,
		Stderr:     io.Discard,
		Stdin:      strings.NewReader(""),
		Project:    &tracker.Project{Name: "introspect", Slug: "introspect", IssueDir: issueDir},
		Now:        time.Now,
		introspect: true,
	}
	args := introspectArgs
	if sub != "" {
		args = append([]string{sub}, introspectArgs...)
	}
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		got, isFlags := r.(introspectedFlags)
		if !isFlags {
			flags, ok = nil, false
			return
		}
		got.fs.VisitAll(func(f *flag.Flag) { flags = append(flags, f.Name) })
		sort.Strings(flags)
		ok = true
	}()
	_ = cmd.Run(ctx, args)
	return nil, true // returned without parsing flags: the command has none
}

// buildManifest describes the CLI surface for the usage report: commands,
// aliases, subcommands, topics, and per-command flags (introspected).
func buildManifest() telemetry.Manifest {
	m := telemetry.Manifest{
		Topics:      append([]string(nil), processTopics...),
		GlobalFlags: append([]string(nil), globalFlagNames...),
	}
	issueDir, err := os.MkdirTemp("", "issue-cli-introspect-")
	if err == nil {
		defer os.RemoveAll(issueDir)
	}
	for _, name := range commandNames() {
		cmd := commandRegistry[name]
		mc := telemetry.ManifestCommand{Name: name, Aliases: aliasesFor(name), Flags: map[string][]string{}}
		addFlags := func(key, sub string) {
			flags, ok := introspectFlags(cmd, sub, issueDir)
			if !ok {
				mc.FlagsUnknown = append(mc.FlagsUnknown, key)
				return
			}
			if len(flags) > 0 {
				mc.Flags[key] = flags
			}
		}
		if len(cmd.Subcommands) > 0 && name != "help" && name != "process" {
			mc.Subcommands = append([]string(nil), cmd.Subcommands...)
			for _, s := range cmd.Subcommands {
				addFlags(name+" "+s, s)
			}
		} else {
			addFlags(name, "")
		}
		if len(cmd.ExtraFlags) > 0 {
			mc.Flags[name] = append(mc.Flags[name], cmd.ExtraFlags...)
			sort.Strings(mc.Flags[name])
		}
		m.Commands = append(m.Commands, mc)
	}
	return m
}
