package agentrun

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// RunGitWorktreeAdd is the seam for tests to stub the actual git invocation.
// Passes --no-checkout so the caller can configure sparse-checkout before the
// working tree is populated; when sparse-checkout is skipped, a follow-up
// checkout step materializes the full tree. A branch that already exists is
// checked out as is; a new one is created from startPoint ("" = HEAD).
var RunGitWorktreeAdd = func(workdir, branch, wtPath, startPoint string) ([]byte, error) {
	if exec.Command("git", "-C", workdir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil {
		return exec.Command("git", "-C", workdir, "worktree", "add", "--no-checkout", wtPath, branch).CombinedOutput()
	}
	args := []string{"-C", workdir, "worktree", "add", "--no-checkout", "-b", branch, wtPath}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	return exec.Command("git", args...).CombinedOutput()
}

// RunGitSparseCheckout configures sparse-checkout in the new worktree to
// exclude the given paths. Patterns are built from the excludes ('/*' to
// include everything, then '!/<path>' per exclude, anchored at the repo root) and passed to
// `sparse-checkout set --no-cone`, which writes the pattern file. Over a
// --no-checkout tree this does NOT materialize files — the caller must run a
// follow-up `git checkout HEAD` to lay down the included paths. Non-cone mode
// is used because we express exclusions rather than inclusions; cone mode is
// include-only.
var RunGitSparseCheckout = func(wtPath string, excludes []string) ([]byte, error) {
	args := []string{"-C", wtPath, "sparse-checkout", "set", "--no-cone", "/*"}
	for _, p := range excludes {
		// Anchored at the repo root: an unanchored "!issues/" also hides
		// nested folders such as demo/issues/.
		args = append(args, "!/"+strings.TrimPrefix(p, "/"))
	}
	return exec.Command("git", args...).CombinedOutput()
}

// RunGitCheckoutHead materializes the working tree after a --no-checkout
// worktree add. It always runs (with or without sparse-checkout configured):
// `git worktree add --no-checkout` leaves the tree empty, and the checkout
// honors any sparse-checkout patterns already written. Seam mirrors the others.
var RunGitCheckoutHead = func(wtPath string) ([]byte, error) {
	return exec.Command("git", "-C", wtPath, "checkout", "HEAD").CombinedOutput()
}

// RunWorktreeSetup is the seam for tests to stub the post-create setup shell
// command. Real implementation runs the user-supplied command via `sh -c` with
// cwd set to the new worktree so relative paths in the command resolve there.
var RunWorktreeSetup = func(wtPath, cmd string) ([]byte, error) {
	c := exec.Command("sh", "-c", cmd)
	c.Dir = wtPath
	return c.CombinedOutput()
}

// EnsureWorktree creates the worktree wt if needed and returns the effective
// working directory for the session. A zero wt (no path) means "no worktree":
// the caller gets ok=true with no steps and the original workdir. Returns
// ok=false only when creation (or post-create setup) fails. Setup runs only
// on fresh creation, never on reuse: re-running an arbitrary user command on
// every redispatch would be a footgun.
func EnsureWorktree(workdir string, wt tracker.WorktreeSpec, wf *tracker.WorkflowConfig) (string, []Step, bool) {
	if wt.Path == "" {
		return workdir, nil, true
	}
	wtPath, branch := wt.Path, wt.Branch
	if _, err := os.Stat(wtPath); err == nil {
		return wtPath, []Step{{Name: fmt.Sprintf("Reuse worktree %s on %s", wtPath, branch), Status: "ok"}}, true
	}
	if out, err := RunGitWorktreeAdd(workdir, branch, wtPath, wt.StartPoint); err != nil {
		return workdir, []Step{{
			Name:   fmt.Sprintf("git worktree add %s on %s", wtPath, branch),
			Status: "error",
			Detail: outputOrErr(out, err),
		}}, false
	}
	created := fmt.Sprintf("Created worktree %s on %s", wtPath, branch)
	if wt.StartPoint != "" {
		created += " from " + wt.StartPoint
	}
	steps := []Step{{Name: created, Status: "ok"}}

	// Populate the working tree. `git worktree add --no-checkout` leaves the
	// tree empty with a populated index, so the tree must be materialized
	// explicitly. With excludes, lay down the sparse-checkout pattern file
	// first so the subsequent checkout honors it. `sparse-checkout set` only
	// writes the pattern file and reconciles an *already-materialized* tree —
	// over a --no-checkout tree it writes the pattern and leaves the tree
	// empty, which reads as every file staged-for-deletion. So a separate
	// `git checkout HEAD` always runs afterward to write the included files.
	excludes := wf.WorktreeSparseExcludes()
	if len(excludes) > 0 {
		name := fmt.Sprintf("Sparse-checkout excludes: %s", strings.Join(excludes, ", "))
		if out, err := RunGitSparseCheckout(wtPath, excludes); err != nil {
			steps = append(steps, Step{Name: name, Status: "error", Detail: outputOrErr(out, err)})
			return workdir, steps, false
		}
		steps = append(steps, Step{Name: name, Status: "ok"})
	}
	if out, err := RunGitCheckoutHead(wtPath); err != nil {
		steps = append(steps, Step{Name: "git checkout HEAD", Status: "error", Detail: outputOrErr(out, err)})
		return workdir, steps, false
	}

	if setup := wf.WorktreeSetupCmd(); setup != "" {
		name := fmt.Sprintf("Worktree setup: %s", setup)
		if out, err := RunWorktreeSetup(wtPath, setup); err != nil {
			steps = append(steps, Step{Name: name, Status: "error", Detail: outputOrErr(out, err)})
			return workdir, steps, false
		}
		steps = append(steps, Step{Name: name, Status: "ok"})
	}
	return wtPath, steps, true
}

func outputOrErr(out []byte, err error) string {
	if detail := strings.TrimSpace(string(out)); detail != "" {
		return detail
	}
	return err.Error()
}
