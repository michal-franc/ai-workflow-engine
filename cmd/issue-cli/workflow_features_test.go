package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

func TestRunWorkflowScanDryRunWritesNothing(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"== Workflow scan ==", "Mode:   dry run", "✓ human-approval-gates", "transitions[0].actions[0].type", "· worktree", "+ installed human-approval-gates@", "Record after this scan:", "features:"} {
		assertContains(t, out, want)
	}
	if got, _ := os.ReadFile(proj.WorkflowFile); string(got) != oldDevelopmentWorkflow {
		t.Fatal("scan --dry-run modified workflow.yaml")
	}
}

func TestRunWorkflowScanWritesRecordAndIsIdempotent(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan", "--declined", "scoring,worktree"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Mode:   wrote the features: record")
	got, _ := os.ReadFile(proj.WorkflowFile)
	if !strings.HasSuffix(string(got), oldDevelopmentWorkflow) {
		t.Fatalf("scan changed more than the features: block:\n%s", got)
	}
	rec, ok := workflowupgrade.ReadRecord(got)
	if !ok || strings.Join(rec.Declined, ",") != "scoring,worktree" || len(rec.Installed) == 0 {
		t.Fatalf("record = %+v", rec)
	}
	if _, err := tracker.LoadWorkflow(proj.WorkflowFile); err != nil {
		t.Fatalf("LoadWorkflow after scan: %v", err)
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "record already up to date, nothing written")
	assertContains(t, stdout.String(), "✗ scoring")
	again, _ := os.ReadFile(proj.WorkflowFile)
	if string(again) != string(got) {
		t.Fatal("second scan changed the file")
	}
}

func TestRunWorkflowScanJSON(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runWorkflowScan(ctx, []string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	var res map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"workflow_file", "dry_run", "written", "had_record", "features", "record", "block", "changes", "notes"} {
		if _, ok := res[key]; !ok {
			t.Errorf("JSON missing %q", key)
		}
	}
	if res["written"] != false || res["dry_run"] != true {
		t.Errorf("dry_run/written = %v/%v", res["dry_run"], res["written"])
	}
}

func TestRunWorkflowScanRejectsArgs(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, _, _ := newTestContext(proj, false)
	if err := runWorkflowScan(ctx, []string{"extra"}); err == nil || !strings.Contains(err.Error(), "takes no arguments") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunWorkflowUpgradeListsDeclinedAndRecord(t *testing.T) {
	proj := writeUpgradeProject(t, "features:\n  declined: [scoring]\nworktree: true\n"+oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "Record:   features: (0 installed, 1 declined)")
	assertContains(t, out, "Declined (not proposed; re-open with `issue-cli workflow scan --forget <id>`): scoring")
	assertContains(t, out, "Detected but not in the features: record (run `issue-cli workflow scan`): ")
	assertContains(t, out, "worktree\n")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade", "--all"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "declined earlier")
}

// Each category heading prints once, however the catalog orders entries.
func TestRunWorkflowScanPrintsEachCategoryOnce(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, c := range workflowupgrade.Categories {
		if n := strings.Count(out, "\n  "+c+"\n"); n != 1 {
			t.Errorf("heading %q printed %d times:\n%s", c, n, out)
		}
	}
}

// A stamp at the bundled version with a stale record: upgrade doesn't say
// up to date, and process workflow warns.
func TestStaleRecordIsReportedByUpgradeAndProcess(t *testing.T) {
	data, err := workflowupgrade.ReadTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	proj := writeUpgradeProject(t, string(data)+"\nci:\n  run:\n    - cmd: go test ./...\n")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "Status:   "+workflowupgrade.StatusStaleRecord)
	assertContains(t, out, "issue-cli workflow scan")
	if strings.Contains(out, "up_to_date") {
		t.Errorf("upgrade calls a stale record up to date:\n%s", out)
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow"}); err != nil {
		t.Fatal(err)
	}
	out = stdout.String()
	assertContains(t, out, "== Warnings (workflow.yaml) ==")
	assertContains(t, out, "features: record")
	assertContains(t, out, "local-ci")

	// Undecided proposals alone are the owner's call: upgrade says so,
	// process workflow stays quiet.
	proj = writeUpgradeProject(t, string(data))
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Status:   "+workflowupgrade.StatusUndecided)
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "features: record") {
		t.Errorf("process workflow warns about undecided proposals:\n%s", stdout.String())
	}
}
