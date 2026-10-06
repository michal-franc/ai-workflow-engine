package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// TestDemoConfig guards `make demo`, the README's "just looking?" path: the
// config it loads must be committed, point at real directories and its own
// workflow, and serve the board.
func TestDemoConfig(t *testing.T) {
	const config = "demo/projects.yaml"

	// A bare `projects.yaml` rule in .gitignore once hid this file, so a
	// fresh clone had no demo config.
	if _, err := exec.LookPath("git"); err == nil {
		if err := exec.Command("git", "check-ignore", "-q", "--no-index", config).Run(); err == nil {
			t.Fatalf("%s is gitignored; a fresh clone would not have it", config)
		}
	}

	projects, err := tracker.LoadProjects(config)
	if err != nil {
		t.Fatalf("LoadProjects: %v", err)
	}
	if len(projects) == 0 {
		t.Fatalf("%s defines no projects", config)
	}

	for _, p := range projects {
		for _, dir := range []string{p.IssueDir, p.DocsDir, p.WorkDir} {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Errorf("project %s: %q is not a directory", p.Slug, dir)
			}
		}
		if p.WorkflowFile == "" {
			t.Errorf("project %s: no workflow set; the board would fall back to the repo's own workflow.yaml", p.Slug)
		} else if _, err := tracker.LoadWorkflow(p.WorkflowFile); err != nil {
			t.Errorf("project %s: workflow %s: %v", p.Slug, p.WorkflowFile, err)
		}
		if p.Terminal != "none" {
			t.Errorf("project %s: terminal is %q; the demo should not assume a desktop (use \"none\")", p.Slug, p.Terminal)
		}
		issues, err := tracker.LoadIssues(p.IssueDir)
		if err != nil {
			t.Errorf("project %s: LoadIssues: %v", p.Slug, err)
		} else if len(issues) == 0 {
			t.Errorf("project %s: no sample issues in %s", p.Slug, p.IssueDir)
		}
	}

	srv, err := NewServer(projects)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	for _, path := range []string{"/p/" + projects[0].Slug + "/", "/p/" + projects[0].Slug + "/board"} {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d", path, rec.Code)
		}
	}
}
