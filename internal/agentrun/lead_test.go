package agentrun

import (
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The project lead runs in the project checkout, never in a worktree.
func TestLeadSpec_ProjectLead(t *testing.T) {
	root := t.TempDir()
	proj := &tracker.Project{Slug: "p", WorkDir: root}

	spec := LeadSpec(proj, nil, root, time.Now())
	if spec.Session != "lead-p" || spec.LeadScope != tracker.LeadScopeProject || spec.WorkDir != root {
		t.Fatalf("spec = %+v", spec)
	}
	if spec.Worktree != (tracker.WorktreeSpec{}) {
		t.Fatalf("worktree = %+v, want none", spec.Worktree)
	}
	if !strings.Contains(spec.Prompt, "Role: project lead (lead-p)") {
		t.Fatalf("prompt lacks the project-lead role:\n%s", spec.Prompt)
	}
}
