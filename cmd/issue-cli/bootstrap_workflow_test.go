package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const otherWorkflow = `
statuses:
  - name: todo
  - name: finished
`

// bootstrapFixture lays out two project roots under one temp dir — "proj"
// with ./issues and "other" — and a projects.yaml listing proj first and
// other last, each with its own workflow file. Bootstrap from proj/ used to
// pick other's workflow because the last entry won.
type bootstrapFixture struct {
	root, proj, other, cfg string
}

func newBootstrapFixture(t *testing.T, localWorkflow bool) bootstrapFixture {
	t.Helper()
	root := t.TempDir()
	f := bootstrapFixture{root: root, proj: filepath.Join(root, "proj"), other: filepath.Join(root, "other")}
	for _, dir := range []string{filepath.Join(f.proj, "issues"), filepath.Join(f.other, "issues")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if localWorkflow {
		mustWrite(t, filepath.Join(f.proj, "workflow.yaml"), typedWorkflow)
	}
	mustWrite(t, filepath.Join(f.other, "workflow.yaml"), otherWorkflow)
	f.cfg = filepath.Join(root, "projects.yaml")
	return f
}

// writeConfig writes projects.yaml from (slug, workdir, issues, workflow)
// rows in order.
func (f bootstrapFixture) writeConfig(t *testing.T, rows ...[4]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("projects:\n")
	for _, r := range rows {
		b.WriteString("  - name: " + r[0] + "\n    slug: " + r[0] + "\n")
		if r[1] != "" {
			b.WriteString("    workdir: " + r[1] + "\n")
		}
		if r[2] != "" {
			b.WriteString("    issues: " + r[2] + "\n")
		}
		if r[3] != "" {
			b.WriteString("    workflow: " + r[3] + "\n")
		}
	}
	mustWrite(t, f.cfg, b.String())
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrap_LocalWorkflowBeatsLastConfiguredProject(t *testing.T) {
	f := newBootstrapFixture(t, true)
	f.writeConfig(t,
		[4]string{"proj", f.proj, filepath.Join(f.proj, "issues"), filepath.Join(f.proj, "workflow.yaml")},
		[4]string{"other", f.other, filepath.Join(f.other, "issues"), filepath.Join(f.other, "workflow.yaml")},
	)
	withCwd(t, f.proj)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", f.cfg, "process", "workflow"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("process workflow: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "todo") || !strings.Contains(out, "discussion") {
		t.Fatalf("bootstrap loaded the wrong workflow:\n%s", out)
	}
	wantFile := filepath.Join(f.proj, "workflow.yaml")
	if !strings.Contains(out, "Workflow file: "+wantFile) {
		t.Fatalf("process workflow should print the workflow file %s:\n%s", wantFile, out)
	}

	stdout.Reset()
	if err := run([]string{"--config", f.cfg, "create", "--title", "x", "--type", "tweak"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("create --type with a local typed workflow.yaml: %v", err)
	}
}

func TestBootstrap_LocalWorkflowWithoutAnyConfigEntry(t *testing.T) {
	// No configured project matches cwd: the local file still wins over the
	// last entry, which used to be applied regardless of where it lives.
	f := newBootstrapFixture(t, true)
	f.writeConfig(t, [4]string{"other", f.other, filepath.Join(f.other, "issues"), filepath.Join(f.other, "workflow.yaml")})
	withCwd(t, f.proj)

	proj, _, err := loadProjectOrErr(f.cfg, "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := proj.LoadWorkflow().GetStatusOrder(); len(got) == 0 || got[0] != "idea" {
		t.Fatalf("expected local workflow (idea first), got %v", got)
	}
}

func TestBootstrap_NoLocalWorkflowUsesMatchingConfiguredProject(t *testing.T) {
	f := newBootstrapFixture(t, false)
	projWF := filepath.Join(f.root, "proj-workflow.yaml")
	mustWrite(t, projWF, typedWorkflow)
	// Match by issues path only, then by workdir only.
	for _, row := range [][4]string{
		{"proj", "", filepath.Join(f.proj, "issues"), projWF},
		{"proj", f.proj, "", projWF},
	} {
		f.writeConfig(t, row, [4]string{"other", f.other, filepath.Join(f.other, "issues"), filepath.Join(f.other, "workflow.yaml")})
		withCwd(t, f.proj)

		proj, _, err := loadProjectOrErr(f.cfg, "")
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		if proj.WorkflowFile != projWF {
			t.Fatalf("expected the matching project's workflow %s, got %q", projWF, proj.WorkflowFile)
		}
	}
}

func TestBootstrap_NoMatchFallsBackToDefaultNotLastProject(t *testing.T) {
	f := newBootstrapFixture(t, false)
	f.writeConfig(t, [4]string{"other", f.other, filepath.Join(f.other, "issues"), filepath.Join(f.other, "workflow.yaml")})
	withCwd(t, f.proj)

	proj, _, err := loadProjectOrErr(f.cfg, "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if proj.WorkflowFile != "" {
		t.Fatalf("an unrelated project's workflow leaked into bootstrap: %q", proj.WorkflowFile)
	}
}

func TestBootstrap_SeveralMatchingProjectsFailLoud(t *testing.T) {
	f := newBootstrapFixture(t, false)
	f.writeConfig(t,
		[4]string{"proj", f.proj, "", filepath.Join(f.other, "workflow.yaml")},
		[4]string{"proj-too", "", filepath.Join(f.proj, "issues"), filepath.Join(f.other, "workflow.yaml")},
	)
	withCwd(t, f.proj)

	_, _, err := loadProjectOrErr(f.cfg, "")
	if !errors.Is(err, errAmbiguousProject) {
		t.Fatalf("expected errAmbiguousProject, got %v", err)
	}
	for _, want := range []string{"proj\n", "proj-too\n", "Retry with: issue-cli --project"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ambiguous error should list %q:\n%v", want, err)
		}
	}
}

func TestShow_PrintsWorkflowFile(t *testing.T) {
	proj := typedProject(t, typedWorkflow, map[string]string{"a": issueFile("A", "idea", "")})
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{"a"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(stdout.String(), "Workflow file: "+proj.WorkflowFile+"\n") {
		t.Fatalf("show should print the workflow file:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "/issue/a?tab=timeline\n") {
		t.Fatalf("show should print the Timeline tab link:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Evidence: ") || !strings.Contains(stdout.String(), "/issue/a?tab=evidence\n") {
		t.Fatalf("show should print the Evidence tab link:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Brief: body\n") {
		t.Fatalf("show should print the Issue tab's brief:\n%s", stdout.String())
	}
}

func TestShow_HintsWhenNoBrief(t *testing.T) {
	file := "---\ntitle: \"A\"\nstatus: \"idea\"\n---\n\n## Idea\n- [ ] box\n"
	proj := typedProject(t, typedWorkflow, map[string]string{"a": file})
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{"a"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(stdout.String(), "Brief: none. ") || !strings.Contains(stdout.String(), "**In short:**") {
		t.Fatalf("show should hint how to write a brief:\n%s", stdout.String())
	}
}

func TestProcessTransitions_PrintsWorkflowFile(t *testing.T) {
	proj := typedProject(t, typedWorkflow, nil)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcessTransitions(ctx, nil); err != nil {
		t.Fatalf("process transitions: %v", err)
	}
	if !strings.Contains(stdout.String(), "Workflow file: "+proj.WorkflowFile+"\n") {
		t.Fatalf("process transitions should print the workflow file:\n%s", stdout.String())
	}
}
