package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// chdir switches to dir and registers a cleanup that restores the original CWD.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestAvailableWorkflowTemplatesIncludesAllThree(t *testing.T) {
	got := availableWorkflowTemplates()
	want := map[string]bool{"development": false, "review": false, "writing": false}
	for _, name := range got {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("missing template %q in availableWorkflowTemplates() = %v", name, got)
		}
	}
}

func TestBundledTemplatesParseAsWorkflowConfig(t *testing.T) {
	for _, name := range availableWorkflowTemplates() {
		t.Run(name, func(t *testing.T) {
			data, err := readWorkflowTemplate(name)
			if err != nil {
				t.Fatalf("read template %q: %v", name, err)
			}
			tmp := filepath.Join(t.TempDir(), "workflow.yaml")
			if err := os.WriteFile(tmp, data, 0644); err != nil {
				t.Fatalf("write tmp: %v", err)
			}
			cfg, err := tracker.LoadWorkflow(tmp)
			if err != nil {
				t.Fatalf("LoadWorkflow(%s): %v", name, err)
			}
			if len(cfg.Statuses) == 0 {
				t.Fatalf("template %q has no statuses", name)
			}
			if len(cfg.Transitions) == 0 {
				t.Fatalf("template %q has no transitions", name)
			}
		})
	}
}

func TestDoWorkflowInitWritesFileAndScaffolds(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out bytes.Buffer
	if err := doWorkflowInit("development", false, strings.NewReader(""), &out, false); err != nil {
		t.Fatalf("doWorkflowInit: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "workflow.yaml")); err != nil {
		t.Fatalf("workflow.yaml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "issues")); err != nil {
		t.Fatalf("issues/ not scaffolded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs")); err != nil {
		t.Fatalf("docs/ not scaffolded: %v", err)
	}

	msg := out.String()
	if !strings.Contains(msg, "workflow.yaml") || !strings.Contains(msg, "development") {
		t.Errorf("unexpected stdout: %q", msg)
	}
	if !strings.Contains(msg, "issues/") || !strings.Contains(msg, "docs/") {
		t.Errorf("expected scaffold message to list issues/ and docs/, got: %q", msg)
	}
}

func TestDoWorkflowInitRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	existing := []byte("# pre-existing user content\n")
	if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), existing, 0644); err != nil {
		t.Fatalf("seed workflow.yaml: %v", err)
	}

	var out bytes.Buffer
	err := doWorkflowInit("development", false, strings.NewReader(""), &out, false)
	if err == nil {
		t.Fatal("expected error when workflow.yaml exists without --force, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected error to mention 'already exists' and '--force', got: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "workflow.yaml"))
	if string(got) != string(existing) {
		t.Errorf("workflow.yaml was modified despite refusal: %q", string(got))
	}
}

func TestDoWorkflowInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte("old content\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out bytes.Buffer
	if err := doWorkflowInit("development", true, strings.NewReader(""), &out, false); err != nil {
		t.Fatalf("doWorkflowInit force: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(got), "old content") {
		t.Errorf("--force did not overwrite, content still: %q", string(got))
	}
	if !strings.Contains(string(got), "statuses:") {
		t.Errorf("written file does not look like a workflow yaml: %q", string(got))
	}
}

func TestDoWorkflowInitUnknownTemplate(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out bytes.Buffer
	err := doWorkflowInit("does-not-exist", false, strings.NewReader(""), &out, false)
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown template") {
		t.Errorf("error should name the problem, got: %v", err)
	}
	if !strings.Contains(err.Error(), "development") {
		t.Errorf("error should list available templates, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "workflow.yaml")); statErr == nil {
		t.Error("workflow.yaml was created on unknown-template path")
	}
}

func TestDoWorkflowInitNonTTYWithoutFlagFails(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out bytes.Buffer
	err := doWorkflowInit("", false, strings.NewReader(""), &out, false)
	if err == nil {
		t.Fatal("expected error when no --template and not a TTY")
	}
	if !strings.Contains(err.Error(), "--template is required") {
		t.Errorf("error should explain non-TTY rule, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "workflow.yaml")); statErr == nil {
		t.Error("workflow.yaml was created on non-TTY-without-flag path")
	}
}

func TestDoWorkflowInitInteractiveSelectionByNumber(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	templates := availableWorkflowTemplates()
	if len(templates) == 0 {
		t.Skip("no templates bundled")
	}

	var out bytes.Buffer
	if err := doWorkflowInit("", false, strings.NewReader("1\n"), &out, true); err != nil {
		t.Fatalf("doWorkflowInit interactive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "workflow.yaml")); err != nil {
		t.Fatalf("workflow.yaml not written from interactive selection: %v", err)
	}
	if !strings.Contains(out.String(), "Pick a workflow template") {
		t.Errorf("expected interactive prompt in output, got: %q", out.String())
	}
}

func TestDoWorkflowInitInteractiveSelectionByName(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out bytes.Buffer
	if err := doWorkflowInit("", false, strings.NewReader("review\n"), &out, true); err != nil {
		t.Fatalf("doWorkflowInit interactive by name: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "inbox") {
		t.Errorf("expected review template (inbox status) in written file, got: %q", string(got))
	}
}

func TestScaffoldProjectDirsIdempotent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	first, err := scaffoldProjectDirs()
	if err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	if len(first) != 2 {
		t.Errorf("first scaffold expected 2 created dirs, got %v", first)
	}

	second, err := scaffoldProjectDirs()
	if err != nil {
		t.Fatalf("second scaffold: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second scaffold should be a no-op, got %v", second)
	}
}

func TestDoWorkflowInitProducesProjectThatLoadProjectAccepts(t *testing.T) {
	// Acceptance-criterion check: after `workflow init`, the directory should
	// satisfy issue-cli's project-detection contract. loadProject treats any
	// directory containing ./issues as a project root, so we just verify that
	// scaffolding produced that.
	dir := t.TempDir()
	chdir(t, dir)

	var out bytes.Buffer
	if err := doWorkflowInit("development", false, strings.NewReader(""), &out, false); err != nil {
		t.Fatalf("doWorkflowInit: %v", err)
	}

	info, err := os.Stat("issues")
	if err != nil || !info.IsDir() {
		t.Fatalf("issues/ missing or not a directory after init")
	}
	if _, err := tracker.LoadWorkflow("workflow.yaml"); err != nil {
		t.Fatalf("written workflow.yaml does not load: %v", err)
	}
}
