package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// makeGateProject is makeLeadProject with a human gate on in progress →
// human-testing: ui/child (in progress) waits on it, loner (backlog) does
// not. Nothing is live.
func makeGateProject(t *testing.T) *tracker.Project {
	t.Helper()
	proj := makeLeadProject(t)
	mustWrite(t, proj.WorkflowFile, `worktree: true
statuses:
  - name: "backlog"
  - name: "in progress"
  - name: "human-testing"
  - name: "done"
transitions:
  - from: "in progress"
    to: "human-testing"
    actions:
      - type: "require_human_approval"
        status: "human-testing"
`)
	orig := agentrun.HasSession
	agentrun.HasSession = func(string) bool { return false }
	t.Cleanup(func() { agentrun.HasSession = orig })
	// Attach hints carry TMUX_TMPDIR when it is set.
	t.Setenv("TMUX_TMPDIR", "")
	return proj
}

// exitOf is the process exit code err maps to (0 for nil).
func exitOf(err error) int {
	if err == nil {
		return 0
	}
	return processExitCode(err)
}

// dispatch starts the same spec ▶ does: agentrun.DispatchSpec's prompt,
// session and worktree, rooted at the project checkout.
func TestDispatch_StartsTheBoardsSpec(t *testing.T) {
	proj := makeGateProject(t)
	spec := captureStart(t, "dispatched")
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runDispatch(ctx, []string{"loner", "--agent", "codex", "--quote", "start loner"}); err != nil {
		t.Fatal(err)
	}
	want, refused := agentrun.DispatchSpec(proj, loadLeadIssue(t, proj, "loner"), "codex", proj.WorkDir, false)
	if refused != nil {
		t.Fatalf("loner refused: %+v", refused)
	}
	if spec.Prompt != want.Prompt || spec.Session != "agent-loner" || spec.Worktree != want.Worktree || spec.AgentType != "codex" || spec.WorkDir != proj.WorkDir {
		t.Fatalf("spec = %+v\nwant  %+v", *spec, want)
	}
	if spec.RepromptHint != "issue-cli --project proj dispatch loner --reprompt --quote \"<the human's words>\"" {
		t.Fatalf("RepromptHint = %q", spec.RepromptHint)
	}
	out := stdout.String()
	for _, w := range []string{"✓ Dispatched agent-loner for loner", "  ✓ stubbed", "Inside tmux:  tmux switch-client -t agent-loner", "From a shell: tmux attach -t agent-loner"} {
		if !strings.Contains(out, w) {
			t.Errorf("stdout missing %q:\n%s", w, out)
		}
	}
}

func TestDispatch_Reattached(t *testing.T) {
	proj := makeGateProject(t)
	captureStart(t, "reattached")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runDispatch(ctx, []string{"loner", "--quote", "go"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "✓ Already live: agent-loner (re-attached, prompt not re-sent)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// An issue that only waits on the owner's approval is refused with exit 3,
// the gate, the approve link and --force; nothing starts. --force starts it,
// and a live worker is re-attached without the refusal.
func TestDispatch_RefusesAtAReadyGate(t *testing.T) {
	proj := makeGateProject(t)
	proj.ViewerURL = "https://issues.example.com"
	started := false
	orig := startAgent
	startAgent = func(_ *tracker.Project, spec agentrun.Spec) agentrun.Response {
		started = true
		return agentrun.Response{Status: "dispatched", Session: spec.Session}
	}
	t.Cleanup(func() { startAgent = orig })

	ctx, stdout, _ := newTestContext(proj, false)
	err := runDispatch(ctx, []string{"ui/child", "--quote", "start child"})
	if exitOf(err) != 3 || started {
		t.Fatalf("err = %v (exit %d), started = %v; want exit 3 and nothing started", err, exitOf(err), started)
	}
	for _, w := range []string{
		`ui/child waits on the owner's approval for "human-testing"`,
		"A human approves at: https://issues.example.com/p/proj/issue/ui/child#approve-human-testing",
		"Dispatch anyway:     issue-cli --project proj dispatch ui/child --force --quote \"start child\"",
	} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("refusal missing %q:\n%s", w, err)
		}
	}

	ctx, stdout, _ = newTestContext(proj, true)
	err = runDispatch(ctx, []string{"ui/child", "--quote", "start child"})
	var got agentrun.Response
	if jerr := json.Unmarshal(stdout.Bytes(), &got); jerr != nil || exitOf(err) != 3 || got.Status != agentrun.StatusWaitingOnGate || got.Gate != "human-testing" {
		t.Fatalf("--json: exit %d, %+v (%v)", exitOf(err), got, jerr)
	}

	ctx, _, _ = newTestContext(proj, false)
	if err := runDispatch(ctx, []string{"ui/child", "--force", "--quote", "start child anyway"}); err != nil || !started {
		t.Fatalf("--force: err = %v, started = %v", err, started)
	}

	started = false
	agentrun.HasSession = func(string) bool { return true }
	ctx, _, _ = newTestContext(proj, false)
	if err := runDispatch(ctx, []string{"ui/child", "--quote", "go"}); err != nil || !started {
		t.Fatalf("live worker: err = %v, started = %v; want Start to re-attach", err, started)
	}
}

func TestDispatch_Reprompt(t *testing.T) {
	proj := makeGateProject(t)
	var gotSlug string
	orig := repromptWorker
	t.Cleanup(func() { repromptWorker = orig })

	repromptWorker = func(_ *tracker.Project, issue *tracker.Issue, workDir string) agentrun.Response {
		gotSlug = issue.Slug
		return agentrun.Response{Status: "reprompted", Session: "agent-loner", Steps: []agentrun.Step{{Name: "Prompt delivered", Status: "ok"}}}
	}
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runDispatch(ctx, []string{"loner", "--reprompt", "--quote", "re-send it"}); err != nil || gotSlug != "loner" {
		t.Fatalf("err = %v, slug %q", err, gotSlug)
	}
	if !strings.Contains(stdout.String(), "✓ Re-sent the briefing to agent-loner") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	// The real Reprompt with nothing live: exit 1, the dispatch hint.
	repromptWorker = agentrun.Reprompt
	ctx, stdout, _ = newTestContext(proj, false)
	err := runDispatch(ctx, []string{"loner", "--reprompt", "--quote", "re-send it"})
	if exitOf(err) != 1 || !strings.Contains(err.Error(), "no live worker for loner") {
		t.Fatalf("no session: err = %v (exit %d)", err, exitOf(err))
	}
	if !strings.Contains(stdout.String(), "dispatch the issue instead: issue-cli --project proj dispatch loner") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(proj.WorkDir + "/.worktrees"); !os.IsNotExist(err) {
		t.Fatalf("reprompt with no session created a worktree (err %v)", err)
	}
}

func TestDispatch_Refusals(t *testing.T) {
	proj := makeGateProject(t)
	captureStart(t, "dispatched")
	for _, args := range [][]string{
		{"loner", "--agent", "gpt", "--quote", "go"},
		{"loner", "--reprompt", "--force", "--quote", "go"},
		{"loner", "--reprompt", "--agent", "codex", "--quote", "go"},
		{"--agent", "codex", "--quote", "go"},
		// One explicit slug per call: nothing fans out over an epic.
		{"ui/epic", "ui/child", "--quote", "go"},
	} {
		ctx, _, _ := newTestContext(proj, false)
		if err := runDispatch(ctx, args); exitOf(err) != 2 {
			t.Errorf("%v: err = %v (exit %d), want a usage error", args, err, exitOf(err))
		}
	}

	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/child")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDispatch(ctx, []string{"loner", "--quote", "the human said so"}); err == nil || !strings.Contains(err.Error(), "workers can't dispatch") {
		t.Fatalf("worker: err = %v", err)
	}
}

// Without the human's words nothing starts: exit 2, no agent, no comment.
func TestDispatch_NeedsQuote(t *testing.T) {
	proj := makeGateProject(t)
	started := false
	orig := startAgent
	startAgent = func(*tracker.Project, agentrun.Spec) agentrun.Response {
		started = true
		return agentrun.Response{Status: "dispatched"}
	}
	t.Cleanup(func() { startAgent = orig })
	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "project")
	for _, args := range [][]string{{"loner"}, {"loner", "--quote", "  "}, {"loner", "--force"}, {"loner", "--reprompt"}} {
		ctx, _, _ := newTestContext(proj, false)
		err := runDispatch(ctx, args)
		if exitOf(err) != 2 || !strings.Contains(err.Error(), "dispatch needs --quote") || !strings.Contains(err.Error(), "Nothing was started") {
			t.Errorf("%v: err = %v (exit %d), want the --quote usage error", args, err, exitOf(err))
		}
	}
	if started {
		t.Fatal("dispatch without --quote started an agent")
	}
	if cs := comments(t, loadLeadIssue(t, proj, "loner").FilePath); len(cs) != 0 {
		t.Fatalf("comments = %+v", cs)
	}
}

// A dispatch leaves the human's words on the issue: dispatch: "<quote>"
// (<how>, <date>), signed by the lead; a re-attach starts nothing and leaves
// none.
func TestDispatch_LeavesTheQuote(t *testing.T) {
	proj := makeGateProject(t)
	status := "dispatched"
	orig := startAgent
	startAgent = func(_ *tracker.Project, spec agentrun.Spec) agentrun.Response {
		return agentrun.Response{Status: status, Session: spec.Session}
	}
	t.Cleanup(func() { startAgent = orig })
	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "project")
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

	ctx, stdout, _ := newTestContext(proj, false)
	ctx.Now = func() time.Time { return now }
	if err := runDispatch(ctx, []string{"loner", "--quote", "dispatch agents for loner"}); err != nil {
		t.Fatal(err)
	}
	want := `dispatch: "dispatch agents for loner" (lead chat, 2026-10-24) — recorded by lead-proj`
	path := loadLeadIssue(t, proj, "loner").FilePath
	if cs := comments(t, path); len(cs) != 1 || cs[0].Text != want {
		t.Fatalf("comments = %+v\nwant %q", cs, want)
	}
	assertContains(t, stdout.String(), "  comment: "+want)

	status = "reattached"
	ctx, _, _ = newTestContext(proj, false)
	if err := runDispatch(ctx, []string{"loner", "--quote", "again", "--how", "on a call"}); err != nil {
		t.Fatal(err)
	}
	if cs := comments(t, path); len(cs) != 1 {
		t.Fatalf("a re-attach left a comment: %+v", cs)
	}

	// The owner's own shell: no role, the default how says so.
	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "")
	status = "dispatched"
	ctx, _, _ = newTestContext(proj, false)
	ctx.Now = func() time.Time { return now }
	if err := runDispatch(ctx, []string{"ui/old", "--quote", "start old"}); err != nil {
		t.Fatal(err)
	}
	if cs := comments(t, loadLeadIssue(t, proj, "ui/old").FilePath); len(cs) != 1 || cs[0].Text != `dispatch: "start old" (owner's shell, 2026-10-24)` {
		t.Fatalf("owner's comments = %+v", cs)
	}
}
