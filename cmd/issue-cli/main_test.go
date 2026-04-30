package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// newTestContext builds a Context whose Stdout/Stderr are captured into the
// returned buffer. Tests use this in place of the old captureStdout helper so
// per-test output stays isolated from os.Stdout (and from any concurrent
// tests).
func newTestContext(proj *tracker.Project, jsonOut bool) (*Context, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	ctx := &Context{
		JSONOutput: jsonOut,
		Stdout:     &stdout,
		Stderr:     &stderr,
		Stdin:      strings.NewReader(""),
		Project:    proj,
		Now:        time.Now,
	}
	return ctx, &stdout, &stderr
}

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
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "in progress"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	output := stdout.String()

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
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "in progress"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	output := stdout.String()

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

func TestParseFieldFlags(t *testing.T) {
	got, err := parseFieldFlags([]string{"--to", "testing", "--field", "waiting=design review", "--field", "deferred_to=alice"})
	if err != nil {
		t.Fatalf("parseFieldFlags: %v", err)
	}
	if got["waiting"] != "design review" {
		t.Errorf("waiting = %q, want \"design review\"", got["waiting"])
	}
	if got["deferred_to"] != "alice" {
		t.Errorf("deferred_to = %q, want alice", got["deferred_to"])
	}
}

func TestParseFieldFlags_RejectsMalformed(t *testing.T) {
	if _, err := parseFieldFlags([]string{"--field", "no-equals-sign"}); err == nil {
		t.Fatal("expected error for missing =")
	}
	if _, err := parseFieldFlags([]string{"--field", "=value"}); err == nil {
		t.Fatal("expected error for empty key")
	}
	if _, err := parseFieldFlags([]string{"--field"}); err == nil {
		t.Fatal("expected error for trailing --field with no value")
	}
}

func TestParseFieldFlags_NoFieldsReturnsEmpty(t *testing.T) {
	got, err := parseFieldFlags([]string{"--to", "testing"})
	if err != nil {
		t.Fatalf("parseFieldFlags: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestRunAppendAutoRoutesEscapedDuplicateHeading(t *testing.T) {
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

	body := normalizeEscapedText(`## Design\n- [ ] auto routed`)
	_, changed, err := tracker.UpdateIssueBody(issuePath, func(existing string) (string, bool, error) {
		return tracker.AppendIssueBody(existing, body)
	})
	if err != nil {
		t.Fatalf("AppendIssueBody returned error: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}

	finalIssue := loadIssueByPath(t, issuesDir, issuePath)
	if !strings.Contains(finalIssue.BodyRaw, "auto routed") {
		t.Fatalf("issue body missing auto-routed content:\n%s", finalIssue.BodyRaw)
	}
	if strings.Count(finalIssue.BodyRaw, "## Design") != 1 {
		t.Fatalf("expected exactly one ## Design heading, got:\n%s", finalIssue.BodyRaw)
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
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runSetMeta(ctx, []string{"cli/sample", "--key", "waiting", "--value", "design review"}); err != nil {
		t.Fatalf("runSetMeta set: %v", err)
	}
	output := stdout.String()
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

	stdout.Reset()
	if err := runSetMeta(ctx, []string{"cli/sample", "--key", "waiting", "--clear"}); err != nil {
		t.Fatalf("runSetMeta clear: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Cleared waiting")

	got = loadIssueByPath(t, issuesDir, issuePath)
	for _, ef := range got.ExtraFields {
		if ef.Key == "waiting" {
			t.Fatalf("waiting field still present after clear: %+v", ef)
		}
	}
}

func TestRunTransitionNextHintSkipsOptionalStatus(t *testing.T) {
	proj, _ := makeOptionalNextFixture(t)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "in progress"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	output := stdout.String()

	assertContains(t, output, "== Next ==\n  issue-cli transition cli/sample --to \"testing\"")
	assertContains(t, output, "Optional side-paths:")
	assertContains(t, output, "issue-cli transition cli/sample --to \"team-feedback\"")
	if strings.Contains(output, "== Next ==\n  issue-cli transition cli/sample --to \"team-feedback\"") {
		t.Fatalf("primary Next hint should not point at the optional status:\n%s", output)
	}
}

func TestRunTransitionJSONCarriesOptionalNextStatuses(t *testing.T) {
	proj, _ := makeOptionalNextFixture(t)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "in progress"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}

	var got transitionOutput
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal transition output: %v\noutput:\n%s", err, stdout.String())
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
// is declared optional, and the required path sits after it.
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

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q\noutput:\n%s", want, got)
	}
}

func TestProcessSchemaIncludesEveryTaggedField(t *testing.T) {
	ctx, stdout, _ := newTestContext(nil, false)
	if err := runProcessSchema(ctx); err != nil {
		t.Fatalf("runProcessSchema: %v", err)
	}
	out := stdout.String()

	for _, section := range tracker.WorkflowSchemaSections() {
		for _, f := range section.Fields {
			if !strings.Contains(out, f.Name) {
				t.Errorf("schema output missing field %q from %s", f.Name, section.Path)
			}
			if f.Description == "" {
				t.Errorf("field %s.%s missing desc:\"...\" tag", section.Path, f.Name)
			}
		}
	}

	for _, a := range tracker.WorkflowActionTypes {
		if !strings.Contains(out, a.Name) {
			t.Errorf("schema output missing action type %q", a.Name)
		}
	}

	for _, r := range tracker.WorkflowValidationRules {
		if !strings.Contains(out, r.Name) {
			t.Errorf("schema output missing validation rule %q", r.Name)
		}
	}

	assertContains(t, out, "workflow.yaml schema")
	assertContains(t, out, "Action types")
	assertContains(t, out, "Validation rules")
}

func TestProcessSchemaMarksOptionalFields(t *testing.T) {
	ctx, stdout, _ := newTestContext(nil, false)
	if err := runProcessSchema(ctx); err != nil {
		t.Fatalf("runProcessSchema: %v", err)
	}
	out := stdout.String()

	assertContains(t, out, "optional?")
	assertContains(t, out, "prompt?")
	if strings.Contains(out, "name?") {
		t.Errorf("required field `name` should not be suffixed with ?")
	}
}

func TestProcessChangesEmbedsChangelog(t *testing.T) {
	orig := fetchReleases
	fetchReleases = func(string) ([]githubRelease, error) {
		return nil, fmt.Errorf("test: offline")
	}
	t.Cleanup(func() { fetchReleases = orig })

	ctx, stdout, _ := newTestContext(nil, false)
	if err := runProcessChanges(ctx); err != nil {
		t.Fatalf("runProcessChanges: %v", err)
	}
	out := stdout.String()

	assertContains(t, out, "release history")
	assertContains(t, out, "# Changelog")
	for _, line := range strings.Split(changelogMD, "\n") {
		if strings.HasPrefix(line, "## v") {
			if !strings.Contains(out, line) {
				t.Errorf("process changes output missing version line %q", line)
			}
		}
	}
}

func TestProcessChangesPrefersGitHubReleases(t *testing.T) {
	orig := fetchReleases
	fetchReleases = func(repo string) ([]githubRelease, error) {
		if repo != releasesRepo {
			t.Errorf("fetchReleases called with repo %q, want %q", repo, releasesRepo)
		}
		return []githubRelease{
			{
				TagName:     "v9.9.9",
				Name:        "v9.9.9 — test release",
				Body:        "- first test change\n- second test change",
				PublishedAt: "2026-04-24T10:00:00Z",
			},
		}, nil
	}
	t.Cleanup(func() { fetchReleases = orig })

	ctx, stdout, _ := newTestContext(nil, false)
	if err := runProcessChanges(ctx); err != nil {
		t.Fatalf("runProcessChanges: %v", err)
	}
	out := stdout.String()

	assertContains(t, out, "release history")
	assertContains(t, out, "v9.9.9 — test release")
	assertContains(t, out, "2026-04-24")
	assertContains(t, out, "first test change")
	if strings.Contains(out, "# Changelog") {
		t.Error("releases path should not print embedded CHANGELOG heading")
	}
	if strings.Contains(out, "(offline)") {
		t.Error("releases path should not mark output as offline")
	}
}

func TestTrimChangelogToVersions(t *testing.T) {
	t.Parallel()

	md := "# Changelog\n\npreamble here\n\n"
	for i := 25; i >= 1; i-- {
		md += "## v0.0." + itoaLocal(i) + " — 2026-01-01\n\n- change\n\n"
	}

	trimmed, omitted := trimChangelogToVersions(md, 20)
	if omitted != 5 {
		t.Fatalf("omitted = %d, want 5", omitted)
	}
	assertContains(t, trimmed, "# Changelog")
	assertContains(t, trimmed, "preamble here")
	assertContains(t, trimmed, "## v0.0.25")
	assertContains(t, trimmed, "## v0.0.6")
	if strings.Contains(trimmed, "## v0.0.5 ") {
		t.Errorf("v0.0.5 should have been trimmed")
	}
	if strings.Contains(trimmed, "## v0.0.1 ") {
		t.Errorf("v0.0.1 should have been trimmed")
	}
}

func TestTrimChangelogToVersions_UnderCapKeepsAll(t *testing.T) {
	t.Parallel()

	md := "# Changelog\n\n## v0.1.1 — 2026-04-23\n\n- change\n\n## v0.1.0 — 2026-04-23\n\n- change\n"
	trimmed, omitted := trimChangelogToVersions(md, 20)
	if omitted != 0 {
		t.Fatalf("omitted = %d, want 0", omitted)
	}
	assertContains(t, trimmed, "## v0.1.1")
	assertContains(t, trimmed, "## v0.1.0")
}

func itoaLocal(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func makeProcessTransitionsFixture(t *testing.T) *tracker.Project {
	t.Helper()

	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	cliDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(cliDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", cliDir, err)
	}

	workflowPath := filepath.Join(dir, "workflow.yaml")
	workflow := strings.TrimSpace(`
statuses:
  - name: "idea"
  - name: "in design"
  - name: "backlog"
  - name: "in progress"
  - name: "blocked"
    global: true
  - name: "deferred"
    optional: true
  - name: "done"
transitions:
  - from: "idea"
    to: "in design"
    actions:
      - type: validate
        rule: body_not_empty
  - from: "in design"
    to: "backlog"
    actions:
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
      - type: append_section
        title: "Implementation"
        body: |
          - [ ] Code changes complete
systems:
  CLI:
    transitions:
      - from: "backlog"
        to: "in progress"
        actions:
          - type: require_human_approval
            status: "in progress"
          - type: validate
            rule: has_assignee
          - type: inject_prompt
            prompt: "CLI-specific guidance for new work."
`)
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	cliIssue := strings.TrimSpace(`
---
title: "cli sample"
status: "backlog"
system: "CLI"
---

body
`)
	if err := os.WriteFile(filepath.Join(cliDir, "cli-sample.md"), []byte(cliIssue), 0644); err != nil {
		t.Fatalf("write cli issue: %v", err)
	}

	plainIssue := strings.TrimSpace(`
---
title: "plain sample"
status: "backlog"
---

body
`)
	if err := os.WriteFile(filepath.Join(issuesDir, "plain-sample.md"), []byte(plainIssue), 0644); err != nil {
		t.Fatalf("write plain issue: %v", err)
	}

	return &tracker.Project{
		Name:         "test",
		Slug:         "test",
		IssueDir:     issuesDir,
		WorkflowFile: workflowPath,
	}
}

func TestProcessTransitionsRendersFromActiveWorkflow(t *testing.T) {
	proj := makeProcessTransitionsFixture(t)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcessTransitions(ctx, nil); err != nil {
		t.Fatalf("runProcessTransitions: %v", err)
	}
	out := stdout.String()

	assertContains(t, out, "== Transition Rules ==")
	assertContains(t, out, "→ idea")
	assertContains(t, out, "idea → in design")
	assertContains(t, out, "in design → backlog")
	assertContains(t, out, "backlog → in progress")
	assertContains(t, out, "Validate issue body is not empty")
	assertContains(t, out, "Validate issue has assignee")
	assertContains(t, out, `Must be human-approved for "backlog" in the issue viewer`)
	assertContains(t, out, "Side-effect: clears assignee")
	assertContains(t, out, "Side-effect: appends ## Implementation section")
	assertContains(t, out, "Optional statuses (skippable on forward transitions): deferred")
	assertContains(t, out, "Global statuses (transitions from them to any status are allowed): blocked")
	assertContains(t, out, "Per-system overlays are configured for:")
	assertContains(t, out, "CLI")
}

func TestProcessTransitionsScopedBySystemFlag(t *testing.T) {
	proj := makeProcessTransitionsFixture(t)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcessTransitions(ctx, []string{"--system", "CLI"}); err != nil {
		t.Fatalf("runProcessTransitions: %v", err)
	}
	out := stdout.String()

	assertContains(t, out, `== Transition Rules — system "CLI"`)
	assertContains(t, out, "Side-effect: injects entry guidance prompt")
	if strings.Contains(out, "Per-system overlays are configured for:") {
		t.Errorf("scoped output should not list per-system overlay hint:\n%s", out)
	}
}

func TestProcessTransitionsScopedByIssueSlug(t *testing.T) {
	proj := makeProcessTransitionsFixture(t)
	ctx, cliStdout, _ := newTestContext(proj, false)
	if err := runProcessTransitions(ctx, []string{"cli-sample"}); err != nil {
		t.Fatalf("runProcessTransitions cli-sample: %v", err)
	}
	cliOut := cliStdout.String()
	assertContains(t, cliOut, `== Transition Rules — system "CLI" (issue cli/cli-sample) ==`)
	assertContains(t, cliOut, "Side-effect: injects entry guidance prompt")

	ctx2, plainStdout, _ := newTestContext(proj, false)
	if err := runProcessTransitions(ctx2, []string{"plain-sample"}); err != nil {
		t.Fatalf("runProcessTransitions plain-sample: %v", err)
	}
	plainOut := plainStdout.String()
	assertContains(t, plainOut, "== Transition Rules — issue plain-sample (no system overlay; project default) ==")
}

func makeListScoringFixture(t *testing.T, scoringEnabled bool) *tracker.Project {
	t.Helper()

	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", issuesDir, err)
	}

	enabled := "false"
	if scoringEnabled {
		enabled = "true"
	}
	workflowPath := filepath.Join(dir, "workflow.yaml")
	workflow := strings.TrimSpace(fmt.Sprintf(`
statuses:
  - name: "backlog"
  - name: "in progress"
  - name: "done"
scoring:
  enabled: %s
  default_sort: score_desc
  formula:
    priority:
      p0: 100
      p1: 75
      p2: 50
      p3: 25
`, enabled))
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	issues := []struct {
		name string
		body string
	}{
		{
			name: "high.md",
			body: strings.TrimSpace(`
---
title: "high priority"
status: "backlog"
priority: "P0"
---
body
`),
		},
		{
			name: "mid.md",
			body: strings.TrimSpace(`
---
title: "mid priority"
status: "backlog"
priority: "P2"
---
body
`),
		},
	}
	for _, iss := range issues {
		if err := os.WriteFile(filepath.Join(issuesDir, iss.name), []byte(iss.body), 0644); err != nil {
			t.Fatalf("write %s: %v", iss.name, err)
		}
	}

	return &tracker.Project{
		Name:         "test",
		Slug:         "test",
		IssueDir:     issuesDir,
		WorkflowFile: workflowPath,
	}
}

func TestRunListJSONIncludesScoreWhenScoringEnabled(t *testing.T) {
	proj := makeListScoringFixture(t, true)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runList(ctx, nil); err != nil {
		t.Fatalf("runList: %v", err)
	}

	var got []listJSONIssue
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal list output: %v\noutput:\n%s", err, stdout.String())
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2", len(got))
	}

	if got[0].Slug != "high-priority" {
		t.Fatalf("first slug = %q, want high-priority (sorted by score desc)", got[0].Slug)
	}
	if got[0].Score == nil {
		t.Fatal("first issue Score is nil, want populated float")
	}
	if *got[0].Score != 100 {
		t.Fatalf("first issue Score = %v, want 100", *got[0].Score)
	}
	if got[0].ScoreBreakdown == nil {
		t.Fatal("first issue ScoreBreakdown is nil, want populated breakdown")
	}
	if got[0].ScoreBreakdown.Total != 100 {
		t.Fatalf("first ScoreBreakdown.Total = %v, want 100", got[0].ScoreBreakdown.Total)
	}
	if len(got[0].ScoreBreakdown.Components) == 0 {
		t.Fatal("ScoreBreakdown.Components empty, want priority component")
	}
	if got[0].ScoreBreakdown.Components[0].Name != "priority" {
		t.Fatalf("first component Name = %q, want priority", got[0].ScoreBreakdown.Components[0].Name)
	}

	if got[1].Score == nil || *got[1].Score != 50 {
		t.Fatalf("second issue Score = %v, want 50", got[1].Score)
	}
}

func TestRunListJSONOmitsScoreWhenScoringDisabled(t *testing.T) {
	proj := makeListScoringFixture(t, false)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runList(ctx, nil); err != nil {
		t.Fatalf("runList: %v", err)
	}

	var got []listJSONIssue
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal list output: %v\noutput:\n%s", err, stdout.String())
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2", len(got))
	}
	for i, entry := range got {
		if entry.Score != nil {
			t.Errorf("issue[%d].Score = %v, want nil when scoring disabled", i, *entry.Score)
		}
		if entry.ScoreBreakdown != nil {
			t.Errorf("issue[%d].ScoreBreakdown = %+v, want nil when scoring disabled", i, entry.ScoreBreakdown)
		}
	}
}

func TestChangelogEmbeddedAndHasEntries(t *testing.T) {
	t.Parallel()

	if strings.TrimSpace(changelogMD) == "" {
		t.Fatal("changelogMD is empty — //go:embed CHANGELOG.md failed")
	}
	versionCount := 0
	for _, line := range strings.Split(changelogMD, "\n") {
		if strings.HasPrefix(line, "## v") {
			versionCount++
		}
	}
	if versionCount == 0 {
		t.Fatal("CHANGELOG.md has no '## v...' entries")
	}
}

