package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/health"
)

var healthCommand = &Command{
	Name:      "health",
	ShortHelp: "Project health snapshots: codebase size, big files, growth (sub: snapshot, backfill)",
	LongHelp: `Project health snapshots: dated JSON files in .health/snapshots/ of the main
checkout (gitignored), one row per metric. The viewer takes one every day and
issue-cli ci run takes one after each run, so nobody has to.

Usage:
  issue-cli health
      Print the latest snapshot: lines by kind with 7d/30d growth, test ratio,
      areas, languages, big files, and each family's result. --json (global
      flag) prints the snapshot file.
  issue-cli health snapshot
      Take a snapshot now, then print it.
  issue-cli health backfill [--days 90]
      Compute one snapshot for each past day without one, as it stood at the
      end of that day: the codebase at the branch's commit then, the workflow
      over the issues done by then. The stats page's trend charts draw from
      them. The milestone family only knows the present and is left out.

Families: codebase (git objects at the main branch head: size by kind, area
and language, tests, big files, growth), plus any other registered family
(workflow, milestone, ...). A family that fails is recorded; the others are still written.

Config (workflow.yaml, all optional):
  health:
    big_file_lines: 1000
    test_globs: ["**/*_test.go", "**/*Tests.cs"]   # replaces the defaults
    exclude: ["vendor/**", "**/*.min.js"]
    timeout: 20s                                  # per family
    off: true                                     # no snapshots
    limits:                                       # stats page gauges
      waiting_share: 0.25
      big_files: 10
Charts: the viewer's Stats page (/p/<project>/stats).
See docs/workbench-files.md, "Health snapshots".`,
	Run:         runHealth,
	Subcommands: []string{"snapshot", "backfill"},
}

func init() {
	registerCommand(healthCommand)
}

const healthNoSnapshot = "No health snapshot yet: run issue-cli health snapshot (the viewer takes one daily)"

// healthBackfillDays is how far back issue-cli health backfill goes by
// default.
const healthBackfillDays = 90

// statsURL is the viewer's stats page for the project.
func statsURL(slug string) string {
	return strings.TrimRight(viewerBaseURL(), "/") + "/p/" + slug + "/stats"
}

func runHealth(ctx *Context, args []string) error {
	if len(args) > 0 && args[0] == "snapshot" {
		return runHealthSnapshot(ctx, args[1:])
	}
	if len(args) > 0 && args[0] == "backfill" {
		return runHealthBackfill(ctx, args[1:])
	}
	fs := newFlagSet("health", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	snap, path, err := health.Latest(env.Root)
	if err != nil {
		return err
	}
	if snap == nil {
		if ctx.JSONOutput {
			return writeJSON(ctx.Stdout, map[string]any{"snapshot": nil, "hint": healthNoSnapshot})
		}
		fmt.Fprintln(ctx.Stdout, healthNoSnapshot)
		return nil
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, snap)
	}
	printHealth(ctx.Stdout, snap, path)
	fmt.Fprintf(ctx.Stdout, "\nCharts: %s\n", statsURL(ctx.Project.Slug))
	if n := health.Count(env.Root); n < 2 {
		fmt.Fprintf(ctx.Stdout, "Trends need history (%d snapshot so far): issue-cli health backfill\n", n)
	}
	return nil
}

// runHealthBackfill computes the past days' snapshots.
func runHealthBackfill(ctx *Context, args []string) error {
	fs := newFlagSet("health backfill", ctx)
	days := fs.Int("days", healthBackfillDays, "how many past days to fill, 1 to 3650")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	if *days < 1 || *days > 3650 {
		return &usageError{msg: fmt.Sprintf("--days %d: want 1 to 3650", *days)}
	}
	if !ctx.Project.LoadWorkflow().Health.Enabled() {
		return errors.New("health snapshots are off for this project (health.off: true in workflow.yaml)")
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	type dayResult struct {
		Day     string `json:"day"`
		Path    string `json:"path,omitempty"`
		Skipped string `json:"skipped,omitempty"`
		Error   string `json:"error,omitempty"`
		// NoCommit: before the branch's first commit, so no codebase rows.
		NoCommit bool `json:"no_commit,omitempty"`
	}
	var results []dayResult
	written, skipped, failed := 0, 0, 0
	err = health.Backfill(context.Background(), env, *days, func(d health.BackfillDay) {
		r := dayResult{Day: d.Day.Format("2006-01-02"), Path: d.Path, Skipped: d.Skipped, NoCommit: d.NoCommit}
		switch {
		case d.Err != nil:
			r.Error = d.Err.Error()
			failed++
		case d.Skipped != "":
			skipped++
		default:
			written++
		}
		results = append(results, r)
		if ctx.JSONOutput {
			return
		}
		switch {
		case r.Error != "":
			fmt.Fprintf(ctx.Stdout, "  %s  error: %s\n", r.Day, r.Error)
		case r.Skipped != "":
			fmt.Fprintf(ctx.Stdout, "  %s  skipped: %s\n", r.Day, r.Skipped)
		case r.NoCommit:
			fmt.Fprintf(ctx.Stdout, "  %s  %s (before the first commit: no codebase)\n", r.Day, r.Path)
		default:
			fmt.Fprintf(ctx.Stdout, "  %s  %s\n", r.Day, r.Path)
		}
	})
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]any{"written": written, "skipped": skipped, "failed": failed, "days": results, "charts": statsURL(ctx.Project.Slug)})
	}
	fmt.Fprintf(ctx.Stdout, "✓ Backfilled %d of %d days (%d skipped, %d failed) · charts: %s\n", written, *days, skipped, failed, statsURL(ctx.Project.Slug))
	return nil
}

func runHealthSnapshot(ctx *Context, args []string) error {
	fs := newFlagSet("health snapshot", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	if !ctx.Project.LoadWorkflow().Health.Enabled() {
		return errors.New("health snapshots are off for this project (health.off: true in workflow.yaml)")
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	snap, path, err := health.Take(context.Background(), env, health.TriggerManual)
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, snap)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Snapshot written: %s\n\n", path)
	printHealth(ctx.Stdout, snap, path)
	return nil
}

// ciHealthSnapshot takes the snapshot after a CI run, at the tested commit.
// It never changes the run's result: a failure is one line.
func ciHealthSnapshot(ctx *Context, w io.Writer, sha string) {
	proj := ctx.Project
	if !proj.LoadWorkflow().Health.Enabled() {
		return
	}
	env, err := health.NewEnv(proj, projectRoot(proj), ctx.now())
	if err == nil {
		env.Commit = sha
		var path string
		if _, path, err = health.Take(context.Background(), env, health.TriggerCI); err == nil {
			fmt.Fprintf(w, "health: snapshot %s (issue-cli health)\n", path)
			return
		}
	}
	fmt.Fprintf(w, "health: no snapshot: %v\n", err)
}

// printHealth is the text form of a snapshot. Only the codebase family has a
// summary here; other families show their metric count (--json has all).
func printHealth(w io.Writer, s *health.Snapshot, path string) {
	fmt.Fprintf(w, "Health snapshot %s · %s at %s · %s · %d ms\n", s.Run.Date, s.Run.Branch, s.Run.Commit, s.Run.Trigger, s.Run.DurationMS)
	fmt.Fprintf(w, "File: %s\n", path)

	if res, ok := s.Families["codebase"]; ok && res.Error == "" {
		printCodebase(w, s)
	}

	fmt.Fprintln(w, "\nFamilies:")
	names := make([]string, 0, len(s.Families))
	for name := range s.Families {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		res := s.Families[name]
		if res.Error != "" {
			fmt.Fprintf(w, "  %-10s error: %s (%d ms)\n", name, res.Error, res.MS)
			continue
		}
		hint := ""
		if res.Hint != "" {
			hint = " · " + res.Hint
		}
		fmt.Fprintf(w, "  %-10s ok, %d metrics (%d ms)%s\n", name, res.Metrics, res.MS, hint)
	}
}

func printCodebase(w io.Writer, s *health.Snapshot) {
	val := func(metric string, labels map[string]string) float64 {
		v, _ := s.Value(metric, labels)
		return v
	}
	growth := func(kind, window string) string {
		v, ok := s.Value("codebase.growth.lines", map[string]string{"kind": kind, "window": window})
		if !ok {
			return "n/a"
		}
		return fmt.Sprintf("%+d", int(v))
	}

	fmt.Fprintln(w, "\nCodebase (lines by kind; growth against 7 and 30 days ago):")
	fmt.Fprintf(w, "  %-6s %7s %9s %8s %8s\n", "kind", "files", "lines", "7d", "30d")
	for _, k := range []string{health.KindCode, health.KindTest, health.KindDocs, health.KindData, health.KindOther} {
		l := map[string]string{"kind": k}
		fmt.Fprintf(w, "  %-6s %7d %9d %8s %8s\n", k, int(val("codebase.files", l)), int(val("codebase.lines", l)), growth(k, "7d"), growth(k, "30d"))
	}
	var bases []string
	for _, win := range []string{"7d", "30d"} {
		if age, ok := s.Value("codebase.growth.base_age_days", map[string]string{"window": win}); ok {
			bases = append(bases, fmt.Sprintf("%s against %.1f days ago", win, age))
		}
	}
	if len(bases) > 0 {
		fmt.Fprintf(w, "  (%s)\n", strings.Join(bases, ", "))
	}
	fmt.Fprintf(w, "  source lines (code + test): %d · test ratio: %.2f test lines per code line · binary files: %d\n",
		int(val("codebase.source_lines", nil)), val("codebase.test_ratio", nil), int(val("codebase.binary_files", nil)))

	for _, by := range []struct{ label, title string }{{"area", "Areas"}, {"lang", "Languages"}} {
		rows := labelRows(s, "codebase.lines", by.label)
		fmt.Fprintf(w, "\n%s (top 10 by lines, of %d):\n", by.title, len(rows))
		for i, r := range rows {
			if i == 10 {
				break
			}
			files := val("codebase.files", map[string]string{by.label: r.Labels[by.label]})
			fmt.Fprintf(w, "  %-24s %6d files %9d lines\n", r.Labels[by.label], int(files), int(r.Value))
		}
	}

	big := s.Rows("codebase.big_file.lines")
	fmt.Fprintf(w, "\nBig files: %d", int(val("codebase.big_files", nil)))
	if len(big) == 0 {
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, " (biggest %d):\n", len(big))
	for _, r := range big {
		fmt.Fprintf(w, "  %7d  %s\n", int(r.Value), r.Labels["path"])
	}
}

// labelRows are the rows of metric with only the given label, biggest first.
func labelRows(s *health.Snapshot, metric, label string) []health.Row {
	var out []health.Row
	for _, r := range s.Rows(metric) {
		if _, ok := r.Labels[label]; ok && len(r.Labels) == 1 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	return out
}
