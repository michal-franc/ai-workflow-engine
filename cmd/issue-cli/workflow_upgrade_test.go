package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// oldDevelopmentWorkflow is a trimmed copy of the development template as it
// shipped before stamps: an older approval prompt, no types, no actions.
const oldDevelopmentWorkflow = `statuses:
  - name: "idea"
  - name: "in design"
    prompt: |
      When the design is complete, stop and ask for backlog approval in the issue viewer before attempting the transition.
  - name: "backlog"
  - name: "in progress"
  - name: "done"
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: "require_human_approval"
        status: "backlog"
`

func writeUpgradeProject(t *testing.T, workflow string) *tracker.Project {
	t.Helper()
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: issuesDir, WorkflowFile: path}
}

func TestRunWorkflowUpgradeIsReadOnly(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"== Workflow upgrade ==", "Status:   unstamped", "== Proposals ==", "[1] ", "Ask:", "Snippet:", "== Steps ==", "workflow.old1.yaml"} {
		assertContains(t, out, want)
	}
	got, err := os.ReadFile(proj.WorkflowFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != oldDevelopmentWorkflow {
		t.Fatal("upgrade modified workflow.yaml")
	}
	entries, _ := os.ReadDir(filepath.Dir(proj.WorkflowFile))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".old") {
			t.Fatalf("upgrade created %s", e.Name())
		}
	}
}

func TestRunWorkflowUpgradeJSON(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runWorkflowUpgrade(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"status", "template", "current", "latest", "record_source", "proposals", "already_present", "declined", "detected_unrecorded", "backup_path", "steps", "message"} {
		if _, ok := plan[key]; !ok {
			t.Errorf("JSON plan missing %q", key)
		}
	}
	proposals, _ := plan["proposals"].([]any)
	if len(proposals) == 0 {
		t.Fatal("no proposals in JSON")
	}
	first, _ := proposals[0].(map[string]any)
	for _, key := range []string{"number", "id", "kind", "version", "category", "title", "what", "why", "where", "snippet", "question"} {
		if _, ok := first[key]; !ok {
			t.Errorf("proposal missing %q", key)
		}
	}
}

func TestRunWorkflowUpgradeWithoutWorkflowFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	ctx, _, _ := newTestContext(&tracker.Project{Name: "test", IssueDir: dir}, false)
	err := runWorkflowUpgrade(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "workflow init") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpgradeAliasIsRegistered(t *testing.T) {
	if lookupCommand("upgrade") == nil {
		t.Fatal("upgrade command not registered")
	}
}

// No command nudges about upgrades (D-8): start and process workflow on a
// project with proposals print nothing about them; the Updates tab and an
// explicit `workflow upgrade` are the only places they show.
func TestStartAndProcessWorkflowPrintNoUpgradeHint(t *testing.T) {
	proj, _ := makeContractFixture(t)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runStart(ctx, []string{"cli/sample"}); err != nil {
		t.Fatalf("runStart: %v", err)
	}
	ctx2, stdout2, _ := newTestContext(proj, false)
	if err := runProcessWorkflow(ctx2, nil); err != nil {
		t.Fatalf("runProcessWorkflow: %v", err)
	}
	for _, out := range []string{stdout.String(), stdout2.String()} {
		if strings.Contains(out, "workflow upgrade") || strings.Contains(out, "upgrade proposal") {
			t.Fatalf("unexpected upgrade hint:\n%s", out)
		}
	}
}
