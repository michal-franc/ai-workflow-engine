package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/checks"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// makeCheckScriptFixture is a project without worktrees (scripts run from
// its folder) and one issue at human-testing with Manual boxes.
func makeCheckScriptFixture(t *testing.T, workflow string) (*tracker.Project, string) {
	t.Helper()
	dir := t.TempDir()
	issues := filepath.Join(dir, "issues", "CLI")
	if err := os.MkdirAll(issues, 0o755); err != nil {
		t.Fatal(err)
	}
	if workflow == "" {
		workflow = "statuses:\n  - name: \"testing\"\n  - name: \"human-testing\"\n"
	}
	wfPath := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wfPath, []byte(workflow), 0o644)
	body := `---
title: "lead tab"
status: "human-testing"
system: "CLI"
---

## Test Plan

### Automated
- [x] Tests pass

### Manual
- [x] Sandbox prepared
- [ ] The tab opens detached
- [ ] Spawning twice adds nothing

## Human Testing
- [ ] Owner checked it
`
	os.WriteFile(filepath.Join(issues, "lead-tab.md"), []byte(body), 0o644)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return &tracker.Project{Name: "t", Slug: "t", IssueDir: filepath.Join(dir, "issues"), WorkflowFile: wfPath, WorkDir: dir}, dir
}

const checkSlug = "cli/lead-tab"

func TestCheckScriptLibPrintsTheLibrary(t *testing.T) {
	ctx, out, _ := newTestContext(nil, false)
	if err := runCheckScript(ctx, []string{"--lib"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(checks.Lib) {
		t.Fatal("--lib doesn't print the embedded library")
	}
}

func TestCheckScriptNeedsASlug(t *testing.T) {
	ctx, _, _ := newTestContext(nil, false)
	err := runCheckScript(ctx, nil)
	var ue *usageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "check-script --lib") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckScriptNoScriptsSaysHowToStartOne(t *testing.T) {
	proj, _ := makeCheckScriptFixture(t, "")
	ctx, _, _ := newTestContext(proj, false)
	err := runCheckScript(ctx, []string{checkSlug})
	var ce *exitCodeError
	if !errors.As(err, &ce) || ce.Code != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	assertContains(t, ce.Msg, "no check scripts for cli/lead-tab")
	assertContains(t, ce.Msg, "issue-cli check-script cli/lead-tab --new <name>")
}

func TestCheckScriptNewWritesOneStepPerUntickedManualBox(t *testing.T) {
	proj, dir := makeCheckScriptFixture(t, "")
	ctx, out, _ := newTestContext(proj, false)
	if err := runCheckScript(ctx, []string{checkSlug, "--new", "lead-tab"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scripts", "checks", "lead-tab.sh")
	assertContains(t, out.String(), "✓ Wrote "+path)
	assertContains(t, out.String(), "check-script cli/lead-tab --yes")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	assertContains(t, s, "# issue: cli/lead-tab\n")
	assertContains(t, s, "step TP3 'The tab opens detached' --box TP3")
	assertContains(t, s, "step TP4 'Spawning twice adds nothing' --box TP4")
	assertNotContains(t, s, "Sandbox prepared")
	assertNotContains(t, s, "Owner checked it")

	// A second --new with the same name refuses to overwrite.
	ctx, _, _ = newTestContext(proj, false)
	if err := runCheckScript(ctx, []string{checkSlug, "--new", "lead-tab"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second --new: %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	var ue *usageError
	if err := runCheckScript(ctx, []string{checkSlug, "--new", "Lead Tab"}); !errors.As(err, &ue) {
		t.Fatalf("bad name: %v", err)
	}
}

func writeCheckScript(t *testing.T, dir, name, slug, body string) {
	t.Helper()
	path := filepath.Join(dir, "scripts", "checks", name+".sh")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("#!/usr/bin/env bash\n# issue: "+slug+"\nsource \"${CHECKS_LIB:?}\"\n"+body), 0o755)
}

func TestCheckScriptListAndJSON(t *testing.T) {
	proj, dir := makeCheckScriptFixture(t, "")
	writeCheckScript(t, dir, "lead-tab", checkSlug, "summary\n")
	writeCheckScript(t, dir, "elsewhere", "cli/other", "summary\n")
	ctx, out, _ := newTestContext(proj, false)
	if err := runCheckScript(ctx, []string{checkSlug, "--list"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, out.String(), "lead-tab  "+filepath.Join(dir, "scripts", "checks", "lead-tab.sh")+"  (no owner run yet)")
	assertNotContains(t, out.String(), "elsewhere")

	ctx, out, _ = newTestContext(proj, true)
	if err := runCheckScript(ctx, []string{checkSlug, "--list"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, out.String(), `"name": "lead-tab"`)
	assertContains(t, out.String(), `"owner_run": false`)
}

func TestCheckScriptRunsInAMachineRunFromThePipe(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	proj, dir := makeCheckScriptFixture(t, "")
	writeCheckScript(t, dir, "lead-tab", checkSlug, `
echo "ENV slug=$CHECKS_SLUG project=$CHECKS_PROJECT root=$CHECKS_ROOT name=$CHECKS_NAME"
step TP3 "passes"
expect true fine
summary
`)
	writeCheckScript(t, dir, "broken", checkSlug, "step TP4 fails\nexpect false nope\nsummary\n")

	ctx, out, _ := newTestContext(proj, false)
	err := runCheckScript(ctx, []string{checkSlug, "lead-tab"})
	if err != nil {
		t.Fatalf("one passing script: %v\n%s", err, out)
	}
	assertContains(t, out.String(), "ENV slug=cli/lead-tab project=t root="+dir+" name=lead-tab")
	assertContains(t, out.String(), "Machine run")
	assertNotContains(t, out.String(), "broken")

	ctx, out, _ = newTestContext(proj, false)
	err = runCheckScript(ctx, []string{checkSlug, "--yes"})
	var ce *exitCodeError
	if !errors.As(err, &ce) || ce.Code != 1 || ce.Msg != "check script failed: broken" {
		t.Fatalf("err = %v\n%s", err, out)
	}
	assertContains(t, out.String(), "✓ TP3 pass")
	assertContains(t, out.String(), "✗ TP4 fail: nope")

	ctx, _, _ = newTestContext(proj, false)
	if err := runCheckScript(ctx, []string{checkSlug, "nope"}); !errors.As(err, &ce) || !strings.Contains(ce.Msg, "no check script nope") {
		t.Fatalf("unknown name: %v", err)
	}
}

func TestCheckScriptWithoutItsWorktree(t *testing.T) {
	proj, _ := makeCheckScriptFixture(t, "worktree: true\nstatuses:\n  - name: \"human-testing\"\n")
	ctx, _, _ := newTestContext(proj, false)
	err := runCheckScript(ctx, []string{checkSlug})
	if err == nil || !strings.Contains(err.Error(), "no worktree for cli/lead-tab") || !strings.Contains(err.Error(), "git worktree add") {
		t.Fatalf("err = %v", err)
	}
	// --list still answers (exit 1), so the mod's probe never hangs.
	ctx, _, _ = newTestContext(proj, false)
	var ce *exitCodeError
	if err := runCheckScript(ctx, []string{checkSlug, "--list"}); !errors.As(err, &ce) || ce.Code != 1 {
		t.Fatalf("--list without a worktree: %v", err)
	}
}

func TestManualBoxesStopAtTheNextHeading(t *testing.T) {
	body := "## Test Plan\n\n### Manual\n- [ ] one\n\n### Notes\n- [ ] not manual\n\n## Other\n- [ ] no\n"
	got := manualBoxes(body)
	if len(got) != 1 || got[0].Text != "one" {
		t.Fatalf("manualBoxes = %+v", got)
	}
}

func TestCheckScriptHintAtTestingAndHumanTesting(t *testing.T) {
	wf := "statuses:\n  - name: \"testing\"\n    prompt: \"Pass issue-cli check-script <slug> --yes.\"\n  - name: \"human-testing\"\n"
	proj, dir := makeCheckScriptFixture(t, wf)
	ctx, _, _ := newTestContext(proj, false)
	issue, _, err := findIssueOrErr(ctx, checkSlug)
	if err != nil {
		t.Fatal(err)
	}
	hint := func(status string) string {
		var b strings.Builder
		i := *issue
		i.Status = status
		printCheckScriptHint(&b, proj, proj.LoadWorkflowForIssue(&i), &i)
		return b.String()
	}
	assertContains(t, hint("human-testing"), "None yet.")
	assertContains(t, hint("human-testing"), "issue-cli check-script cli/lead-tab --new <name>")
	if got := hint("in progress"); got != "" {
		t.Fatalf("in progress: %q", got)
	}

	writeCheckScript(t, dir, "lead-tab", checkSlug, "summary\n")
	got := hint("testing")
	assertContains(t, got, "Scripts: lead-tab")
	assertContains(t, got, "issue-cli check-script cli/lead-tab --yes")
	assertNotContains(t, hint("human-testing"), "--yes")

	// A workflow without the feature says nothing.
	plain, _ := makeCheckScriptFixture(t, "")
	var b strings.Builder
	printCheckScriptHint(&b, plain, plain.LoadWorkflowForIssue(issue), issue)
	if b.Len() != 0 {
		t.Fatalf("without the feature: %q", b.String())
	}
}

func TestCheckScriptLibNeedsNoProject(t *testing.T) {
	var out, errw strings.Builder
	cfg := filepath.Join(t.TempDir(), "projects.yaml")
	os.WriteFile(cfg, []byte("projects:\n  - {name: a, slug: a, issues: /nonexistent/a}\n  - {name: b, slug: b, issues: /nonexistent/b}\n"), 0o644)
	if err := runTraced([]string{"--config", cfg, "check-script", "--lib"}, strings.NewReader(""), &out, &errw, &invocation{}); err != nil {
		t.Fatalf("--lib with an ambiguous project: %v %s", err, errw.String())
	}
	if out.String() != string(checks.Lib) {
		t.Fatal("--lib didn't print the library")
	}
}
