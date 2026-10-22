package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func init() {
	ciSpawn = func(*tracker.Project) error { panic("test started a real CI run; stub ciSpawn") }
}

// ciRepo is a throwaway git repo on main holding a workflow.yaml with the
// given ci: block (none when ""), an issues/ dir and a project pointing at it.
func ciRepo(t *testing.T, slug, ciBlock string) (tracker.Project, string) {
	t.Helper()
	root := t.TempDir()
	runGitT(t, root, "init", "-q", "-b", "main")
	runGitT(t, root, "config", "user.email", "ci@test")
	runGitT(t, root, "config", "user.name", "ci")
	runGitT(t, root, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(root, "issues"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := "statuses:\n  - name: \"idea\"\n  - name: \"done\"\ntransitions: []\n" + ciBlock
	if err := os.WriteFile(filepath.Join(root, "workflow.yaml"), []byte(wf), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, root, "add", "workflow.yaml")
	runGitT(t, root, "commit", "-q", "-m", "initial")
	return tracker.Project{Name: slug, Slug: slug, IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root}, root
}

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCIWatcherSpawnsOncePerUntestedHead(t *testing.T) {
	on, root := ciRepo(t, "on", "ci:\n  run:\n    - make check\n")
	off, _ := ciRepo(t, "off", "")
	// A second project on the same repo must not start a second run.
	twin := on
	twin.Slug = "twin"

	var spawned []string
	orig := ciSpawn
	defer func() { ciSpawn = orig }()
	ciSpawn = func(p *tracker.Project) error { spawned = append(spawned, p.Slug); return nil }

	w := newCIWatcher()
	projects := []tracker.Project{off, on, twin}
	w.check(projects)
	if strings.Join(spawned, ",") != "on" {
		t.Fatalf("spawned = %v, want [on]", spawned)
	}
	w.check(projects)
	if len(spawned) != 1 {
		t.Errorf("same head spawned again: %v", spawned)
	}
	runGitT(t, root, "commit", "-q", "--allow-empty", "-m", "merge lands")
	w.check(projects)
	if len(spawned) != 2 {
		t.Errorf("new head did not spawn: %v", spawned)
	}
}

func TestCIWatcherLogsBranchErrorsOnce(t *testing.T) {
	proj, _ := ciRepo(t, "p", "ci:\n  branch: nope\n  run:\n    - make check\n")
	orig := ciSpawn
	defer func() { ciSpawn = orig }()
	ciSpawn = func(*tracker.Project) error { t.Error("spawned for a missing branch"); return nil }
	w := newCIWatcher()
	w.check([]tracker.Project{proj})
	w.check([]tracker.Project{proj})
	if len(w.lastErr) != 1 {
		t.Errorf("lastErr = %v", w.lastErr)
	}
}
