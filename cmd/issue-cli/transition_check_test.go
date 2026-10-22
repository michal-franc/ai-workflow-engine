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
	assertContains(t, out, "tell the human what to approve, then stop and end your turn; the approval nudge resumes you")
	assertNotContains(t, out, "--wait")
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

func TestNextBlockTellsAgentToStopAtGatedStep(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "backlog", true)
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runTransition(ctx, []string{"cli/sample", "--to", "backlog"}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// backlog is a handoff status, so the gated next step goes through start.
	assertContains(t, stdout.String(), "  issue-cli start cli/sample\n")
	assertContains(t, stdout.String(), `(needs human approval for "in progress" — tell the human what to approve, then stop and end your turn; run it once they say it is approved)`)
}

func TestNextStepCommand(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	wf := proj.LoadWorkflow()

	cmd, note := nextStepCommand(wf, "cli/x", "in design", "backlog")
	if cmd != `issue-cli transition cli/x --to "backlog"` || note == "" {
		t.Fatalf("gated non-handoff: %q / %q", cmd, note)
	}
	cmd, _ = nextStepCommand(wf, "cli/x", "backlog", "in progress")
	if cmd != "issue-cli start cli/x" {
		t.Fatalf("gated handoff: %q", cmd)
	}
	cmd, note = nextStepCommand(wf, "cli/x", "in progress", "done")
	if cmd != `issue-cli transition cli/x --to "done"` || note != "" {
		t.Fatalf("done must never carry an approval note: %q / %q", cmd, note)
	}
}

func TestApprovalErrorTellsAgentToStop(t *testing.T) {
	proj, _ := makeGateFixture(t, "in design", "", true)
	ctx, _, _ := newTestContext(proj, false)

	err := decorateApprovalError(runTransition(ctx, []string{"cli/sample", "--to", "backlog"}), ctx)
	if !errors.Is(err, tracker.ErrApprovalMissing) {
		t.Fatalf("want approval error, got %v", err)
	}
	assertContains(t, err.Error(), "then stop and end your turn")
	assertNotContains(t, err.Error(), "--wait")

	ctx, _, _ = newTestContext(proj, false)
	proj2, _ := makeGateFixture(t, "backlog", "", true)
	ctx.Project = proj2
	err = decorateApprovalError(runStart(ctx, []string{"cli/sample"}), ctx)
	assertNotContains(t, err.Error(), "--wait")
}

func TestPrintRetryHint(t *testing.T) {
	var buf bytes.Buffer
	printRetryHint(&buf, 3, fmt.Errorf("wrapped: %w", tracker.ErrApprovalMissing))
	assertContains(t, buf.String(), "failed 3 times")
	assertContains(t, buf.String(), "then stop and end your turn")

	buf.Reset()
	printRetryHint(&buf, 3, errors.New("something else"))
	assertContains(t, buf.String(), "try a different approach")
}

func TestRemovedWaitFlagsFailWithStopHint(t *testing.T) {
	proj, issuePath := makeGateFixture(t, "in design", "", true)
	before := readFile(t, issuePath)
	cases := []struct {
		run  func(*Context, []string) error
		args []string
		want string
	}{
		{runTransition, []string{"cli/sample", "--to", "backlog", "--wait"}, "--wait was removed in v0.49.0: stop and end your turn; the approval nudge resumes you"},
		{runTransition, []string{"cli/sample", "--to", "backlog", "--wait", "--timeout", "9m"}, "--wait was removed in v0.49.0: stop and end your turn; the approval nudge resumes you"},
		{runTransition, []string{"cli/sample", "--to", "backlog", "-wait=true"}, "--wait was removed in v0.49.0: stop and end your turn; the approval nudge resumes you"},
		{runTransition, []string{"cli/sample", "--to", "backlog", "--timeout=9m"}, "--timeout was removed in v0.49.0 along with --wait: stop and end your turn; the approval nudge resumes you"},
		{runStart, []string{"cli/sample", "--wait", "--timeout", "9m"}, "--wait was removed in v0.49.0: stop and end your turn; the approval nudge resumes you"},
		{runStart, []string{"cli/sample", "--interval", "2s"}, "--interval was removed in v0.49.0 along with --wait: stop and end your turn; the approval nudge resumes you"},
	}
	for _, c := range cases {
		ctx, _, stderr := newTestContext(proj, false)
		err := c.run(ctx, c.args)
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: err = %v, want %q", c.args, err, c.want)
		}
		if strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Errorf("%v: generic flag error leaked to stderr:\n%s", c.args, stderr.String())
		}
	}
	if after := readFile(t, issuePath); after != before {
		t.Fatalf("a removed flag must change nothing:\n%s", after)
	}
}
