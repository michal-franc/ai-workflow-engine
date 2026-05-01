package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// makeSimpleProject builds a temp project with a single issue at the given
// status. Returns the project, the slug used for findIssueOrErr lookups, and
// the issue file path. Tests that mutate the issue should reload via
// loadIssueByPath after invoking the command.
func makeSimpleProject(t *testing.T, status string) (proj *tracker.Project, slug string, issuePath string) {
	t.Helper()

	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	systemDir := filepath.Join(issuesDir, "CLI")
	if err := os.MkdirAll(systemDir, 0755); err != nil {
		t.Fatalf("mkdir issue dir: %v", err)
	}

	issuePath = filepath.Join(systemDir, "sample.md")
	body := strings.TrimSpace(fmt.Sprintf(`
---
title: "sample"
status: %q
system: "CLI"
version: "0.1"
priority: "high"
---

## Design
- [ ] first task
- [ ] second task

## Test Plan

### Automated
- thing one

### Manual
- thing two
`, status))
	if err := os.WriteFile(issuePath, []byte(body), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	proj = &tracker.Project{
		Name:     "test",
		Slug:     "test",
		IssueDir: issuesDir,
		Version:  "0.1",
	}
	return proj, "cli/sample", issuePath
}

// ------------------- claim / unclaim -------------------

func TestRunClaimSetsAssignee(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "backlog")
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runClaim(ctx, []string{slug, "--assignee", "alice"}); err != nil {
		t.Fatalf("runClaim: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Claimed: cli/sample (assignee: alice)")

	got := loadIssueByPath(t, proj.IssueDir, issuePath)
	if got.Assignee != "alice" {
		t.Fatalf("assignee = %q, want alice", got.Assignee)
	}
}

func TestRunClaimRefusesAlreadyClaimedWithoutForce(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "backlog")
	if err := os.WriteFile(issuePath, []byte(strings.Replace(string(mustRead(t, issuePath)), `system: "CLI"`, "system: \"CLI\"\nassignee: \"bob\"", 1)), 0644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	ctx, _, _ := newTestContext(proj, false)

	err := runClaim(ctx, []string{slug, "--assignee", "alice"})
	if err == nil {
		t.Fatal("expected already-claimed error")
	}
	if !strings.Contains(err.Error(), `already claimed by "bob"`) {
		t.Fatalf("error = %q, want already-claimed message", err.Error())
	}
}

func TestRunClaimRequiresSlug(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "backlog")
	ctx, _, _ := newTestContext(proj, false)
	if err := runClaim(ctx, nil); err == nil || !strings.Contains(err.Error(), "claim requires <slug>") {
		t.Fatalf("expected slug-required error, got %v", err)
	}
}

func TestRunUnclaimClearsAssignee(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "backlog")
	ctx, _, _ := newTestContext(proj, false)
	if err := runClaim(ctx, []string{slug, "--assignee", "alice"}); err != nil {
		t.Fatalf("runClaim: %v", err)
	}
	if err := runUnclaim(ctx, []string{slug}); err != nil {
		t.Fatalf("runUnclaim: %v", err)
	}
	got := loadIssueByPath(t, proj.IssueDir, issuePath)
	if got.Assignee != "" {
		t.Fatalf("assignee = %q, want empty", got.Assignee)
	}
}

func TestRunUnclaimUnknownSlug(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "backlog")
	ctx, _, _ := newTestContext(proj, false)
	if err := runUnclaim(ctx, []string{"does-not-exist"}); err == nil {
		t.Fatal("expected not-found error")
	}
}

// ------------------- done -------------------

func TestRunDoneRejectsWrongStatus(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "backlog")
	ctx, _, _ := newTestContext(proj, false)
	err := runDone(ctx, []string{slug})
	if err == nil {
		t.Fatal("expected done error from wrong status")
	}
}

func TestRunDoneUnknownSlug(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "backlog")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDone(ctx, []string{"missing"}); err == nil {
		t.Fatal("expected not-found error")
	}
}

// ------------------- comment -------------------

func TestRunCommentAddsComment(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runComment(ctx, []string{slug, "--text", "tests: 3 added"}); err != nil {
		t.Fatalf("runComment: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Comment added to cli/sample")

	comments, err := tracker.LoadComments(issuePath)
	if err != nil {
		t.Fatalf("LoadComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments len = %d, want 1", len(comments))
	}
	if !strings.Contains(comments[0].Text, "tests: 3 added") {
		t.Fatalf("comment text = %q", comments[0].Text)
	}
}

func TestRunCommentRequiresText(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runComment(ctx, []string{slug}); err == nil {
		t.Fatal("expected text-required error")
	}
}

// ------------------- check -------------------

func TestRunCheckMatchesByText(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCheck(ctx, []string{slug, "first task"}); err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	assertContains(t, stdout.String(), `✓ Checked: "first task"`)

	got := loadIssueByPath(t, proj.IssueDir, issuePath)
	if !strings.Contains(got.BodyRaw, "- [x] first task") {
		t.Fatalf("first task not checked:\n%s", got.BodyRaw)
	}
}

func TestRunCheckRejectsMissingMatch(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	err := runCheck(ctx, []string{slug, "no such item exists"})
	if err == nil {
		t.Fatal("expected no-match error")
	}
	if !strings.Contains(err.Error(), "no unchecked item matched") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunCheckRequiresQuery(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runCheck(ctx, []string{slug}); err == nil {
		t.Fatal("expected query-required error")
	}
}

// ------------------- update -------------------

func TestRunUpdateChangesTitle(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runUpdate(ctx, []string{slug, "--title", "new title"}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Updated: cli/sample")

	got := loadIssueByPath(t, proj.IssueDir, issuePath)
	if got.Title != "new title" {
		t.Fatalf("title = %q, want \"new title\"", got.Title)
	}
}

func TestRunUpdateRequiresAtLeastOneFlag(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runUpdate(ctx, []string{slug}); err == nil || !strings.Contains(err.Error(), "--title and/or --body") {
		t.Fatalf("expected title-or-body error, got %v", err)
	}
}

// ------------------- replace -------------------

func TestRunReplaceUpdatesSection(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runReplace(ctx, []string{slug, "--section", "Design", "--body", "rewritten content"}); err != nil {
		t.Fatalf("runReplace: %v", err)
	}
	assertContains(t, stdout.String(), `✓ Replaced section "Design" in cli/sample`)

	got := loadIssueByPath(t, proj.IssueDir, issuePath)
	if !strings.Contains(got.BodyRaw, "rewritten content") {
		t.Fatalf("section not replaced:\n%s", got.BodyRaw)
	}
}

func TestRunReplaceRequiresSection(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runReplace(ctx, []string{slug, "--body", "x"}); err == nil || !strings.Contains(err.Error(), "--section") {
		t.Fatalf("expected --section error, got %v", err)
	}
}

func TestRunReplaceRequiresBody(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runReplace(ctx, []string{slug, "--section", "Design"}); err == nil || !strings.Contains(err.Error(), "--body") {
		t.Fatalf("expected --body error, got %v", err)
	}
}

// ------------------- append -------------------

func TestRunAppendAppendsToBody(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runAppend(ctx, []string{slug, "--body", "appended note"}); err != nil {
		t.Fatalf("runAppend: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Appended to cli/sample")

	got := loadIssueByPath(t, proj.IssueDir, issuePath)
	if !strings.Contains(got.BodyRaw, "appended note") {
		t.Fatalf("appended note missing:\n%s", got.BodyRaw)
	}
}

func TestRunAppendRequiresBody(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runAppend(ctx, []string{slug}); err == nil || !strings.Contains(err.Error(), "--body") {
		t.Fatalf("expected --body error, got %v", err)
	}
}

// ------------------- retrospective -------------------

func TestRunRetrospectiveWritesFile(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runRetrospective(ctx, []string{slug, "--body", "tooling friction: x"}); err != nil {
		t.Fatalf("runRetrospective: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Retrospective saved for cli/sample")

	retroDir := filepath.Join(projectRoot(proj), "retros")
	entries, err := os.ReadDir(retroDir)
	if err != nil {
		t.Fatalf("read retros dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("retros dir has %d entries, want 1", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(retroDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read retro file: %v", err)
	}
	if !strings.Contains(string(body), "tooling friction: x") {
		t.Fatalf("retro body missing input: %s", body)
	}
	if !strings.Contains(string(body), "Issue: cli/sample") {
		t.Fatalf("retro body missing issue header: %s", body)
	}
}

func TestRunRetrospectiveRequiresBody(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runRetrospective(ctx, []string{slug}); err == nil || !strings.Contains(err.Error(), "--body") {
		t.Fatalf("expected --body error, got %v", err)
	}
}

// ------------------- report-bug -------------------

func TestRunReportBugWritesFile(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	bugRoot := t.TempDir()
	t.Setenv("ISSUE_VIEWER_SERVER_PWD", bugRoot)
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "cli/sample")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runReportBug(ctx, []string{"transition rejected valid input"}); err != nil {
		t.Fatalf("runReportBug: %v", err)
	}
	if !strings.Contains(stdout.String(), "Bug reported to") {
		t.Fatalf("missing bug-reported confirmation: %q", stdout.String())
	}

	bugDir := filepath.Join(bugRoot, "bugs")
	entries, err := os.ReadDir(bugDir)
	if err != nil {
		t.Fatalf("read bug dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("bug dir entries = %d, want 1", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(bugDir, entries[0].Name()))
	var entry map[string]interface{}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal bug file: %v\n%s", err, raw)
	}
	if entry["description"] != "transition rejected valid input" {
		t.Fatalf("description = %v", entry["description"])
	}
	if entry["issue_slug"] != "cli/sample" {
		t.Fatalf("issue_slug = %v", entry["issue_slug"])
	}
}

func TestRunReportBugRequiresDescription(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runReportBug(ctx, nil); err == nil || !strings.Contains(err.Error(), "requires a description") {
		t.Fatalf("expected description-required error, got %v", err)
	}
}

// ------------------- search -------------------

func TestRunSearchMatchesTitle(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runSearch(ctx, []string{"sample"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, `Search: "sample"`)
	assertContains(t, out, "cli/sample")
}

func TestRunSearchJSON(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runSearch(ctx, []string{"sample"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1", len(got))
	}
}

func TestRunSearchRequiresQuery(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runSearch(ctx, nil); err == nil || !strings.Contains(err.Error(), "requires <query>") {
		t.Fatalf("expected query-required error, got %v", err)
	}
}

// ------------------- next -------------------

func TestRunNextListsBacklogForVersion(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "backlog")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runNext(ctx, []string{"--version", "0.1"}); err != nil {
		t.Fatalf("runNext: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "== Work for v0.1 ==")
	assertContains(t, out, "Backlog — unclaimed")
	assertContains(t, out, "cli/sample")
}

func TestRunNextDesignFlagListsIdeas(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "idea")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runNext(ctx, []string{"--design", "--version", "0.1"}); err != nil {
		t.Fatalf("runNext: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "Issues needing design work")
	assertContains(t, out, "cli/sample")
}

func TestRunNextRequiresVersion(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "backlog")
	proj.Version = ""
	ctx, _, _ := newTestContext(proj, false)
	if err := runNext(ctx, nil); err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("expected version-required error, got %v", err)
	}
}

// ------------------- checklist -------------------

func TestRunChecklistText(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runChecklist(ctx, []string{slug}); err != nil {
		t.Fatalf("runChecklist: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "== Checklist (")
	assertContains(t, out, "first task")
}

func TestRunChecklistJSON(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runChecklist(ctx, []string{slug}); err != nil {
		t.Fatalf("runChecklist: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if got["total"] == nil || got["checked"] == nil {
		t.Fatalf("missing total/checked: %v", got)
	}
}

func TestRunChecklistUnknownSlug(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runChecklist(ctx, []string{"missing"}); err == nil {
		t.Fatal("expected not-found error")
	}
}

// ------------------- show / context -------------------

func TestRunShowText(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{slug}); err != nil {
		t.Fatalf("runShow: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "== sample ==")
	assertContains(t, out, "Status: in progress")
	assertContains(t, out, "== Body ==")
	assertContains(t, out, "first task")
	assertContains(t, out, "== Test Plan ==")
}

func TestRunShowJSON(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runShow(ctx, []string{slug}); err != nil {
		t.Fatalf("runShow: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if got["issue"] == nil {
		t.Fatalf("missing issue field: %v", got)
	}
}

func TestRunShowUnknownSlug(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{"missing"}); err == nil {
		t.Fatal("expected not-found error")
	}
}

// ------------------- create -------------------

func TestRunCreateNewIssue(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCreate(ctx, []string{"--title", "another idea", "--system", "CLI", "--status", "idea"}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "✓ Created:")
	assertContains(t, out, "Slug: cli/another-idea")

	if _, err := os.Stat(filepath.Join(proj.IssueDir, "CLI", "another-idea.md")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
}

func TestRunCreateRequiresTitle(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runCreate(ctx, nil); err == nil || !strings.Contains(err.Error(), "--title is required") {
		t.Fatalf("expected title-required error, got %v", err)
	}
}

func TestRunCreateRejectsBacklogStatus(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	err := runCreate(ctx, []string{"--title", "x", "--status", "backlog"})
	if err == nil || !strings.Contains(err.Error(), "cannot create issue with status") {
		t.Fatalf("expected status-rejected error, got %v", err)
	}
}

// ------------------- help -------------------

func TestRunHelpNoArgsPrintsTopLevel(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	ctx.AllProjects = []tracker.Project{*proj}
	if err := runHelp(ctx, nil); err != nil {
		t.Fatalf("runHelp: %v", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("expected help text, got empty")
	}
}

func TestRunHelpWithTopicDelegatesToProcess(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runHelp(ctx, []string{"workflow"}); err != nil {
		t.Fatalf("runHelp: %v", err)
	}
	assertContains(t, stdout.String(), "Status Lifecycle")
}

// ------------------- stats -------------------

func TestRunStatsText(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runStats(ctx, nil); err != nil {
		t.Fatalf("runStats: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "Project Stats")
	assertContains(t, out, "By status:")
	assertContains(t, out, "By system:")
	assertContains(t, out, "CLI")
}

func TestRunStatsJSON(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runStats(ctx, nil); err != nil {
		t.Fatalf("runStats: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if got["total"] == nil || got["by_status"] == nil || got["by_system"] == nil {
		t.Fatalf("missing top-level keys: %v", got)
	}
	if int(got["total"].(float64)) != 1 {
		t.Fatalf("total = %v, want 1", got["total"])
	}
}

// ------------------- data -------------------

func TestRunDataAddCreatesSidecar(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runDataAdd(ctx, []string{slug, "--description", "first finding", "--status", "open"}); err != nil {
		t.Fatalf("runDataAdd: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "1" {
		t.Fatalf("stdout = %q, want \"1\"", stdout.String())
	}

	store, err := tracker.LoadData(issuePath)
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(store.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(store.Entries))
	}
	e := store.Entries[0]
	if e.ID != 1 || e.Description != "first finding" || e.Status != "open" {
		t.Fatalf("entry = %+v", e)
	}

	sidecar := tracker.SidecarPath(issuePath)
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	raw, _ := os.ReadFile(sidecar)
	var onDisk tracker.DataStore
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal sidecar: %v\n%s", err, raw)
	}
	if onDisk.NextID != 2 {
		t.Fatalf("next_id = %d, want 2", onDisk.NextID)
	}
}

func TestRunDataAddRequiresDescription(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataAdd(ctx, []string{slug}); err == nil || !strings.Contains(err.Error(), "--description") {
		t.Fatalf("expected description-required error, got %v", err)
	}
}

func TestRunDataListJSON(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataAdd(ctx, []string{slug, "--description", "f1"}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	jctx, jstdout, _ := newTestContext(proj, true)
	if err := runDataList(jctx, []string{slug}); err != nil {
		t.Fatalf("runDataList: %v", err)
	}
	var got []tracker.DataEntry
	if err := json.Unmarshal([]byte(jstdout.String()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, jstdout.String())
	}
	if len(got) != 1 || got[0].Description != "f1" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestRunDataListEmpty(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runDataList(ctx, []string{slug}); err != nil {
		t.Fatalf("runDataList: %v", err)
	}
	assertContains(t, stdout.String(), "(no entries)")
}

func TestRunDataSetStatusUpdatesEntry(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataAdd(ctx, []string{slug, "--description", "f1", "--status", "open"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := runDataSetStatus(ctx, []string{slug, "1", "resolved"}); err != nil {
		t.Fatalf("runDataSetStatus: %v", err)
	}
	store, _ := tracker.LoadData(issuePath)
	if store.Entries[0].Status != "resolved" {
		t.Fatalf("status = %q, want resolved", store.Entries[0].Status)
	}
}

func TestRunDataSetStatusRequiresAllArgs(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataSetStatus(ctx, []string{slug, "1"}); err == nil {
		t.Fatal("expected requires-args error")
	}
}

func TestRunDataSetStatusRejectsInvalidID(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataSetStatus(ctx, []string{slug, "abc", "open"}); err == nil || !strings.Contains(err.Error(), "invalid id") {
		t.Fatalf("expected invalid-id error, got %v", err)
	}
}

func TestRunDataSetCommentUpdatesComment(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataAdd(ctx, []string{slug, "--description", "f1"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := runDataSetComment(ctx, []string{slug, "1", "--text", "note"}); err != nil {
		t.Fatalf("runDataSetComment: %v", err)
	}
	store, _ := tracker.LoadData(issuePath)
	if store.Entries[0].Comment != "note" {
		t.Fatalf("comment = %q, want note", store.Entries[0].Comment)
	}
}

func TestRunDataSetCommentRequiresArgs(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataSetComment(ctx, []string{slug}); err == nil {
		t.Fatal("expected requires-args error")
	}
}

func TestRunDataRemoveDeletesEntry(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataAdd(ctx, []string{slug, "--description", "f1"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := runDataRemove(ctx, []string{slug, "1"}); err != nil {
		t.Fatalf("runDataRemove: %v", err)
	}
	store, _ := tracker.LoadData(issuePath)
	if len(store.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(store.Entries))
	}
}

func TestRunDataRemoveRequiresArgs(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runDataRemove(ctx, nil); err == nil {
		t.Fatal("expected requires-args error")
	}
}

func TestRunDataDispatcherRequiresSubcommand(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runData(ctx, nil); err == nil || !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("expected requires-subcommand error, got %v", err)
	}
}

func TestRunDataDispatcherRejectsUnknownSubcommand(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runData(ctx, []string{"frobnicate"}); err == nil || !strings.Contains(err.Error(), "unknown data subcommand") {
		t.Fatalf("expected unknown-subcommand error, got %v", err)
	}
}

func TestRunDataDispatcherRoutesToList(t *testing.T) {
	proj, slug, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runData(ctx, []string{"list", slug}); err != nil {
		t.Fatalf("runData list: %v", err)
	}
	assertContains(t, stdout.String(), "(no entries)")
}

// ------------------- process dispatcher -------------------

func TestRunProcessOverviewNoTopic(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcess(ctx, nil); err != nil {
		t.Fatalf("runProcess: %v", err)
	}
	assertContains(t, stdout.String(), "AI-Native Project Viewer")
}

func TestRunProcessFormatTopic(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcess(ctx, []string{"format"}); err != nil {
		t.Fatalf("runProcess format: %v", err)
	}
	assertContains(t, stdout.String(), "Issue File Format")
}

func TestRunProcessUnknownTopic(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runProcess(ctx, []string{"nope"}); err == nil || !strings.Contains(err.Error(), "unknown topic") {
		t.Fatalf("expected unknown-topic error, got %v", err)
	}
}

func TestRunProcessWorkflowPrintsLifecycle(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcessWorkflow(ctx, nil); err != nil {
		t.Fatalf("runProcessWorkflow: %v", err)
	}
	assertContains(t, stdout.String(), "Status Lifecycle")
}

func TestRunProcessWorkflowRejectsNilProject(t *testing.T) {
	ctx, _, _ := newTestContext(nil, false)
	if err := runProcessWorkflow(ctx, nil); err == nil || !strings.Contains(err.Error(), "needs a project") {
		t.Fatalf("expected needs-a-project error, got %v", err)
	}
}

func TestRunProcessSystemsListsSystems(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcessSystems(ctx); err != nil {
		t.Fatalf("runProcessSystems: %v", err)
	}
	out := stdout.String()
	assertContains(t, out, "Available Systems")
	assertContains(t, out, "CLI")
}

func TestRunProcessSystemsRejectsNilProject(t *testing.T) {
	ctx, _, _ := newTestContext(nil, false)
	if err := runProcessSystems(ctx); err == nil || !strings.Contains(err.Error(), "needs a project") {
		t.Fatalf("expected needs-a-project error, got %v", err)
	}
}

// ------------------- workflow dispatcher -------------------

func TestRunWorkflowRequiresSubcommand(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, nil); err == nil || !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("expected requires-subcommand error, got %v", err)
	}
}

func TestRunWorkflowRejectsUnknownSubcommand(t *testing.T) {
	proj, _, _ := makeSimpleProject(t, "in progress")
	ctx, _, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"frobnicate"}); err == nil || !strings.Contains(err.Error(), "unknown workflow subcommand") {
		t.Fatalf("expected unknown-subcommand error, got %v", err)
	}
}

// ------------------- helpers -------------------

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
