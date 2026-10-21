package main

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// TestProjectPagesRenderSidebar renders every project page and checks it
// carries the shared sidebar with the right entry active. A new page that
// skips {{template "sidebar" ...}} fails here, naming the page.
func TestProjectPagesRenderSidebar(t *testing.T) {
	proj, _ := setupTestProject(t)
	proj.SupportsGitHub = true
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	pages := map[string]string{
		"/":                     "list",
		"/board":                "board",
		"/workboard":            "workboard",
		"/graph":                "graph",
		"/canvas":               "canvas",
		"/agents":               "agents",
		"/docs":                 "docs",
		"/docs/getting-started": "docs",
		"/retros":               "retros",
		"/stats":                "stats",
		"/github":               "github",
		"/decisions":            "decisions",
		"/balance":              "",
		"/issue/bug-in-login":   "",
	}
	for path, active := range pages {
		code, html := getBody(t, ts.URL+"/p/test-project"+path)
		if code != 200 {
			t.Errorf("%s: status %d", path, code)
			continue
		}
		if !strings.Contains(html, `data-testid="nav.sidebar"`) {
			t.Errorf("%s: page has no sidebar; render {{template \"sidebar\" (sidebar .Prefix \"<key>\")}} (templates/_sidebar.html)", path)
			continue
		}
		if n := strings.Count(html, `aria-current="page"`); active == "" && n != 0 || active != "" && n != 1 {
			t.Errorf("%s: %d active nav entries", path, n)
		}
		if active != "" && !strings.Contains(html, `data-testid="nav.tab.`+active+`" aria-current="page"`) {
			t.Errorf("%s: nav.tab.%s is not the active entry", path, active)
		}
		if strings.Contains(html, "ZgotmplZ") {
			t.Errorf("%s: html/template rejected a sidebar value (ZgotmplZ)", path)
		}
	}
}

func TestProjectTemplatesUseSharedSidebar(t *testing.T) {
	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, _ := fs.ReadFile(templateFS, f)
		src := string(b)
		if strings.Contains(src, `class="view-tab`) {
			t.Errorf("%s: copies its own view tabs; use the shared sidebar", f)
		}
		if strings.Contains(src, "{{.Prefix}}") && !strings.HasPrefix(f, "templates/_") && !strings.Contains(src, `{{template "sidebar"`) {
			t.Errorf("%s: project page without the shared sidebar", f)
		}
	}
}

func TestSidebarNav(t *testing.T) {
	proj, _ := setupTestProject(t)
	proj.Version = "0.38.0"
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-bug-in-login"}})
	srv, err := NewServer([]tracker.Project{proj})
	if err != nil {
		t.Fatal(err)
	}

	nav := srv.sidebarNav("/p/test-project", "graph")
	if nav.Version != "v0.38.0" || nav.Open != 2 || nav.ActiveBots != 1 {
		t.Errorf("version/open/bots = %q/%d/%d, want v0.38.0/2/1", nav.Version, nav.Open, nav.ActiveBots)
	}
	keys := func(items []NavItem) string {
		var k []string
		for _, it := range items {
			k = append(k, it.Key)
		}
		return strings.Join(k, ",")
	}
	if got := keys(nav.Main); got != "workboard,list,decisions,docs" {
		t.Errorf("main = %s (Balance needs workbench.balance)", got)
	}
	if got := keys(nav.More); got != "board,graph,canvas,agents,retros,stats" {
		t.Errorf("more = %s (GitHub needs supports_github)", got)
	}
	if !nav.MoreActive {
		t.Error("More should start open on a view inside it")
	}
	if nav.CreateInPage || nav.Systems != nil {
		t.Error("graph has no create modal and no systems panel")
	}
	for _, it := range nav.More {
		if it.Key == "agents" && (it.Note != "1 running" || !it.Live) {
			t.Errorf("agents note = %q live=%v", it.Note, it.Live)
		}
	}

	board := srv.sidebarNav("/p/test-project", "board")
	if !board.MoreActive || !board.CreateInPage || board.Legend {
		t.Error("classic board: under an open More, create modal in page, no legend")
	}
	var systems []string
	for _, s := range board.Systems {
		systems = append(systems, s.Name+"="+s.Href)
	}
	if got := strings.Join(systems, " "); got != "Auth=/p/test-project/board?system=Auth UI=/p/test-project/board?system=UI" {
		t.Errorf("board systems = %s", got)
	}

	wb := srv.sidebarNav("/p/test-project", "workboard")
	if wb.MoreActive || wb.CreateInPage || !wb.Legend {
		t.Error("workboard: More closed, New issue links to the list, card legend shown")
	}
	if len(wb.Systems) == 0 || wb.Systems[0].Href != "/p/test-project/workboard?system=Auth" {
		t.Errorf("workboard systems link the workboard: %+v", wb.Systems)
	}

	if got := srv.sidebarNav("/p/missing", "list"); got.ProjectName != "" || got.Main != nil {
		t.Errorf("unknown project nav = %+v", got)
	}
}
