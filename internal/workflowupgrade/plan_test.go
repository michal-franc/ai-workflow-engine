package workflowupgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// oldDevelopmentWorkflow is a trimmed copy of the development template as it
// shipped before stamps: an older approval prompt, no types, no actions.
const oldDevelopmentWorkflow = `statuses:
  - name: "idea"
  - name: "in design"
    prompt: |
      When the design is complete, stop and ask for backlog approval in the issue viewer before attempting the transition.
  - name: "backlog"
  - name: "in progress"
  - name: "done"
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: "require_human_approval"
        status: "backlog"
`

func mustLatest(t *testing.T, name string) string {
	t.Helper()
	v, err := BundledVersion(name)
	if err != nil {
		t.Fatalf("BundledVersion(%s): %v", name, err)
	}
	return v
}

func proposableIDs(entries []Entry, template string) []string {
	var ids []string
	for _, e := range entries {
		if e.Propose && e.appliesTo(template) && len(e.Requires) == 0 {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func proposalIDs(plan *Plan) []string {
	ids := make([]string, 0, len(plan.Proposals))
	for _, p := range plan.Proposals {
		ids = append(ids, p.ID)
	}
	return ids
}

// Each bundled template's stamp must be backed by catalog entries, and its
// features: record must be exactly what scan detects in it.
func TestWorkflowFeatureCatalogMatchesBundledTemplates(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range Templates() {
		t.Run(name, func(t *testing.T) {
			latest := mustLatest(t, name)
			data, err := ReadTemplate(name)
			if err != nil {
				t.Fatal(err)
			}
			hasStampEntry := false
			for _, e := range entries {
				if !e.appliesTo(name) {
					continue
				}
				if CompareVersions(e.Version, latest) > 0 {
					t.Errorf("%s: entry version %s is newer than the template stamp %s — bump the stamp", e.ID, e.Version, latest)
				}
				if e.Version == latest {
					hasStampEntry = true
				}
			}
			if !hasStampEntry {
				t.Errorf("template stamp %s@%s has no catalog entry with that version", name, latest)
			}
			rec, ok := ReadRecord(data)
			if !ok {
				t.Fatalf("%s.yaml has no features: record; run issue-cli workflow scan on it", name)
			}
			res, err := Scan(name+".yaml", data, ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Changes) > 0 || strings.Join(res.Record.Installed, ",") != strings.Join(rec.Installed, ",") {
				t.Errorf("%s.yaml features: record is stale; re-run issue-cli workflow scan on it. Changes: %v", name, res.Changes)
			}
			if len(rec.Declined) > 0 {
				t.Errorf("a bundled template declines nothing; got %v", rec.Declined)
			}
		})
	}
}

func TestBundledTemplatesAreStampedAndLintClean(t *testing.T) {
	for _, name := range Templates() {
		t.Run(name, func(t *testing.T) {
			data, err := ReadTemplate(name)
			if err != nil {
				t.Fatal(err)
			}
			stampName, _, ok := ParseStamp(ReadStamp(data))
			if !ok || stampName != name {
				t.Fatalf("template %s has stamp %q, want %s@vX.Y.Z", name, ReadStamp(data), name)
			}
			path := filepath.Join(t.TempDir(), "workflow.yaml")
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			wf, err := tracker.LoadWorkflow(path)
			if err != nil {
				t.Fatal(err)
			}
			if wf.Template != ReadStamp(data) {
				t.Errorf("WorkflowConfig.Template = %q, want the stamp", wf.Template)
			}
			if warnings := wf.Lint(); len(warnings) > 0 {
				t.Errorf("lint warnings: %v", warnings)
			}
		})
	}
}

func TestTemplateStampAndRecordSurviveMarshalRoundTrip(t *testing.T) {
	data, err := ReadTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	var wf tracker.WorkflowConfig
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(wf.Clone())
	if err != nil {
		t.Fatal(err)
	}
	if got := ReadStamp(out); got != ReadStamp(data) {
		t.Fatalf("stamp after round trip = %q, want %q", got, ReadStamp(data))
	}
	want, _ := ReadRecord(data)
	got, ok := ReadRecord(out)
	if !ok || strings.Join(got.Installed, ",") != strings.Join(want.Installed, ",") {
		t.Fatalf("features after round trip = %+v, want %+v", got, want)
	}
}

func TestBuildUpgradePlanUnstampedProposesMissingFeatures(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan("workflow.yaml", []byte(oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusUnstamped || plan.Template != "development" || plan.Current != "" || plan.RecordSource != "detection" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Latest != mustLatest(t, "development") {
		t.Errorf("latest = %q", plan.Latest)
	}
	want := proposableIDs(entries, "development")
	if strings.Join(proposalIDs(plan), ",") != strings.Join(want, ",") {
		t.Errorf("proposals = %v, want every proposable development entry %v", proposalIDs(plan), want)
	}
	for i, p := range plan.Proposals {
		if p.Number != i+1 || p.Question == "" || p.Snippet == "" || p.Kind != ProposalNew {
			t.Errorf("proposal %d = %+v", i, p)
		}
	}
	steps := strings.Join(plan.Steps, "\n")
	if !strings.Contains(steps, "workflow.old1.yaml") || !strings.Contains(steps, "workflow scan --installed") {
		t.Errorf("steps should name the backup file and the scan record step: %v", plan.Steps)
	}
}

func TestBuildUpgradePlanSkipsFeaturesAlreadyPresent(t *testing.T) {
	raw := oldDevelopmentWorkflow + "worktree: true\ndefault_type: feature\ntypes:\n  feature:\n    description: x\n"
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	ids := strings.Join(proposalIDs(plan), ",")
	for _, id := range []string{"worktree", "types-of-work"} {
		if strings.Contains(ids, id) {
			t.Errorf("%s is present but was proposed: %s", id, ids)
		}
		if !strings.Contains(strings.Join(plan.Present, ","), id) {
			t.Errorf("%s missing from already_present %v", id, plan.Present)
		}
	}
}

func TestBuildUpgradePlanStatuses(t *testing.T) {
	latest := mustLatest(t, "development")
	cases := []struct {
		name, raw, flag, status string
		proposals               bool
	}{
		{"up to date", "template: development@" + latest + "\n" + oldDevelopmentWorkflow, "", StatusUpToDate, false},
		{"newer than bundled", "template: development@v999.0.0\n" + oldDevelopmentWorkflow, "", StatusUpToDate, false},
		{"behind", "template: development@v0.1.0\n" + oldDevelopmentWorkflow, "", StatusBehind, true},
		{"custom", "template: custom\n" + oldDevelopmentWorkflow, "", StatusCustom, false},
		{"unknown name", "template: kanban@v0.1.0\n" + oldDevelopmentWorkflow, "", StatusUnknownTemplate, false},
		{"malformed stamp", "template: development\n" + oldDevelopmentWorkflow, "", StatusUnknownTemplate, false},
		{"unknown --template", oldDevelopmentWorkflow, "kanban", StatusUnknownTemplate, false},
		{"--template review", oldDevelopmentWorkflow, "review", StatusUnstamped, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := BuildPlan("workflow.yaml", []byte(tc.raw), tc.flag, false)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Status != tc.status {
				t.Fatalf("status = %q, want %q (%s)", plan.Status, tc.status, plan.Message)
			}
			if (len(plan.Proposals) > 0) != tc.proposals {
				t.Fatalf("proposals = %v, want any=%v", proposalIDs(plan), tc.proposals)
			}
			if plan.Message == "" {
				t.Error("message is empty")
			}
		})
	}
}

func TestBuildUpgradePlanOnlyProposesEntriesNewerThanStamp(t *testing.T) {
	latest := mustLatest(t, "development")
	plan, err := BuildPlan("workflow.yaml", []byte("template: development@v0.1.0\n"+oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Proposals {
		if CompareVersions(p.Version, "v0.1.0") <= 0 || CompareVersions(p.Version, latest) > 0 {
			t.Errorf("proposal %s version %s outside (v0.1.0, %s]", p.ID, p.Version, latest)
		}
	}
}

func TestBuildUpgradePlanRejectsConflictingTemplateFlag(t *testing.T) {
	_, err := BuildPlan("workflow.yaml", []byte("template: development@v0.1.0\n"), "review", false)
	if err == nil || !strings.Contains(err.Error(), "--template only applies") {
		t.Fatalf("err = %v", err)
	}
}

func TestNextWorkflowBackupPathSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	for _, n := range []string{"workflow.old1.yaml", "workflow.old2.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := NextBackupPath(path), filepath.Join(dir, "workflow.old3.yaml"); got != want {
		t.Fatalf("backup = %s, want %s", got, want)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.9.0", "v0.10.0", -1},
		{"v0.36.0", "v0.36.0", 0},
		{"v1.0.0", "v0.99.99", 1},
		{"0.2.0", "v0.1.9", 1},
		{"junk", "v0.1.0", -1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUpgradeProposesProofLinksFromToday(t *testing.T) {
	raw := "template: development@v0.36.0\n" + oldDevelopmentWorkflow
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Proposals {
		if p.ID != "proof-links" {
			continue
		}
		today := time.Now().Format("2006-01-02")
		if !strings.Contains(p.Snippet, `rule: "criteria_proven: since `+today+`"`) || strings.Contains(p.Snippet, "{{today}}") {
			t.Errorf("snippet should carry today's date:\n%s", p.Snippet)
		}
		return
	}
	t.Fatalf("proof-links not proposed to a v0.36.0 development workflow: %v", proposalIDs(plan))
}

// guided-check-scripts reaches every development-template project (D-1):
// upgrade proposes it to an older stamp, and a new project's template has it.
func TestUpgradeProposesGuidedCheckScripts(t *testing.T) {
	raw := "template: development@v0.36.0\n" + oldDevelopmentWorkflow
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range plan.Proposals {
		if p.ID == "guided-check-scripts" {
			found = true
			if !strings.Contains(p.Snippet, "issue-cli check-script <slug> --new <name>") || p.Question == "" {
				t.Errorf("proposal lacks the --new hint or the onboarding question: %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("guided-check-scripts not proposed to a v0.36.0 development workflow: %v", proposalIDs(plan))
	}
	tmpl, err := ReadTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tmpl), "guided-check-scripts@") || strings.Count(string(tmpl), "check-script") < 4 {
		t.Fatal("init's development template doesn't carry guided-check-scripts")
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("missing %q in:\n%s", want, got)
	}
}
