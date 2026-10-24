package main

import (
	"os"
	"strings"
	"testing"
)

// A worker runs its own issue's workflow and stops: start, claim and
// transition on another slug fail and leave the file as it was. Its own
// issue, a lead and the human are not limited.
func TestWorkerScope_OwnIssueOnly(t *testing.T) {
	proj := makeLeadProject(t)
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/child")
	loner := loadLeadIssue(t, proj, "loner").FilePath
	before, err := os.ReadFile(loner)
	if err != nil {
		t.Fatal(err)
	}

	for name, run := range map[string]func(*Context, []string) error{"start": runStart, "claim": runClaim, "transition": runTransition} {
		args := []string{"loner"}
		if name == "transition" {
			args = append(args, "--to", "in progress")
		}
		ctx, _, _ := newTestContext(proj, false)
		err := run(ctx, args)
		if err == nil || !strings.Contains(err.Error(), "you work on ui/child only; ask your lead. "+name+" loner changed nothing") {
			t.Errorf("%s: err = %v", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), `issue-cli --project proj comment ui/child "question: ..."`) {
			t.Errorf("%s: refusal doesn't point at the issue trail: %v", name, err)
		}
	}
	if after, _ := os.ReadFile(loner); string(after) != string(before) {
		t.Fatalf("a refused command changed loner:\n%s", after)
	}

	// Its own issue: claimed under its session name, the SendMessage address.
	ctx, _, _ := newTestContext(proj, false)
	if err := runClaim(ctx, []string{"ui/child"}); err != nil {
		t.Fatalf("own issue: %v", err)
	}
	if got := loadLeadIssue(t, proj, "ui/child").Assignee; got != "agent-ui-child" {
		t.Errorf("assignee = %q, want agent-ui-child", got)
	}

	// The lead and the human may claim anything.
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "")
	for _, scope := range []string{"project", ""} {
		t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", scope)
		ctx, _, _ := newTestContext(proj, false)
		if err := runClaim(ctx, []string{"loner", "--force"}); err != nil {
			t.Errorf("lead scope %q: %v", scope, err)
		}
	}
}
