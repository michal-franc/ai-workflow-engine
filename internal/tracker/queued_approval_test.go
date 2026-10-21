package tracker

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const designReadyIssue = `---
title: "fast lane"
status: "in design"
assignee: "agent-fast-lane"
human_approval: "backlog"
%s---

## Design
- [x] Approach documented

## Acceptance Criteria
- [ ] it works
`

func writeDesignReady(t *testing.T, extra string) string {
	t.Helper()
	fp := filepath.Join(t.TempDir(), "fast-lane.md")
	content := []byte(fmt.Sprintf(designReadyIssue, extra))
	if err := os.WriteFile(fp, content, 0644); err != nil {
		t.Fatal(err)
	}
	return fp
}

func loadIssueFile(t *testing.T, fp string) *Issue {
	t.Helper()
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := ParseIssue(filepath.Base(fp), data)
	if err != nil {
		t.Fatal(err)
	}
	return issue
}

func TestQueuedApproval_PromotedThenStartAdvances(t *testing.T) {
	fp := writeDesignReady(t, "queued_approval: \"in progress\"\n")
	wf := DefaultWorkflow()

	_, result, err := wf.ApplyTransitionToFile(fp, "backlog")
	if err != nil {
		t.Fatalf("design → backlog: %v", err)
	}
	if result.PromotedApproval != "in progress" {
		t.Fatalf("PromotedApproval = %q, want in progress", result.PromotedApproval)
	}
	issue := loadIssueFile(t, fp)
	if issue.Status != "backlog" || issue.HumanApproval != "in progress" || issue.QueuedApproval != "" {
		t.Fatalf("after backlog: status=%q human_approval=%q queued=%q", issue.Status, issue.HumanApproval, issue.QueuedApproval)
	}
	for _, f := range issue.ExtraFields {
		if f.Key == "queued_approval" {
			t.Fatal("queued_approval shown as a custom field")
		}
	}

	started, err := wf.StartIssueOnce(fp, "fast-lane", "agent-next")
	if err != nil {
		t.Fatalf("start after promoted approval: %v", err)
	}
	if started.ToStatus != "in progress" {
		t.Fatalf("start landed on %q, want in progress", started.ToStatus)
	}
	if got := loadIssueFile(t, fp).HumanApproval; got != "" {
		t.Fatalf("promoted approval not consumed by start: %q", got)
	}
}

// Regression: without a queued approval, the consumed approval is cleared as
// before and nothing is promoted.
func TestQueuedApproval_NormalApprovalStillConsumed(t *testing.T) {
	fp := writeDesignReady(t, "")
	wf := DefaultWorkflow()
	_, result, err := wf.ApplyTransitionToFile(fp, "backlog")
	if err != nil {
		t.Fatal(err)
	}
	if !result.ClearedApproval || result.PromotedApproval != "" {
		t.Fatalf("cleared=%v promoted=%q", result.ClearedApproval, result.PromotedApproval)
	}
	if got := loadIssueFile(t, fp).HumanApproval; got != "" {
		t.Fatalf("approval not consumed: %q", got)
	}
}

// A queued approval chained to a different target is stale and dropped.
func TestQueuedApproval_StaleQueueDropped(t *testing.T) {
	issue := &Issue{Status: "in progress", HumanApproval: "", QueuedApproval: "documentation"}
	result := DefaultWorkflow().ApplyTransition(issue, "in progress", "testing")
	if result.Update.QueuedApproval == nil || *result.Update.QueuedApproval != "" {
		t.Fatalf("stale queued approval not cleared: %#v", result.Update.QueuedApproval)
	}
	if result.Update.HumanApproval != nil || result.PromotedApproval != "" {
		t.Fatalf("stale queued approval promoted: %#v", result.Update.HumanApproval)
	}
}
