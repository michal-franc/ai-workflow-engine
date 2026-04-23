package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestCollectChecklist(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"## Header",
		"- [x] done item",
		"- [ ] pending item",
		"not a checkbox",
		"  - [X] nested done",
	}, "\n")

	got := collectChecklist(body)
	if len(got) != 3 {
		t.Fatalf("collectChecklist len = %d, want 3", len(got))
	}
	if !got[0].Checked || got[0].Text != "done item" {
		t.Fatalf("first checklist item = %+v", got[0])
	}
	if got[1].Checked || got[1].Text != "pending item" {
		t.Fatalf("second checklist item = %+v", got[1])
	}
	if !got[2].Checked || got[2].Text != "nested done" {
		t.Fatalf("third checklist item = %+v", got[2])
	}
}

func TestTransitionSideEffects(t *testing.T) {
	t.Parallel()

	empty := ""
	result := tracker.TransitionResult{
		Update: tracker.IssueUpdate{
			Assignee: &empty,
		},
		BodyAppended:    true,
		ClearedApproval: true,
		InjectedPrompts: []string{"prompt one", "prompt two"},
	}

	got := transitionSideEffects(result)
	want := []string{
		"assignee cleared",
		"approval consumed",
		"workflow content appended to issue body",
		"2 entry guidance prompt(s) injected",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("transitionSideEffects = %#v, want %#v", got, want)
	}
}

func TestRunTransitionPrintsPostTransitionState(t *testing.T) {
	proj, issuePath := makeTransitionFixture(t)
	jsonOutput = false
	output := captureStdout(t, func() {
		runTransition(proj, "cli/sample", "in progress")
	})

	assertContains(t, output, "✓ backlog → in progress")
	assertContains(t, output, "Status: in progress")
	assertContains(t, output, "✓ Assignee cleared")
	assertContains(t, output, "✓ Approval consumed")
	assertContains(t, output, "✓ Workflow content appended to issue body")
	assertContains(t, output, "✓ 1 entry guidance prompt(s) injected")
	assertContains(t, output, "== Checklist (1/3) ==")
	assertContains(t, output, "- [x] already done")
	assertContains(t, output, "- [ ] Code changes complete")
	assertContains(t, output, "- [ ] Tests written or updated")
	assertContains(t, output, "== Guidance ==")
	assertContains(t, output, "- Implement the accepted design.")
	assertContains(t, output, "- Run tests before entering testing.")
	assertContains(t, output, "- Verify the implementation.")
	assertContains(t, output, "issue-cli transition cli/sample --to \"testing\"")

	issue := loadIssueByPath(t, proj.IssueDir, issuePath)
	if issue.Status != "in progress" {
		t.Fatalf("issue status = %q, want in progress", issue.Status)
	}
	if issue.Assignee != "" {
		t.Fatalf("issue assignee = %q, want empty", issue.Assignee)
	}
	if issue.HumanApproval != "" {
		t.Fatalf("issue human approval = %q, want empty", issue.HumanApproval)
	}
	if !strings.Contains(issue.BodyRaw, "## Implementation") {
		t.Fatalf("issue body missing appended Implementation section:\n%s", issue.BodyRaw)
	}
}

func TestRunTransitionJSONIncludesPostTransitionFields(t *testing.T) {
	proj, _ := makeTransitionFixture(t)
	jsonOutput = true
	defer func() { jsonOutput = false }()

	output := captureStdout(t, func() {
		runTransition(proj, "cli/sample", "in progress")
	})

	var got transitionOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("unmarshal transition output: %v\noutput:\n%s", err, output)
	}

	if got.From != "backlog" || got.To != "in progress" {
		t.Fatalf("transition = %q -> %q, want backlog -> in progress", got.From, got.To)
	}
	if got.Status != "in progress" {
		t.Fatalf("status = %q, want in progress", got.Status)
	}
	if got.Slug != "cli/sample" {
		t.Fatalf("slug = %q, want cli/sample", got.Slug)
	}
	if got.NextStatus != "testing" {
		t.Fatalf("next_status = %q, want testing", got.NextStatus)
	}
	if !got.BodyChanged {
		t.Fatal("body_changed = false, want true")
	}
	if got.CommentsChanged {
		t.Fatal("comments_changed = true, want false")
	}
	if len(got.Checklist) != 3 {
		t.Fatalf("checklist len = %d, want 3", len(got.Checklist))
	}
	if len(got.SideEffects) != 4 {
		t.Fatalf("side_effects len = %d, want 4", len(got.SideEffects))
	}
	if len(got.Guidance) != 3 {
		t.Fatalf("guidance len = %d, want 3", len(got.Guidance))
	}
}


func TestNormalizeEscapedText(t *testing.T) {
	got := normalizeEscapedText(`line1\nline2\r\nline3\tend`)
	want := "line1\nline2\nline3\tend"
	if got != want {
		t.Fatalf("normalizeEscapedText = %q, want %q", got, want)
	}
}

func TestRunAppendRejectsEscapedDuplicateHeading(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	issuePath := filepath.Join(systemDir, "sample.md")
	issue := strings.TrimSpace(`
---
title: "sample"
status: "in progress"
system: "CLI"
---

## Design
Existing note
`)
	if err := os.WriteFile(issuePath, []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	body := normalizeEscapedText(`## Design\n- [ ] escaped duplicate`)
	_, _, err := tracker.UpdateIssueBody(issuePath, func(existing string) (string, bool, error) {
		return tracker.AppendIssueBody(existing, body)
	})
	if err == nil {
		t.Fatal("expected duplicate heading error")
	}
	if !strings.Contains(err.Error(), "duplicate heading") {
		t.Fatalf("error = %q, want duplicate heading guidance", err)
	}

	finalIssue := loadIssueByPath(t, issuesDir, issuePath)
	if strings.Contains(finalIssue.BodyRaw, "escaped duplicate") {
		t.Fatalf("issue body unexpectedly changed:\n%s", finalIssue.BodyRaw)
	}
}

func TestConcurrentCheckboxUpdatesPreserveAllChecks(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	issuePath := filepath.Join(systemDir, "sample.md")
	issue := strings.TrimSpace(`
---
title: "sample"
status: "in progress"
system: "CLI"
---

- [ ] first task
- [ ] second task
- [ ] third task
`)
	if err := os.WriteFile(issuePath, []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	var wg sync.WaitGroup
	for _, query := range []string{"first task", "second task"} {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()

			_, changed, err := tracker.UpdateIssueBody(issuePath, func(body string) (string, bool, error) {
				updated, found := tracker.CheckCheckbox(body, query)
				return updated, found, nil
			})
			if err != nil {
				t.Errorf("update %q failed: %v", query, err)
				return
			}
			if !changed {
				t.Errorf("update %q did not change the issue body", query)
			}
		}(query)
	}
	wg.Wait()

	finalIssue := loadIssueByPath(t, issuesDir, issuePath)
	if !strings.Contains(finalIssue.BodyRaw, "- [x] first task") {
		t.Fatalf("first task was not preserved:\n%s", finalIssue.BodyRaw)
	}
	if !strings.Contains(finalIssue.BodyRaw, "- [x] second task") {
		t.Fatalf("second task was not preserved:\n%s", finalIssue.BodyRaw)
	}
	if !strings.Contains(finalIssue.BodyRaw, "- [ ] third task") {
		t.Fatalf("unexpected third task state:\n%s", finalIssue.BodyRaw)
	}
}

func TestRunSetMetaSetsAndClears(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	issuePath := filepath.Join(systemDir, "sample.md")
	issue := strings.TrimSpace(`
---
title: "sample"
status: "in progress"
system: "CLI"
---

Body
`)
	if err := os.WriteFile(issuePath, []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	proj := &tracker.Project{Name: "test", Slug: "test", IssueDir: issuesDir}
	jsonOutput = false

	output := captureStdout(t, func() {
		runSetMeta(proj, "cli/sample", "waiting", "design review", false)
	})
	assertContains(t, output, `✓ Set waiting = "design review"`)
	assertContains(t, output, "file: "+issuePath)

	got := loadIssueByPath(t, issuesDir, issuePath)
	var waiting string
	for _, ef := range got.ExtraFields {
		if ef.Key == "waiting" {
			waiting = ef.Value
		}
	}
	if waiting != "design review" {
		t.Fatalf("waiting = %q, want %q", waiting, "design review")
	}

	clearOutput := captureStdout(t, func() {
		runSetMeta(proj, "cli/sample", "waiting", "", true)
	})
	assertContains(t, clearOutput, "✓ Cleared waiting")

	got = loadIssueByPath(t, issuesDir, issuePath)
	for _, ef := range got.ExtraFields {
		if ef.Key == "waiting" {
			t.Fatalf("waiting field still present after clear: %+v", ef)
		}
	}
}

func TestRunTransitionNextHintSkipsOptionalStatus(t *testing.T) {
	proj, _ := makeOptionalNextFixture(t)
	jsonOutput = false
	output := captureStdout(t, func() {
		runTransition(proj, "cli/sample", "in progress")
	})

	// Primary Next must point at the required status, not the optional side-path.
	assertContains(t, output, "== Next ==\n  issue-cli transition cli/sample --to \"testing\"")
	assertContains(t, output, "Optional side-paths:")
	assertContains(t, output, "issue-cli transition cli/sample --to \"team-feedback\"")
	if strings.Contains(output, "== Next ==\n  issue-cli transition cli/sample --to \"team-feedback\"") {
		t.Fatalf("primary Next hint should not point at the optional status:\n%s", output)
	}
}

func TestRunTransitionJSONCarriesOptionalNextStatuses(t *testing.T) {
	proj, _ := makeOptionalNextFixture(t)
	jsonOutput = true
	defer func() { jsonOutput = false }()

	output := captureStdout(t, func() {
		runTransition(proj, "cli/sample", "in progress")
	})

	var got transitionOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("unmarshal transition output: %v\noutput:\n%s", err, output)
	}
	if got.NextStatus != "testing" {
		t.Fatalf("next_status = %q, want testing", got.NextStatus)
	}
	if got.NextStatusOptional {
		t.Fatal("next_status_optional = true, want false")
	}
	if len(got.OptionalNextStatuses) != 1 || got.OptionalNextStatuses[0] != "team-feedback" {
		t.Fatalf("optional_next_statuses = %v, want [team-feedback]", got.OptionalNextStatuses)
	}
}

// makeOptionalNextFixture builds a workflow where the status following "in progress"
// is declared optional, and the required path sits after it. Used to verify the
// "Next:" hint skips optional statuses when suggesting the default forward target.
func makeOptionalNextFixture(t *testing.T) (*tracker.Project, string) {
	t.Helper()

	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	workflowPath := filepath.Join(dir, "workflow.yaml")
	workflow := strings.TrimSpace(`
statuses:
  - name: "backlog"
  - name: "in progress"
  - name: "team-feedback"
    optional: true
  - name: "testing"
transitions:
  - from: "backlog"
    to: "in progress"
    actions:
      - type: require_human_approval
        status: "in progress"
      - type: validate
        rule: has_assignee
`)
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	issuePath := filepath.Join(systemDir, "sample.md")
	issue := strings.TrimSpace(`
---
title: "sample"
status: "backlog"
system: "CLI"
assignee: "agent-optional-next"
human_approval: "in progress"
---

- [x] already done
`)
	if err := os.WriteFile(issuePath, []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	return &tracker.Project{
		Name:         "test",
		Slug:         "test",
		IssueDir:     issuesDir,
		WorkflowFile: workflowPath,
	}, issuePath
}

func makeTransitionFixture(t *testing.T) (*tracker.Project, string) {
	t.Helper()

	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	workflowPath := filepath.Join(dir, "workflow.yaml")
	workflow := strings.TrimSpace(`
statuses:
  - name: "backlog"
    prompt: "Queued for implementation."
  - name: "in progress"
    prompt: "Implement the accepted design."
  - name: "testing"
    prompt: "Verify the implementation."
transitions:
  - from: "backlog"
    to: "in progress"
    actions:
      - type: require_human_approval
        status: "in progress"
      - type: validate
        rule: has_assignee
      - type: append_section
        title: "Implementation"
        body: |
          - [ ] Code changes complete
          - [ ] Tests written or updated
      - type: inject_prompt
        prompt: "Run tests before entering testing."
      - type: set_fields
        field: "assignee"
        value: ""
`)
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	issuePath := filepath.Join(systemDir, "sample.md")
	issue := strings.TrimSpace(`
---
title: "sample"
status: "backlog"
system: "CLI"
assignee: "agent-transtion-improvement"
human_approval: "in progress"
---

- [x] already done
`)
	if err := os.WriteFile(issuePath, []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	return &tracker.Project{
		Name:         "test",
		Slug:         "test",
		IssueDir:     issuesDir,
		WorkflowFile: workflowPath,
	}, issuePath
}

func loadIssueByPath(t *testing.T, issuesDir, issuePath string) *tracker.Issue {
	t.Helper()

	issues, err := tracker.LoadIssues(issuesDir)
	if err != nil {
		t.Fatalf("load issues: %v", err)
	}
	for _, issue := range issues {
		if issue.FilePath == issuePath {
			return issue
		}
	}
	t.Fatalf("issue %s not found", issuePath)
	return nil
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q\noutput:\n%s", want, got)
	}
}
