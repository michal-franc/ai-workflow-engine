package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// setupBalanceProject: the workbench fixture plus a workbench.balance block, two
// pass issues (2 done, 3 planned) and balance comments.
func setupBalanceProject(t *testing.T) tracker.Project {
	t.Helper()
	f := setupWorkbenchProject(t, time.Now())
	wf, err := os.ReadFile(f.proj.WorkflowFile)
	if err != nil {
		t.Fatal(err)
	}
	wf = append(wf, []byte("  balance:\n    comment_prefix: \"balance:\"\n")...)
	if err := os.WriteFile(f.proj.WorkflowFile, wf, 0644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.proj.IssueDir, "Sim")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, status, extra string) string {
		p := filepath.Join(dir, name+".md")
		content := "---\ntitle: \"" + name + "\"\nstatus: \"" + status + "\"\nsystem: \"Sim\"\n" + extra + "---\n\nBody.\n"
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("pass-2", "done", "balance_pass: 2\ndone_at: \"2026-09-30\"\n")
	write("pass-3", "backlog", "balance_pass: 3\n")
	finish := write("finish", "backlog", "")
	scout := write("scout", "done", "")
	coord := write("balance", "backlog", "")
	add := func(path, text string) {
		if err := tracker.AddComment(path, 0, text, "cli"); err != nil {
			t.Fatal(err)
		}
	}
	add(finish, "balance: full-holds · open\nhypothesis: Nobody finishes a <downed> attacker.\nchange: FinishChance ↑\nevidence: finish rate 0%")
	add(scout, "balance: dominant-plays · open\nchange: ScoutAccuracy 1.0 → 0.7")
	add(scout, "balance: dominant-plays · kept\nchange: ScoutAccuracy 1.0 → 0.7\nverdict: Inside band.\npass: 2")
	add(coord, "balance: [#sim/scout] scouting "+strings.Repeat("is strong ", 40))
	return f.proj
}

func TestHandleBalance_RendersBoard(t *testing.T) {
	proj := setupBalanceProject(t)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	page := pageGet(t, ts.URL+"/p/wb/balance")
	for _, want := range []string{
		`href="/p/wb/balance" data-testid="nav.tab.balance" class="view-tab active"`,
		`<span class="wb-stat-num">2</span><span class="wb-stat-label">notes</span>`,
		`<span class="wb-stat-num">1</span><span class="wb-stat-label">unsorted</span>`,
		`class="balance-pass done">Pass 2 · 30 Sep · 1 note</a>`,
		`class="balance-pass planned">Pass 3 · planned · 1 note collected</a>`,
		"agent not running",
		`data-slug="sim/pass-3" data-pass="3">Start pass 3</button>`,
		`<h3 class="balance-metric">full-holds</h3>`,
		"Nobody finishes a &lt;downed&gt; attacker.",
		`<dd class="mono">FinishChance ↑</dd>`,
		"planned pass 3",
		"1 earlier note",
		`class="decision-state s-kept" data-testid="balance.card.`,
		`.state">kept</span>`,
		"Unsorted notes",
		`from <a href="/p/wb/issue/sim/scout" class="decision-ref">sim/scout</a>`,
		`<span class="balance-more-link">more</span>`,
		"Kept 1", "Open 1",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("balance page missing %q", want)
		}
	}
	// Open group first.
	if strings.Index(page, `id="metric-full-holds"`) > strings.Index(page, `id="metric-dominant-plays"`) {
		t.Error("group with open notes should come first")
	}

	page = pageGet(t, ts.URL+"/p/wb/balance?state=kept")
	if strings.Contains(page, `id="metric-full-holds"`) || !strings.Contains(page, `id="metric-dominant-plays"`) {
		t.Error("state filter wrong")
	}
	page = pageGet(t, ts.URL+"/p/wb/balance?state=reverted")
	if !strings.Contains(page, "No notes in this state") {
		t.Error("no-match state missing")
	}
}

func TestHandleBalance_RunningAgentHidesStart(t *testing.T) {
	proj := setupBalanceProject(t)
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-sim-pass-3"}})
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	page := pageGet(t, ts.URL+"/p/wb/balance")
	if !strings.Contains(page, "agent running in agent-sim-pass-3") || strings.Contains(page, "Start pass 3</button>") {
		t.Error("running pass agent should replace the Start button")
	}
}

func TestHandleBalance_OffWithoutWorkbench(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	page := pageGet(t, ts.URL+"/p/wb/balance")
	if !strings.Contains(page, "The balance board is off for this project") {
		t.Fatal("off page not rendered")
	}
	for _, path := range []string{"/", "/board", "/decisions", "/stats"} {
		if strings.Contains(pageGet(t, ts.URL+"/p/wb"+path), `href="/p/wb/balance"`) {
			t.Errorf("%s: Balance tab shown without workbench.balance", path)
		}
	}
}

func TestNavLinksToBalance(t *testing.T) {
	proj := setupBalanceProject(t)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	for _, path := range []string{"/", "/board", "/graph", "/docs", "/stats", "/decisions", "/retros", "/workflow-flow", "/workflow-designer"} {
		page := pageGet(t, ts.URL+"/p/wb"+path)
		decisions := strings.Index(page, `href="/p/wb/decisions"`)
		balance := strings.Index(page, `href="/p/wb/balance"`)
		if balance < 0 || balance < decisions {
			t.Errorf("%s: Balance nav link missing or not after Decisions", path)
		}
	}
}

func TestBalanceEmptyBoard(t *testing.T) {
	f := setupWorkbenchProject(t, time.Now())
	wf, _ := os.ReadFile(f.proj.WorkflowFile)
	os.WriteFile(f.proj.WorkflowFile, append(wf, []byte("  balance:\n    comment_prefix: \"tuning:\"\n")...), 0644)
	withMockTmuxSessions(t, nil)
	ts := newTestServer(t, []tracker.Project{f.proj})
	defer ts.Close()
	page := pageGet(t, ts.URL+"/p/wb/balance")
	for _, want := range []string{"No balance notes yet", "No joint passes yet", "<code>tuning:</code>"} {
		if !strings.Contains(page, want) {
			t.Errorf("empty board missing %q", want)
		}
	}
}

func TestShortDateLabel(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local)
	if got := shortDateLabel("2026-10-06", now); got != "6 Oct" {
		t.Errorf("got %q", got)
	}
	if got := shortDateLabel("someday", now); got != "someday" {
		t.Errorf("got %q", got)
	}
}
