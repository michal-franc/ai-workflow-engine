package agentrun

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestExpandTerminal(t *testing.T) {
	shared := &tracker.Project{Slug: "p", TmuxSession: "work"}
	got := ExpandTerminal(shared, "x {{session}} {{project}} {{name}} {{role}} {{window}}", "work", "lead-p", RoleLead)
	if want := "x work p lead-p lead '=work:=lead-p'"; got != want {
		t.Errorf("shared: %q, want %q", got, want)
	}
	own := &tracker.Project{Slug: "p"}
	if got := ExpandTerminal(own, "{{window}} {{role}}", "agent-a", "agent-a", RoleOf("agent-a")); got != "agent-a worker" {
		t.Errorf("per-agent: %q", got)
	}
	if RoleOf("lead-p") != RoleLead || RoleOf("agent-x") != RoleWorker {
		t.Error("RoleOf")
	}
}

func TestLeadHeadless(t *testing.T) {
	for lt, want := range map[string]bool{"": true, " none ": true, "tmux-window": false, "alacritty": false} {
		if got := LeadHeadless(&tracker.Project{LeadTerminal: lt}); got != want {
			t.Errorf("LeadHeadless(%q) = %v", lt, got)
		}
	}
}

// isolateTmux points tmux at a private server for the test, so it never
// touches the developer's sessions, and kills that server afterwards.
func isolateTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })
}

func tmuxRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// leadViewSetup starts the project session work (lead-p and a worker
// window) and the human's own session main.
func leadViewSetup(t *testing.T) *tracker.Project {
	t.Helper()
	isolateTmux(t)
	tmuxRun(t, "new-session", "-d", "-s", "work", "-n", "lead-p", "/bin/sh")
	tmuxRun(t, "new-window", "-d", "-t", "=work:", "-n", "agent-a", "/bin/sh")
	tmuxRun(t, "new-session", "-d", "-s", "main", "-n", "shell", "/bin/sh")
	return &tracker.Project{Slug: "p", Terminal: "none", TmuxSession: "work", LeadTerminal: LeadTerminalTmuxWindow, LeadWindowSession: "main"}
}

func windowsOf(t *testing.T, session string) []string {
	t.Helper()
	return strings.Split(tmuxRun(t, "list-windows", "-t", "="+session, "-F", "#{window_index}:#{window_name}"), "\n")
}

func activeOf(t *testing.T, session string) string {
	t.Helper()
	return tmuxRun(t, "display-message", "-p", "-t", "="+session+":", "#{window_name}")
}

// waitFor polls cond for up to two seconds: the nested client starts
// asynchronously in its pane.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func stepNames(steps []Step) string {
	var names []string
	for _, s := range steps {
		names = append(names, s.Status+": "+s.Name+" "+s.Detail)
	}
	return strings.Join(names, "\n")
}

// tmux-window adds exactly one detached tab to lead_window_session, a second
// call adds none, and the tab is a nested client pinned on the lead: a worker
// window selected in the project session doesn't move it, closing it leaves
// the lead running, and its view session is never listed as an agent.
func TestLeadView_AddsOneDetachedPinnedTab(t *testing.T) {
	proj := leadViewSetup(t)
	var steps []Step
	if ok, shown := LeadTerminalStep(proj, "lead-p", &steps); !ok || !shown {
		t.Fatalf("ok %v shown %v\n%s", ok, shown, stepNames(steps))
	}
	if got := windowsOf(t, "main"); len(got) != 2 || got[1] != "1:view-lead-p" {
		t.Fatalf("main windows = %v", got)
	}
	if !strings.Contains(stepNames(steps), "Lead tab added at main:1 — not selected") {
		t.Fatalf("steps:\n%s", stepNames(steps))
	}
	if got := activeOf(t, "main"); got != "shell" {
		t.Fatalf("main's active window moved to %q", got)
	}

	steps = nil
	LeadTerminalStep(proj, "lead-p", &steps)
	if got := windowsOf(t, "main"); len(got) != 2 || !strings.Contains(stepNames(steps), "Lead tab already at main:1 — left as is") {
		t.Fatalf("second call: windows %v\n%s", got, stepNames(steps))
	}

	waitFor(t, "the view session", func() bool { return HasSession("=view-lead-p") })
	waitFor(t, "the view to select the lead", func() bool { return activeOf(t, "view-lead-p") == "lead-p" })
	tmuxRun(t, "select-window", "-t", "=work:=agent-a")
	if got := activeOf(t, "view-lead-p"); got != "lead-p" {
		t.Fatalf("selecting a worker moved the lead view to %q", got)
	}
	for _, s := range ListSessions() {
		if strings.Contains(s.Name, "view-") {
			t.Fatalf("view listed as an agent: %+v", ListSessions())
		}
	}

	tmuxRun(t, "kill-window", "-t", "=main:=view-lead-p")
	waitFor(t, "the view session to go", func() bool { return !HasSession("=view-lead-p") })
	if !HasSession("=work:=lead-p") {
		t.Fatal("closing the tab killed the lead")
	}
}

// lead_window pins the tab's index; a taken slot falls back to the next free
// index with a warning naming both.
func TestLeadView_WindowSlot(t *testing.T) {
	proj := leadViewSetup(t)
	slot := 5
	proj.LeadWindow = &slot
	var steps []Step
	LeadTerminalStep(proj, "lead-p", &steps)
	if got := windowsOf(t, "main"); len(got) != 2 || got[1] != "5:view-lead-p" {
		t.Fatalf("windows = %v\n%s", got, stepNames(steps))
	}

	tmuxRun(t, "kill-window", "-t", "=main:5")
	tmuxRun(t, "new-window", "-d", "-t", "=main:5", "-n", "mine", "/bin/sh")
	steps = nil
	LeadTerminalStep(proj, "lead-p", &steps)
	if got := windowsOf(t, "main"); len(got) != 3 || got[1] != "1:view-lead-p" {
		t.Fatalf("taken slot: windows = %v", got)
	}
	if !strings.Contains(stepNames(steps), "warning: Lead window slot taken window 5 in main is taken (lead_window) — used 1") {
		t.Fatalf("steps:\n%s", stepNames(steps))
	}
}

// Without a usable lead_window_session nothing is created and the caller
// gets shown=false (so it hands out the hints); naming the lead's own home
// creates nothing either.
func TestLeadView_Misconfigured(t *testing.T) {
	proj := leadViewSetup(t)
	for _, tc := range []struct {
		session, want string
		shown         bool
	}{
		{"", "lead_terminal: tmux-window needs lead_window_session", false},
		{"gone", "session gone (lead_window_session) is not running", false},
		{"work", "Lead already lives in session work — no tab added", true},
	} {
		proj.LeadWindowSession = tc.session
		var steps []Step
		ok, shown := LeadTerminalStep(proj, "lead-p", &steps)
		if !ok || shown != tc.shown || !strings.Contains(stepNames(steps), tc.want) {
			t.Errorf("%q: ok %v shown %v\n%s", tc.session, ok, shown, stepNames(steps))
		}
	}
	if got := windowsOf(t, "main"); len(got) != 1 {
		t.Fatalf("main windows = %v", got)
	}
	if got := windowsOf(t, "work"); len(got) != 2 {
		t.Fatalf("work windows = %v", got)
	}
}

// Through Start: the lead follows lead_terminal (one tab, then reattach adds
// none), a misconfigured one leaves the hints, and a worker dispatched with
// terminal: none changes no session's windows or selection.
func TestStart_LeadTerminalAndHeadlessWorkers(t *testing.T) {
	isolateTmux(t)
	orig := PromptDeliveryTimeout
	PromptDeliveryTimeout = 0
	t.Cleanup(func() { PromptDeliveryTimeout = orig })
	tmuxRun(t, "new-session", "-d", "-s", "main", "-n", "shell", "/bin/sh")
	proj := &tracker.Project{Slug: "p", Terminal: "none", TmuxSession: "work", WorkDir: t.TempDir(),
		LeadTerminal: LeadTerminalTmuxWindow, LeadWindowSession: "main"}
	lead := Spec{Session: "lead-p", LeadScope: tracker.LeadScopeProject, Prompt: "prompt", AgentType: "true"}

	if resp := Start(proj, lead); resp.Status != "dispatched" || resp.AttachCmd != "" {
		t.Fatalf("lead: %q attach %q\n%s", resp.Status, resp.AttachCmd, stepNames(resp.Steps))
	}
	if resp := Start(proj, lead); resp.Status != "reattached" || !strings.Contains(stepNames(resp.Steps), "Lead tab already at main:1") {
		t.Fatalf("lead again: %q\n%s", resp.Status, stepNames(resp.Steps))
	}
	if got := windowsOf(t, "main"); len(got) != 2 {
		t.Fatalf("main windows after two spawns = %v", got)
	}
	headless := lead
	headless.Headless = true
	if resp := Start(proj, headless); resp.AttachCmd == "" || strings.Contains(stepNames(resp.Steps), "Lead tab") {
		t.Fatalf("--headless: attach %q\n%s", resp.AttachCmd, stepNames(resp.Steps))
	}

	before := strings.Join(windowsOf(t, "main"), ",") + "|" + strings.Join(windowsOf(t, "work"), ",")
	activeMain, activeWork := activeOf(t, "main"), activeOf(t, "work")
	worker := Spec{Session: "agent-a", IssueSlug: "a", Prompt: "prompt", AgentType: "true"}
	if resp := Start(proj, worker); resp.Status != "dispatched" || resp.AttachCmd == "" {
		t.Fatalf("worker: %q\n%s", resp.Status, stepNames(resp.Steps))
	}
	if got := strings.Join(windowsOf(t, "main"), ","); before[:strings.Index(before, "|")] != got {
		t.Fatalf("worker changed main's windows: %s", got)
	}
	if activeOf(t, "main") != activeMain || activeOf(t, "work") != activeWork {
		t.Fatalf("worker changed a selection: main %q work %q", activeOf(t, "main"), activeOf(t, "work"))
	}

	proj.LeadWindowSession = ""
	tmuxRun(t, "kill-window", "-t", "=work:=lead-p")
	if resp := Start(proj, lead); resp.Status != "dispatched" || resp.AttachCmd == "" {
		t.Fatalf("misconfigured lead: %q attach %q\n%s", resp.Status, resp.AttachCmd, stepNames(resp.Steps))
	}
}
