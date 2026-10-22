package main

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const systemsWorkflow = `statuses:
  - name: "idea"
  - name: "in design"
  - name: "in progress"
  - name: "testing"
  - name: "done"
  - name: "obsolete"
    optional: true
transitions:
  - from: "idea"
    to: "in design"
  - from: "in design"
    to: "in progress"
    actions:
      - type: require_human_approval
        status: "in progress"
  - from: "in progress"
    to: "testing"
  - from: "testing"
    to: "done"
workbench:
  systems:
    AI:
      summary: "How teams choose plays"
      design_doc: docs/ai/play-calls.md
      families:
        Play calls:
          summary: "Calls per stage"
          umbrella: ai/play-calls
`

type systemsFixture struct {
	proj tracker.Project
	root string
}

// writeSystemIssue writes issues/<slug>.md with the given frontmatter lines.
func writeSystemIssue(t *testing.T, root, slug, title, status, system, extra, body string) {
	t.Helper()
	fp := filepath.Join(root, "issues", slug+".md")
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\n"
	if system != "" {
		fm += "system: \"" + system + "\"\n"
	}
	if err := os.WriteFile(fp, []byte(fm+extra+"---\n\n"+body+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func setupSystemsProject(t *testing.T) systemsFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "workflow.yaml"), []byte(systemsWorkflow), 0644); err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(docs, "ai"), 0755); err != nil {
		t.Fatal(err)
	}
	doc := "---\ntitle: \"Play calls\"\n---\n\n# Play calls\n\n## Rules\n\nCards per map.\n\n## Shouts and tempo\n\nAs decided in ai/shouts#D-1.\n\n## The AI caller\n\nFit vs read.\n"
	if err := os.WriteFile(filepath.Join(docs, "ai", "play-calls.md"), []byte(doc), 0644); err != nil {
		t.Fatal(err)
	}

	writeSystemIssue(t, root, "ai/play-calls", "Play calls", "in progress", "AI", "", "")
	writeSystemIssue(t, root, "ai/call-modal", "Call modal", "done", "AI", "parent: \"ai/play-calls\"\n", "")
	writeSystemIssue(t, root, "ai/caller-fit", "Caller fit", "testing", "AI", "parent: \"ai/play-calls\"\n", "")
	writeSystemIssue(t, root, "ai/shouts", "Shouts", "done", "AI", "",
		"## Decisions\n- **D-1** · 2026-10-01 · chat: Shouts set the tempo, \"ok\"\n- **D-2** · 2026-10-03 · chat: A card's **tempo** replaces the shouts, \"yes\" (replaces ai/shouts#D-1)")
	writeSystemIssue(t, root, "ai/scouting", "Scouting", "idea", "ai", "",
		"<!-- issue-viewer-comments\n{\"id\":1,\"block\":0,\"date\":\"2026-10-09\",\"text\":\"balance: dominant-plays · open\\nhypothesis: one card per book dominates\",\"status\":\"open\",\"source\":\"cli\"}\n-->")
	writeSystemIssue(t, root, "ai/old", "Old", "obsolete", "AI", "", "")
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		writeSystemIssue(t, root, "ai/idea-"+n, "Idea "+n, "idea", "AI", "", "")
	}
	writeSystemIssue(t, root, "sim/siege", "Siege", "in design", "Sim", "", "")
	writeSystemIssue(t, root, "sim/lanes", "Lanes", "idea", "Sim", "parent: \"sim/siege\"\n", "## Decisions\nWe picked option B (free-form).")
	writeSystemIssue(t, root, "ai/heroes", "Heroes", "idea", "AI", "parent: \"sim/siege\"\n", "")
	writeSystemIssue(t, root, "loose", "Loose", "idea", "", "", "")

	return systemsFixture{
		root: root,
		proj: tracker.Project{Name: "Raid", Slug: "rd", IssueDir: filepath.Join(root, "issues"), DocsDir: docs, WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root},
	}
}

// noBranches stubs the doc panel's git scan.
func noBranches(t *testing.T) {
	t.Helper()
	old := openDocBranches
	openDocBranches = func(string, string, []*tracker.Issue) ([]docBranch, bool) { return nil, true }
	t.Cleanup(func() { openDocBranches = old })
}

func assertContains(t *testing.T, page string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(page, w) {
			t.Errorf("page is missing %q", w)
		}
	}
}

func TestSystemsIndex_CardsMostOpenFirst(t *testing.T) {
	f := setupSystemsProject(t)
	noBranches(t)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/rd/systems")
	assertContains(t, page,
		`data-testid="systems.card.ai"`, `data-testid="systems.card.sim"`,
		"How teams choose plays", "docs/ai/play-calls.md", "Play calls · 1 of 2 done",
		"11 open · 2 done · 1 obsolete", // "ai" and "AI" are one system
		`data-testid="systems.nosystem"`, "No system: 1 open, 0 done",
	)
	if strings.Index(page, "systems.card.ai") > strings.Index(page, "systems.card.sim") {
		t.Error("AI (11 open) should come before Sim (2 open)")
	}
	assertContains(t, page, `data-testid="nav.tab.systems"`, `aria-current="page"`, `data-testid="nav.panel.system.ai"`, `data-testid="nav.panel.family.play_calls"`)
}

func TestSystemPage_Configured(t *testing.T) {
	f := setupSystemsProject(t)
	noBranches(t)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/rd/systems/AI")
	assertContains(t, page,
		`data-testid="system.summary"`, "How teams choose plays",
		"11 open · 2 done · 1 obsolete",
		"Play calls · 1 of 2 done",
		"workflow.yaml › workbench.systems.AI",
		`href="/p/rd/board?system=AI"`, `href="/p/rd/decisions?system=AI"`,
		// furthest along first: the testing child before the in-progress umbrella
		"system.issue.ai/caller-fit",
		"epic · 1/2",
		"3 idea, 2 done and 1 obsolete not shown", "All 14 in the list",
		// families and the derived epic from another system
		`data-testid="system.family.play_calls"`, `data-testid="system.epic.sim/siege"`,
		"The epic has 2 children in 2 systems (0 of 2 done); 1 of them are AI.",
		// design doc: the section citing replaced D-1 is stale
		`data-testid="system.doc.section.shouts_and_tempo" data-state="stale"`,
		"cites D-1, replaced by D-2",
		`href="/p/rd/docs/ai/play-calls#shouts-and-tempo"`,
		`data-testid="system.doc.section.rules" data-state="current"`,
		// decisions, newest first, bold kept
		`data-testid="system.decision.d-2"`, "replaces D-1", "<strong>tempo</strong>",
		// balance notes from the typed comment
		`data-testid="system.balance"`, "dominant-plays", "one card per book dominates",
	)
	if strings.Index(page, "system.issue.ai/caller-fit") > strings.Index(page, "system.issue.ai/play-calls") {
		t.Error("testing should rank before in progress")
	}
	if strings.Contains(page, `data-testid="system.hint"`) {
		t.Error("a configured system shows the config hint")
	}
}

func TestSystemPage_Family(t *testing.T) {
	f := setupSystemsProject(t)
	noBranches(t)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/rd/systems/ai/play%20CALLS")
	assertContains(t, page,
		`<a href="/p/rd/systems/AI">AI</a> / Play calls`,
		"Calls per stage", "1 open · 1 done", "umbrella · 1 of 2 done",
		"system.issue.ai/caller-fit", "system.issue.ai/call-modal",
		"(the system's)", `data-testid="nav.panel.family.play_calls"`,
	)
	if strings.Contains(page, "system.issue.ai/idea-a") {
		t.Error("a family page lists issues outside the umbrella")
	}
}

func TestSystemPage_Unconfigured(t *testing.T) {
	f := setupSystemsProject(t)
	noBranches(t)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/rd/systems/Sim")
	assertContains(t, page,
		`data-testid="system.hint"`, "workbench.systems.Sim",
		"Epics with issues here", `data-testid="system.epic.sim/siege"`,
		"1 Sim issue keeps a <code>## Decisions</code> section",
	)
	for _, absent := range []string{`data-testid="system.doc"`, `data-testid="system.balance"`, `data-testid="system.summary"`} {
		if strings.Contains(page, absent) {
			t.Errorf("unconfigured page shows %s", absent)
		}
	}
}

func TestSystemPage_UnknownIs404(t *testing.T) {
	f := setupSystemsProject(t)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	for _, path := range []string{"/p/rd/systems/Nope", "/p/rd/systems/AI/Nope", "/p/rd/systems/AI/Play%20calls/x"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
		if !strings.Contains(string(body), `data-testid="systems.notfound"`) || !strings.Contains(string(body), `systems.card.ai`) {
			t.Errorf("GET %s: 404 page should name the miss and list the systems", path)
		}
	}
}

func TestSidebar_SystemsEntryVisibility(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "workflow.yaml"), []byte("statuses:\n  - name: idea\n  - name: done\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeSystemIssue(t, root, "plain", "Plain", "idea", "", "", "")
	proj := tracker.Project{Name: "Plain", Slug: "pl", IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml")}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	if page := pageGet(t, ts.URL+"/p/pl/board"); strings.Contains(page, `data-testid="nav.tab.systems"`) {
		t.Error("Systems shows for a project whose issues have no system")
	}
	writeSystemIssue(t, root, "sys", "With system", "idea", "Core", "", "")
	page := pageGet(t, ts.URL+"/p/pl/board")
	assertContains(t, page, `data-testid="nav.tab.systems"`, `href="/p/pl/board?system=Core"`)
}

func TestBalance_SystemFilter(t *testing.T) {
	f := setupSystemsProject(t)
	wf := strings.Replace(systemsWorkflow, "workbench:\n", "workbench:\n  balance:\n    comment_prefix: \"balance:\"\n", 1)
	if err := os.WriteFile(f.proj.WorkflowFile, []byte(wf), 0644); err != nil {
		t.Fatal(err)
	}
	writeSystemIssue(t, f.root, "sim/creeps", "Creeps", "idea", "Sim", "",
		"<!-- issue-viewer-comments\n{\"id\":1,\"block\":0,\"date\":\"2026-10-09\",\"text\":\"balance: creep-gold · open\\nhypothesis: camps pay too much\",\"status\":\"open\",\"source\":\"cli\"}\n-->")
	noBranches(t)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/rd/balance?system=ai")
	assertContains(t, page, `data-testid="balance.system"`, "dominant-plays", `href="/p/rd/balance?state=open&amp;system=ai"`)
	if strings.Contains(page, "creep-gold") {
		t.Error("?system=ai shows a Sim note")
	}
	sys := pageGet(t, ts.URL+"/p/rd/systems/AI")
	assertContains(t, sys, `href="/p/rd/balance?system=AI"`)
}

// gitRun runs git in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDocPanel_BranchEditsSinceMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	f := setupSystemsProject(t)
	root := f.root
	docPath := filepath.Join(root, "docs", "ai", "play-calls.md")
	gitRun(t, root, "init", "-q", "-b", "main")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "base")

	// An old branch, cut before main changed "The AI caller": behind main,
	// with no doc edits of its own, it must mark nothing.
	gitRun(t, root, "branch", "work/ai/old")
	gitRun(t, root, "checkout", "-q", "work/ai/old")
	writeFile(t, filepath.Join(root, "other.txt"), "unrelated\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "old work")
	gitRun(t, root, "checkout", "-q", "main")
	doc, _ := os.ReadFile(docPath)
	writeFile(t, docPath, strings.Replace(string(doc), "Fit vs read.", "Fit vs read, now with habits.", 1))
	gitRun(t, root, "commit", "-q", "-am", "main edits the caller")

	// An open branch that edits Rules, adds a section and drops The AI caller.
	gitRun(t, root, "checkout", "-q", "-b", "work/ai/caller-fit")
	doc, _ = os.ReadFile(docPath)
	edited := strings.Replace(string(doc), "Cards per map.", "Cards per map and side.", 1)
	edited = edited[:strings.Index(edited, "## The AI caller")] + "## Metrics\n\nNew.\n"
	writeFile(t, docPath, edited)
	gitRun(t, root, "commit", "-q", "-am", "caller-fit edits the doc")
	gitRun(t, root, "checkout", "-q", "main")

	issues, err := tracker.LoadIssues(f.proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	index := tracker.BuildDecisionIndex(issues, tracker.ClosedIssueFunc(&f.proj))
	p := s.docPanel(&f.proj, issues, "/p/rd", "docs/ai/play-calls.md", "/p/rd/docs/ai/play-calls", index)

	states := map[string]DocSectionView{}
	for _, sec := range p.Sections {
		states[sec.Title] = sec
	}
	want := map[string]string{
		"Rules":            sectionChanged,
		"Shouts and tempo": sectionStale,
		"The AI caller":    sectionRemoved,
		"Metrics":          sectionNew,
	}
	for title, st := range want {
		if states[title].State != st {
			t.Errorf("%s = %q, want %q (sections %+v)", title, states[title].State, st, p.Sections)
		}
	}
	if got := states["Rules"]; got.Note != "caller-fit · 1 commit" || got.NoteHref != "/p/rd/issue/ai/caller-fit" {
		t.Errorf("Rules note = %q → %q", got.Note, got.NoteHref)
	}
	if p.Branches != 1 || p.Changed != 3 || p.Stale != 1 {
		t.Errorf("counts: branches %d changed %d stale %d", p.Branches, p.Changed, p.Stale)
	}

	// A doc that exists only on a branch.
	gitRun(t, root, "checkout", "-q", "work/ai/caller-fit")
	writeFile(t, filepath.Join(root, "docs", "ai", "heroes.md"), "## Heroes\n\nOnly here.\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "heroes doc")
	gitRun(t, root, "checkout", "-q", "main")
	m := s.docPanel(&f.proj, issues, "/p/rd", "docs/ai/heroes.md", "", index)
	if !m.Missing || len(m.OnBranches) != 1 || m.OnBranches[0] != "work/ai/caller-fit" {
		t.Errorf("branch-only doc: missing %v on %v", m.Missing, m.OnBranches)
	}
}

func TestSystemPage_MetricPanels(t *testing.T) {
	r := setupMetricsRepo(t, metricsTestWorkflow)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{r.proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/mp/systems/Sim")
	assertContains(t, page, `data-testid="panel.guard_metrics"`, `data-testid="panel.guard_metrics.row.stuck"`, `data-testid="panel.knobs"`)
	// A system that lists no metrics or knobs gets neither panel (no typed nil
	// slipping through {{with}}).
	writeSystemIssue(t, filepath.Dir(r.proj.IssueDir), "economy/shop", "Shop", "idea", "Economy", "", "")
	other := pageGet(t, ts.URL+"/p/mp/systems/Economy")
	if strings.Contains(other, "panel.guard_metrics") || strings.Contains(other, "panel.knobs") {
		t.Error("Economy lists no metrics or knobs but shows a panel")
	}
}
