package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchConfig_ParseAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yaml")
	yaml := `statuses:
  - name: idea
workbench:
  systems:
    AI:
      summary: "How teams choose plays"
      design_doc: docs/ai/play-calls.md
      umbrella: ai/play-calls
  metrics:
    targets: Tests/Balance/Targets.cs
    probe_results: artifacts/probe/*.json
  tuning:
    - Sim/Tuning.cs
  balance:
    comment_prefix: "balance:"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	wb := wf.Workbench
	if wb.IsZero() || !wb.HasSystems() || !wb.HasBalance() {
		t.Fatalf("workbench flags wrong: %+v", wb)
	}
	if wb.Systems["AI"].DesignDoc != "docs/ai/play-calls.md" || wb.Metrics.ProbeResults != "artifacts/probe/*.json" || wb.Tuning[0] != "Sim/Tuning.cs" {
		t.Fatalf("workbench parsed as %+v", wb)
	}
	if problems := wb.Validate(); len(problems) != 0 {
		t.Fatalf("valid config has problems: %v", problems)
	}

	clone := wf.Clone()
	clone.Workbench.Tuning[0] = "changed"
	clone.Workbench.Systems["AI"] = WorkbenchSystem{}
	if wf.Workbench.Tuning[0] != "Sim/Tuning.cs" || wf.Workbench.Systems["AI"].Summary == "" {
		t.Fatal("Clone shares workbench slices/maps with the original")
	}
	if got := wf.ForSystem("AI").Workbench; !got.HasSystems() {
		t.Fatal("ForSystem dropped the project-wide workbench block")
	}
}

func TestWorkbenchConfig_ValidateRejectsEscapingPaths(t *testing.T) {
	wb := WorkbenchConfig{
		Systems: map[string]WorkbenchSystem{"AI": {DesignDoc: "../other/doc.md"}},
		Metrics: WorkbenchMetrics{Targets: "/etc/passwd", ProbeResults: "ok/*.json"},
		Tuning:  []string{"Sim/../../x.cs", "~/tuning.cs", "Sim/Tuning.cs"},
	}
	problems := wb.Validate()
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"systems[AI].design_doc", "metrics.targets", "tuning[0]", "tuning[1]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem for %s in:\n%s", want, joined)
		}
	}
	if len(problems) != 4 {
		t.Fatalf("problems = %d, want 4:\n%s", len(problems), joined)
	}
}

func TestWorkbenchConfig_AbsentBlock(t *testing.T) {
	wf := DefaultWorkflow()
	if !wf.Workbench.IsZero() || wf.Workbench.HasSystems() || wf.Workbench.HasBalance() {
		t.Fatalf("default workflow should have no workbench: %+v", wf.Workbench)
	}
	if len(wf.Workbench.Validate()) != 0 {
		t.Fatal("empty workbench reported problems")
	}
}

func TestWorkflowSchemaSections_IncludeWorkbench(t *testing.T) {
	found := map[string]bool{}
	for _, s := range WorkflowSchemaSections() {
		found[s.Path] = true
	}
	for _, p := range []string{"workbench", "workbench.systems[<name>]", "workbench.metrics", "workbench.balance"} {
		if !found[p] {
			t.Errorf("schema missing %s", p)
		}
	}
}
