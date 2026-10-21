package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// setupDecisionsProject: pivot (done) replaces phases#D-1; creeps (backlog)
// still cites phases#D-1, so it is stale; shop has an answered question and a
// deferred decision.
func setupDecisionsProject(t *testing.T) tracker.Project {
	t.Helper()
	f := setupWorkbenchProject(t, time.Now())
	dir := filepath.Join(f.proj.IssueDir, "Sim")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, status, body string) {
		content := "---\ntitle: \"" + name + "\"\nstatus: \"" + status + "\"\nsystem: \"Sim\"\n---\n\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("pivot", "done", "## Decisions\n- **D-1** · 2026-09-28 · chat: The raid becomes an **ET-style** objective format <6v6>., \"no readable tension\" (replaces sim/phases#D-1)")
	write("phases", "done", "## Decisions\n- **D-1** · 2026-09-27 · project rule: Three phases of 20 minutes.")
	write("creeps", "backlog", "Camps follow sim/phases#D-1 and sim/pivot#D-1.")
	writeWorkbenchIssue(t, f.proj.IssueDir, "shop", "in design", "", "## Decisions\n"+
		"- **D-1** · 2026-10-02 · answered in lead chat (Q-1): Does gold carry over? → **B. Capped at 300**, \"revisit\" (also considered: A Reset)\n"+
		"- **D-2** · 2026-10-04 · deferred · chat: Balance later.")
	return f.proj
}

func TestHandleDecisions_RendersIndex(t *testing.T) {
	proj := setupDecisionsProject(t)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/wb/decisions")
	for _, want := range []string{
		`data-testid="nav.tab.decisions" aria-current="page">Decisions`,
		`<span class="wb-stat-num">4</span><span class="wb-stat-label">decisions</span>`,
		`wb-stat stale" data-testid="decisions.stat.stale"><span class="wb-stat-num">1</span>`,
		"All systems 4", "Economy 2", "Sim 2", "Has stale citations 1",
		`data-ref="sim/phases#D-1"`,
		"The raid becomes an <strong>ET-style</strong> objective format &lt;6v6&gt;.",
		`, "no readable tension"`,
		`replaces <a href="/p/wb/issue/sim/phases" class="decision-ref">sim/phases#D-1</a>`,
		`replaced by <a href="/p/wb/issue/sim/pivot" class="decision-ref">sim/pivot#D-1</a> on 28 Sep`,
		"1 open issue still cites it",
		`<a href="/p/wb/issue/sim/creeps">sim/creeps</a><span class="decision-status">· backlog</span>`,
		`<span class="decision-state s-stale" data-testid="decisions.row.`,
		`.state">replaced · stale</span>`,
		`<span class="decision-state s-deferred" data-testid="decisions.row.`,
		`.state">deferred</span>`,
		"answered in lead chat (Q-1) · also considered: A Reset",
		"Does gold carry over? → <strong>B. Capped at 300</strong>",
		"none yet",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("decisions page missing %q", want)
		}
	}
	// Newest first: shop D-2 (4 Oct) before pivot (28 Sep) before phases (27 Sep).
	if a, b, c := strings.Index(page, `data-ref="shop#D-2"`), strings.Index(page, `data-ref="sim/pivot#D-1"`), strings.Index(page, `data-ref="sim/phases#D-1"`); !(a >= 0 && a < b && b < c) {
		t.Errorf("rows not newest first: %d %d %d", a, b, c)
	}
}

func TestHandleDecisions_Filters(t *testing.T) {
	proj := setupDecisionsProject(t)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/wb/decisions?state=stale")
	if !strings.Contains(page, `data-ref="sim/phases#D-1"`) || strings.Contains(page, `data-ref="sim/pivot#D-1"`) {
		t.Error("stale filter wrong")
	}
	page = pageGet(t, ts.URL+"/p/wb/decisions?system=economy&state=deferred")
	if !strings.Contains(page, `data-ref="shop#D-2"`) || strings.Contains(page, `data-ref="shop#D-1"`) || strings.Contains(page, `data-ref="sim/`) {
		t.Error("system+state filter wrong")
	}
	// Chips keep the other row's filter.
	if !strings.Contains(page, `href="/p/wb/decisions?state=deferred&amp;system=Sim"`) {
		t.Error("system chip drops the state filter")
	}
	page = pageGet(t, ts.URL+"/p/wb/decisions?system=Sim&state=deferred")
	if !strings.Contains(page, "No decisions match these filters") {
		t.Error("no-match state missing")
	}
	page = pageGet(t, ts.URL+"/p/wb/decisions?state=replaced")
	if !strings.Contains(page, `data-ref="sim/phases#D-1"`) || strings.Contains(page, `data-ref="shop`) {
		t.Error("replaced filter should include stale replaced decisions only")
	}
}

func TestHandleDecisions_EmptyState(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	page := pageGet(t, ts.URL+"/p/wb/decisions")
	if !strings.Contains(page, "No decisions recorded yet") || strings.Contains(page, "decisions-table") {
		t.Fatal("empty decisions state not rendered")
	}
}

func TestNavLinksToDecisions(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	for _, path := range []string{"/", "/board", "/graph", "/docs", "/stats", "/retros"} {
		page := pageGet(t, ts.URL+"/p/wb"+path)
		if !strings.Contains(page, `href="/p/wb/decisions"`) {
			t.Errorf("%s: Decisions nav link missing", path)
		}
		if strings.Contains(page, `/p/wb/inbox`) || strings.Contains(page, "inbox-badge") {
			t.Errorf("%s: the removed Inbox tab is still in the nav", path)
		}
	}
}

func TestDecisionDateLabel(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local)
	if got := decisionDateLabel(time.Date(2026, 9, 28, 0, 0, 0, 0, time.Local), now); got != "28 Sep" {
		t.Errorf("same year = %q", got)
	}
	if got := decisionDateLabel(time.Date(2025, 9, 28, 0, 0, 0, 0, time.Local), now); got != "28 Sep 2025" {
		t.Errorf("other year = %q", got)
	}
	if decisionDateLabel(time.Time{}, now) != "" {
		t.Error("zero date")
	}
}

// The generated pages render the /hash value as their auto-reload baseline,
// so a change made between the render and the first poll is not missed.
func TestGeneratedPagesRenderHashBaseline(t *testing.T) {
	proj := setupDecisionsProject(t)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	hash := pageGet(t, ts.URL+"/p/wb/hash")
	i := strings.Index(hash, `"hash":"`)
	if i < 0 {
		t.Fatalf("no hash in %q", hash)
	}
	want := hash[i+len(`"hash":"`):]
	want = want[:strings.Index(want, `"`)]
	for _, page := range []string{"/p/wb/decisions", "/p/wb/balance"} {
		if got := pageGet(t, ts.URL+page); !strings.Contains(got, `var last = "`+want+`" || null;`) {
			t.Errorf("%s does not render the /hash baseline %s", page, want)
		}
	}
}
