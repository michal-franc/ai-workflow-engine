package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestRewriteRelativeImages(t *testing.T) {
	root := "/proj"
	mdDir := "/proj/issues/Manager"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"parent relative", `<img src="../../docs/a/preview.png" alt="x">`, `<img src="/p/wm/files/docs/a/preview.png" alt="x">`},
		{"sibling", `<img src="shot.png">`, `<img src="/p/wm/files/issues/Manager/shot.png">`},
		{"dot slash", `<img src="./img/shot.png">`, `<img src="/p/wm/files/issues/Manager/img/shot.png">`},
		{"percent encoded space", `<img src="my%20shot.png">`, `<img src="/p/wm/files/issues/Manager/my%20shot.png">`},
		{"query stripped", `<img src="shot.png?v=2">`, `<img src="/p/wm/files/issues/Manager/shot.png">`},
		{"absolute url untouched", `<img src="https://example.com/a.png">`, `<img src="https://example.com/a.png">`},
		{"root relative untouched", `<img src="/p/wm/attachments/a.png">`, `<img src="/p/wm/attachments/a.png">`},
		{"data uri untouched", `<img src="data:image/png;base64,AAAA">`, `<img src="data:image/png;base64,AAAA">`},
		{"escapes root untouched", `<img src="../../../etc/x.png">`, `<img src="../../../etc/x.png">`},
		{"non-img untouched", `<a href="shot.png">x</a>`, `<a href="shot.png">x</a>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteRelativeImages(tt.in, "/p/wm", mdDir, root); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestProjectFileRoute(t *testing.T) {
	proj, tmpDir := setupTestProject(t)
	os.MkdirAll(filepath.Join(tmpDir, "docs", "art"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "docs", "art", "preview.png"), []byte("\x89PNG\r\n\x1a\nfake"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "secret.txt"), []byte("nope"), 0644)

	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "leak.png"), []byte("leak"), 0644)
	os.Symlink(filepath.Join(outside, "leak.png"), filepath.Join(tmpDir, "link.png"))

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	tests := []struct {
		path string
		code int
	}{
		{"/p/test-project/files/docs/art/preview.png", http.StatusOK},
		{"/p/test-project/files/secret.txt", http.StatusNotFound},
		{"/p/test-project/files/missing.png", http.StatusNotFound},
		{"/p/test-project/files/link.png", http.StatusNotFound},
		{"/p/test-project/files/..%2f..%2fleak.png", http.StatusNotFound},
	}
	for _, tt := range tests {
		resp, err := http.Get(ts.URL + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.code {
			t.Errorf("%s: status %d, want %d", tt.path, resp.StatusCode, tt.code)
		}
	}
}

func TestDetailAndDocsRewriteRelativeImages(t *testing.T) {
	proj, tmpDir := setupTestProject(t)
	os.MkdirAll(filepath.Join(tmpDir, "issues", "Manager"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "issues", "Manager", "road.md"), []byte("---\ntitle: \"Road\"\n---\n\n![map](../../docs/art/preview.png)\n"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "docs", "guide"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "docs", "guide", "intro.md"), []byte("---\ntitle: \"Intro\"\n---\n\n![map](../art/preview.png)\n"), 0644)

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	var slug string
	for _, is := range issues {
		if is.Title == "Road" {
			slug = is.Slug
		}
	}
	docs, _ := tracker.LoadDocs(proj.DocsDir)
	var docSlug string
	for _, d := range docs {
		if d.Title == "Intro" {
			docSlug = d.Slug
		}
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	for _, path := range []string{"/p/test-project/issue/" + slug, "/p/test-project/docs/" + docSlug} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if want := `src="/p/test-project/files/docs/art/preview.png"`; !strings.Contains(string(body), want) {
			t.Errorf("%s: body missing %s", path, want)
		}
	}
}
