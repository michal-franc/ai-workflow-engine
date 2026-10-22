package agentrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// gitRepo makes a committed repo holding a root issues/ tree, a nested
// demo/issues/ tree and a source file. Git runs without the user's config.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	repo := t.TempDir()
	for _, f := range []string{"issues/API/a.md", "demo/issues/b.md", "main.go"} {
		p := filepath.Join(repo, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// A dispatched worktree, made with real git, hides the root issues/ tree and
// keeps any nested folder called issues, such as demo/issues/ (TestDemoConfig
// needs it, so make validate fails without it). Each exclude spelling must
// stay anchored at the repo root.
func TestEnsureWorktree_SparseExcludeKeepsNestedIssues(t *testing.T) {
	for _, exclude := range []string{"issues/", "/issues/"} {
		t.Run(exclude, func(t *testing.T) {
			repo := gitRepo(t)
			wtPath := filepath.Join(t.TempDir(), "wt")
			wf := &tracker.WorkflowConfig{WorktreeSparseExclude: &[]string{exclude}}

			dir, steps, ok := EnsureWorktree(repo, tracker.WorktreeSpec{Path: wtPath, Branch: "work/x"}, wf)
			if !ok || dir != wtPath {
				t.Fatalf("EnsureWorktree = %q, ok=%v, steps=%+v", dir, ok, steps)
			}
			for _, keep := range []string{"main.go", "demo/issues/b.md"} {
				if _, err := os.Stat(filepath.Join(wtPath, keep)); err != nil {
					t.Errorf("%s missing from the worktree: %v", keep, err)
				}
			}
			if _, err := os.Stat(filepath.Join(wtPath, "issues")); !os.IsNotExist(err) {
				t.Errorf("root issues/ is in the worktree (stat err = %v), want it excluded", err)
			}
		})
	}
}
