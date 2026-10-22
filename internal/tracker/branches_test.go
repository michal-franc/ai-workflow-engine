package tracker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo is a throwaway repository for the branch-scan tests.
type gitRepo struct {
	t    *testing.T
	root string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(k, v)
	}
	r := &gitRepo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.write("a.txt", "a\n")
	r.write("shared.txt", "s\n")
	r.commit("init")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	return r.gitIn(r.root, args...)
}

func (r *gitRepo) gitIn(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) write(path, text string) { r.writeIn(r.root, path, text) }

func (r *gitRepo) writeIn(dir, path, text string) {
	r.t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *gitRepo) commit(msg string) { r.commitIn(r.root, msg) }

func (r *gitRepo) commitIn(dir, msg string) {
	r.t.Helper()
	r.gitIn(dir, "add", "-A")
	r.gitIn(dir, "commit", "-q", "-m", msg)
}

// worktree adds .worktrees/<name> on a new branch from start.
func (r *gitRepo) worktree(branch, start string) string {
	r.t.Helper()
	dir := filepath.Join(r.root, ".worktrees", strings.ReplaceAll(branch, "/", "-"))
	r.git("worktree", "add", "-q", "-b", branch, dir, start)
	return dir
}

func branchNames(bs []OpenBranch) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out
}

func findBranch(bs []OpenBranch, name string) *OpenBranch {
	for i := range bs {
		if bs[i].Name == name {
			return &bs[i]
		}
	}
	return nil
}

func TestOpenBranchesListsWorktreesAndOpenWorkBranches(t *testing.T) {
	r := newGitRepo(t)
	wt := r.worktree("work/ui/one", "main")
	r.writeIn(wt, "a.txt", "a\none\n")
	r.commitIn(wt, "one")

	// Unmerged branch without a worktree, open issue: listed.
	r.git("branch", "work/ui/two", "main")
	r.git("checkout", "-q", "work/ui/two")
	r.write("b.txt", "b\n")
	r.commit("two")
	// Unmerged branch whose issue is done: left out.
	r.git("checkout", "-q", "-b", "work/ui/done", "main")
	r.write("c.txt", "c\n")
	r.commit("done")
	// Unmerged branch with no issue and no worktree: left out.
	r.git("checkout", "-q", "-b", "work/ui/orphan", "main")
	r.write("d.txt", "d\n")
	r.commit("orphan")
	r.git("checkout", "-q", "main")

	issues := []*Issue{
		{Slug: "ui/one", Status: "in progress"},
		{Slug: "ui/two", Status: "backlog"},
		{Slug: "ui/done", Status: "done"},
	}
	got, err := OpenBranchesErr(r.root, "", issues)
	if err != nil {
		t.Fatal(err)
	}
	if names := strings.Join(branchNames(got), ","); names != "work/ui/one,work/ui/two" {
		t.Fatalf("branches = %s", names)
	}
	one := findBranch(got, "work/ui/one")
	if one.Worktree == "" || one.IssueSlug != "ui/one" || one.Base != "main" || one.Ahead != 1 || one.MergeBase == "" {
		t.Errorf("one = %+v", *one)
	}
	if two := findBranch(got, "work/ui/two"); two.Worktree != "" || two.Ahead != 1 {
		t.Errorf("two = %+v", *two)
	}
}

// An old base: on an epic no longer moves its children off main.
func TestOpenBranchesIgnoresEpicBase(t *testing.T) {
	r := newGitRepo(t)
	r.git("branch", "base/epic", "main")
	r.git("checkout", "-q", "base/epic")
	r.write("epic.txt", "e\n")
	r.commit("epic work")
	r.git("checkout", "-q", "main")
	wt := r.worktree("work/ui/child", "base/epic")
	r.writeIn(wt, "child.txt", "c\n")
	r.commitIn(wt, "child")

	issues := []*Issue{
		{Slug: "ui/epic", Status: "in progress", Type: "epic", ExtraFields: []ExtraField{{Key: "base", Value: "base/epic"}}},
		{Slug: "ui/child", Status: "in progress", ExtraFields: []ExtraField{{Key: "parent", Value: "ui/epic"}}},
	}
	got := OpenBranches(r.root, "main", issues)
	child := findBranch(got, "work/ui/child")
	if child == nil || child.Base != "main" || child.Ahead != 2 {
		t.Fatalf("child = %+v (all %v)", child, branchNames(got))
	}
}

func TestHotFilesAndSharedCommits(t *testing.T) {
	r := newGitRepo(t)
	a := r.worktree("work/a", "main")
	r.writeIn(a, "shared.txt", "s\na\n")
	r.commitIn(a, "a edits shared")
	b := r.worktree("work/b", "main")
	r.writeIn(b, "shared.txt", "s\nb\n")
	r.writeIn(b, "base.txt", "b\n")
	r.commitIn(b, "b edits shared")
	// d forks from b, then both go on: they share b's first commit and
	// neither head is an ancestor of the other.
	d := r.worktree("work/d", "work/b")
	r.writeIn(b, "b2.txt", "b2\n")
	r.commitIn(b, "b goes on")
	r.writeIn(d, "own.txt", "d\n")
	r.commitIn(d, "d")

	got := OpenBranches(r.root, "main", nil)
	bb, dd := findBranch(got, "work/b"), findBranch(got, "work/d")
	if bb.SharesWith["work/d"] != 1 || dd.SharesWith["work/b"] != 1 {
		t.Fatalf("shares: b=%v d=%v", bb.SharesWith, dd.SharesWith)
	}
	if len(findBranch(got, "work/a").SharesWith) != 0 {
		t.Errorf("a shares nothing, got %v", findBranch(got, "work/a").SharesWith)
	}

	// b and d are both 2 commits ahead; the tie goes to b by name, so the
	// shared commit is b's and d's copy of it is marked Shared.
	all := HotFiles(r.root, got, false)
	if len(all) != 2 || all[0].Path != "shared.txt" || len(all[0].Changes) != 3 {
		t.Fatalf("hot files = %+v", all)
	}
	for _, ch := range all[0].Changes {
		if want := ch.Branch == "work/d"; ch.Shared != want {
			t.Errorf("%s shared = %v, want %v", ch.Branch, ch.Shared, want)
		}
		if ch.Added != 1 {
			t.Errorf("%s added = %d", ch.Branch, ch.Added)
		}
	}
	if all[1].Path != "base.txt" || all[1].Branches() != 1 {
		t.Errorf("base.txt = %+v", all[1])
	}

	// Hiding shared-commit overlap: shared.txt keeps a and b; base.txt (b's
	// change that d only inherited) drops out.
	hidden := HotFiles(r.root, got, true)
	if len(hidden) != 1 || hidden[0].Path != "shared.txt" || len(hidden[0].Changes) != 2 {
		t.Fatalf("hidden = %+v", hidden)
	}

	changed := ChangedOnBranches(r.root, got, "./shared.txt")
	if len(changed) != 3 {
		t.Errorf("changed on = %+v", changed)
	}
	if none := ChangedOnBranches(r.root, got, "a.txt"); len(none) != 0 {
		t.Errorf("a.txt changed on = %+v", none)
	}
}

func TestOpenBranchesRestacksOnParentBranch(t *testing.T) {
	r := newGitRepo(t)
	b := r.worktree("work/b", "main")
	r.writeIn(b, "b.txt", "b\n")
	r.commitIn(b, "b1")
	r.writeIn(b, "b.txt", "b\nb\n")
	r.commitIn(b, "b2")
	// c is stacked on b's head: compared with b, not main.
	c := r.worktree("work/c", "work/b")
	r.writeIn(c, "c.txt", "c\n")
	r.commitIn(c, "c")

	got := OpenBranches(r.root, "main", nil)
	cc := findBranch(got, "work/c")
	if cc.Base != "work/b" || cc.Ahead != 1 || cc.MergeBase != findBranch(got, "work/b").Head {
		t.Fatalf("c = %+v", *cc)
	}
	if hot := HotFiles(r.root, got, true); len(hot) != 0 {
		t.Errorf("stacked branch inherits no overlap, got %+v", hot)
	}
}

func TestHotFilesIgnoresTheBaseBranch(t *testing.T) {
	r := newGitRepo(t)
	epic := r.worktree("base/epic", "main")
	r.writeIn(epic, "lane.txt", "e\n")
	r.commitIn(epic, "epic")
	child := r.worktree("work/ui/child", "base/epic")
	r.writeIn(child, "lane.txt", "e\nc\n")
	r.commitIn(child, "child")
	issues := []*Issue{
		{Slug: "ui/epic", Status: "in progress", Type: "epic", ExtraFields: []ExtraField{{Key: "base", Value: "base/epic"}}},
		{Slug: "ui/child", Status: "in progress", ExtraFields: []ExtraField{{Key: "parent", Value: "ui/epic"}}},
	}
	got := OpenBranches(r.root, "main", issues)
	if child := findBranch(got, "work/ui/child"); child == nil || child.Base != "base/epic" {
		t.Fatalf("child = %+v", child)
	}
	if hot := HotFiles(r.root, got, true); len(hot) != 0 {
		t.Errorf("a child and its epic base do not overlap, got %+v", hot)
	}
	all := HotFiles(r.root, got, false)
	if len(all) != 1 || all[0].Branches() != 1 {
		t.Errorf("unhidden = %+v", all)
	}
}

func TestOpenBranchesNoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	_, err := OpenBranchesErr(t.TempDir(), "", nil)
	if err != ErrNoGit {
		t.Fatalf("err = %v, want ErrNoGit", err)
	}
}

func TestMainBranchFallsBackToMaster(t *testing.T) {
	r := newGitRepo(t)
	r.git("branch", "-m", "main", "master")
	if got, err := MainBranch(r.root); err != nil || got != "master" {
		t.Fatalf("MainBranch = %q, %v", got, err)
	}
}
