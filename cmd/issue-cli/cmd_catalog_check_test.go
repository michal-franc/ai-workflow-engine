package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

// catalogRepo makes a throwaway git repo shaped like issue-viewer: a catalog
// with one entry, a changelog with a release, and the entry's source file,
// committed on main.
func catalogRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(workflowupgrade.CatalogPath, "entries:\n  - id: health-tuning\n    version: v0.53.0\n    sources: [\"internal/health/**\"]\n")
	write(workflowupgrade.ChangelogPath, "## Unreleased\n\n## v0.53.0 — 2026-10-23\n")
	write("internal/health/score.go", "package health\n")
	catalogRepoGit(t, dir, "init", "-q", "-b", "main")
	catalogRepoGit(t, dir, "add", "-A")
	catalogRepoGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func catalogRepoGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCatalogCheckOnABranch(t *testing.T) {
	dir := catalogRepo(t)
	chdir(t, dir)
	proj := &tracker.Project{Name: "t", IssueDir: dir}

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"catalog-check"}); err != nil {
		t.Fatalf("on main: %v", err)
	}
	assertContains(t, stdout.String(), "skipped: not a work branch")

	catalogRepoGit(t, dir, "checkout", "-q", "-b", "work/x")
	if err := os.WriteFile(filepath.Join(dir, "internal/health/score.go"), []byte("package health\n\nconst x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogRepoGit(t, dir, "commit", "-q", "-am", "change health")
	ctx, stdout, _ = newTestContext(proj, false)
	err := runCatalogCheck(ctx, nil)
	if err == nil {
		t.Fatalf("stale entry passed:\n%s", stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"✗ catalog: this branch changes internal/health/score.go", "entry health-tuning is v0.53.0; the next release is v0.54.0", "set version: v0.54.0", "Catalog: n/a health-tuning <why>"} {
		assertContains(t, out, want)
	}

	catalogRepoGit(t, dir, "commit", "-q", "--allow-empty", "-m", "note\n\nCatalog: n/a health-tuning only a constant")
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runCatalogCheck(ctx, nil); err != nil {
		t.Fatalf("n/a trailer: %v\n%s", err, stdout.String())
	}
	assertContains(t, stdout.String(), "health-tuning marked n/a: only a constant")

	ctx, stdout, _ = newTestContext(proj, true)
	if err := runCatalogCheck(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"state": "n/a"`) {
		t.Errorf("JSON: %s", stdout.String())
	}
}

func TestCatalogCheckOutsideTheRepo(t *testing.T) {
	dir := t.TempDir()
	catalogRepoGit(t, dir, "init", "-q")
	chdir(t, dir)
	ctx, _, _ := newTestContext(&tracker.Project{Name: "t", IssueDir: dir}, false)
	err := runCatalogCheck(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "issue-viewer repository") {
		t.Fatalf("err = %v", err)
	}
}
