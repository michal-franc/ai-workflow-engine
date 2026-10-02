package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// makeGateFixture builds a project whose workflow has two approval gates
// (in design → backlog, backlog → in progress) and one issue at status with
// the given Design checkboxes and human_approval.
func makeGateFixture(t *testing.T, status, approval string, designChecked bool) (*tracker.Project, string) {
	t.Helper()
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	workflowPath := filepath.Join(dir, "workflow.yaml")
	workflow := strings.TrimSpace(`
statuses:
  - name: "in design"
  - name: "backlog"
  - name: "in progress"
  - name: "done"
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: validate
        rule: "section_checkboxes_checked: Design"
      - type: require_human_approval
        status: "backlog"
      - type: set_fields
        field: "assignee"
        value: ""
  - from: "backlog"
    to: "in progress"
    actions:
      - type: require_human_approval
        status: "in progress"
      - type: validate
        rule: has_assignee
  - from: "in progress"
    to: "done"
    actions:
      - type: require_human_approval
        status: "done"
`)
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	mark := " "
	if designChecked {
		mark = "x"
	}
	issuePath := filepath.Join(systemDir, "sample.md")
	issue := fmt.Sprintf(`---
title: "sample"
status: %q
system: "CLI"
human_approval: %q
---

## Design
- [x] Approach documented
- [%s] Dependencies identified
`, status, approval, mark)
	if err := os.WriteFile(issuePath, []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: issuesDir, WorkflowFile: workflowPath}, issuePath
}

// fakeClock drives --wait deterministically: Sleep advances Now and runs an
// optional hook on the given poll (1-based) to simulate a human acting.
type fakeClock struct {
	now    time.Time
	sleeps int
	onPoll map[int]func()
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps++
	c.now = c.now.Add(d)
	if hook := c.onPoll[c.sleeps]; hook != nil {
		hook()
	}
}

func newWaitContext(proj *tracker.Project, clock *fakeClock) (*Context, *bytes.Buffer, *bytes.Buffer) {
	ctx, stdout, stderr := newTestContext(proj, false)
	ctx.Now = clock.Now
	ctx.Sleep = clock.Sleep
	return ctx, stdout, stderr
}

func approve(t *testing.T, issuePath, status string, at time.Time) {
	t.Helper()
	if err := tracker.UpdateIssueFrontmatter(issuePath, tracker.IssueUpdate{HumanApproval: &status}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := tracker.RecordApproval(issuePath, status, at); err != nil {
		t.Fatalf("record approval: %v", err)
	}
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("want *exitCodeError, got %T: %v", err, err)
	}
	return codeErr.Code
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestTransitionDryRunListsEveryProblem(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", false)
	before := readFile(t, issuePath)
	ctx, stdout, _ := newTestContext(proj, false)

	err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--dry-run"})
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	out := stdout.String()
	assertContains(t, out, "✗ 2 unmet requirement(s)")
	assertContains(t, out, "[validator] Validate section Design checkboxes are checked")
	assertContains(t, out, "issue-cli check cli/sample D2")
	if strings.Count(out, "issue-cli check cli/sample D2") != 1 {
		t.Fatalf("checkbox fix should appear once, not repeated after the validator message:\n%s", out)
	}
	assertContains(t, out, `[approval] Must be human-approved for "backlog"`)
	assertContains(t, out, "#approve-backlog")
	assertContains(t, out, `issue-cli transition cli/sample --to "backlog" --wait --timeout 9m`)
	assertContains(t, out, "Nothing was changed.")
	if after := readFile(t, issuePath); after != before {
		t.Fatalf("dry run modified the issue:\n%s", after)
	}
}

func TestTransitionDryRunReady(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "backlog", true)
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--dry-run"}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Ready: in design → backlog (dry run — nothing was changed)")
	assertContains(t, out, "Side-effect: clears assignee")
	if issue := loadIssueByPath(t, proj.IssueDir, filepath.Join(proj.IssueDir, "CLI", "sample.md")); issue.Status != "in design" {
		t.Fatalf("status = %q, dry run must not transition", issue.Status)
	}
}

func TestTransitionDryRunJSON(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, stdout, _ := newTestContext(proj, true)

	err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--dry-run"})
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	var out dryRunOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout.String())
	}
	if !out.DryRun || out.Ready || out.From != "in design" || out.To != "backlog" {
		t.Fatalf("unexpected header: %+v", out)
	}
	if len(out.Problems) != 1 || out.Problems[0].Kind != "approval" {
		t.Fatalf("problems = %+v, want one approval problem", out.Problems)
	}
}

func TestTransitionDryRunWrongOrder(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, stdout, _ := newTestContext(proj, false)

	err := runTransition(ctx, []string{"cli/sample", "--to", "in progress", "--dry-run"})
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	assertContains(t, stdout.String(), "[order]")
	assertContains(t, stdout.String(), `must go to "backlog" next`)
}

func TestTransitionWaitFailsFastOnMachineCheck(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", false)
	clock := &fakeClock{now: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)}
	ctx, stdout, stderr := newWaitContext(proj, clock)

	err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--wait"})
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	assertContains(t, err.Error(), "Not waiting")
	assertContains(t, stdout.String(), "issue-cli check cli/sample D2")
	if strings.Contains(stdout.String(), "[approval]") {
		t.Fatalf("fail-fast report should list only machine problems:\n%s", stdout.String())
	}
	if strings.Contains(stderr.String(), "Waiting for") || clock.sleeps != 0 {
		t.Fatalf("must not wait when a machine check fails (sleeps=%d, stderr=%q)", clock.sleeps, stderr.String())
	}
	if store, _ := tracker.LoadStats(issuePath); store.PendingWait != nil {
		t.Fatalf("fail-fast must not record a pending wait: %+v", store.PendingWait)
	}
}

func TestTransitionWaitBlocksUntilApproved(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	clock.onPoll = map[int]func(){3: func() { approve(t, issuePath, "backlog", clock.now) }}
	ctx, stdout, stderr := newWaitContext(proj, clock)

	if err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--wait"}); err != nil {
		t.Fatalf("wait: %v", err)
	}
	assertContains(t, stderr.String(), `Waiting for human approval for "backlog" — approve: `)
	assertContains(t, stderr.String(), "(timeout: none)")
	assertContains(t, stdout.String(), "✓ in design → backlog")
	assertContains(t, stdout.String(), "✓ Waited: ")
	if clock.sleeps != 3 {
		t.Fatalf("sleeps = %d, want 3 (transition on the poll after approval)", clock.sleeps)
	}

	store, err := tracker.LoadStats(issuePath)
	if err != nil || len(store.Transitions) != 1 {
		t.Fatalf("stats = %+v, err %v", store, err)
	}
	row := store.Transitions[0]
	if row.WaitStartedAt == nil || !row.WaitStartedAt.Equal(start) {
		t.Fatalf("wait_started_at = %v, want %v", row.WaitStartedAt, start)
	}
	if row.ApprovedAt == nil || !row.ApprovedAt.Equal(start.Add(6*time.Second)) {
		t.Fatalf("approved_at = %v, want %v", row.ApprovedAt, start.Add(6*time.Second))
	}
	if store.PendingWait != nil || store.LastApproval != nil {
		t.Fatalf("scratch state not cleared: %+v %+v", store.PendingWait, store.LastApproval)
	}
}

func TestTransitionWaitAlreadyApprovedDoesNotWait(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "backlog", true)
	clock := &fakeClock{now: time.Now()}
	ctx, stdout, stderr := newWaitContext(proj, clock)

	if err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--wait"}); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if clock.sleeps != 0 || stderr.Len() != 0 {
		t.Fatalf("should transition immediately (sleeps=%d, stderr=%q)", clock.sleeps, stderr.String())
	}
	if strings.Contains(stdout.String(), "Waited:") {
		t.Fatalf("no wait happened, so no Waited line:\n%s", stdout.String())
	}
	if store, _ := tracker.LoadStats(issuePath); store.Transitions[0].WaitStartedAt != nil {
		t.Fatalf("wait_started_at set without a wait")
	}
}

func TestTransitionWaitTimeoutExits3AndKeepsStart(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	before := readFile(t, issuePath)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	ctx, _, _ := newWaitContext(proj, clock)

	err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--wait", "--timeout", "5s"})
	if code := exitCode(t, err); code != exitWaitTimeout {
		t.Fatalf("exit code = %d, want %d", code, exitWaitTimeout)
	}
	assertContains(t, err.Error(), `Still waiting for human approval for "backlog" after 5s — nothing changed.`)
	assertContains(t, err.Error(), `issue-cli transition cli/sample --to "backlog" --wait --timeout 5s`)
	if after := readFile(t, issuePath); after != before {
		t.Fatalf("timeout modified the issue")
	}

	// Re-run after the timeout: the original start time is kept.
	clock.now = start.Add(time.Hour)
	clock.onPoll = map[int]func(){clock.sleeps + 1: func() { approve(t, issuePath, "backlog", clock.now) }}
	ctx2, stdout, _ := newWaitContext(proj, clock)
	if err := runTransition(ctx2, []string{"cli/sample", "--to", "backlog", "--wait", "--timeout", "5s"}); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Waited: 1h0m")
	store, _ := tracker.LoadStats(issuePath)
	if got := store.Transitions[0].WaitStartedAt; got == nil || !got.Equal(start) {
		t.Fatalf("wait_started_at = %v, want original %v", got, start)
	}
}

func TestTransitionWaitStopsWhenMachineCheckBreaks(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	clock := &fakeClock{now: time.Now()}
	clock.onPoll = map[int]func(){1: func() {
		// A human unticks a box and approves in the same breath.
		body := strings.Replace(readFile(t, issuePath), "- [x] Dependencies identified", "- [ ] Dependencies identified", 1)
		if err := os.WriteFile(issuePath, []byte(body), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		approve(t, issuePath, "backlog", clock.now)
	}}
	ctx, _, _ := newWaitContext(proj, clock)

	err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--wait"})
	if err == nil || errors.Is(err, tracker.ErrApprovalMissing) {
		t.Fatalf("want a validation error, got %v", err)
	}
	assertContains(t, err.Error(), "box still open")
	if issue := loadIssueByPath(t, proj.IssueDir, issuePath); issue.Status != "in design" {
		t.Fatalf("status = %q, must not transition", issue.Status)
	}
}

func TestTransitionWaitStopsWhenStatusMoves(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	clock := &fakeClock{now: time.Now()}
	clock.onPoll = map[int]func(){1: func() {
		moved := "done"
		if err := tracker.UpdateIssueFrontmatter(issuePath, tracker.IssueUpdate{Status: &moved}); err != nil {
			t.Fatalf("move: %v", err)
		}
	}}
	ctx, _, _ := newWaitContext(proj, clock)

	err := runTransition(ctx, []string{"cli/sample", "--to", "backlog", "--wait"})
	if err == nil {
		t.Fatal("want an error when the status moves under the wait")
	}
	assertContains(t, err.Error(), `cannot transition from "done"`)
}

func TestStartWaitBlocksUntilApproved(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "backlog", "", true)
	clock := &fakeClock{now: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)}
	clock.onPoll = map[int]func(){2: func() { approve(t, issuePath, "in progress", clock.now) }}
	ctx, stdout, stderr := newWaitContext(proj, clock)

	if err := runStart(ctx, []string{"cli/sample", "--wait", "--timeout", "1m"}); err != nil {
		t.Fatalf("start --wait: %v", err)
	}
	assertContains(t, stderr.String(), `Waiting for human approval for "in progress"`)
	assertContains(t, stderr.String(), "(timeout: 1m0s)")
	assertContains(t, stdout.String(), "AUTO-ADVANCED  backlog → in progress")
	assertContains(t, stdout.String(), "✓ Waited: 4s")
	issue := loadIssueByPath(t, proj.IssueDir, issuePath)
	if issue.Status != "in progress" || issue.Assignee == "" {
		t.Fatalf("status=%q assignee=%q, want in progress + claimed", issue.Status, issue.Assignee)
	}
	store, _ := tracker.LoadStats(issuePath)
	if len(store.Transitions) != 1 || store.Transitions[0].To != "in progress" || store.Transitions[0].ApprovedAt == nil {
		t.Fatalf("start must record its transition with approval time: %+v", store.Transitions)
	}
}

func TestStartWaitTimeout(t *testing.T) {
	proj, _ := makeGateFixture(t, "backlog", "", true)
	clock := &fakeClock{now: time.Now()}
	ctx, _, _ := newWaitContext(proj, clock)

	err := runStart(ctx, []string{"cli/sample", "--wait", "--timeout", "3s"})
	if code := exitCode(t, err); code != exitWaitTimeout {
		t.Fatalf("exit code = %d, want %d", code, exitWaitTimeout)
	}
	assertContains(t, err.Error(), "issue-cli start cli/sample --wait --timeout 3s")
}

func TestWaitFlagValidation(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	cases := map[string][]string{
		"--timeout only applies with --wait": {"--timeout", "1m"},
		"--interval must be positive":        {"--wait", "--interval", "0s"},
		"--timeout must not be negative":     {"--wait", "--timeout", "-1s"},
		"mutually exclusive":                 {"--wait", "--dry-run"},
	}
	for want, flags := range cases {
		ctx, _, _ := newTestContext(proj, false)
		err := runTransition(ctx, append([]string{"cli/sample", "--to", "backlog"}, flags...))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("flags %v: err = %v, want %q", flags, err, want)
		}
	}
}

func TestNextBlockSuggestsWaitForGatedStep(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "backlog", true)
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runTransition(ctx, []string{"cli/sample", "--to", "backlog"}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// backlog is a handoff status, so the gated next step goes through start.
	assertContains(t, stdout.String(), "  issue-cli start cli/sample --wait --timeout 9m\n")
	assertContains(t, stdout.String(), `(needs human approval for "in progress" — blocks until approved; exit 3 = still waiting, re-run it)`)
}

func TestNextStepCommand(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	wf := proj.LoadWorkflow()

	cmd, note := nextStepCommand(wf, "cli/x", "in design", "backlog")
	if cmd != `issue-cli transition cli/x --to "backlog" --wait --timeout 9m` || note == "" {
		t.Fatalf("gated non-handoff: %q / %q", cmd, note)
	}
	cmd, _ = nextStepCommand(wf, "cli/x", "backlog", "in progress")
	if cmd != "issue-cli start cli/x --wait --timeout 9m" {
		t.Fatalf("gated handoff: %q", cmd)
	}
	cmd, note = nextStepCommand(wf, "cli/x", "in progress", "done")
	if cmd != `issue-cli transition cli/x --to "done"` || note != "" {
		t.Fatalf("done must never suggest --wait: %q / %q", cmd, note)
	}
}

func TestApprovalErrorSuggestsWaitCommand(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, _, _ := newTestContext(proj, false)

	err := decorateApprovalError(runTransition(ctx, []string{"cli/sample", "--to", "backlog"}), ctx)
	if !errors.Is(err, tracker.ErrApprovalMissing) {
		t.Fatalf("want approval error, got %v", err)
	}
	assertContains(t, err.Error(), "To block until it is approved instead of retrying:")
	assertContains(t, err.Error(), `issue-cli transition cli/sample --to "backlog" --wait --timeout 9m`)

	ctx, _, _ = newTestContext(proj, false)
	proj2, _ := makeGateFixture(t, "backlog", "", true)
	ctx.Project = proj2
	err = decorateApprovalError(runStart(ctx, []string{"cli/sample"}), ctx)
	assertContains(t, err.Error(), "issue-cli start cli/sample --wait --timeout 9m")
}

func TestPrintRetryHint(t *testing.T) {
	var buf bytes.Buffer
	printRetryHint(&buf, 3, fmt.Errorf("wrapped: %w", tracker.ErrApprovalMissing))
	assertContains(t, buf.String(), "failed 3 times")
	assertContains(t, buf.String(), "run the --wait command above")

	buf.Reset()
	printRetryHint(&buf, 3, errors.New("something else"))
	assertContains(t, buf.String(), "try a different approach")
}
