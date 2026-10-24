package ci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// tailLines is how much of the log a run record and the state keep.
const tailLines = 40

// maxFailures caps the test names pulled from one command's output.
const maxFailures = 50

// Options is one `issue-cli ci run`.
type Options struct {
	// Root is the main checkout (see RepoRoot).
	Root   string
	Config *tracker.CIConfig
	// Tier is "" or "default" for the commands that run on every move, or a
	// named tier, which runs the default commands and its own, once.
	Tier string
	// OnlyUntested skips the run when the head was already tested (the
	// watchers set it; a manual `ci run` re-tests the head).
	OnlyUntested bool
	// Out receives the live output (the tmux window); nil discards it.
	Out io.Writer
	// Issues resolves merged branches to issues.
	Issues func() []*tracker.Issue
	// Report is called after each run with the state before and after it.
	Report func(prev *State, run *Run, next *State)
	Now    func() time.Time
}

func (o *Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Execute runs CI under the project lock until the branch head has been
// tested: merges that land during a run give one more run, of the newest
// head. It returns the runs it made; ErrBusy (with the holder) when another
// run is live.
func Execute(opts Options) ([]*Run, *Lock, error) {
	if !opts.Config.Enabled() {
		return nil, nil, errors.New("ci is not configured: add a ci: block to workflow.yaml")
	}
	tier := opts.Tier
	if tier == "" {
		tier = defaultTier
	}
	if tier != defaultTier && !hasTier(opts.Config, tier) {
		return nil, nil, fmt.Errorf("ci.run has no command with tier %q (tiers: %s)", tier, strings.Join(append([]string{defaultTier}, opts.Config.Tiers()...), ", "))
	}
	branch, err := ResolveBranch(opts.Root, opts.Config)
	if err != nil {
		return nil, nil, err
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}

	me := Lock{PID: os.Getpid(), Tier: tier, Started: opts.now()}
	if held, err := acquireLock(opts.Root, me); err != nil {
		return nil, held, err
	}
	defer releaseLock(opts.Root, me.PID)

	var runs []*Run
	for first := true; ; first = false {
		head, err := Head(opts.Root, branch)
		if err != nil || head == "" {
			return runs, nil, fmt.Errorf("branch %s not found in %s", branch, opts.Root)
		}
		prev, err := ReadState(opts.Root)
		if err != nil {
			return runs, nil, err
		}
		if tier != defaultTier {
			if !first {
				break
			}
		} else if prev != nil && prev.LastSha == head && (!first || opts.OnlyUntested) {
			break
		}

		me.Sha = head
		updateLock(opts.Root, me)
		run := runOnce(opts, branch, tier, head, prev)
		next := applyRun(prev, run)
		if err := writeRun(opts.Root, run); err != nil {
			return runs, nil, err
		}
		if err := WriteState(opts.Root, next); err != nil {
			return runs, nil, err
		}
		runs = append(runs, run)
		if opts.Report != nil {
			opts.Report(prev, run, next)
		}
		prune(opts.Root)
		if tier != defaultTier {
			break
		}
	}
	return runs, nil, nil
}

func hasTier(cfg *tracker.CIConfig, tier string) bool {
	for _, t := range cfg.Tiers() {
		if t == tier {
			return true
		}
	}
	return false
}

// runOnce tests one commit.
func runOnce(opts Options, branch, tier, sha string, prev *State) *Run {
	root := opts.Root
	run := &Run{Sha: sha, Subject: subject(root, sha), Branch: branch, Tier: tier, Started: opts.now(), Log: RelLog(sha, tier)}
	if prev != nil && tier == defaultTier {
		run.RangeFrom = prev.GreenSha
		var issues []*tracker.Issue
		if opts.Issues != nil {
			issues = opts.Issues()
		}
		run.Merged = blame(root, branch, prev.GreenSha, sha, issues)
	}

	logPath := filepath.Join(root, run.Log)
	logFile, err := os.Create(logPath)
	if err != nil {
		return finishError(opts, run, fmt.Errorf("cannot write the log: %w", err))
	}
	defer logFile.Close()
	out := io.MultiWriter(logFile, opts.Out)
	fmt.Fprintf(out, "== local CI: %s at %s (%s), tier %s, %s ==\n", branch, Short(sha), run.Subject, tier, run.Started.Format("2006-01-02 15:04:05"))

	dir, err := prepareWorktree(root, sha)
	if err != nil {
		fmt.Fprintf(out, "CI can't run: %v\n", err)
		return finishError(opts, run, err)
	}

	limit := opts.Config.TimeoutDuration()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	resultsDir, err := prepareResultsDir(root, sha, tier)
	if err != nil {
		fmt.Fprintf(out, "CI can't run: %v\n", err)
		return finishError(opts, run, err)
	}
	run.State = Green
	for _, c := range opts.Config.Commands(tier) {
		fmt.Fprintf(out, "\n$ %s\n", c.Cmd)
		offset, _ := logFile.Seek(0, io.SeekCurrent)
		start := time.Now()
		exit, timedOut, startErr := runCommand(ctx, dir, c.Cmd, opts.Config.NiceLevel(), resultsDir, out)
		collectResults(root, run, c, resultsDir, out)
		res := CommandResult{Cmd: c.Cmd, Tier: c.TierName(), Exit: exit, DurationMS: time.Since(start).Milliseconds(), TimedOut: timedOut}
		if startErr != nil {
			run.Commands = append(run.Commands, res)
			fmt.Fprintf(out, "CI can't run %q: %v\n", c.Cmd, startErr)
			return finishError(opts, run, fmt.Errorf("%s: %w", c.Cmd, startErr))
		}
		if exit != 0 || timedOut {
			res.Failures = c.FailureNames(readFrom(logPath, offset), maxFailures)
			run.Commands = append(run.Commands, res)
			run.State, run.Failed, run.Failures = Red, c.Cmd, res.Failures
			if timedOut {
				run.Failed = fmt.Sprintf("%s (timeout after %s)", c.Cmd, limit)
			}
			break
		}
		run.Commands = append(run.Commands, res)
	}
	run.Finished = opts.now()
	verdict := run.State
	if run.Failed != "" {
		verdict += ": " + run.Failed
	}
	fmt.Fprintf(out, "\n== local CI %s at %s in %s ==\n", verdict, Short(sha), run.Finished.Sub(run.Started).Round(time.Second))
	logFile.Sync()
	run.LogTail = tail(logPath, tailLines)
	return run
}

func finishError(opts Options, run *Run, err error) *Run {
	run.State, run.Error, run.Finished = Error, err.Error(), opts.now()
	run.LogTail = tail(filepath.Join(opts.Root, run.Log), tailLines)
	return run
}

// runCommand runs one command under sh in its own session: its own process
// group, so a timeout kills everything it started, and no controlling
// terminal, so a command that wants one (an interactive shell) does not stop
// on the runner's tty. startErr means it could not start.
func runCommand(ctx context.Context, dir, command string, nice int, resultsDir string, out io.Writer) (exit int, timedOut bool, startErr error) {
	args := []string{"sh", "-c", command}
	if nice > 0 {
		if p, err := exec.LookPath("nice"); err == nil {
			args = append([]string{p, "-n", strconv.Itoa(nice)}, args...)
		}
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = commandEnv(os.Environ())
	if resultsDir != "" {
		cmd.Env = append(cmd.Env, ResultsEnv+"="+resultsDir)
	}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if cmd.Process != nil {
		// Whatever the command left running in the background goes too.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return -1, true, nil
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, false, nil
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), false, nil
	case errors.Is(err, exec.ErrWaitDelay):
		return 0, false, nil
	default:
		return -1, false, err
	}
}

// commandEnv is the environment CI commands get: CI=true, and nothing that
// would make issue-cli inside a test think it is a dispatched agent or reach
// the real tmux server.
func commandEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(key, "ISSUE_VIEWER_"), key == "TMUX", key == "TMUX_PANE", key == "AGENT_NAME", key == "CI", key == ResultsEnv:
			continue
		}
		out = append(out, kv)
	}
	return append(out, "CI=true")
}

// applyRun is the state after run.
func applyRun(prev *State, run *Run) *State {
	next := &State{}
	if prev != nil {
		cp := *prev
		next = &cp
	}
	next.Branch, next.Updated = run.Branch, run.Finished
	if run.Tier != defaultTier {
		tiers := map[string]TierState{}
		for k, v := range next.Tiers {
			tiers[k] = v
		}
		tiers[run.Tier] = TierState{Sha: run.Sha, State: run.State, Failed: run.Failed, Failures: run.Failures, Finished: run.Finished, Log: run.Log}
		next.Tiers = tiers
		return next
	}
	next.LastSha, next.Log = run.Sha, run.Log
	switch run.State {
	case Green:
		next.State, next.GreenSha, next.GreenAt = Green, run.Sha, run.Finished
		next.RedSince, next.Failed, next.Failures, next.Error = "", "", nil, ""
		next.Merged, next.Blamed, next.RedIssue, next.LogTail = nil, nil, "", nil
	case Red:
		if !prev.IsRed() {
			next.RedSince = run.Sha
		}
		next.State, next.Failed, next.Failures, next.Error = Red, run.Failed, run.Failures, ""
		next.Merged, next.Blamed, next.LogTail = run.Merged, Slugs(run.Merged), run.LogTail
	default:
		next.State, next.Error, next.LogTail = Error, run.Error, run.LogTail
		next.RedSince, next.Failed, next.Failures, next.Merged, next.Blamed = "", "", nil, nil, nil
	}
	return next
}

// readFrom returns the log from offset on, at most the last 4 MB of it.
func readFrom(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const max = 4 << 20
	if info, err := f.Stat(); err == nil && info.Size()-offset > max {
		offset = info.Size() - max
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, _ := io.ReadAll(f)
	return string(data)
}

// tail returns the last n lines of the file (read from its last 64 KB).
func tail(path string, n int) []string {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	var offset int64
	if info.Size() > 64<<10 {
		offset = info.Size() - 64<<10
	}
	text := readFrom(path, offset)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if offset > 0 && len(lines) > 1 {
		lines = lines[1:] // the first line is cut
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
