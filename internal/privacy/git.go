// Package privacy keeps a project's work files and development history
// private: the tracker repo (its git folder outside the checkout, synced by
// `issue-cli tracker sync` and the viewer's watcher) and the state the
// Status page and `issue-cli privacy check` show. See docs/workflow.md,
// "Private work and code".
package privacy

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds every git call; a push or pull over the network is the
// slow one.
var gitTimeout = 2 * time.Minute

// git runs git in dir with extra leading args (e.g. --git-dir) and returns
// trimmed stdout.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), lastLines(msg, 3))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// lastLines keeps the last n lines of git's error output.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

// lines splits git output into non-empty lines.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
