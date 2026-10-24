package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// setupChecksProject has one issue at human-testing whose worktree holds a
// guided check script, and one at testing with a script too.
func setupChecksProject(t *testing.T) (tracker.Project, string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("issues/CLI/lead-tab.md", "---\ntitle: \"Lead tab\"\nstatus: \"human-testing\"\n---\n\nBody.\n")
	write("issues/CLI/early.md", "---\ntitle: \"Early\"\nstatus: \"testing\"\n---\n\nBody.\n")
	write("workflow.yaml", "worktree: true\nstatuses:\n  - name: \"testing\"\n  - name: \"human-testing\"\n  - name: \"done\"\n")
	wt := filepath.Join(root, ".worktrees", "cli", "lead-tab")
	write(".worktrees/cli/lead-tab/scripts/checks/lead-tab.sh", "#!/usr/bin/env bash\n# issue: cli/lead-tab\n")
	write(".worktrees/cli/early/scripts/checks/early.sh", "#!/usr/bin/env bash\n# issue: cli/early\n")
	return tracker.Project{Name: "Checks", Slug: "chk", IssueDir: filepath.Join(root, "issues"), WorkDir: root, WorkflowFile: filepath.Join(root, "workflow.yaml")}, wt
}

func TestOwnerAttentionChecksDueUntilTheOwnerRun(t *testing.T) {
	proj, wt := setupChecksProject(t)
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	bySlug := map[string]*tracker.Issue{}
	for _, i := range issues {
		bySlug[i.Slug] = i
	}
	a := ownerAttention(&proj, bySlug["cli/lead-tab"])
	if !a.ChecksDue || !reflect.DeepEqual(a.Checks, []string{"lead-tab"}) {
		t.Fatalf("human-testing with a script: %+v", a)
	}
	if b := ownerAttention(&proj, bySlug["cli/early"]); b.ChecksDue {
		t.Fatal("a script at testing is the agent's, not the owner's")
	}
	items := waitingOnOwner(&proj, issues)
	if len(items) != 1 || items[0].Kind != "checks" || items[0].Sessions[0] != "lead-tab" {
		t.Fatalf("waiting = %+v", items)
	}

	os.MkdirAll(filepath.Join(wt, ".agent-logs", "checks", "lead-tab"), 0o755)
	os.WriteFile(filepath.Join(wt, ".agent-logs", "checks", "lead-tab", "20261023T120000Z-owner.txt"), nil, 0o644)
	if a := ownerAttention(&proj, bySlug["cli/lead-tab"]); a.ChecksDue {
		t.Fatal("still due after the owner's run")
	}
}

func TestNeedsYouChecks(t *testing.T) {
	in := tabInput(&tracker.Issue{Slug: "cli/lead-tab", Status: "human-testing"})
	in.Project = "chk"
	in.Att.ChecksDue, in.Att.Checks = true, []string{"lead-tab"}
	n := buildIssueTab(in).Needs
	if n.State != "checks" || n.Title != "Run the guided checks: 1 script." || n.Command != "issue-cli --project chk check-script cli/lead-tab" {
		t.Fatalf("got %+v", n)
	}
	if !strings.Contains(n.Text, "tmux window check-script-cli-lead-tab") {
		t.Errorf("text = %q", n.Text)
	}
	// Play first, then the checks, then the approval.
	in.Att.PlaytestDue, in.Att.Playtests = true, []string{"lead"}
	in.Att.Gate = &GateInfo{Target: "documentation", Ready: true}
	in.Targets = []string{"documentation"}
	n = buildIssueTab(in).Needs
	if n.State != "playtest" || !reflect.DeepEqual(n.Also, []string{"run the checks", "approve → documentation"}) {
		t.Fatalf("playtest first: %+v", n)
	}
}

func TestCardMarkerForChecksHoldsTheGate(t *testing.T) {
	issue := &tracker.Issue{Slug: "cli/lead-tab", Status: "human-testing"}
	ctx := &markerCtx{attention: map[string]Attention{issue.Slug: {
		ChecksDue: true, Checks: []string{"lead-tab"}, Gate: &GateInfo{Target: "documentation", Ready: true},
	}}, now: at(0)}
	ms := cardMarkers(ctx, issue)
	if len(ms) != 1 || ms[0].Kind != "checks" || !strings.Contains(ms[0].Title, "issue-cli check-script cli/lead-tab") {
		t.Fatalf("markers = %+v", ms)
	}
}

func TestRunChecksOpensTheCheckScriptWindow(t *testing.T) {
	proj, wt := setupChecksProject(t)
	type created struct {
		name, dir string
		cmd       []string
	}
	var made []created
	var opened []string
	live := false
	origCreate, origLive, origAttach := createCheckWindow, checkWindowLive, attachAgent
	createCheckWindow = func(_ *tracker.Project, name, dir string, _, command []string, steps *[]DispatchStep) bool {
		made = append(made, created{name, dir, command})
		*steps = append(*steps, agentrun.Step{Name: "Create tmux window " + name, Status: "ok"})
		return true
	}
	checkWindowLive = func(*tracker.Project, string) bool { return live }
	attachAgent = func(_ *tracker.Project, name string, steps *[]agentrun.Step) bool {
		opened = append(opened, name)
		return true
	}
	t.Cleanup(func() { createCheckWindow, checkWindowLive, attachAgent = origCreate, origLive, origAttach })
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	code, resp := postJSON(t, ts.URL+"/p/chk/issue/cli/lead-tab/check-script", `{}`)
	if code != 200 || resp["status"] != "started" || resp["session"] != "check-script-cli-lead-tab" {
		t.Fatalf("start: %d %v", code, resp)
	}
	if len(made) != 1 || made[0].name != "check-script-cli-lead-tab" || made[0].dir != wt {
		t.Fatalf("created = %+v", made)
	}
	if cmd := strings.Join(made[0].cmd, " "); !strings.Contains(cmd, "issue-cli --project 'chk' check-script 'cli/lead-tab'") || !strings.Contains(cmd, "Press Enter to close") {
		t.Fatalf("command = %q", cmd)
	}
	if len(opened) != 1 || opened[0] != "check-script-cli-lead-tab" {
		t.Fatalf("opened = %v", opened)
	}

	// A second click while it runs only brings the window back.
	live = true
	if code, resp := postJSON(t, ts.URL+"/p/chk/issue/cli/lead-tab/check-script", `{}`); code != 200 || resp["status"] != "already-running" || len(made) != 1 || len(opened) != 2 {
		t.Fatalf("second click: %d %v made=%d opened=%v", code, resp, len(made), opened)
	}

	// No script for the issue: nothing starts.
	os.Remove(filepath.Join(wt, "scripts", "checks", "lead-tab.sh"))
	live = false
	if code, resp := postJSON(t, ts.URL+"/p/chk/issue/cli/lead-tab/check-script", `{}`); code != 404 || resp["status"] != "no-scripts" || len(made) != 1 {
		t.Fatalf("no scripts: %d %v", code, resp)
	}
}
