package agentrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// A sub-lead's base branch starts from main even when the project checkout is
// on another branch.
func TestLeadSpec_EpicBaseStartsFromMain(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "start")
	git("checkout", "-q", "-b", "feature")

	epicPath := filepath.Join(root, "issues", "epic.md")
	if err := os.MkdirAll(filepath.Dir(epicPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epicPath, []byte("---\ntitle: \"Epic\"\nstatus: \"in progress\"\ntype: \"epic\"\n---\n\nBody.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	issues, err := tracker.LoadIssues(filepath.Join(root, "issues"))
	if err != nil || len(issues) != 1 {
		t.Fatalf("issues = %v, err = %v", issues, err)
	}
	proj := &tracker.Project{Slug: "p", IssueDir: filepath.Join(root, "issues"), WorkDir: root}

	spec, err := LeadSpec(proj, nil, issues, issues[0], root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Worktree.Branch != "base/epic" || spec.Worktree.StartPoint != "main" {
		t.Fatalf("worktree = %+v, want base/epic from main", spec.Worktree)
	}
}
