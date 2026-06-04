package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// importedStatus reads the status frontmatter line from a freshly imported issue file.
func importedStatus(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading imported issue: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "status:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "status:")), `"`)
		}
	}
	t.Fatalf("no status line in %s", path)
	return ""
}

// writeWorkflow writes a minimal workflow file whose first status is distinctive,
// so a passing test proves the import default is derived from the workflow rather
// than a hardcoded literal.
func writeWorkflow(t *testing.T, dir, firstStatus string) string {
	t.Helper()
	path := filepath.Join(dir, "workflow.yaml")
	content := "statuses:\n  - name: \"" + firstStatus + "\"\n  - name: \"backlog\"\n  - name: \"done\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportGitHubIssue_DefaultsToWorkflowFirstStatus(t *testing.T) {
	tmp := t.TempDir()
	issueDir := filepath.Join(tmp, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}
	proj := &tracker.Project{
		Slug:         "test-project",
		IssueDir:     issueDir,
		WorkflowFile: writeWorkflow(t, tmp, "spark"),
		// ImportStatus intentionally left empty to exercise the fallback.
	}
	gh := GitHubIssue{Number: 42, Title: "Imported", Body: "body", UpdatedAt: time.Now()}

	path, err := ImportGitHubIssue(proj, gh)
	if err != nil {
		t.Fatalf("ImportGitHubIssue: %v", err)
	}
	if got := importedStatus(t, path); got != "spark" {
		t.Errorf("imported status = %q, want the workflow first status %q (not backlog)", got, "spark")
	}
}

func TestImportGitHubIssue_ExplicitImportStatusOverrides(t *testing.T) {
	tmp := t.TempDir()
	issueDir := filepath.Join(tmp, "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatal(err)
	}
	proj := &tracker.Project{
		Slug:         "test-project",
		IssueDir:     issueDir,
		WorkflowFile: writeWorkflow(t, tmp, "spark"),
		ImportStatus: "triage",
	}
	gh := GitHubIssue{Number: 7, Title: "Imported", Body: "body", UpdatedAt: time.Now()}

	path, err := ImportGitHubIssue(proj, gh)
	if err != nil {
		t.Fatalf("ImportGitHubIssue: %v", err)
	}
	if got := importedStatus(t, path); got != "triage" {
		t.Errorf("imported status = %q, want explicit override %q", got, "triage")
	}
}
