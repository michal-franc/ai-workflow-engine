package validations

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func init() { register("command_succeeds", CommandSucceeds) }

const (
	defaultCommandTimeout = 10 * time.Second
	commandOutputTrunc    = 400
)

// CommandSucceeds runs a shell command via /bin/sh -c and passes when the
// exit code is 0. The command is templated with {{slug}}, {{number}},
// {{repo}}, {{system}} from the issue's frontmatter. Working directory is
// cfg.IssuesRoot. Environment is scrubbed to PATH/HOME/GH_TOKEN.
//
// This is the only validator that runs arbitrary shell, so it is gated by
// cfg.AllowShell — when false, the validator fails with a fix-it hint.
func CommandSucceeds(action Action, issue *IssueView, cfg Config) error {
	if !cfg.AllowShell {
		return fail(action, issue,
			"command_succeeds is opt-in, but allow_shell is false",
			"enable it: set 'allow_shell: true' at the top of workflow.yaml")
	}
	cmd := strings.TrimSpace(action.Command)
	if cmd == "" {
		return fmt.Errorf("command_succeeds requires 'command'")
	}
	rendered, err := renderTemplate(cmd, issue)
	if err != nil {
		return fmt.Errorf("command_succeeds: template error: %w", err)
	}

	timeout := defaultCommandTimeout
	if action.TimeoutSeconds > 0 {
		timeout = time.Duration(action.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "/bin/sh", "-c", rendered)
	c.Env = scrubbedEnv()
	if cfg.IssuesRoot != "" {
		c.Dir = cfg.IssuesRoot
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	runErr := c.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return fail(action, issue,
			fmt.Sprintf("shell check timed out after %s: %q", timeout, rendered),
			"increase timeout_seconds or fix the slow command")
	}
	if runErr != nil {
		exitCode := -1
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		return fail(action, issue,
			fmt.Sprintf("shell check failed (exit %d): %q\n  stdout=%q\n  stderr=%q",
				exitCode, rendered,
				truncate(stdout.String(), commandOutputTrunc),
				truncate(stderr.String(), commandOutputTrunc)),
			"fix the underlying state or the command itself")
	}
	return nil
}
