package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const pagesWorkflow = `statuses:
  - name: "idea"
  - name: "in design"
  - name: "done"
workbench:
  status_page: docs/status/index.html
  pages:
    - title: "Balance report"
      path: docs/reports/balance.html
`

// setupPagesProject is a project with a status page (plus a sibling CSS
// file), one pages: entry, a JSON file outside the page folders and a
// symlink that leaves the root. workflow is the workflow.yaml text.
func setupPagesProject(t *testing.T, workflow string) (tracker.Project, string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("workflow.yaml", workflow)
	write("issues/UI/one.md", "---\ntitle: \"One\"\nstatus: \"in design\"\n---\n\nBody.\n")
	write("issues/UI/two.md", "---\ntitle: \"Two\"\nstatus: \"done\"\n---\n\nBody.\n")
	write("docs/status/index.html", "<!DOCTYPE html><h1>Atlas status</h1><script src=\"app.js\"></script>")
	write("docs/status/style.css", "body { color: red; }")
	write("docs/status/notes.txt", "not an allowed type")
	write("docs/reports/balance.html", "<h1>Balance</h1>")
	write("docs/private/notes.json", `{"secret": true}`)
	outside := filepath.Join(t.TempDir(), "outside.html")
	os.WriteFile(outside, []byte("<h1>outside</h1>"), 0644)
	if err := os.Symlink(outside, filepath.Join(root, "docs/status/escape.html")); err != nil {
		t.Fatal(err)
	}
	proj := tracker.Project{Name: "Atlas", Slug: "atlas", IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml")}
	return proj, root
}

func pageResp(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestProjectPages_NoConfigLeavesStatusUnchanged(t *testing.T) {
	proj, _ := setupPagesProject(t, "statuses:\n  - name: \"idea\"\n  - name: \"in design\"\n  - name: \"done\"\n")
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	html := pageGet(t, ts.URL+"/p/atlas/status")
	if !strings.Contains(html, `data-testid="status.milestone"`) {
		t.Fatal("built-in status page did not render")
	}
	for _, id := range []string{"status.tabs", "page.frame", "nav.page."} {
		if strings.Contains(html, `data-testid="`+id) {
			t.Errorf("/status without status_page renders %s", id)
		}
	}
	var hash map[string]string
	json.Unmarshal([]byte(pageGet(t, ts.URL+"/p/atlas/hash")), &hash)
	if _, ok := hash["pages"]; ok || hash["hash"] == "" {
		t.Errorf("/hash without pages = %v, want only hash", hash)
	}
	if resp, _ := pageResp(t, mustReq(t, "GET", ts.URL+"/p/atlas/page-raw/docs/status/index.html")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("raw page without config = %d, want 404", resp.StatusCode)
	}
	if resp, _ := pageResp(t, mustReq(t, "GET", ts.URL+"/p/atlas/pages/0")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/pages/0 without config = %d, want 404", resp.StatusCode)
	}
}

func TestProjectPages_StatusShowsProjectPageWithTabs(t *testing.T) {
	proj, _ := setupPagesProject(t, pagesWorkflow)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	html := pageGet(t, ts.URL+"/p/atlas/status")
	for _, want := range []string{
		`data-testid="page.frame"`,
		`sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation"`,
		`src="/p/atlas/page-raw/docs/status/index.html"`,
		`class="status-tab active" data-testid="status.tab.project"`,
		`href="/p/atlas/status?view=builtin"`,
		`data-testid="nav.page.0"`,
		`>Balance report</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("/status missing %s", want)
		}
	}
	if strings.Contains(html, "allow-same-origin") {
		t.Error("iframe sandbox must not allow same origin")
	}
	if strings.Contains(html, `data-testid="status.milestone"`) {
		t.Error("/status with status_page rendered the built-in page")
	}

	builtin := pageGet(t, ts.URL+"/p/atlas/status?view=builtin")
	for _, want := range []string{`data-testid="status.milestone"`, `class="status-tab active" data-testid="status.tab.builtin"`} {
		if !strings.Contains(builtin, want) {
			t.Errorf("/status?view=builtin missing %s", want)
		}
	}
	if md := pageGet(t, ts.URL+"/p/atlas/status.md"); !strings.HasPrefix(md, "#") || strings.Contains(md, "<iframe") {
		t.Errorf("/status.md is not the built-in markdown: %.80q", md)
	}
}

func TestProjectPages_PagesEntry(t *testing.T) {
	proj, _ := setupPagesProject(t, pagesWorkflow)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	html := pageGet(t, ts.URL+"/p/atlas/pages/0")
	for _, want := range []string{
		`<h1 class="page-title">Balance report</h1>`,
		`src="/p/atlas/page-raw/docs/reports/balance.html"`,
		`class="nav-sub active" data-testid="nav.page.0"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("/pages/0 missing %s", want)
		}
	}
	if strings.Contains(html, `data-testid="status.tabs"`) {
		t.Error("a pages: entry shows the status tabs")
	}
	if strings.Contains(html, `class="nav-item active" data-testid="nav.tab.status"`) {
		t.Error("Status is highlighted on a pages: entry")
	}
	for _, bad := range []string{"/p/atlas/pages/1", "/p/atlas/pages/-1", "/p/atlas/pages/x"} {
		if resp, _ := pageResp(t, mustReq(t, "GET", ts.URL+bad)); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", bad, resp.StatusCode)
		}
	}
	// The entry shows on other views too.
	if board := pageGet(t, ts.URL+"/p/atlas/workboard"); !strings.Contains(board, `data-testid="nav.page.0"`) {
		t.Error("workboard sidebar lacks the pages: entry")
	}
}

func TestProjectPages_RawRoute(t *testing.T) {
	proj, _ := setupPagesProject(t, pagesWorkflow)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	base := ts.URL + "/p/atlas/page-raw/"

	resp, body := pageResp(t, mustReq(t, "GET", base+"docs/status/index.html"))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Atlas status") {
		t.Fatalf("raw page = %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.HasPrefix(csp, "sandbox allow-scripts allow-popups") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("ACAO set without Origin: null")
	}

	resp, _ = pageResp(t, mustReq(t, "GET", base+"docs/status/style.css"))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("sibling css = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	req := mustReq(t, "GET", base+"docs/status/style.css")
	req.Header.Set("Origin", "null")
	if resp, _ := pageResp(t, req); resp.Header.Get("Access-Control-Allow-Origin") != "null" {
		t.Errorf("ACAO for the sandboxed page = %q, want null", resp.Header.Get("Access-Control-Allow-Origin"))
	}

	for _, bad := range []string{
		"docs/private/notes.json", // inside the root, outside the page folders
		"docs/status/notes.txt",   // not an allowed type
		"docs/status/escape.html", // symlink out of the root
		"docs/status/",            // no listing
		"docs/status",
		"../workflow.yaml",
		"..%2F..%2Fworkflow.yaml",
		"docs/status/../../workflow.yaml",
		"workflow.yaml",
		"docs/status/missing.html",
	} {
		if resp, _ := pageResp(t, mustReq(t, "GET", base+bad)); resp.StatusCode != http.StatusNotFound {
			t.Errorf("raw %s = %d, want 404", bad, resp.StatusCode)
		}
	}
	if resp, _ := pageResp(t, mustReq(t, "POST", base+"docs/status/index.html")); resp.StatusCode == http.StatusOK {
		t.Error("POST to the raw route succeeded")
	}
}

func TestProjectPages_OriginNullPostRefused(t *testing.T) {
	proj, _ := setupPagesProject(t, pagesWorkflow)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	req := mustReq(t, "POST", ts.URL+"/p/atlas/issue/ui/one/dispatch")
	req.Header.Set("Origin", "null")
	if resp, _ := pageResp(t, req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST with Origin: null = %d, want 403", resp.StatusCode)
	}
	req = mustReq(t, "POST", ts.URL+"/p/atlas/issue/ui/one/comments")
	req.Header.Set("Origin", "null")
	if resp, _ := pageResp(t, req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("comment POST with Origin: null = %d, want 403", resp.StatusCode)
	}
	// Reads stay open, and a same-origin POST is not affected by the guard.
	req = mustReq(t, "GET", ts.URL+"/p/atlas/hash")
	req.Header.Set("Origin", "null")
	if resp, _ := pageResp(t, req); resp.StatusCode != http.StatusOK {
		t.Errorf("GET with Origin: null = %d, want 200", resp.StatusCode)
	}
	req = mustReq(t, "POST", ts.URL+"/p/atlas/issue/ui/one/comments")
	req.Header.Set("Origin", ts.URL)
	if resp, _ := pageResp(t, req); resp.StatusCode == http.StatusForbidden {
		t.Error("same-origin POST was refused")
	}
}

func TestProjectPages_MissingAndBadPaths(t *testing.T) {
	proj, root := setupPagesProject(t, pagesWorkflow)
	os.Remove(filepath.Join(root, "docs/status/index.html"))
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	html := pageGet(t, ts.URL+"/p/atlas/status")
	for _, want := range []string{`data-testid="page.missing"`, "The project status page is missing", "docs/status/index.html", `data-testid="page.missing.builtin_link"`, `data-testid="status.tabs"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing status page lacks %s", want)
		}
	}
	if strings.Contains(html, "<iframe") {
		t.Error("missing page still renders the iframe")
	}

	os.WriteFile(filepath.Join(root, "workflow.yaml"), []byte(strings.Replace(pagesWorkflow, "docs/reports/balance.html", "../shared/balance.html", 1)), 0644)
	html = pageGet(t, ts.URL+"/p/atlas/pages/0")
	if !strings.Contains(html, `data-testid="page.missing"`) || !strings.Contains(html, "points outside the project root") {
		t.Errorf("escaping page path not reported:\n%s", html)
	}
	if strings.Contains(html, `data-testid="page.missing.builtin_link"`) {
		t.Error("a pages: entry offers the built-in status link")
	}
}

func TestProjectPages_HashFollowsPageFiles(t *testing.T) {
	proj, root := setupPagesProject(t, pagesWorkflow)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	read := func() map[string]string {
		var out map[string]string
		json.Unmarshal([]byte(pageGet(t, ts.URL+"/p/atlas/hash")), &out)
		return out
	}
	before := read()
	if before["pages"] == "" {
		t.Fatal("/hash has no pages value")
	}
	if html := pageGet(t, ts.URL+"/p/atlas/status"); !strings.Contains(html, before["pages"]) {
		t.Error("the frame does not start from the current pages hash")
	}
	later := time.Now().Add(2 * time.Second)
	css := filepath.Join(root, "docs/status/style.css")
	os.WriteFile(css, []byte("body { color: blue; }"), 0644)
	os.Chtimes(css, later, later)
	after := read()
	if after["pages"] == before["pages"] {
		t.Error("pages hash unchanged after an asset was rewritten")
	}
	if after["hash"] != before["hash"] {
		t.Error("issue hash changed when only a page file did")
	}
	// Files outside the page folders don't count.
	notes := filepath.Join(root, "docs/private/notes.json")
	os.Chtimes(notes, later.Add(time.Second), later.Add(time.Second))
	if read()["pages"] != after["pages"] {
		t.Error("pages hash follows a file outside the page folders")
	}
}

func TestProjectPages_StatusJSON(t *testing.T) {
	proj, _ := setupPagesProject(t, pagesWorkflow)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	var got map[string]any
	if err := json.Unmarshal([]byte(pageGet(t, ts.URL+"/p/atlas/status.json")), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"project", "as_of", "open", "all", "versions", "epics", "active", "waiting", "shipped"} {
		if _, ok := got[key]; !ok {
			t.Errorf("status.json lacks %q", key)
		}
	}
	if got["project"] != "Atlas" || got["open"] != float64(1) {
		t.Errorf("status.json project/open = %v/%v, want Atlas/1", got["project"], got["open"])
	}
	all, _ := got["all"].(map[string]any)
	if all["done"] != float64(1) || all["total"] != float64(2) {
		t.Errorf("status.json all = %v", all)
	}
}

func mustReq(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
