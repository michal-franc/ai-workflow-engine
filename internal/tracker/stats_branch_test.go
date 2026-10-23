package tracker

import (
	"os"
	"path/filepath"
	"testing"
)

// branchFixture is a repo whose issue cli/x is in shipping, with a
// work/cli/x branch one commit ahead of main.
func branchFixture(t *testing.T) (*gitRepo, *WorkflowConfig, string) {
	t.Helper()
	r := newGitRepo(t)
	r.git("branch", "-M", "main")
	r.git("checkout", "-q", "-b", "work/cli/x")
	r.write("feature.txt", "f\n")
	r.commit("feature")
	r.git("checkout", "-q", "main")

	issues := filepath.Join(t.TempDir(), "CLI")
	if err := os.MkdirAll(issues, 0o755); err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(issues, "x.md")
	if err := os.WriteFile(fp, []byte("---\ntitle: \"x\"\nstatus: \"shipping\"\nsystem: \"CLI\"\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &WorkflowConfig{Statuses: []WorkflowStatus{{Name: "shipping"}, {Name: "done"}}, RepoRoot: r.root, IssuesRoot: filepath.Dir(issues)}
	return r, wf, fp
}

func lastStat(t *testing.T, fp string) TransitionStat {
	t.Helper()
	store, err := LoadStats(fp)
	if err != nil || len(store.Transitions) == 0 {
		t.Fatalf("LoadStats: %v, %d rows", err, len(store.Transitions))
	}
	return store.Transitions[len(store.Transitions)-1]
}

func TestTransitionToDoneRecordsBranchHeadAndMergeSha(t *testing.T) {
	r, wf, fp := branchFixture(t)
	head := r.git("rev-parse", "work/cli/x")
	r.git("merge", "--ff-only", "-q", "work/cli/x")

	if _, _, err := wf.ApplyTransitionToFile(fp, "done"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	got := lastStat(t, fp)
	if got.BranchHead != head || got.MergeSHA != head {
		t.Fatalf("branch_head %q merge_sha %q, want both %q", got.BranchHead, got.MergeSHA, head)
	}
}

func TestTransitionToDoneWithoutMergeRecordsOnlyBranchHead(t *testing.T) {
	r, wf, fp := branchFixture(t)
	head := r.git("rev-parse", "work/cli/x")
	if _, _, err := wf.ApplyTransitionToFile(fp, "done"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if got := lastStat(t, fp); got.BranchHead != head || got.MergeSHA != "" {
		t.Fatalf("branch_head %q merge_sha %q, want %q and none", got.BranchHead, got.MergeSHA, head)
	}
}

func TestTransitionWithoutBranchOrRepoStillSucceeds(t *testing.T) {
	r, wf, fp := branchFixture(t)
	r.git("branch", "-D", "work/cli/x")
	if _, _, err := wf.ApplyTransitionToFile(fp, "done"); err != nil {
		t.Fatalf("transition without branch: %v", err)
	}
	if got := lastStat(t, fp); got.BranchHead != "" || got.MergeSHA != "" {
		t.Fatalf("want no shas, got %+v", got)
	}

	if h, m := branchShas(t.TempDir(), "cli/x", "done"); h != "" || m != "" {
		t.Fatalf("not a repo: %q %q", h, m)
	}
}
