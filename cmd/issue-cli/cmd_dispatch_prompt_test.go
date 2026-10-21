package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func makeDispatchPromptProject(t *testing.T) (*tracker.Project, string) {
	t.Helper()
	proj, slug, _ := makeSimpleProject(t, "in progress")
	root := filepath.Dir(proj.IssueDir)
	wfPath := filepath.Join(root, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte("worktree: true\nstatuses:\n  - name: \"in progress\"\n    prompt: \"Implement it.\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj.WorkflowFile = wfPath
	return proj, slug
}

func TestDispatchPrompt_PrintsServerPromptVerbatim(t *testing.T) {
	proj, slug := makeDispatchPromptProject(t)
	proj.WorkDir = t.TempDir()
	ctx, stdout, _ := newTestContext(proj, false)

	if err := runDispatchPrompt(ctx, []string{slug}); err != nil {
		t.Fatal(err)
	}
	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	// The server builds its prompt with this call and the project's workdir.
	want := tracker.BuildDispatchPrompt(proj, issue, proj.LoadWorkflowForIssue(issue), proj.WorkDir).Prompt
	if stdout.String() != want {
		t.Fatalf("output differs from the server's prompt\ngot:  %q\nwant: %q", stdout.String(), want)
	}
	if !strings.Contains(want, "Implement it.") || !strings.Contains(want, filepath.Join(proj.WorkDir, ".worktrees", "cli/sample")) {
		t.Fatalf("prompt missing status guidance or worktree path: %q", want)
	}
	if _, err := os.Stat(filepath.Join(proj.WorkDir, ".worktrees")); !os.IsNotExist(err) {
		t.Fatalf("dispatch-prompt created a worktree dir (err=%v)", err)
	}
}

func TestDispatchPrompt_JSONFlagAndGlobalJSON(t *testing.T) {
	proj, slug := makeDispatchPromptProject(t)
	root := filepath.Dir(proj.IssueDir)

	for _, tc := range []struct {
		name   string
		global bool
		args   []string
	}{
		{"local flag", false, []string{slug, "--json"}},
		{"global flag", true, []string{slug}},
	} {
		ctx, stdout, _ := newTestContext(proj, tc.global)
		if err := runDispatchPrompt(ctx, tc.args); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var dp tracker.DispatchPrompt
		if err := json.Unmarshal(stdout.Bytes(), &dp); err != nil {
			t.Fatalf("%s: invalid JSON %q: %v", tc.name, stdout.String(), err)
		}
		// No workdir configured: worktrees resolve under the issues dir's parent.
		if dp.Slug != "cli/sample" || dp.Status != "in progress" || dp.Session != "agent-cli-sample" ||
			dp.Worktree != filepath.Join(root, ".worktrees", "cli/sample") || dp.Branch != "work/cli/sample" ||
			!strings.HasPrefix(dp.Prompt, "You have been assigned this issue: cli/sample") {
			t.Fatalf("%s: got %+v", tc.name, dp)
		}
	}
}

func TestDispatchPrompt_Errors(t *testing.T) {
	proj, _ := makeDispatchPromptProject(t)
	ctx, _, _ := newTestContext(proj, false)
	if err := runDispatchPrompt(ctx, nil); err == nil {
		t.Fatal("expected an error without a slug")
	}
	if err := runDispatchPrompt(ctx, []string{"missing"}); err == nil {
		t.Fatal("expected an error for an unknown issue")
	}
}
