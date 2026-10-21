package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleProjectList_RedirectsSingleProject(t *testing.T) {
	proj, _ := setupTestProject(t)
	srv, err := NewServer([]tracker.Project{proj})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/p/test-project/" {
		t.Fatalf("expected redirect to /p/test-project/, got %s", loc)
	}
}

func TestHandleProjectList_ListsMultipleProjects(t *testing.T) {
	proj1, _ := setupTestProject(t)
	proj2 := tracker.Project{
		Name:     "Second Project",
		Slug:     "second-project",
		IssueDir: proj1.IssueDir, // reuse dir
		DocsDir:  proj1.DocsDir,
	}

	ts := newTestServer(t, []tracker.Project{proj1, proj2})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleProjectList_404ForNonRootPath(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj, proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleProjectRoutes_404ForUnknownProject(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/unknown-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// /project-for maps a folder to the project whose work directory holds it,
// the longest (most nested) match winning, as for issue-viewer and its demo.
func TestProjectFor(t *testing.T) {
	root := t.TempDir()
	outer := tracker.Project{Slug: "outer", WorkDir: root, IssueDir: filepath.Join(root, "issues")}
	inner := tracker.Project{Slug: "inner", WorkDir: filepath.Join(root, "demo"), IssueDir: filepath.Join(root, "demo", "issues")}
	ts := newTestServer(t, []tracker.Project{outer, inner})
	defer ts.Close()

	for cwd, want := range map[string]string{
		root: "outer",
		filepath.Join(root, ".worktrees", "ui/x"): "outer",
		filepath.Join(root, "demo"):               "inner",
		filepath.Join(root, "demo", "sub"):        "inner",
	} {
		resp, body := getResp(t, ts.URL+"/project-for?cwd="+url.QueryEscape(cwd))
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"project":"`+want+`"`) {
			t.Errorf("cwd %s: %d %s, want %s", cwd, resp.StatusCode, body, want)
		}
	}
	if resp, _ := getResp(t, ts.URL+"/project-for?cwd=/elsewhere"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unrelated cwd: status %d, want 404", resp.StatusCode)
	}
}
