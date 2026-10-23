package timeline

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLoadCommitsFirstParent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "base")
	run("checkout", "-q", "-b", "work/ui/x")
	run("commit", "-q", "--allow-empty", "-m", "mine 1")
	run("checkout", "-q", "main")
	run("commit", "-q", "--allow-empty", "-m", "other issue")
	run("checkout", "-q", "work/ui/x")
	run("merge", "-q", "--no-edit", "main")
	run("commit", "-q", "--allow-empty", "-m", "mine 2")

	ref, name := GitRef(dir, "ui/x", nil)
	if ref == "" || name != "work/ui/x" {
		t.Fatalf("ref = %q %q", ref, name)
	}
	commits, err := LoadCommits(dir, ref, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range commits {
		s := c.Subject
		if c.Merge {
			s = "merge"
		}
		subjects = append(subjects, s)
	}
	if got := strings.Join(subjects, ","); got != "base,mine 1,merge,mine 2" {
		t.Errorf("commits = %s; the merge is one line and the other issue's commit never shows", got)
	}
	if ref, _ := GitRef(dir, "ui/gone", nil); ref != "" {
		t.Errorf("a missing branch with no recorded head = %q", ref)
	}
}
