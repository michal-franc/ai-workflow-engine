package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func canvasWorkflow() *tracker.WorkflowConfig {
	return &tracker.WorkflowConfig{Statuses: []tracker.WorkflowStatus{
		{Name: "idea"},
		{Name: "in progress"},
		{Name: "done"},
		{Name: "obsolete", Optional: true},
	}}
}

// canvasIssue parses frontmatter lines into an issue with the given slug.
func canvasIssue(t *testing.T, slug string, fm ...string) *tracker.Issue {
	t.Helper()
	src := "---\ntitle: \"" + slug + "\"\n" + strings.Join(fm, "\n") + "\n---\n\nbody\n"
	issue, err := tracker.ParseIssue(slug+".md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	issue.Slug = slug
	return issue
}

// canvasFixture: epic E (with sub-epic S), umbrella U, loose issues, and a
// child with an unknown parent.
func canvasFixture(t *testing.T) []*tracker.Issue {
	return []*tracker.Issue{
		canvasIssue(t, "e", `type: "epic"`, `status: "in progress"`, `base: "base/e"`, `system: "Core"`),
		canvasIssue(t, "c1", `parent: "e"`, `version: "0.1"`, `status: "done"`, `system: "Core"`),
		canvasIssue(t, "c2", `umbrella: "E"`, `version: "0.2"`, `status: "idea"`, `system: "UI"`),
		canvasIssue(t, "c3", `parent: "e"`, `status: "in progress"`, `system: "UI"`),
		canvasIssue(t, "s", `type: "epic"`, `parent: "e"`, `status: "idea"`),
		canvasIssue(t, "u", `status: "idea"`, `system: "Core"`),
		canvasIssue(t, "u1", `parent: "u"`, `version: "0.10"`, `status: "idea"`, `system: "Core"`),
		canvasIssue(t, "l1", `version: "0.1"`, `status: "obsolete"`, `system: "Core"`),
		canvasIssue(t, "l2", `version: "0.1"`, `status: "idea"`, `system: "Core"`),
		canvasIssue(t, "l3", `status: "idea"`),
		canvasIssue(t, "l4", `parent: "missing"`, `version: "0.2"`, `status: "idea"`, `system: "UI"`),
	}
}

func laneByTitle(t *testing.T, data CanvasData, title string) CanvasLane {
	t.Helper()
	for _, l := range data.Lanes {
		if l.Title == title {
			return l
		}
	}
	t.Fatalf("no lane %q", title)
	return CanvasLane{}
}

func cardSlugs(cell CanvasCell) []string {
	var out []string
	for _, c := range cell.Cards {
		out = append(out, c.Slug)
	}
	return out
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{
		{"0.2", "0.10", true},
		{"0.10", "0.2", false},
		{"1.0", "1.0.1", true},
		{"1.1", "1.10", true},
		{"0.9", "1.0", true},
		{"beta", "rc", true},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b) < 0; got != c.less {
			t.Errorf("compareVersions(%q, %q) < 0 = %v, want %v", c.a, c.b, got, c.less)
		}
	}
}

func TestBuildCanvas_EpicRowsDefaults(t *testing.T) {
	data := buildCanvas(canvasFixture(t), canvasWorkflow(), CanvasOptions{})

	var cols []string
	for _, c := range data.Columns {
		cols = append(cols, c.Name)
	}
	if got := strings.Join(cols, ","); got != "0.1,0.2,0.10" {
		t.Fatalf("columns = %s, want 0.1,0.2,0.10 (no-version hidden)", got)
	}
	if data.HiddenNoVersion != 5 || data.NoVersionForced {
		t.Errorf("HiddenNoVersion=%d NoVersionForced=%v, want 5 false", data.HiddenNoVersion, data.NoVersionForced)
	}
	if data.ShownIssues != 6 || data.TotalIssues != 11 {
		t.Errorf("shown/total = %d/%d, want 6/11", data.ShownIssues, data.TotalIssues)
	}
	if c := data.Columns[0]; c.Progress != "1/2 done" || c.Extra != "3 issues · 1 obsolete" {
		t.Errorf("column 0.1 = %q / %q", c.Progress, c.Extra)
	}

	var order []string
	for _, l := range data.Lanes {
		order = append(order, l.Kind+":"+l.Title)
	}
	if got := strings.Join(order, ","); got != "EPIC:e,EPIC:s,UMBRELLA:u,NO EPIC:No epic" {
		t.Fatalf("lanes = %s", got)
	}

	e := laneByTitle(t, data, "e")
	if e.Depth != 0 {
		t.Errorf("epic e depth=%d", e.Depth)
	}
	if e.Summary != "3 children · 1/3 done · 1 without a version" {
		t.Errorf("epic e summary = %q", e.Summary)
	}
	if len(e.Cells) != 3 || strings.Join(cardSlugs(e.Cells[1]), ",") != "c2" {
		t.Errorf("epic e cells = %+v", e.Cells)
	}

	s := laneByTitle(t, data, "s")
	if s.Depth != 1 || s.ParentTitle != "e" || !strings.HasPrefix(s.Note, "No children yet") {
		t.Errorf("sub-epic s = %+v", s)
	}

	loose := laneByTitle(t, data, "No epic")
	if loose.Summary != "3 shown of 4 · 0/2 done · 1 obsolete" {
		t.Errorf("no-epic summary = %q", loose.Summary)
	}
	// Open cards first, side statuses last.
	if got := strings.Join(cardSlugs(loose.Cells[0]), ","); got != "l2,l1" {
		t.Errorf("no-epic 0.1 cards = %s, want l2,l1", got)
	}
	l4 := loose.Cells[1].Cards[0]
	if l4.Slug != "l4" || l4.UnknownParent != "missing" {
		t.Errorf("unknown parent card = %+v", l4)
	}
}

func TestBuildCanvas_AllChildrenWithoutVersionGetANote(t *testing.T) {
	issues := []*tracker.Issue{
		canvasIssue(t, "e", `type: "epic"`),
		canvasIssue(t, "c", `parent: "e"`),
		canvasIssue(t, "v", `version: "1.0"`),
	}
	data := buildCanvas(issues, canvasWorkflow(), CanvasOptions{})
	e := laneByTitle(t, data, "e")
	if e.Note != "All 1 child have no version." || !e.NoteNoVersion || e.Cells != nil {
		t.Errorf("lane = %+v", e)
	}

	data = buildCanvas(issues, canvasWorkflow(), CanvasOptions{ShowNoVersion: true})
	e = laneByTitle(t, data, "e")
	if e.Note != "" || len(e.Cells) != 2 {
		t.Errorf("with no-version shown, lane = %+v", e)
	}
	if last := data.Columns[len(data.Columns)-1]; last.Name != "no version" {
		t.Errorf("last column = %q", last.Name)
	}
}

func TestBuildCanvas_NoVersionsForcesTheColumn(t *testing.T) {
	issues := []*tracker.Issue{canvasIssue(t, "a"), canvasIssue(t, "b")}
	data := buildCanvas(issues, canvasWorkflow(), CanvasOptions{})
	if !data.NoVersionForced || len(data.Columns) != 1 || data.Columns[0].Name != "no version" {
		t.Errorf("data = %+v", data)
	}
	if data.ShownIssues != 2 {
		t.Errorf("shown = %d", data.ShownIssues)
	}
}

func TestBuildCanvas_CollapseDone(t *testing.T) {
	issues := append(canvasFixture(t), canvasIssue(t, "l5", `version: "0.1"`, `status: "done"`))
	data := buildCanvas(issues, canvasWorkflow(), CanvasOptions{CollapseDone: true})
	cell := laneByTitle(t, data, "No epic").Cells[0]
	if got := strings.Join(cardSlugs(cell), ","); got != "l2" {
		t.Errorf("cards = %s, want l2", got)
	}
	if cell.Closed != "1 done · 1 obsolete" {
		t.Errorf("closed chip = %q", cell.Closed)
	}

	e := laneByTitle(t, data, "e")
	if len(e.Cells[0].Cards) != 0 || e.Cells[0].Closed != "1 done" || e.Cells[0].Empty {
		t.Errorf("epic 0.1 cell = %+v", e.Cells[0])
	}
}

func TestBuildCanvas_SystemRowsAndHighlight(t *testing.T) {
	data := buildCanvas(canvasFixture(t), canvasWorkflow(), CanvasOptions{Rows: "system", ShowNoVersion: true, Highlight: "E"})
	if data.EpicRows || data.Options.Highlight != "e" || data.HighlightName != "e" {
		t.Fatalf("rows/highlight = %v %q %q", data.EpicRows, data.Options.Highlight, data.HighlightName)
	}
	var order []string
	for _, l := range data.Lanes {
		order = append(order, l.Title)
	}
	if got := strings.Join(order, ","); got != "Core,UI,No system" {
		t.Fatalf("lanes = %s", got)
	}

	core := laneByTitle(t, data, "Core")
	if !core.Lit || core.Epics != "e u" {
		t.Errorf("core lane lit=%v epics=%q", core.Lit, core.Epics)
	}
	// The no-version cell holds epic e and umbrella u; the highlighted epic
	// sorts first and badges itself.
	nv := core.Cells[len(core.Cells)-1]
	first := nv.Cards[0]
	if first.Slug != "e" || !first.Lit || first.Type != "epic" || first.EpicName != "e" {
		t.Errorf("first no-version card = %+v", first)
	}
	if nv.Cards[1].Slug != "u" || nv.Cards[1].Lit {
		t.Errorf("second card = %+v", nv.Cards[1])
	}

	ui := laneByTitle(t, data, "UI")
	if got := ui.Cells[1].Cards; len(got) != 2 || got[0].Slug != "c2" || got[0].EpicName != "e" {
		t.Errorf("UI 0.2 cards = %+v", got)
	}

	// A non-epic highlight is ignored.
	data = buildCanvas(canvasFixture(t), canvasWorkflow(), CanvasOptions{Highlight: "l2"})
	if data.Options.Highlight != "" {
		t.Errorf("highlight = %q, want none", data.Options.Highlight)
	}
}

func TestBuildCanvas_ParentCycleListsEachEpicOnce(t *testing.T) {
	issues := []*tracker.Issue{
		canvasIssue(t, "a", `parent: "b"`),
		canvasIssue(t, "b", `parent: "a"`),
		canvasIssue(t, "self", `parent: "self"`),
	}
	data := buildCanvas(issues, canvasWorkflow(), CanvasOptions{})
	var titles []string
	for _, l := range data.Lanes {
		titles = append(titles, l.Title)
	}
	if got := strings.Join(titles, ","); got != "a,b,No epic" {
		t.Errorf("lanes = %s", got)
	}
}

func TestEpicShortName(t *testing.T) {
	for in, want := range map[string]string{
		"Adoption: make it easy":                 "Adoption",
		"Three-lane siege experiment (umbrella)": "Three-lane siege experiment",
		"Plain":                                  "Plain",
	} {
		if got := epicShortName(in); got != want {
			t.Errorf("epicShortName(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeCanvasIssues(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleCanvas_Renders(t *testing.T) {
	proj, _ := setupTestProject(t)
	writeCanvasIssues(t, proj.IssueDir, map[string]string{
		"umbrella.md": "---\ntitle: \"Big umbrella: the plan\"\nstatus: \"idea\"\nbase: \"base/umbrella\"\n---\n\nbody\n",
		"child.md":    "---\ntitle: \"Child\"\nstatus: \"idea\"\nversion: \"1.0\"\nparent: \"big-umbrella-the-plan\"\n---\n\nbody\n",
	})
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	code, html := getBody(t, ts.URL+"/p/test-project/canvas?hl=big-umbrella-the-plan")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		`href="/p/test-project/canvas" class="nav-item active" data-testid="nav.tab.canvas" aria-current="page"`,
		`id="canvas-world"`,
		`>1.0</b>`,
		`>2.0</b>`,
		`UMBRELLA`,
		`class="canvas-card lit" data-testid="canvas.card.child" data-epic="big-umbrella-the-plan"`,
		`data-testid="canvas.highlight.name">Big umbrella</span>`,
		`2 issues without a version hidden`,
		`function initPanZoom(`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("canvas page missing %q", want)
		}
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Error("canvas page has a value html/template rejected (ZgotmplZ)")
	}

	code, html = getBody(t, ts.URL+"/p/test-project/canvas?rows=system&done=collapse&nover=1")
	if code != http.StatusOK {
		t.Fatalf("system rows status = %d", code)
	}
	for _, want := range []string{`SYSTEM ↓`, `>no version</b>`, `✓ 1 done`, `⧉ Big umbrella`} {
		if !strings.Contains(html, want) {
			t.Errorf("system rows page missing %q", want)
		}
	}

	if code, _ := getBody(t, ts.URL+"/p/nope/canvas"); code != http.StatusNotFound {
		t.Errorf("unknown project status = %d, want 404", code)
	}
}

func TestDetail_EpicLinksToCanvas(t *testing.T) {
	proj, _ := setupTestProject(t)
	writeCanvasIssues(t, proj.IssueDir, map[string]string{
		"umbrella.md": "---\ntitle: \"Umbrella\"\nstatus: \"idea\"\n---\n\nbody\n",
		"child.md":    "---\ntitle: \"Child\"\nstatus: \"idea\"\nparent: \"umbrella\"\n---\n\nbody\n",
	})
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	_, html := getBody(t, ts.URL+"/p/test-project/issue/umbrella")
	if !strings.Contains(html, `href="/p/test-project/canvas?hl=umbrella" data-testid="detail.canvas_link">View on canvas</a>`) {
		t.Error("epic detail page has no View on canvas link")
	}
	_, html = getBody(t, ts.URL+"/p/test-project/issue/child")
	if strings.Contains(html, "View on canvas") {
		t.Error("non-epic detail page links to the canvas")
	}
}

func TestStatusColor_MultiWordStatuses(t *testing.T) {
	color := funcMap["statusColor"].(func(string) string)
	text := funcMap["statusTextColor"].(func(string) string)
	for status, want := range map[string]string{
		"in progress": "#eab308",
		"In Design":   "#3b82f6",
		"in-progress": "#eab308",
		"playtest":    "#6b7280",
	} {
		if got := color(status); got != want {
			t.Errorf("statusColor(%q) = %s, want %s", status, got, want)
		}
	}
	if got := text("in progress"); got != "#000000" {
		t.Errorf("statusTextColor(in progress) = %s, want #000000", got)
	}
}
