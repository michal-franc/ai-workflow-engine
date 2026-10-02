package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
)

var telemetryCommand = &Command{
	Name:      "telemetry",
	ShortHelp: "Local usage telemetry (sub: report|path)",
	LongHelp: `Inspect the local, names-only usage telemetry issue-cli records for every
invocation. Nothing is ever sent off-machine.

Subcommands:
  report [--since 30d|12h|2026-09-01|all] [--global] [--top N]
      Usage report: calls per command, never-used commands/aliases/
      subcommands/topics/flags, top errors, unknown commands/flags bots
      tried, and fail→next-call retry sequences. Pass --json (global flag)
      for the machine-readable v1 report the viewer's /stats page renders.
  path
      Print the active telemetry file and whether telemetry is enabled.

Files:
  per project:  <workdir>/.agent-logs/telemetry.jsonl  (+ .1 after 10 MB)
  no project:   ${XDG_STATE_HOME:-~/.local/state}/issue-cli/telemetry.jsonl  (--global)

Opt out: ISSUE_CLI_TELEMETRY=off, or "telemetry: false" on the project in
projects.yaml. This does not affect the agent timeline or retry hints, which
use a separate log.`,
	Run:         runTelemetry,
	Subcommands: []string{"report", "path"},
}

func init() {
	registerCommand(telemetryCommand)
}

func runTelemetry(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("telemetry requires a subcommand\n\nUsage:\n  issue-cli telemetry report [--since 30d] [--global] [--top N]\n  issue-cli telemetry path [--global]")
	}
	switch args[0] {
	case "report":
		return runTelemetryReport(ctx, args[1:])
	case "path":
		return runTelemetryPath(ctx, args[1:])
	default:
		return fmt.Errorf("unknown telemetry subcommand: %s\n\nValid: report, path", args[0])
	}
}

// telemetryTarget resolves which file a telemetry subcommand reads and
// whether recording into it is enabled. disabled is a human-readable reason,
// "" when enabled.
func telemetryTarget(ctx *Context, global bool) (path, disabled string, err error) {
	if global {
		path = telemetry.GlobalPath()
		if path == "" {
			return "", "", fmt.Errorf("cannot determine the global telemetry path (no $XDG_STATE_HOME or home directory)")
		}
	} else {
		if ctx.Project == nil {
			return "", "", fmt.Errorf("telemetry needs a project — pass --project <slug>, or --global for the no-project fallback file")
		}
		if !ctx.Project.TelemetryEnabled() {
			disabled = fmt.Sprintf("telemetry: false is set for project %q in projects.yaml", ctx.Project.Slug)
		}
		root := ctx.Project.TelemetryRoot()
		if root == "" {
			return "", "", fmt.Errorf("cannot determine the telemetry directory for project %q (no workdir or issues dir)", ctx.Project.Slug)
		}
		path = telemetry.ProjectPath(root)
	}
	if disabled == "" && telemetry.EnvDisabled() {
		disabled = telemetry.DisableEnv + " is set to " + strings.TrimSpace(strings.ToLower(os.Getenv(telemetry.DisableEnv)))
	}
	return path, disabled, nil
}

func runTelemetryReport(ctx *Context, args []string) error {
	fs := newFlagSet("telemetry report", ctx)
	sinceFlag := fs.String("since", "30d", "window: duration with d/h/m suffix (30d, 12h), a date (2026-09-01), or all")
	globalFlag := fs.Bool("global", false, "read the global no-project fallback file instead of the project's")
	topFlag := fs.Int("top", 10, "rows to show in the errors / unknown / retry sections")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	since, err := parseSince(*sinceFlag, ctx.Now())
	if err != nil {
		return err
	}
	path, disabled, err := telemetryTarget(ctx, *globalFlag)
	if err != nil {
		return err
	}

	var events []telemetry.Event
	if err := telemetry.ReadFiles(path, since, func(e telemetry.Event) { events = append(events, e) }); err != nil {
		return fmt.Errorf("reading telemetry: %w", err)
	}
	report := telemetry.Aggregate(events, buildManifest(), telemetry.Options{Source: path, Since: since, Top: *topFlag})
	report.Enabled = disabled == ""
	report.Disabled = disabled

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, report)
	}
	if disabled != "" {
		fmt.Fprintf(ctx.Stdout, "note: telemetry recording is disabled (%s); showing previously recorded events\n\n", disabled)
	}
	telemetry.RenderText(ctx.Stdout, report)
	return nil
}

func runTelemetryPath(ctx *Context, args []string) error {
	fs := newFlagSet("telemetry path", ctx)
	globalFlag := fs.Bool("global", false, "show the global no-project fallback file")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	path, disabled, err := telemetryTarget(ctx, *globalFlag)
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]interface{}{
			"path":     path,
			"enabled":  disabled == "",
			"disabled": disabled,
		})
	}
	fmt.Fprintln(ctx.Stdout, path)
	if disabled != "" {
		fmt.Fprintf(ctx.Stdout, "disabled: %s\n", disabled)
	} else {
		fmt.Fprintln(ctx.Stdout, "enabled")
	}
	return nil
}

// parseSince turns --since into a lower time bound; zero means no bound.
func parseSince(v string, now time.Time) (time.Time, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", "all", "0":
		return time.Time{}, nil
	}
	if strings.HasSuffix(v, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(v, "d")); err == nil && n >= 0 {
			return now.Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(v); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: use a duration like 30d or 12h, a date like 2026-09-01, or all", v)
}
