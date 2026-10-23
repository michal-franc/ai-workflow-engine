package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// listAllProject has version 0.2 set, with issues in 0.2, 0.1 and none.
func listAllProject(t *testing.T) *tracker.Project {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "issues")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, it := range []struct{ name, status, version string }{
		{"current-open", "idea", "0.2"},
		{"current-done", "done", "0.2"},
		{"old-done", "done", "0.1"},
		{"unversioned", "idea", ""},
	} {
		v := ""
		if it.version != "" {
			v = fmt.Sprintf("version: %q\n", it.version)
		}
		mustWrite(t, filepath.Join(dir, it.name+".md"), fmt.Sprintf("---\ntitle: %q\nstatus: %q\n%s---\n", it.name, it.status, v))
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: dir, Version: "0.2"}
}

func TestListDefaultVersionAndAll(t *testing.T) {
	proj := listAllProject(t)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runList(ctx, nil); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "2 issues (version 0.2; 2 more in other versions: issue-cli list --all)")
	if strings.Contains(out, "old-done") {
		t.Errorf("default list should hide other versions:\n%s", out)
	}

	stdout.Reset()
	if err := runList(ctx, []string{"--all"}); err != nil {
		t.Fatal(err)
	}
	out = stdout.String()
	for _, slug := range []string{"current-open", "current-done", "old-done", "unversioned"} {
		assertContains(t, out, slug)
	}
	assertContains(t, out, "\n4 issues\n")

	stdout.Reset()
	if err := runList(ctx, []string{"--all", "--status", "done"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "\n2 issues\n")

	// The footer counts only issues the version alone hid.
	stdout.Reset()
	if err := runList(ctx, []string{"--status", "done"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "1 issues (version 0.2; 1 more in other versions")

	// An explicit --version is the caller's filter: no hint.
	stdout.Reset()
	if err := runList(ctx, []string{"--version", "0.1"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "\n1 issues\n")
}
