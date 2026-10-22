package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func init() {
	// No test consults the real tmux server or inherits a dispatched
	// session's identity: nothing is live and nobody is anybody unless a test
	// says so.
	sessionLive = func(*tracker.Project) func(string) bool {
		return func(string) bool { return false }
	}
	liveSessionNames = func(*tracker.Project) map[string]bool { return nil }
	currentTmuxNames = func() (string, string) { return "", "" }
	workingDir = func() (string, error) { return "/home/someone/project", nil }
	startAgent = func(*tracker.Project, agentrun.Spec) agentrun.Response {
		panic("test started a real agent; stub startAgent")
	}
	pasteIntoSession = func(*tracker.Project, string, string, string, string) agentrun.Response {
		panic("test pasted into a real session; stub pasteIntoSession")
	}
	os.Unsetenv("ISSUE_VIEWER_LEAD_SCOPE")
	os.Unsetenv("ISSUE_VIEWER_ISSUE_SLUG")
}

// makeLeadProject builds a project with an epic (ui/epic) and three issues:
// ui/child (parent:), ui/old (legacy umbrella:) and loner (no epic).
func makeLeadProject(t *testing.T) *tracker.Project {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(title, status, extra string) string {
		return "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\n" + extra + "---\n\nBody of " + title + ".\n"
	}
	write("issues/UI/epic.md", issue("Epic", "in progress", ""))
	write("issues/UI/child.md", issue("Child", "in progress", "parent: \"ui/epic\"\n"))
	write("issues/UI/old.md", issue("Old", "backlog", "umbrella: \"ui/epic\"\n"))
	write("issues/loner.md", issue("Loner", "backlog", ""))
	write("workflow.yaml", "worktree: true\nstatuses:\n  - name: \"backlog\"\n  - name: \"in progress\"\n  - name: \"human-testing\"\n  - name: \"done\"\n")
	return &tracker.Project{Slug: "proj", Name: "Proj", IssueDir: filepath.Join(root, "issues"), WorkDir: root, WorkflowFile: filepath.Join(root, "workflow.yaml")}
}

func loadLeadIssue(t *testing.T, proj *tracker.Project, slug string) *tracker.Issue {
	t.Helper()
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		t.Fatal(err)
	}
	issue := tracker.FindIssue(issues, slug)
	if issue == nil {
		t.Fatalf("issue %s not found", slug)
	}
	return issue
}

func captureStart(t *testing.T, status string) *agentrun.Spec {
	t.Helper()
	var got agentrun.Spec
	orig := startAgent
	startAgent = func(proj *tracker.Project, spec agentrun.Spec) agentrun.Response {
		got = spec
		return agentrun.Response{Status: status, Session: spec.Session, Steps: []agentrun.Step{{Name: "stubbed", Status: "ok"}}}
	}
	t.Cleanup(func() { startAgent = orig })
	return &got
}

func withLive(t *testing.T, names ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	origLive, origNames := sessionLive, liveSessionNames
	sessionLive = func(*tracker.Project) func(string) bool { return func(n string) bool { return set[n] } }
	liveSessionNames = func(*tracker.Project) map[string]bool { return set }
	t.Cleanup(func() { sessionLive, liveSessionNames = origLive, origNames })
}

func TestLeadSpawn_ProjectLead(t *testing.T) {
	proj := makeLeadProject(t)
	os.MkdirAll(filepath.Join(proj.WorkDir, "sessions"), 0755)
	legacy := filepath.Join(proj.WorkDir, "sessions", "2026-10-13-lead-session.md")
	os.WriteFile(legacy, []byte("# handoff"), 0644)
	spec := captureStart(t, "dispatched")
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runLead(ctx, []string{"spawn"}); err != nil {
		t.Fatal(err)
	}
	if spec.Session != "lead-proj" || spec.LeadScope != "project" || !spec.Headless || spec.Worktree.Path != "" || spec.WorkDir != proj.WorkDir {
		t.Fatalf("spec = %+v", *spec)
	}
	for _, want := range []string{
		"You are the project lead of Proj. You run in lead-proj",
		"## Your role",
		"- Role: project lead (lead-proj).",
		"read " + legacy + " first",
		"-lead-proj.md: what is live",
	} {
		if !strings.Contains(spec.Prompt, want) {
			t.Errorf("brief missing %q\n%s", want, spec.Prompt)
		}
	}
	if strings.Contains(spec.Prompt, "sub-lead") || strings.Contains(spec.Prompt, "--epic") {
		t.Errorf("brief still mentions sub-leads:\n%s", spec.Prompt)
	}
	if !strings.Contains(stdout.String(), "✓ Spawned project lead lead-proj") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestLeadSpawn_PrintsSwitchAndAttach(t *testing.T) {
	proj := makeLeadProject(t)
	orig := startAgent
	startAgent = func(proj *tracker.Project, spec agentrun.Spec) agentrun.Response {
		return agentrun.Response{Status: "dispatched", Session: spec.Session,
			AttachCmd: agentrun.AttachCmd(proj, spec.Session), SwitchCmd: agentrun.SwitchCmd(proj, spec.Session)}
	}
	t.Cleanup(func() { startAgent = orig })
	t.Setenv("TMUX_TMPDIR", "")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runLead(ctx, []string{"spawn"}); err != nil {
		t.Fatal(err)
	}
	want := "Inside tmux:  tmux switch-client -t lead-proj\nFrom a shell: tmux attach -t lead-proj\n"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestLeadSpawn_ReattachSaysAlreadyLive(t *testing.T) {
	proj := makeLeadProject(t)
	captureStart(t, "reattached")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runLead(ctx, []string{"spawn"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Already live: project lead lead-proj") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// --epic is gone: every spelling gets the same one-line answer, nothing is
// started and the epic's frontmatter is untouched.
func TestLeadSpawn_EpicRemoved(t *testing.T) {
	proj := makeLeadProject(t)
	for _, args := range [][]string{
		{"spawn", "--epic", "ui/epic"},
		{"spawn", "-epic", "x"},
		{"spawn", "--attach", "--epic=ui/epic"},
	} {
		ctx, stdout, _ := newTestContext(proj, false)
		err := runLead(ctx, args)
		if err == nil || err.Error() != "sub-leads were removed (one lead per project); issue-cli --project proj lead spawn starts the project lead" {
			t.Fatalf("%v: err = %v", args, err)
		}
		if strings.Contains(err.Error(), "\n") || stdout.Len() != 0 {
			t.Fatalf("%v: want one line and no output, got %q / %q", args, err, stdout.String())
		}
	}
	epic := loadLeadIssue(t, proj, "ui/epic")
	if epic.Field("base") != "" || epic.Field("lead") != "" {
		t.Fatalf("epic frontmatter base=%q lead=%q", epic.Field("base"), epic.Field("lead"))
	}
}

func TestLeadSpawn_Refusals(t *testing.T) {
	proj := makeLeadProject(t)
	captureStart(t, "dispatched")
	ctx, _, _ := newTestContext(proj, false)

	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/child")
	if err := runLead(ctx, []string{"spawn"}); err == nil || !strings.Contains(err.Error(), "workers can't spawn leads") {
		t.Fatalf("worker: err = %v", err)
	}
}

func TestLeadWhoami_Roles(t *testing.T) {
	proj := makeLeadProject(t)
	withLive(t, "lead-proj", "lead-ui-epic")
	run := func() string {
		t.Helper()
		ctx, stdout, _ := newTestContext(proj, false)
		if err := runLead(ctx, []string{"whoami"}); err != nil {
			t.Fatal(err)
		}
		return stdout.String()
	}
	expect := func(out string, wants ...string) {
		t.Helper()
		for _, w := range wants {
			if !strings.Contains(out, w) {
				t.Errorf("whoami missing %q:\n%s", w, out)
			}
		}
	}

	expect(run(), "Role: unmanaged", "Reports to: lead-proj (live)", "issue-cli --project proj lead spawn")

	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "project")
	expect(run(), "Role: project lead (lead-proj)", "Reports to: the human", "May tick: gates the human approved in your chat")

	// An old sub-lead's scope is no lead: it falls through to the other
	// signals (here none, so unmanaged).
	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "ui/epic")
	expect(run(), "Role: unmanaged", "Reports to: lead-proj (live)")

	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "")
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/old")
	expect(run(), "Role: worker (agent-ui-old)", "Reports to: lead-proj (live)", "May tick: nothing", "Hint: your issue links its epic with umbrella:")

	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "loner")
	expect(run(), "Role: worker (agent-loner)", "Reports to: lead-proj (live)")
}

func TestLeadWhoami_TmuxNameSignal(t *testing.T) {
	proj := makeLeadProject(t)
	orig := currentTmuxNames
	t.Cleanup(func() { currentTmuxNames = orig })
	run := func() string {
		t.Helper()
		ctx, stdout, _ := newTestContext(proj, false)
		if err := runLead(ctx, []string{"whoami"}); err != nil {
			t.Fatal(err)
		}
		return stdout.String()
	}

	currentTmuxNames = func() (string, string) { return "work", "lead-proj" }
	if out := run(); !strings.Contains(out, "Role: project lead (lead-proj)  [detected from tmux name lead-proj]") {
		t.Fatalf("stdout = %q", out)
	}
	// A leftover lead-<epic> window is not a lead.
	currentTmuxNames = func() (string, string) { return "work", "lead-ui-epic" }
	if out := run(); !strings.Contains(out, "Role: unmanaged") {
		t.Fatalf("stdout = %q", out)
	}
}

// lead list shows the project lead and the open epics, and never suggests a
// sub-lead, even for an epic that still carries lead:/base:.
func TestLeadList(t *testing.T) {
	proj := makeLeadProject(t)
	epic := loadLeadIssue(t, proj, "ui/epic")
	tracker.SetFrontmatterField(epic.FilePath, "lead", "lead-ui-epic", false)
	tracker.SetFrontmatterField(epic.FilePath, "base", "base/ui/epic", false)
	withLive(t, "lead-ui-epic", "agent-ui-child")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runLead(ctx, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	want := "== Lead ==\n  lead-proj                                project lead  not running\n\n== Epics ==\n  ui/epic (in progress) · children running 1/2\n\nThe project lead is not running. Start it: issue-cli --project proj lead spawn\n"
	if out != want {
		t.Errorf("list:\n%s\nwant:\n%s", out, want)
	}

	ctx, stdout, _ = newTestContext(proj, true)
	if err := runLead(ctx, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	var got leadListJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	wantJSON := leadListJSON{Lead: leadRow{Session: "lead-proj"}, Epics: []epicRow{{Slug: "ui/epic", Title: "Epic", Status: "in progress", ChildrenRunning: 1, ChildrenTotal: 2}}}
	if !reflect.DeepEqual(got, wantJSON) {
		t.Errorf("json = %+v", got)
	}
}

func TestLeadMessage(t *testing.T) {
	proj := makeLeadProject(t)
	epic := loadLeadIssue(t, proj, "ui/epic")
	tracker.SetFrontmatterField(epic.FilePath, "lead", "lead-ui-epic", false)
	var gotSession, gotText string
	orig := pasteIntoSession
	pasteIntoSession = func(_ *tracker.Project, session, text, file, _ string) agentrun.Response {
		gotSession, gotText = session, text
		if !strings.Contains(file, filepath.Join(".agent-logs", session, "messages")) {
			t.Errorf("message file %q not under the session's messages dir", file)
		}
		if session == "agent-gone" {
			return agentrun.Response{Status: "no-session", Session: session}
		}
		return agentrun.Response{Status: "sent", Session: session}
	}
	t.Cleanup(func() { pasteIntoSession = orig })

	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "project")
	for target, want := range map[string]string{
		"project":    "lead-proj",
		"ui/epic":    "agent-ui-epic",
		"ui/child":   "agent-ui-child",
		"lead-other": "lead-other",
	} {
		ctx, _, _ := newTestContext(proj, false)
		if err := runLead(ctx, []string{"message", target, "hello", "there"}); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if gotSession != want || gotText != "[from lead-proj] hello there" {
			t.Errorf("%s → %q %q, want %q", target, gotSession, gotText, want)
		}
	}

	ctx, _, _ := newTestContext(proj, false)
	if err := runLead(ctx, []string{"message", "agent-gone", "hi"}); err == nil || !strings.Contains(err.Error(), "is not running; nothing was sent") {
		t.Fatalf("no session: err = %v", err)
	}

	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "")
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/child")
	if err := runLead(ctx, []string{"message", "project", "hi"}); err == nil || !strings.Contains(err.Error(), "workers can't message sessions") {
		t.Fatalf("worker: err = %v", err)
	}
}

func TestApprove(t *testing.T) {
	proj := makeLeadProject(t)
	run := func(args ...string) error {
		ctx, _, _ := newTestContext(proj, false)
		return runApprove(ctx, args)
	}

	if err := run("ui/child", "--to", "backlog"); err == nil || !strings.Contains(err.Error(), "requires --to and --quote") {
		t.Fatalf("no quote: err = %v", err)
	}
	if err := run("ui/child", "--to", "human-testing", "--quote", "ok"); err == nil || !strings.Contains(err.Error(), "Human Testing is approved by the human") {
		t.Fatalf("human testing: err = %v", err)
	}
	if err := run("ui/child", "--to", "nowhere", "--quote", "ok"); err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Fatalf("unknown status: err = %v", err)
	}

	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/child")
	if err := run("ui/child", "--to", "backlog", "--quote", "ok"); err == nil || !strings.Contains(err.Error(), "workers never tick gates") {
		t.Fatalf("worker: err = %v", err)
	}

	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "")
	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "project")
	if err := run("ui/child", "--to", "done", "--quote", "ship it"); err != nil {
		t.Fatal(err)
	}
	child := loadLeadIssue(t, proj, "ui/child")
	if child.HumanApproval != "done" {
		t.Fatalf("human_approval = %q", child.HumanApproval)
	}
	comments, _ := tracker.LoadComments(child.FilePath)
	if len(comments) != 1 || !strings.HasPrefix(comments[0].Text, `approval: "ship it" (in the lead chat, `) || !strings.HasSuffix(comments[0].Text, "recorded by lead-proj") {
		t.Fatalf("comments = %+v", comments)
	}

	// Approving again never toggles the approval off.
	if err := run("ui/child", "--to", "done", "--quote", "yes really"); err != nil {
		t.Fatal(err)
	}
	if child = loadLeadIssue(t, proj, "ui/child"); child.HumanApproval != "done" {
		t.Fatalf("second approve toggled human_approval to %q", child.HumanApproval)
	}
}

func TestShowPrintsLeadAndUmbrellaHint(t *testing.T) {
	proj := makeLeadProject(t)
	withLive(t, "lead-ui-epic", "lead-proj")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{"ui/old"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Lead: lead-proj (live)\n") {
		t.Errorf("missing lead line:\n%s", out)
	}
	if !strings.Contains(out, "Hint: umbrella: is a legacy alias for parent:. Rename it: issue-cli --project proj set-meta ui/old --key parent --value ui/epic") {
		t.Errorf("missing umbrella hint:\n%s", out)
	}
}
