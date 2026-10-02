package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// isolateTelemetryEnv neutralizes everything in the developer's / agent's
// environment that would change telemetry behaviour, and points the global
// fallback at a temp dir so tests never write to ~/.local/state.
func isolateTelemetryEnv(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	for _, k := range []string{telemetry.DisableEnv, telemetry.SkipEnv, "ISSUE_VIEWER_CONFIG", "ISSUE_VIEWER_ISSUE_SLUG", "CLAUDECODE", "AI_AGENT", "TMUX"} {
		t.Setenv(k, "")
	}
	return filepath.Join(state, "issue-cli", telemetry.FileName)
}

// telemetryFixture writes a projects.yaml with an "alpha" project (workdir
// set, telemetry on) and an optional extra YAML line for the project.
func telemetryFixture(t *testing.T, extra string) (cfg, workDir string) {
	t.Helper()
	dir := t.TempDir()
	withCwd(t, dir)
	workDir = filepath.Join(dir, "alpha")
	issues := filepath.Join(workDir, "issues")
	if err := os.MkdirAll(issues, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg = filepath.Join(dir, "projects.yaml")
	body := fmt.Sprintf("projects:\n  - name: Alpha\n    slug: alpha\n    issues: %s\n    workdir: %s\n%s", issues, workDir, extra)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg, workDir
}

// invoke mirrors main(): run the command, then record telemetry for it.
func invoke(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errw bytes.Buffer
	inv := &invocation{}
	start := time.Now()
	err := runTraced(args, strings.NewReader(""), &out, &errw, inv)
	recordTelemetry(inv, args, err, start, false)
	return out.String(), err
}

func readEvents(t *testing.T, path string) []telemetry.Event {
	t.Helper()
	var evs []telemetry.Event
	if err := telemetry.ReadFiles(path, time.Time{}, func(e telemetry.Event) { evs = append(evs, e) }); err != nil {
		t.Fatal(err)
	}
	return evs
}

func lastEvent(t *testing.T, path string) telemetry.Event {
	t.Helper()
	evs := readEvents(t, path)
	if len(evs) == 0 {
		t.Fatalf("no telemetry events in %s", path)
	}
	return evs[len(evs)-1]
}

func TestTelemetryNeverRecordsFreeTextValues(t *testing.T) {
	isolateTelemetryEnv(t)
	cfg, workDir := telemetryFixture(t, "")
	const secret = "SENTINEL-SECRET-4242"
	g := []string{"--config", cfg, "--project", "alpha"}
	run := func(args ...string) { _, _ = invoke(t, append(append([]string{}, g...), args...)...) }

	run("create", "--title", "Hello "+secret, "--status", "idea")
	run("create", "--title", "Plain", "--status", "idea")
	run("comment", "plain", "--text", "tests: "+secret)
	run("append", "plain", "--body", "## Notes "+secret)
	run("append", "plain", "--section", "Notes", "--body", secret)
	run("retrospective", "plain", "--body", secret)
	run("set-meta", "plain", "--key", "owner", "--value", secret)
	run("data", "add", "plain", "--description", secret)
	run("update", "plain", "--title", secret)
	run("transition", "plain", "--to", "in design", "--field", "why="+secret)

	data, err := os.ReadFile(telemetry.ProjectPath(workDir))
	if err != nil {
		t.Fatalf("telemetry file missing: %v", err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("free-text value leaked into telemetry:\n%s", data)
	}
	sawSafe := false
	for _, e := range readEvents(t, telemetry.ProjectPath(workDir)) {
		if e.Safe != nil {
			sawSafe = true
			if e.Safe.To != "" && e.Safe.To != "in design" {
				t.Errorf("unexpected --to value %q", e.Safe.To)
			}
			if e.Safe.Section != "" && e.Safe.Section != "Notes" {
				t.Errorf("unexpected --section value %q", e.Safe.Section)
			}
		}
	}
	if !sawSafe {
		t.Fatal("expected whitelisted --to / --section values to be recorded")
	}
}

func TestTelemetryUnknownCommandAndFlagTokens(t *testing.T) {
	isolateTelemetryEnv(t)
	cfg, workDir := telemetryFixture(t, "")
	path := telemetry.ProjectPath(workDir)
	long := strings.Repeat("x", 100)

	if _, err := invoke(t, "--config", cfg, "--project", "alpha", "add-"+long); err == nil {
		t.Fatal("expected unknown command error")
	}
	e := lastEvent(t, path)
	if e.ErrClass != telemetry.ErrUnknownCommand || e.Cmd != "" || e.Exit != 1 {
		t.Fatalf("unknown command event wrong: %+v", e)
	}
	if len(e.Unknown) != telemetry.MaxUnknownLen || !strings.HasPrefix(e.Unknown, "add-x") {
		t.Fatalf("unknown token not truncated to %d: %q", telemetry.MaxUnknownLen, e.Unknown)
	}
	if e.Project != "alpha" {
		t.Fatalf("unknown command should still land in the project file with project set, got %q", e.Project)
	}

	_, _ = invoke(t, "--config", cfg, "--project", "alpha", "create", "--title", "T")
	if _, err := invoke(t, "--config", cfg, "--project", "alpha", "comment", "t", "--message", "hi"); err == nil {
		t.Fatal("expected unknown flag error")
	}
	e = lastEvent(t, path)
	if e.ErrClass != telemetry.ErrUnknownFlag || e.Unknown != "--message" || e.Cmd != "comment" {
		t.Fatalf("unknown flag event wrong: %+v", e)
	}
}

func TestTelemetryOptOutWritesNothing(t *testing.T) {
	globalPath := isolateTelemetryEnv(t)

	t.Run("env", func(t *testing.T) {
		cfg, workDir := telemetryFixture(t, "")
		t.Setenv(telemetry.DisableEnv, "off")
		out, err := invoke(t, "--config", cfg, "--project", "alpha", "list")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(telemetry.DisableEnv, "")
		out2, _ := invoke(t, "--config", cfg, "--project", "alpha", "list")
		if out != out2 {
			t.Fatalf("output must not depend on telemetry:\n%q\n%q", out, out2)
		}
		if n := len(readEvents(t, telemetry.ProjectPath(workDir))); n != 1 {
			t.Fatalf("expected only the enabled call recorded, got %d", n)
		}
	})

	t.Run("project config", func(t *testing.T) {
		cfg, workDir := telemetryFixture(t, "    telemetry: false\n")
		if _, err := invoke(t, "--config", cfg, "--project", "alpha", "list"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(telemetry.ProjectPath(workDir)); !os.IsNotExist(err) {
			t.Fatalf("telemetry: false must not create the file (err=%v)", err)
		}
		if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
			t.Fatal("opted-out project must not fall back to the global file")
		}
	})
}

func TestRecordTelemetryUnwritablePathIsSilent(t *testing.T) {
	isolateTelemetryEnv(t)
	blocker := filepath.Join(t.TempDir(), "file-not-dir")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &invocation{command: "list", ctx: &Context{Project: &tracker.Project{Slug: "p", WorkDir: blocker}}}
	inv.cmd = lookupCommand("list")
	// Must neither panic nor error out — telemetry failures are swallowed.
	recordTelemetry(inv, []string{"list"}, nil, time.Now(), false)
	recordTelemetry(inv, []string{"list"}, errors.New("boom"), time.Now(), true)
}

func TestTelemetryUnresolvableProjectGoesToGlobalFile(t *testing.T) {
	globalPath := isolateTelemetryEnv(t)
	cfg, _ := telemetryFixture(t, "")
	if _, err := invoke(t, "--config", cfg, "--project", "nope", "list"); err == nil {
		t.Fatal("expected project resolution error")
	}
	e := lastEvent(t, globalPath)
	if e.ErrClass != telemetry.ErrProjectResolution || e.Project != "nope" || e.Cmd != "list" {
		t.Fatalf("global fallback event wrong: %+v", e)
	}
}

func TestTelemetryEventFields(t *testing.T) {
	isolateTelemetryEnv(t)
	cfg, workDir := telemetryFixture(t, "")
	path := telemetry.ProjectPath(workDir)
	g := func(args ...string) []string { return append([]string{"--config", cfg, "--project", "alpha"}, args...) }

	_, _ = invoke(t, g("create", "--title", "Hello", "--status", "idea")...)
	_, _ = invoke(t, g("claim", "hello", "--assignee", "agent-x")...)

	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "hello")
	if _, err := invoke(t, g("show", "hello")...); err != nil {
		t.Fatal(err)
	}
	e := lastEvent(t, path)
	if e.Cmd != "context" || e.Alias != "show" || e.Issue != "hello" || e.Assignee != "agent-x" || e.Caller != telemetry.CallerDispatched {
		t.Fatalf("alias/issue/caller fields wrong: %+v", e)
	}
	if strings.Join(e.Flags, ",") != "config,project" {
		t.Fatalf("flags should be the global flag names: %v", e.Flags)
	}
	if e.V != 1 || e.TS == "" || e.ArgsHash == "" || e.PID == 0 || e.Exit != 0 {
		t.Fatalf("base fields missing: %+v", e)
	}
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "")

	cases := []struct {
		args      []string
		cmd, sub  string
		wantFlags string
	}{
		{g("process", "transitions", "--system", "CLI"), "process", "transitions", "config,project,system"},
		{g("help", "transition"), "help", "transition", "config,project"},
		{g("help", "transitions"), "help", "transitions", "config,project"},
		{g("help", "workflow"), "help", "workflow", "config,project"},
		{g("help", "tests"), "help", "testing", "config,project"},
		{g("data", "rm", "hello", "1"), "data", "remove", "config,project"},
		{g("--json", "list", "--status", "idea"), "list", "", "config,json,project,status"},
	}
	for _, c := range cases {
		_, _ = invoke(t, c.args...)
		e := lastEvent(t, path)
		if e.Cmd != c.cmd || e.Sub != c.sub || strings.Join(e.Flags, ",") != c.wantFlags {
			t.Errorf("%v: got cmd=%q sub=%q flags=%v", c.args, e.Cmd, e.Sub, e.Flags)
		}
	}

	_, _ = invoke(t, g("process", "bogus-topic")...)
	if e := lastEvent(t, path); e.ErrClass != telemetry.ErrUnknownTopic || e.Unknown != "bogus-topic" || e.Sub != "" {
		t.Fatalf("unknown topic event wrong: %+v", e)
	}
	_, _ = invoke(t, g("data", "frobnicate")...)
	if e := lastEvent(t, path); e.ErrClass != telemetry.ErrUnknownSubcommand || e.Unknown != "frobnicate" {
		t.Fatalf("unknown subcommand event wrong: %+v", e)
	}
}

func TestClassifyError(t *testing.T) {
	approval := &tracker.ApprovalMissingError{Slug: "s", Required: "backlog"}
	cases := []struct {
		name  string
		inv   invocation
		err   error
		class string
		tok   string
	}{
		{"unknown command", invocation{phase: phaseUnknownCommand, command: "frob"}, errors.New("unknown command: frob"), telemetry.ErrUnknownCommand, "frob"},
		{"project", invocation{phase: phaseProject}, errors.New("x"), telemetry.ErrProjectResolution, ""},
		{"split", invocation{phase: phaseSplit}, errors.New("--config requires a value"), telemetry.ErrUsage, ""},
		{"approval", invocation{}, fmt.Errorf("wrapped: %w", approval), telemetry.ErrApprovalMissing, ""},
		{"approval sentinel", invocation{}, tracker.ErrApprovalMissing, telemetry.ErrApprovalMissing, ""},
		{"not found", invocation{}, &issueNotFoundError{msg: "issue not found: x"}, telemetry.ErrIssueNotFound, ""},
		{"unknown flag", invocation{}, &flagParseError{err: errors.New("flag provided but not defined: -nope")}, telemetry.ErrUnknownFlag, "--nope"},
		{"flag parse", invocation{}, &flagParseError{err: errors.New("invalid value \"x\" for flag -index")}, telemetry.ErrFlagParse, ""},
		{"topic", invocation{}, errors.New("unknown topic: foo\n\nAvailable"), telemetry.ErrUnknownTopic, "foo"},
		{"subcommand", invocation{}, errors.New("unknown data subcommand: zap\n\nValid"), telemetry.ErrUnknownSubcommand, "zap"},
		{"usage", invocation{}, errors.New("transition requires <slug>\n\nExample"), telemetry.ErrUsage, ""},
		{"other", invocation{}, errors.New("disk on fire"), telemetry.ErrOther, ""},
	}
	for _, c := range cases {
		class, tok := classifyError(&c.inv, c.err)
		if class != c.class || tok != c.tok {
			t.Errorf("%s: got (%q, %q) want (%q, %q)", c.name, class, tok, c.class, c.tok)
		}
	}
}

func TestClassifyTransitionErrorsFromTracker(t *testing.T) {
	isolateTelemetryEnv(t)
	cfg, workDir := telemetryFixture(t, "")
	path := telemetry.ProjectPath(workDir)
	g := func(args ...string) []string { return append([]string{"--config", cfg, "--project", "alpha"}, args...) }
	_, _ = invoke(t, g("create", "--title", "Hello", "--status", "idea")...)

	_, _ = invoke(t, g("transition", "hello", "--to", "done")...)
	if e := lastEvent(t, path); e.ErrClass != telemetry.ErrInvalidTransition {
		t.Fatalf("expected invalid_transition, got %+v", e)
	}
	// idea → in design requires a non-empty body; the new issue has none.
	_, _ = invoke(t, g("transition", "hello", "--to", "in design")...)
	if e := lastEvent(t, path); e.ErrClass != telemetry.ErrValidation {
		t.Fatalf("expected validation, got %+v", e)
	}
	_, _ = invoke(t, g("show", "does-not-exist")...)
	if e := lastEvent(t, path); e.ErrClass != telemetry.ErrIssueNotFound {
		t.Fatalf("expected issue_not_found, got %+v", e)
	}
}

func TestDetectCaller(t *testing.T) {
	isolateTelemetryEnv(t)
	for _, k := range os.Environ() {
		if strings.HasPrefix(k, "CODEX_") {
			t.Setenv(strings.SplitN(k, "=", 2)[0], "")
			os.Unsetenv(strings.SplitN(k, "=", 2)[0])
		}
	}
	orig := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = orig })
	stdinIsTerminal = func() bool { return false }
	if c, a, tm := detectCaller(); c != telemetry.CallerScript || a != "" || tm {
		t.Fatalf("plain: got %q %q %v", c, a, tm)
	}
	stdinIsTerminal = func() bool { return true }
	if c, _, _ := detectCaller(); c != telemetry.CallerTTY {
		t.Fatalf("tty: got %q", c)
	}
	t.Setenv("AI_AGENT", "claude-code_2-1-287_agent")
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	if c, a, tm := detectCaller(); c != telemetry.CallerAgent || a != "claude-code" || !tm {
		t.Fatalf("agent: got %q %q %v", c, a, tm)
	}
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "x")
	if c, _, _ := detectCaller(); c != telemetry.CallerDispatched {
		t.Fatalf("dispatched: got %q", c)
	}
}

// Introspection must be side-effect free for every registered command and
// subcommand: it runs from `telemetry report` in the caller's cwd.
func TestIntrospectFlagsEveryCommandWithoutSideEffects(t *testing.T) {
	isolateTelemetryEnv(t)
	cwd := t.TempDir()
	withCwd(t, cwd)
	issueDir := t.TempDir()

	for _, name := range commandNames() {
		cmd := commandRegistry[name]
		subs := []string{""}
		if len(cmd.Subcommands) > 0 && name != "help" && name != "process" {
			subs = cmd.Subcommands
		}
		for _, sub := range subs {
			if _, ok := introspectFlags(cmd, sub, issueDir); !ok {
				t.Errorf("introspection failed for %q %q — the command must call parseFlags before doing work", name, sub)
			}
		}
	}
	for _, dir := range []string{cwd, issueDir} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatalf("introspection created files in %s: %v", dir, entries)
		}
	}

	m := buildManifest()
	flagsOf := map[string]string{}
	for _, c := range m.Commands {
		if len(c.FlagsUnknown) > 0 {
			t.Errorf("%s has un-introspectable flags: %v", c.Name, c.FlagsUnknown)
		}
		for k, v := range c.Flags {
			flagsOf[k] = strings.Join(v, ",")
		}
	}
	for key, want := range map[string]string{
		"transition":       "field,to",
		"process":          "system,workflow",
		"data add":         "description,status,tier",
		"telemetry report": "global,since,top",
		"comment":          "body,body-file,text",
	} {
		if flagsOf[key] != want {
			t.Errorf("flags for %q: got %q want %q", key, flagsOf[key], want)
		}
	}
}

func writeFixtureEvents(t *testing.T, path string, evs ...telemetry.Event) {
	t.Helper()
	for _, e := range evs {
		if err := (telemetry.FileSink{Path: path}).Write(e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTelemetryReportCommand(t *testing.T) {
	globalPath := isolateTelemetryEnv(t)
	cfg, workDir := telemetryFixture(t, "")
	path := telemetry.ProjectPath(workDir)
	now := time.Now()
	mk := func(cmd string, age time.Duration, exit int, class string) telemetry.Event {
		return telemetry.Event{V: 1, TS: now.Add(-age).UTC().Format(time.RFC3339Nano), Cmd: cmd, Exit: exit, ErrClass: class, Caller: "agent", PPID: 7, ArgsHash: cmd}
	}
	writeFixtureEvents(t, path,
		mk("list", 60*24*time.Hour, 0, ""), // outside the default 30d window
		mk("transition", time.Hour, 1, telemetry.ErrValidation),
		mk("transition", time.Hour-time.Second, 1, telemetry.ErrValidation),
		mk("show", time.Minute, 0, ""),
	)
	g := []string{"--config", cfg, "--project", "alpha"}

	out, err := invoke(t, append(g, "telemetry", "report")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"== Summary ==", "== Commands ==", "== Never used ==", "== Errors ==", "== Unknown ==", "== Retry sequences ==", "events:    3", "validation", "same args again"} {
		if !strings.Contains(out, s) {
			t.Errorf("text report missing %q:\n%s", s, out)
		}
	}

	out, err = invoke(t, append(g, "--json", "telemetry", "report", "--since", "all", "--top", "1")...)
	if err != nil {
		t.Fatal(err)
	}
	var r telemetry.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	// The report call itself was recorded before this one, so the window
	// holds the 4 fixtures + the text report call.
	if r.V != 1 || !r.Enabled || r.Summary.Events != 5 || len(r.Errors) != 1 || r.Source != path {
		t.Fatalf("json report wrong: %+v", r)
	}
	if len(r.NeverUsed.Commands) == 0 {
		t.Fatal("never-used commands should be populated from the manifest")
	}

	writeFixtureEvents(t, globalPath, mk("frob", time.Minute, 1, telemetry.ErrUnknownCommand))
	out, err = invoke(t, append(g, "--json", "telemetry", "report", "--global")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, globalPath) {
		t.Fatalf("--global should read %s:\n%s", globalPath, out)
	}

	if _, err := invoke(t, append(g, "telemetry", "report", "--since", "yesterday-ish")...); err == nil {
		t.Fatal("expected invalid --since error")
	}

	out, err = invoke(t, append(g, "telemetry", "path")...)
	if err != nil || !strings.Contains(out, path) || !strings.Contains(out, "enabled") {
		t.Fatalf("telemetry path output wrong (%v):\n%s", err, out)
	}
}

// The viewer's own report call must not be recorded, yet the report must
// still say telemetry is enabled (SkipEnv is not an opt-out).
func TestTelemetrySkipEnvSkipsRecordingButStaysEnabled(t *testing.T) {
	isolateTelemetryEnv(t)
	cfg, workDir := telemetryFixture(t, "")
	t.Setenv(telemetry.SkipEnv, "1")
	out, err := invoke(t, "--config", cfg, "--project", "alpha", "--json", "telemetry", "report")
	if err != nil {
		t.Fatal(err)
	}
	var r telemetry.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Enabled || r.Disabled != "" {
		t.Fatalf("skip must not read as disabled: %+v", r)
	}
	if _, err := os.Stat(telemetry.ProjectPath(workDir)); !os.IsNotExist(err) {
		t.Fatal("skipped invocation must not be recorded")
	}
}

func TestTelemetryReportDisabledProject(t *testing.T) {
	isolateTelemetryEnv(t)
	cfg, _ := telemetryFixture(t, "    telemetry: false\n")
	out, err := invoke(t, "--config", cfg, "--project", "alpha", "--json", "telemetry", "report")
	if err != nil {
		t.Fatal(err)
	}
	var r telemetry.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Enabled || !strings.Contains(r.Disabled, "telemetry: false") {
		t.Fatalf("disabled report wrong: %+v", r)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"all": {},
		"30d": now.Add(-30 * 24 * time.Hour),
		"12h": now.Add(-12 * time.Hour),
	}
	for in, want := range cases {
		got, err := parseSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%s: got %v %v", in, got, err)
		}
	}
	if got, err := parseSince("2026-09-01", now); err != nil || got.Format("2006-01-02") != "2026-09-01" {
		t.Errorf("date: got %v %v", got, err)
	}
	if _, err := parseSince("soon", now); err == nil {
		t.Error("expected error")
	}
}

func TestParseFlagsMarksErrorsButNotHelp(t *testing.T) {
	ctx := &Context{Stderr: &bytes.Buffer{}}
	fs := newFlagSet("x", ctx)
	if err := parseFlags(ctx, fs, []string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected ErrHelp passthrough, got %v", err)
	}
	fs = newFlagSet("x", ctx)
	err := parseFlags(ctx, fs, []string{"--nope"})
	var fpe *flagParseError
	if !errors.As(err, &fpe) || err.Error() != "flag provided but not defined: -nope" {
		t.Fatalf("parse error should be marked with the stdlib message intact, got %v", err)
	}
	if len(ctx.flagSets) != 2 {
		t.Fatalf("parsed FlagSets should be recorded, got %d", len(ctx.flagSets))
	}
}
