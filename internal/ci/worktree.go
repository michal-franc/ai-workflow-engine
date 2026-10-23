package ci

import (
	"fmt"
	"os"
	"path/filepath"
)

// prepareWorktree puts the CI worktree on sha. It is created once and then
// reused: checkout --force plus clean -fd (no -x) resets tracked and
// untracked files but keeps ignored build output, so builds stay incremental.
func prepareWorktree(root, sha string) (string, error) {
	path := WorktreePath(root)
	if !isWorktree(path) {
		if _, statErr := os.Stat(path); statErr == nil {
			return "", fmt.Errorf("%s exists but is not a git worktree; run issue-cli ci reset", path)
		}
		_, _ = git(root, "worktree", "prune")
		if _, err := gitWithin(worktreeTimeout, root, "worktree", "add", "--detach", path, sha); err != nil {
			return "", err
		}
		return path, nil
	}
	if _, err := gitWithin(worktreeTimeout, path, "checkout", "--detach", "--force", sha); err != nil {
		return "", err
	}
	if _, err := git(path, "clean", "-fd"); err != nil {
		return "", err
	}
	return path, nil
}

// isWorktree reports whether path is the top of its own checkout. A plain
// directory inside the repo would resolve to the main checkout instead, and
// a forced checkout there would wipe it.
func isWorktree(path string) bool {
	top, err := git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	want, err1 := filepath.EvalSymlinks(path)
	got, err2 := filepath.EvalSymlinks(top)
	return err1 == nil && err2 == nil && want == got
}

// ResetWorktree removes the CI worktree, ignored build output included; the
// next run creates it again. It refuses while a run is in progress.
func ResetWorktree(root string) error {
	if l, alive := ReadLock(root); alive {
		return fmt.Errorf("%w at %s (pid %d); wait for it or stop it first", ErrBusy, Short(l.Sha), l.PID)
	}
	path := WorktreePath(root)
	_, _ = git(root, "worktree", "remove", "--force", path)
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	_, err := git(root, "worktree", "prune")
	return err
}
