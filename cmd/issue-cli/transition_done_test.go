package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const parkedPrompt = "This issue is parked. Don't work on it."

// makeDoneParkedFixture builds the bundled workflow's tail: shipping → done,
// then parked (optional, global, reached by a from: "*" edge) as the only
// status after done. Extra statuses are appended after parked.
func makeDoneParkedFixture(t *testing.T, status, extraStatuses, extraTransitions string) *tracker.Project {
	t.Helper()

	dir := t.TempDir()
	systemDir := filepath.Join(dir, "issues", "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	workflow := `statuses:
  - name: "shipping"
  - name: "done"
  - name: "parked"
    optional: true
    global: true
    prompt: |
      ` + parkedPrompt + ` Tell the human it is parked and end your turn.
` + extraStatuses + `transitions:
  - from: "shipping"
    to: "done"
  - from: "*"
    to: "parked"
` + extraTransitions
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	issue := "---\ntitle: \"sample\"\nstatus: \"" + status + "\"\nsystem: \"CLI\"\n---\n\n- [x] shipped\n"
	if err := os.WriteFile(filepath.Join(systemDir, "sample.md"), []byte(issue), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: filepath.Join(dir, "issues"), WorkflowFile: workflowPath}
}

func TestRunTransitionToDoneNeverPrintsParkedGuidance(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "done"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	output := stdout.String()

	assertContains(t, output, "✓ shipping → done")
	assertContains(t, output, "== Done ==")
	assertNotContains(t, output, parkedPrompt)
	assertNotContains(t, output, "== Next ==")
	assertNotContains(t, output, `--to "parked"`)
}

func TestRunTransitionToDoneJSONHasNoParkedNext(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "done"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}

	var got transitionOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal transition output: %v\noutput:\n%s", err, stdout.String())
	}
	if !got.Done {
		t.Error("done = false, want true")
	}
	if got.NextStatus != "" || got.NextCommand != "" || len(got.OptionalNextStatuses) != 0 {
		t.Errorf("next = %q / %q / %v, want none", got.NextStatus, got.NextCommand, got.OptionalNextStatuses)
	}
	for _, g := range got.Guidance {
		if strings.Contains(g, parkedPrompt) {
			t.Errorf("guidance carries the parked prompt: %q", g)
		}
	}
}

// Parking an issue is not finishing it: the parked prompt is the guidance and
// no done confirmation is printed.
func TestRunTransitionToParkedIsNotDone(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping", "", "")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "parked"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	output := stdout.String()

	assertContains(t, output, parkedPrompt)
	assertNotContains(t, output, "== Done ==")
}

// A trailing optional status on the lifecycle (its own edge, not a "*" side
// status) is still offered after done, as before.
func TestRunTransitionToDoneStillOffersTrailingOptionalStatus(t *testing.T) {
	proj := makeDoneParkedFixture(t, "shipping",
		"  - name: \"archived\"\n    optional: true\n",
		"  - from: \"done\"\n    to: \"archived\"\n")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"cli/sample", "--to", "done"}); err != nil {
		t.Fatalf("runTransition: %v", err)
	}
	output := stdout.String()

	assertContains(t, output, `== Next ==`)
	assertContains(t, output, `--to "archived"`)
	assertNotContains(t, output, `--to "parked"`)
	assertNotContains(t, output, parkedPrompt)
	assertNotContains(t, output, "== Done ==")
}
