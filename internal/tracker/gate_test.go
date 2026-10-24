package tracker

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWaitingOnGate(t *testing.T) {
	var wf WorkflowConfig
	if err := yaml.Unmarshal([]byte(`statuses:
  - name: "in design"
  - name: "backlog"
  - name: "done"
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: "validate"
        rule: "section_checkboxes_checked: Design"
      - type: "require_human_approval"
        status: "backlog"
`), &wf); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		issue *Issue
		want  string
	}{
		{"work done, approval missing", &Issue{Status: "in design", BodyRaw: "## Design\n- [x] Approach"}, "backlog"},
		{"work left: a worker still has something to do", &Issue{Status: "in design", BodyRaw: "## Design\n- [ ] Approach"}, ""},
		{"already approved", &Issue{Status: "in design", HumanApproval: "backlog", BodyRaw: "## Design\n- [x] Approach"}, ""},
		{"no gate on the next transition", &Issue{Status: "backlog"}, ""},
		{"last status", &Issue{Status: "done"}, ""},
	} {
		if got := WaitingOnGate(&wf, tc.issue, nil); got != tc.want {
			t.Errorf("%s: WaitingOnGate = %q, want %q", tc.name, got, tc.want)
		}
	}
	if next, gate := NextGate(&wf, &Issue{Status: "in design"}); next != "backlog" || gate != "backlog" {
		t.Errorf("NextGate = %q, %q", next, gate)
	}
}
