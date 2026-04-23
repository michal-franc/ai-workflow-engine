package tracker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjects(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "projects.yaml")

	content := `projects:
  - name: "Game Tracker"
    slug: "game"
    issues: "./issues"
    docs: "./docs"
  - name: "Web App"
    issues: "./web-issues"
`
	os.WriteFile(fp, []byte(content), 0644)

	projects, err := LoadProjects(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}

	if projects[0].Name != "Game Tracker" || projects[0].Slug != "game" {
		t.Errorf("project[0] = {Name: %q, Slug: %q}", projects[0].Name, projects[0].Slug)
	}

	// Second project should get auto-generated slug
	if projects[1].Slug != "web-app" {
		t.Errorf("auto-generated slug = %q, want %q", projects[1].Slug, "web-app")
	}
}

func TestLoadProjects_Empty(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "projects.yaml")

	os.WriteFile(fp, []byte("projects: []\n"), 0644)

	projects, err := LoadProjects(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestLoadProjects_SlugGeneration(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "projects.yaml")

	content := `projects:
  - name: "My Cool Project"
    issues: "./issues"
`
	os.WriteFile(fp, []byte(content), 0644)

	projects, err := LoadProjects(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if projects[0].Slug != "my-cool-project" {
		t.Errorf("slug = %q, want %q", projects[0].Slug, "my-cool-project")
	}
}

func TestLoadProjects_NonexistentFile(t *testing.T) {
	_, err := LoadProjects("/nonexistent/projects.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestProjectLoadWorkflow_WithFile(t *testing.T) {
	dir := t.TempDir()
	wfPath := filepath.Join(dir, "custom-workflow.yaml")

	content := `statuses:
  - name: "todo"
    description: "To do"
    prompt: "Clarify and scope the work"
  - name: "done"
    description: "Complete"
systems:
  Combat:
    transitions:
      - from: "idea"
        to: "in design"
        actions:
          - type: inject_prompt
            prompt: "Combat guidance"
`
	os.WriteFile(wfPath, []byte(content), 0644)

	p := Project{
		Name:         "Test",
		WorkflowFile: wfPath,
	}

	wf := p.LoadWorkflow()

	// Explicit workflow files are the source of truth and should not be merged
	// with built-in defaults.
	doneStatus := wf.GetStatus("done")
	if doneStatus == nil || doneStatus.Description != "Complete" {
		t.Errorf("done description = %q, want %q", doneStatus.Description, "Complete")
	}
	todoStatus := wf.GetStatus("todo")
	if todoStatus == nil || todoStatus.Description != "To do" {
		t.Errorf("todo not found or description wrong")
	}
	if todoStatus == nil || todoStatus.Prompt != "Clarify and scope the work" {
		t.Errorf("todo prompt = %q, want %q", todoStatus.Prompt, "Clarify and scope the work")
	}
	order := wf.GetStatusOrder()
	if len(order) != 2 || order[0] != "todo" || order[1] != "done" {
		t.Errorf("expected explicit workflow order, got %v", order)
	}
	if wf.GetStatus("idea") != nil {
		t.Errorf("did not expect built-in statuses to be merged in: %v", wf.GetStatusOrder())
	}

	combat := p.LoadWorkflowForSystem("Combat")
	if combat == nil {
		t.Fatal("expected workflow for Combat system")
	}
	prompts := combat.TransitionPrompts("idea", "in design")
	if len(prompts) == 0 || prompts[0] != "Combat guidance" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}
}

func TestProjectLoadWorkflow_FallbackToDefault(t *testing.T) {
	// Use a temp dir as cwd to avoid picking up a real workflow.yaml
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	p := Project{
		Name:         "Test",
		WorkflowFile: "/nonexistent/workflow.yaml",
	}

	wf := p.LoadWorkflow()
	order := wf.GetStatusOrder()
	// Should fall back to default workflow
	if len(order) == 0 {
		t.Fatal("expected default workflow statuses")
	}
	if order[0] != "idea" {
		t.Errorf("first status = %q, want %q (default workflow)", order[0], "idea")
	}
}
