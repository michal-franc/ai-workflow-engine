package tracker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func proofWorkflow(since string) *WorkflowConfig {
	return &WorkflowConfig{
		Statuses: []WorkflowStatus{{Name: "in design"}, {Name: "backlog"}, {Name: "testing"}, {Name: "human-testing"}},
		Transitions: []WorkflowTransition{
			{From: "idea", To: "in design", Actions: []WorkflowAction{{Type: "append_section", Title: "Acceptance Criteria", Body: "- [ ] First observable requirement\n- [ ] Second observable requirement\n"}}},
			{From: "in design", To: "backlog", Actions: []WorkflowAction{{Type: "validate", Rule: "criteria_checkboxes: Acceptance Criteria"}}},
			{From: "testing", To: "human-testing", Actions: []WorkflowAction{{Type: "validate", Rule: "criteria_proven: " + since}}},
		},
	}
}

func TestCriteriaCheckboxesRule(t *testing.T) {
	w := proofWorkflow("since 2026-10-23")
	cases := []struct {
		name, body, want string
	}{
		{"all boxes", "## Acceptance Criteria\n- [ ] Board shows the count\n- [x] Filter keeps it\n", ""},
		{"plain bullet", "## Acceptance Criteria\n- [ ] Board shows the count\n- Filter keeps it\n", "plain bullet, not a checkbox: Filter keeps it"},
		{"placeholder", "## Acceptance Criteria\n- [x] First observable requirement\n- [ ] Real one\n", "AC1 still has the template text"},
		{"no boxes", "## Acceptance Criteria\n- just a bullet\n", "no checkboxes found"},
		{"no section", "Body only.\n", "no checkboxes found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issue := &Issue{Slug: "s/x", BodyRaw: c.body}
			err := w.ValidateTransition(issue, "in design", "backlog", nil)
			if c.want == "" {
				if err != nil {
					t.Fatalf("want pass, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestCriteriaProvenRule(t *testing.T) {
	defer SetProofChecker(proofChecker)
	calls := 0
	SetProofChecker(func(w *WorkflowConfig, issue *Issue, comments []Comment) error {
		calls++
		return errors.New("1 of 1 acceptance criteria are not proven")
	})
	w := proofWorkflow("since 2026-10-23")
	body := "## Acceptance Criteria\n- [ ] Board shows the count\n"

	if err := w.ValidateTransition(&Issue{Slug: "s/old", Created: "2026-10-01", BodyRaw: body}, "testing", "human-testing", nil); err != nil {
		t.Errorf("issue created before the gate: %v", err)
	}
	if err := w.ValidateTransition(&Issue{Slug: "s/bug", Created: "2026-10-24", BodyRaw: "## Repro\nsteps\n"}, "testing", "human-testing", nil); err != nil {
		t.Errorf("issue without criteria: %v", err)
	}
	if calls != 0 {
		t.Errorf("checker ran %d times for issues the gate doesn't cover", calls)
	}
	err := w.ValidateTransition(&Issue{Slug: "s/new", Created: "2026-10-23", BodyRaw: body}, "testing", "human-testing", nil)
	if err == nil || calls != 1 {
		t.Errorf("new issue: err %v, calls %d; want the checker's error", err, calls)
	}

	if !w.ProofRequired(&Issue{Created: "2026-10-23", BodyRaw: body}) || w.ProofRequired(&Issue{Created: "2026-10-22", BodyRaw: body}) {
		t.Error("ProofRequired disagrees with the rule's date")
	}
	if !w.TransitionChecksProof("testing", "human-testing") || w.TransitionChecksProof("in design", "backlog") {
		t.Error("TransitionChecksProof picked the wrong edge")
	}

	SetProofChecker(nil)
	if err := w.ValidateTransition(&Issue{Slug: "s/new", Created: "2026-10-23", BodyRaw: body}, "testing", "human-testing", nil); err == nil || !strings.Contains(err.Error(), "issue-cli") {
		t.Errorf("without a checker: %v, want a pointer to issue-cli", err)
	}

	bad := proofWorkflow("after 2026-10-23")
	if err := bad.ValidateTransition(&Issue{Slug: "s/new", Created: "2026-10-23", BodyRaw: body}, "testing", "human-testing", nil); err == nil || !strings.Contains(err.Error(), "since YYYY-MM-DD") {
		t.Errorf("malformed argument: %v", err)
	}
}

func TestIsVisualCriterionAndHasFeature(t *testing.T) {
	if !IsVisualCriterion("Card shows the count (visual)") || IsVisualCriterion("visual (count) shown") {
		t.Error("IsVisualCriterion")
	}
	w := &WorkflowConfig{Features: FeatureRecord{Installed: []string{"playtest-sessions@v0.36.0"}}}
	if !w.HasFeature("playtest-sessions") || w.HasFeature("playtest") {
		t.Error("HasFeature")
	}
}

// The transition parses the issue from its file name; the proof checks
// need its full slug (system folder included) to find work/<slug>.
func TestApplyTransitionGivesTheCheckerTheFullSlug(t *testing.T) {
	defer SetProofChecker(proofChecker)
	var got string
	SetProofChecker(func(w *WorkflowConfig, issue *Issue, comments []Comment) error {
		got = issue.Slug
		return nil
	})
	root := t.TempDir()
	path := filepath.Join(root, "Demo", "count-badge.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(p, c string) {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(path, "---\ntitle: \"count badge\"\nstatus: \"testing\"\ncreated: \"2026-10-25\"\n---\n\n## Acceptance Criteria\n- [ ] Badge shows\n")
	w := proofWorkflow("since 2026-10-23")
	w.IssuesRoot = root
	if _, _, err := w.ApplyTransitionToFile(path, "human-testing"); err != nil {
		t.Fatal(err)
	}
	if got != "demo/count-badge" {
		t.Errorf("checker saw slug %q, want demo/count-badge", got)
	}
}
