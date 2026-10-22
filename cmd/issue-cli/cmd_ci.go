package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var ciCommand = &Command{
	Name:      "ci",
	ShortHelp: "Local CI on the integration branch (sub: status|run|reset|watch)",
	LongHelp: `Local CI: the project's ci: commands (workflow.yaml) run on the head of the
integration branch after every move, in the persistent worktree .worktrees/.ci.
Results go to .ci/ in the main checkout. The viewer starts runs by itself; an
agent never waits for CI.

Subcommands:
  status [--tier <name>]
      One line first: "main green at <sha>", "main red since <sha>: <command>:
      <first failing tests>" or "main: CI can't run: ...". Then the recent runs.
      Pass --json (global flag) for the state and runs.
  run [--tier <name>] [--detach] [--if-untested]
      Run now, in the foreground. Re-tests the head; keeps going while new
      commits land, testing only the newest. --tier slow adds the slow-tier
      commands (never run on merge). --detach starts the run in tmux window
      ci-<project> and returns. --if-untested skips a head already tested.
      Exit code: 0 green, 1 red or CI can't run.
  reset
      Remove the CI worktree (ignored build output included); the next run
      creates it again.
  watch [--interval 5s] [--once]
      Start a detached run whenever the branch head is untested. Use it when
      the viewer is not running.

Config (workflow.yaml):
  ci:
    run:
      - make validate
      - cmd: make ux-test
        tier: slow
    timeout: 20m        # default 10m
    on_red: mark        # or file_issue
See docs/workflow.md, "Local CI".`,
	Run:         runCI,
	Subcommands: []string{"status", "run", "reset", "watch"},
}

func init() {
	registerCommand(ciCommand)
}

const ciNotConfigured = "ci: not configured — add a ci: block to workflow.yaml (see docs/workflow.md, \"Local CI\")"

func runCI(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("ci requires a subcommand\n\nUsage:\n  issue-cli ci status [--tier <name>]\n  issue-cli ci run [--tier <name>] [--detach]\n  issue-cli ci reset\n  issue-cli ci watch [--interval 5s]")
	}
	switch args[0] {
	case "status":
		return runCIStatus(ctx, args[1:])
	case "run":
		return runCIRun(ctx, args[1:])
	case "reset":
		return runCIReset(ctx, args[1:])
	case "watch":
		return runCIWatch(ctx, args[1:])
	default:
		return fmt.Errorf("unknown ci subcommand: %s\n\nValid: status, run, reset, watch", args[0])
	}
}

// ciRoot is the main checkout CI keeps its state in.
func ciRoot(proj *tracker.Project) string {
	return ci.RepoRoot(projectRoot(proj))
}

func runCIStatus(ctx *Context, args []string) error {
	fs := newFlagSet("ci status", ctx)
	tierFlag := fs.String("tier", "", "show the newest run of this tier too")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	proj := ctx.Project
	wf := proj.LoadWorkflow()
	if !wf.CI.Enabled() {
		if ctx.JSONOutput {
			return writeJSON(ctx.Stdout, map[string]any{"configured": false, "hint": ciNotConfigured})
		}
		fmt.Fprintln(ctx.Stdout, ciNotConfigured)
		return nil
	}
	root := ciRoot(proj)
	st, err := ci.ReadState(root)
	if err != nil {
		return err
	}
	lock, alive := ci.ReadLock(root)
	branch, _ := ci.ResolveBranch(root, wf.CI)
	line := ci.OneLine(st, lock, alive, branch, ctx.now())
	runs := ci.RecentRuns(root, 5)

	if ctx.JSONOutput {
		out := map[string]any{"configured": true, "branch": branch, "line": line, "state": st, "runs": runs}
		if alive {
			out["running"] = lock
		}
		return writeJSON(ctx.Stdout, out)
	}
	fmt.Fprintln(ctx.Stdout, line)
	if *tierFlag != "" {
		if ts, ok := st.Tier(*tierFlag); ok {
			fmt.Fprintf(ctx.Stdout, "tier %s: %s at %s (%s ago)%s\n", *tierFlag, ts.State, ci.Short(ts.Sha), ci.Ago(ctx.now().Sub(ts.Finished)), failedSuffix(ts.Failed))
		} else {
			fmt.Fprintf(ctx.Stdout, "tier %s: no run yet — issue-cli ci run --tier %s\n", *tierFlag, *tierFlag)
		}
	}
	if st.IsRed() {
		if len(st.Merged) > 0 {
			fmt.Fprintf(ctx.Stdout, "Landed since green %s:\n", ci.Short(st.GreenSha))
			for _, m := range st.Merged {
				fmt.Fprintf(ctx.Stdout, "  %s\n", mergedLabel(m))
			}
		}
		if st.RedIssue != "" {
			fmt.Fprintf(ctx.Stdout, "Fix tracked in: %s\n", st.RedIssue)
		}
		fmt.Fprintf(ctx.Stdout, "Log: %s\n", filepath.Join(root, st.Log))
	}
	if len(runs) > 0 {
		fmt.Fprintln(ctx.Stdout, "\nRecent runs:")
		for _, r := range runs {
			tier := ""
			if r.Tier != tracker.CIDefaultTier {
				tier = " [" + r.Tier + "]"
			}
			detail := failedSuffix(r.Failed)
			if r.State == ci.Error {
				detail = ": " + r.Error
			}
			fmt.Fprintf(ctx.Stdout, "  %s  %-5s  %s ago  %s%s%s\n", ci.Short(r.Sha), r.State, ci.Ago(ctx.now().Sub(r.Finished)), r.Finished.Sub(r.Started).Round(time.Second), tier, detail)
		}
	}
	return nil
}

func failedSuffix(failed string) string {
	if failed == "" {
		return ""
	}
	return ": " + failed
}

func mergedLabel(m ci.Merged) string {
	switch {
	case m.Slug != "":
		return fmt.Sprintf("%s (%s)", m.Slug, m.Branch)
	case m.Branch != "":
		return m.Branch
	default:
		return fmt.Sprintf("%s %s", ci.Short(m.Sha), m.Subject)
	}
}

func runCIRun(ctx *Context, args []string) error {
	fs := newFlagSet("ci run", ctx)
	tierFlag := fs.String("tier", "", "also run this tier's commands (e.g. slow)")
	detachFlag := fs.Bool("detach", false, "start the run in tmux window ci-<project> and return")
	untestedFlag := fs.Bool("if-untested", false, "skip a head that was already tested (used by the watchers)")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	proj := ctx.Project
	wf := proj.LoadWorkflow()
	if !wf.CI.Enabled() {
		return errors.New(ciNotConfigured)
	}
	root := ciRoot(proj)
	if lock, alive := ci.ReadLock(root); alive {
		fmt.Fprintf(ctx.Stdout, "already running at %s (pid %d); it tests the newest head when done\n", ci.Short(lock.Sha), lock.PID)
		return nil
	}
	if *detachFlag {
		where, err := startDetachedCI(ctx, proj, root, *tierFlag, *untestedFlag)
		if err != nil {
			return err
		}
		fmt.Fprintf(ctx.Stdout, "✓ CI run started in %s\n  issue-cli ci status   # the result\n", where)
		return nil
	}

	out := ctx.Stdout
	if ctx.JSONOutput {
		out = ctx.Stderr
	}
	runs, held, err := ci.Execute(ci.Options{
		Root:         root,
		Config:       wf.CI,
		Tier:         *tierFlag,
		OnlyUntested: *untestedFlag,
		Out:          out,
		Issues:       func() []*tracker.Issue { issues, _ := tracker.LoadIssues(proj.IssueDir); return issues },
		Report:       ciReporter(ctx, proj, wf, root),
		Now:          ctx.now,
	})
	if errors.Is(err, ci.ErrBusy) && held != nil {
		fmt.Fprintf(ctx.Stdout, "already running at %s (pid %d); it tests the newest head when done\n", ci.Short(held.Sha), held.PID)
		return nil
	}
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, runs)
	}
	if len(runs) == 0 {
		st, _ := ci.ReadState(root)
		fmt.Fprintf(ctx.Stdout, "head already tested: %s\n", ci.OneLine(st, nil, false, "", ctx.now()))
		return nil
	}
	last := runs[len(runs)-1]
	st, _ := ci.ReadState(root)
	fmt.Fprintln(ctx.Stdout, ci.OneLine(st, nil, false, "", ctx.now()))
	if last.Tier != tracker.CIDefaultTier {
		fmt.Fprintf(ctx.Stdout, "tier %s: %s at %s%s\n", last.Tier, last.State, ci.Short(last.Sha), failedSuffix(last.Failed))
	}
	fmt.Fprintf(ctx.Stdout, "Log: %s\n", filepath.Join(root, last.Log))
	if last.State != ci.Green {
		return &exitCodeError{Code: 1}
	}
	return nil
}

func runCIReset(ctx *Context, args []string) error {
	fs := newFlagSet("ci reset", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	root := ciRoot(ctx.Project)
	if err := ci.ResetWorktree(root); err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "✓ Removed %s; the next run creates it again (a full, cold build)\n", ci.WorktreePath(root))
	return nil
}

func runCIWatch(ctx *Context, args []string) error {
	fs := newFlagSet("ci watch", ctx)
	intervalFlag := fs.Duration("interval", 5*time.Second, "how often to look at the branch head")
	onceFlag := fs.Bool("once", false, "look once and exit")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	proj := ctx.Project
	root := ciRoot(proj)
	w := &ci.Watcher{Spawn: func(root string) error {
		_, err := startDetachedCI(ctx, proj, root, "", true)
		return err
	}}
	if !*onceFlag {
		fmt.Fprintf(ctx.Stdout, "Watching %s every %s (Ctrl-C stops)\n", root, *intervalFlag)
	}
	for {
		wf := proj.LoadWorkflow()
		if !wf.CI.Enabled() {
			if *onceFlag {
				fmt.Fprintln(ctx.Stdout, ciNotConfigured)
				return nil
			}
		} else if head, spawned, err := w.Check(root, wf.CI); err != nil {
			fmt.Fprintf(ctx.Stderr, "ci watch: %v\n", err)
		} else if spawned {
			fmt.Fprintf(ctx.Stdout, "%s  new head %s: run started\n", ctx.now().Format("15:04:05"), ci.Short(head))
		} else if *onceFlag {
			fmt.Fprintf(ctx.Stdout, "head %s: nothing to run\n", ci.Short(head))
		}
		if *onceFlag {
			return nil
		}
		time.Sleep(*intervalFlag)
	}
}

// ciWindowName is the tmux window (or session) detached runs live in.
func ciWindowName(proj *tracker.Project) string { return "ci-" + proj.Slug }

// startDetachedCI starts `issue-cli ci run` so it outlives the caller: in
// tmux window ci-<project> (kept open after the run so its output can be
// read), or without tmux as a background process logging to .ci/runner.log.
// A var so tests never start processes.
var startDetachedCI = func(ctx *Context, proj *tracker.Project, root, tier string, ifUntested bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	argv := []string{exe}
	// tmux windows don't inherit this process's environment, so the config
	// is passed explicitly.
	if ctx.ConfigPath != "" {
		if abs, err := filepath.Abs(ctx.ConfigPath); err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				argv = append(argv, "--config", abs)
			}
		}
	}
	argv = append(argv, "--project", proj.Slug, "ci", "run")
	if tier != "" {
		argv = append(argv, "--tier", tier)
	}
	if ifUntested {
		argv = append(argv, "--if-untested")
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		return startBackgroundCI(root, argv)
	}
	name := ciWindowName(proj)
	if agentrun.HasSession(agentrun.Target(proj, name)) {
		// The previous run's window, kept open; no run is live (checked).
		_ = agentrun.KillTmux(proj, name)
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	script := strings.Join(quoted, " ") + `; echo; echo "[local CI run finished: press Enter to close]"; read _`
	var cmd *exec.Cmd
	var where string
	shared := agentrun.SharedSession(proj)
	switch {
	case shared == "":
		where = "tmux session " + name
		cmd = exec.Command("tmux", "new-session", "-d", "-s", name, "-c", root, "sh", "-c", script)
	case agentrun.HasSession("=" + shared):
		where = fmt.Sprintf("tmux window %s:%s", shared, name)
		cmd = exec.Command("tmux", "new-window", "-d", "-t", "="+shared+":", "-n", name, "-c", root, "sh", "-c", script)
	default:
		where = fmt.Sprintf("tmux window %s:%s", shared, name)
		cmd = exec.Command("tmux", "new-session", "-d", "-s", shared, "-n", name, "-c", root, "sh", "-c", script)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("starting %s: %v: %s", where, err, strings.TrimSpace(string(out)))
	}
	return where, nil
}

// startBackgroundCI is the no-tmux fallback: a session-leader process with
// its output in .ci/runner.log.
func startBackgroundCI(root string, argv []string) (string, error) {
	logPath := filepath.Join(ci.Dir(root), "runner.log")
	if err := os.MkdirAll(ci.Dir(root), 0o755); err != nil {
		return "", err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	_ = cmd.Process.Release()
	return "the background (no tmux), log " + logPath, nil
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '=' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
