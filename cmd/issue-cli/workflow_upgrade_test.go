package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// oldDevelopmentWorkflow is a trimmed copy of the development template as it
// shipped before stamps: no --wait prompts, no types, no actions.
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
	v, err := bundledTemplateVersion(name)
	if err != nil {
		t.Fatalf("bundledTemplateVersion(%s): %v", name, err)
	}
	return v
}

func changelogIDs(entries []changelogEntry, template string) []string {
	var ids []string
	for _, e := range entries {
		if e.appliesTo(template) {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func proposalIDs(plan *upgradePlan) []string {
	ids := make([]string, 0, len(plan.Proposals))
	for _, p := range plan.Proposals {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestWorkflowChangelogEntriesAreComplete(t *testing.T) {
	entries, err := loadWorkflowChangelog()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("bundled workflow changelog has no entries")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.ID == "" || seen[e.ID] {
			t.Errorf("entry id %q is empty or duplicated", e.ID)
		}
		seen[e.ID] = true
		if parseVersion(e.Version) == nil {
			t.Errorf("%s: version %q is not vX.Y.Z", e.ID, e.Version)
		}
		for field, v := range map[string]string{"title": e.Title, "what": e.What, "why": e.Why, "where": e.Where, "snippet": e.Snippet, "question": e.Question} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is empty", e.ID, field)
			}
		}
		if len(e.Detect.HasKey)+len(e.Detect.Contains) == 0 {
			t.Errorf("%s: detect has no has_key or contains probe", e.ID)
		}
		if len(e.Templates) == 0 {
			t.Errorf("%s: templates is empty", e.ID)
		}
		for _, tmpl := range e.Templates {
			if !isBundledTemplate(tmpl) {
				t.Errorf("%s: template %q is not bundled", e.ID, tmpl)
			}
		}
		var snippet map[string]any
		if err := yaml.Unmarshal([]byte(e.Snippet), &snippet); err != nil {
			t.Errorf("%s: snippet does not parse as YAML: %v", e.ID, err)
		}
	}
}

// Each bundled template's stamp must be backed by changelog entries, and the
// entries must agree with what the template actually ships.
func TestWorkflowChangelogMatchesBundledTemplates(t *testing.T) {
	entries, err := loadWorkflowChangelog()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range availableWorkflowTemplates() {
		t.Run(name, func(t *testing.T) {
			latest := mustLatest(t, name)
			data, err := readWorkflowTemplate(name)
			if err != nil {
				t.Fatal(err)
			}
			keys := topLevelKeys(data)
			hasStampEntry := false
			for _, e := range entries {
				if !e.appliesTo(name) {
					continue
				}
				if compareVersions(e.Version, latest) > 0 {
					t.Errorf("%s: entry version %s is newer than the template stamp %s — bump the stamp", e.ID, e.Version, latest)
				}
				if e.Version == latest {
					hasStampEntry = true
				}
				present := e.presentIn(data, keys)
				if e.InTemplate && !present {
					t.Errorf("%s: in_template is true but its detect probe doesn't find it in %s.yaml", e.ID, name)
				}
				if !e.InTemplate && present {
					t.Errorf("%s: in_template is false but %s.yaml already has it — set in_template: true", e.ID, name)
				}
			}
			if !hasStampEntry {
				t.Errorf("template stamp %s@%s has no changelog entry with that version", name, latest)
			}
		})
	}
}

func TestBundledTemplatesAreStampedAndLintClean(t *testing.T) {
	for _, name := range availableWorkflowTemplates() {
		t.Run(name, func(t *testing.T) {
			data, err := readWorkflowTemplate(name)
			if err != nil {
				t.Fatal(err)
			}
			stampName, _, ok := parseTemplateStamp(readTemplateStamp(data))
			if !ok || stampName != name {
				t.Fatalf("template %s has stamp %q, want %s@vX.Y.Z", name, readTemplateStamp(data), name)
			}
			path := filepath.Join(t.TempDir(), "workflow.yaml")
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			wf, err := tracker.LoadWorkflow(path)
			if err != nil {
				t.Fatal(err)
			}
			if wf.Template != readTemplateStamp(data) {
				t.Errorf("WorkflowConfig.Template = %q, want the stamp", wf.Template)
			}
			if warnings := wf.Lint(); len(warnings) > 0 {
				t.Errorf("lint warnings: %v", warnings)
			}
		})
	}
}

func TestTemplateStampSurvivesMarshalRoundTrip(t *testing.T) {
	data, err := readWorkflowTemplate("development")
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
	if got := readTemplateStamp(out); got != readTemplateStamp(data) {
		t.Fatalf("stamp after round trip = %q, want %q", got, readTemplateStamp(data))
	}
}

func TestBuildUpgradePlanUnstampedProposesMissingFeatures(t *testing.T) {
	entries, err := loadWorkflowChangelog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildUpgradePlan("workflow.yaml", []byte(oldDevelopmentWorkflow), "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != upgradeUnstamped || plan.Template != "development" || plan.Current != "" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Latest != mustLatest(t, "development") {
		t.Errorf("latest = %q", plan.Latest)
	}
	want := changelogIDs(entries, "development")
	if strings.Join(proposalIDs(plan), ",") != strings.Join(want, ",") {
		t.Errorf("proposals = %v, want every development entry %v", proposalIDs(plan), want)
	}
	for i, p := range plan.Proposals {
		if p.Number != i+1 || p.Question == "" || p.Snippet == "" {
			t.Errorf("proposal %d = %+v", i, p)
		}
	}
	if len(plan.Steps) == 0 || !strings.Contains(strings.Join(plan.Steps, "\n"), "workflow.old1.yaml") {
		t.Errorf("steps should name the backup file: %v", plan.Steps)
	}
}

func TestBuildUpgradePlanSkipsFeaturesAlreadyPresent(t *testing.T) {
	raw := oldDevelopmentWorkflow + "worktree: true\ndefault_type: feature\ntypes:\n  feature:\n    description: x\n"
	plan, err := buildUpgradePlan("workflow.yaml", []byte(raw), "")
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
		{"up to date", "template: development@" + latest + "\n" + oldDevelopmentWorkflow, "", upgradeUpToDate, false},
		{"newer than bundled", "template: development@v999.0.0\n" + oldDevelopmentWorkflow, "", upgradeUpToDate, false},
		{"behind", "template: development@v0.1.0\n" + oldDevelopmentWorkflow, "", upgradeBehind, true},
		{"custom", "template: custom\n" + oldDevelopmentWorkflow, "", upgradeCustom, false},
		{"unknown name", "template: kanban@v0.1.0\n" + oldDevelopmentWorkflow, "", upgradeUnknownTemplate, false},
		{"malformed stamp", "template: development\n" + oldDevelopmentWorkflow, "", upgradeUnknownTemplate, false},
		{"unknown --template", oldDevelopmentWorkflow, "kanban", upgradeUnknownTemplate, false},
		{"--template review", oldDevelopmentWorkflow, "review", upgradeUnstamped, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := buildUpgradePlan("workflow.yaml", []byte(tc.raw), tc.flag)
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
	plan, err := buildUpgradePlan("workflow.yaml", []byte("template: development@v0.1.0\n"+oldDevelopmentWorkflow), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Proposals {
		if compareVersions(p.Version, "v0.1.0") <= 0 || compareVersions(p.Version, latest) > 0 {
			t.Errorf("proposal %s version %s outside (v0.1.0, %s]", p.ID, p.Version, latest)
		}
	}
}

func TestBuildUpgradePlanRejectsConflictingTemplateFlag(t *testing.T) {
	_, err := buildUpgradePlan("workflow.yaml", []byte("template: development@v0.1.0\n"), "review")
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
	if got, want := nextWorkflowBackupPath(path), filepath.Join(dir, "workflow.old3.yaml"); got != want {
		t.Fatalf("backup = %s, want %s", got, want)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.9.0", "v0.10.0", -1},
		{"v0.34.0", "v0.34.0", 0},
		{"v1.0.0", "v0.99.99", 1},
		{"0.2.0", "v0.1.9", 1},
		{"junk", "v0.1.0", -1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func writeUpgradeProject(t *testing.T, workflow string) *tracker.Project {
	t.Helper()
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: issuesDir, WorkflowFile: path}
}

func TestRunWorkflowUpgradeIsReadOnly(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"== Workflow upgrade ==", "Status:   unstamped", "== Proposals ==", "[1] ", "Ask:", "Snippet:", "== Steps ==", "workflow.old1.yaml"} {
		assertContains(t, out, want)
	}
	got, err := os.ReadFile(proj.WorkflowFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != oldDevelopmentWorkflow {
		t.Fatal("upgrade modified workflow.yaml")
	}
	entries, _ := os.ReadDir(filepath.Dir(proj.WorkflowFile))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".old") {
			t.Fatalf("upgrade created %s", e.Name())
		}
	}
}

func TestRunWorkflowUpgradeJSON(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runWorkflowUpgrade(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"status", "template", "current", "latest", "proposals", "already_present", "backup_path", "steps", "message"} {
		if _, ok := plan[key]; !ok {
			t.Errorf("JSON plan missing %q", key)
		}
	}
	proposals, _ := plan["proposals"].([]any)
	if len(proposals) == 0 {
		t.Fatal("no proposals in JSON")
	}
	first, _ := proposals[0].(map[string]any)
	for _, key := range []string{"number", "id", "version", "title", "what", "why", "where", "snippet", "question"} {
		if _, ok := first[key]; !ok {
			t.Errorf("proposal missing %q", key)
		}
	}
}

func TestRunWorkflowUpgradeWithoutWorkflowFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	ctx, _, _ := newTestContext(&tracker.Project{Name: "test", IssueDir: dir}, false)
	err := runWorkflowUpgrade(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "workflow init") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpgradeAliasIsRegistered(t *testing.T) {
	if lookupCommand("upgrade") == nil {
		t.Fatal("upgrade command not registered")
	}
}

func TestWorkflowUpgradeHint(t *testing.T) {
	latest := mustLatest(t, "development")
	cases := []struct {
		name, workflow, want string
	}{
		{"unstamped", oldDevelopmentWorkflow, "has no template: stamp"},
		{"behind", "template: development@v0.1.0\n" + oldDevelopmentWorkflow, "is development@v0.1.0 (bundled: development@" + latest + ")"},
		{"up to date", "template: development@" + latest + "\n" + oldDevelopmentWorkflow, ""},
		{"custom", "template: custom\n" + oldDevelopmentWorkflow, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hint := workflowUpgradeHint(writeUpgradeProject(t, tc.workflow))
			if tc.want == "" {
				if hint != "" {
					t.Fatalf("hint = %q, want none", hint)
				}
				return
			}
			assertContains(t, hint, tc.want)
			assertContains(t, hint, "Tell the operator")
			assertContains(t, hint, "only if they ask")
		})
	}
}

func TestStartAndProcessWorkflowPrintUpgradeHint(t *testing.T) {
	proj, _ := makeContractFixture(t)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runStart(ctx, []string{"cli/sample"}); err != nil {
		t.Fatalf("runStart: %v", err)
	}
	assertContains(t, stdout.String(), "run `issue-cli workflow upgrade` only if they ask")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runProcessWorkflow(ctx, nil); err != nil {
		t.Fatalf("runProcessWorkflow: %v", err)
	}
	assertContains(t, stdout.String(), "run `issue-cli workflow upgrade` only if they ask")
}

func TestProcessWorkflowNoHintWhenUpToDate(t *testing.T) {
	data, err := readWorkflowTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	proj := writeUpgradeProject(t, string(data))
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runProcessWorkflow(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "workflow upgrade") {
		t.Fatalf("unexpected upgrade hint:\n%s", stdout.String())
	}
}
