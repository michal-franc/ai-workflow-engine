package tracker

import "testing"

func gateWorkflow() *WorkflowConfig {
	return &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "in design"}, {Name: "backlog"}},
		Transitions: []WorkflowTransition{{
			From: "in design",
			To:   "backlog",
			Actions: []WorkflowAction{
				{Type: "validate", Rule: "section_checkboxes_checked: Design"},
				{Type: "require_human_approval", Status: "backlog"},
				{Type: "validate", Rule: "has_assignee"},
			},
		}},
	}
}

func failedSteps(p TransitionPreview) []string {
	var out []string
	for _, s := range p.Steps {
		if s.Outcome == "failed" {
			out = append(out, s.ActionType)
		}
	}
	return out
}

func TestPreviewTransitionAll_CollectsEveryFailure(t *testing.T) {
	issue := &Issue{Slug: "x", BodyRaw: "## Design\n- [ ] todo\n"}
	wf := gateWorkflow()

	all := wf.PreviewTransitionAll(issue, "in design", "backlog", "", nil)
	if got := failedSteps(all); len(got) != 3 {
		t.Fatalf("failed steps = %v, want validate+approval+validate", got)
	}
	if all.Allowed || all.ValidationError == "" {
		t.Fatalf("preview should be disallowed with the first error: %+v", all)
	}

	first := wf.PreviewTransition(issue, "in design", "backlog", "", nil)
	if got := failedSteps(first); len(got) != 1 {
		t.Fatalf("PreviewTransition must keep stopping at the first failure, got %v", got)
	}
	if first.ValidationError != all.ValidationError {
		t.Fatalf("first error differs: %q vs %q", first.ValidationError, all.ValidationError)
	}
}

func TestPreviewTransitionAll_PassesWhenMet(t *testing.T) {
	issue := &Issue{Slug: "x", Assignee: "a", HumanApproval: "backlog", BodyRaw: "## Design\n- [x] done\n"}
	p := gateWorkflow().PreviewTransitionAll(issue, "in design", "backlog", "", nil)
	if !p.Allowed || len(failedSteps(p)) != 0 {
		t.Fatalf("preview = %+v, want allowed", p)
	}
}
