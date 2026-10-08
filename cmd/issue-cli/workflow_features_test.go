package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var featureCategories = map[string]bool{"lifecycle": true, "gates": true, "automation": true, "actions": true, "board": true, "dispatch": true, "conventions": true}

func TestWorkflowFeatureCatalogIsComplete(t *testing.T) {
	entries, err := loadFeatureCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("bundled feature catalog has no entries")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.ID == "" || seen[e.ID] || strings.ContainsAny(e.ID, "@ ,") {
			t.Errorf("entry id %q is empty, duplicated or has @/space/comma", e.ID)
		}
		seen[e.ID] = true
		if parseVersion(e.Version) == nil {
			t.Errorf("%s: version %q is not vX.Y.Z", e.ID, e.Version)
		}
		if !featureCategories[e.Category] {
			t.Errorf("%s: unknown category %q", e.ID, e.Category)
		}
		for field, v := range map[string]string{"title": e.Title, "what": e.What, "why": e.Why, "where": e.Where, "snippet": e.Snippet, "question": e.Question} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is empty", e.ID, field)
			}
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
		if len(e.Detect) == 0 {
			t.Errorf("%s: detect has no probes", e.ID)
		}
		for _, p := range e.Detect {
			conds := 0
			for _, set := range []bool{p.Equals != nil, len(p.OneOf) > 0, len(p.RuleIn) > 0, p.Contains != ""} {
				if set {
					conds++
				}
			}
			if conds > 1 {
				t.Errorf("%s: probe %s has %d conditions, want at most one", e.ID, p.Path, conds)
			}
			if err := schemaPathExists(reflect.TypeOf(tracker.WorkflowConfig{}), p.Path); err != nil {
				t.Errorf("%s: probe path %s: %v", e.ID, p.Path, err)
			}
		}
	}
}

// schemaPathExists checks a probe path against the WorkflowConfig yaml tags,
// so a probe can't silently look at a key the parser doesn't have.
func schemaPathExists(typ reflect.Type, path string) error {
	for _, seg := range strings.Split(path, ".") {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if seg == "*" {
			if typ.Kind() != reflect.Map {
				return &probePathError{seg, typ}
			}
			typ = typ.Elem()
			continue
		}
		key, isList := strings.CutSuffix(seg, "[]")
		if typ.Kind() != reflect.Struct {
			return &probePathError{seg, typ}
		}
		var next reflect.Type
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if name, _, _ := strings.Cut(f.Tag.Get("yaml"), ","); name == key {
				next = f.Type
			}
		}
		if next == nil {
			return &probePathError{seg, typ}
		}
		for next.Kind() == reflect.Pointer {
			next = next.Elem()
		}
		if isList {
			if next.Kind() != reflect.Slice {
				return &probePathError{seg, next}
			}
			next = next.Elem()
		}
		typ = next
	}
	return nil
}

type probePathError struct {
	seg string
	typ reflect.Type
}

func (e *probePathError) Error() string {
	return "segment " + e.seg + " does not resolve in " + e.typ.String()
}

func detectedIDs(t *testing.T, raw string) []string {
	t.Helper()
	entries, err := loadFeatureCatalog()
	if err != nil {
		t.Fatal(err)
	}
	doc := parseWorkflowDoc([]byte(raw))
	var ids []string
	for _, e := range entries {
		if detectFeature(e, doc).Matches > 0 {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func TestFeatureProbesAreStructural(t *testing.T) {
	cases := []struct {
		name, raw string
		want, not []string
	}{
		{
			name: "optional alone is not a parked status",
			raw:  "statuses:\n  - name: testing\n    optional: true\n",
			want: []string{"optional-statuses"},
			not:  []string{"parked-status"},
		},
		{
			name: "global status is parked",
			raw:  "statuses:\n  - name: blocked\n    global: true\n",
			want: []string{"parked-status"},
		},
		{
			name: "sandbox in a description is not the sandbox convention",
			raw:  "statuses:\n  - name: testing\n    description: run it in a sandbox\n",
			not:  []string{"sandbox-manual-tests", "status-prompts"},
		},
		{
			name: "rule names match before the colon",
			raw:  "transitions:\n  - from: a\n    to: b\n    actions:\n      - type: validate\n        rule: \"section_checkboxes_checked: Design\"\n      - type: validate\n        rule: has_any_label\n",
			want: []string{"checklist-gates", "label-gates"},
			not:  []string{"human-approval-gates"},
		},
		{
			name: "type overlays are searched",
			raw:  "types:\n  bugfix:\n    transitions:\n      - from: a\n        to: b\n        actions:\n          - type: require_human_approval\n            status: b\n",
			want: []string{"types-of-work", "human-approval-gates"},
		},
		{
			name: "system overlays are searched",
			raw:  "systems:\n  API:\n    statuses:\n      - name: a\n        prompt: say what you need, then end your turn\n",
			want: []string{"system-overlays", "lead-handoff-prompts", "status-prompts"},
		},
		{
			name: "false and empty values are absent",
			raw:  "worktree: false\nallow_shell: false\nscoring:\n  enabled: false\nsystems: {}\nproject_actions: []\n",
			not:  []string{"worktree", "shell-gate", "scoring", "system-overlays", "project-actions"},
		},
		{
			name: "deprecated actions alias counts as issue actions",
			raw:  "actions:\n  - id: x\n    label: X\n    prompt: p\n",
			want: []string{"issue-actions"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := "," + strings.Join(detectedIDs(t, tc.raw), ",") + ","
			for _, id := range tc.want {
				if !strings.Contains(got, ","+id+",") {
					t.Errorf("%s not detected in %s", id, got)
				}
			}
			for _, id := range tc.not {
				if strings.Contains(got, ","+id+",") {
					t.Errorf("%s detected but should not be: %s", id, got)
				}
			}
		})
	}
}

func TestDetectFeatureEvidenceNamesTheConcretePath(t *testing.T) {
	entries, _ := loadFeatureCatalog()
	e := catalogIndex(entries)["test-plan-gate"]
	raw := "transitions:\n  - from: a\n    to: b\n    actions:\n      - type: inject_prompt\n      - type: validate\n        rule: has_test_plan\n"
	d := detectFeature(e, parseWorkflowDoc([]byte(raw)))
	if d.Matches != 1 || d.Evidence != `transitions[0].actions[1].rule = "has_test_plan"` {
		t.Fatalf("detection = %+v", d)
	}
}

const recordWorkflow = `# leading comment
template: development@v0.1.0

# statuses comment
statuses:
  - name: "idea"
`

func TestWriteFeatureRecordInsertsAfterTemplate(t *testing.T) {
	rec := tracker.FeatureRecord{Installed: []string{"status-prompts@v0.34.0"}, Declined: []string{"scoring"}}
	out, err := writeFeatureRecord([]byte(recordWorkflow), rec)
	if err != nil {
		t.Fatal(err)
	}
	want := "# leading comment\ntemplate: development@v0.1.0\n\n" + featureRecordComment + "\n" + renderFeatureRecord(rec) + "\n# statuses comment\nstatuses:\n  - name: \"idea\"\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestWriteFeatureRecordInsertsBeforeFirstKeyWithoutTemplate(t *testing.T) {
	raw := "# header\n# more\n\nstatuses:\n  - name: idea\n"
	out, err := writeFeatureRecord([]byte(raw), tracker.FeatureRecord{Installed: []string{"x@v1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "# header\n# more\n\n"+featureRecordComment+"\nfeatures:\n") || !strings.HasSuffix(string(out), "\nstatuses:\n  - name: idea\n") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestWriteFeatureRecordReplacesExistingBlock(t *testing.T) {
	for _, existing := range []string{
		"features:\n  installed:\n    - old@v0.1.0\n  declined: [a, b]\n",
		"features: {installed: [old@v0.1.0]}\n",
	} {
		raw := "template: development@v0.1.0\n" + existing + "\n# keep me\nstatuses:\n  - name: idea\n"
		rec := tracker.FeatureRecord{Installed: []string{"new@v0.34.0"}}
		out, err := writeFeatureRecord([]byte(raw), rec)
		if err != nil {
			t.Fatal(err)
		}
		want := "template: development@v0.1.0\n" + renderFeatureRecord(rec) + "\n# keep me\nstatuses:\n  - name: idea\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", out, want)
		}
	}
}

func TestWriteFeatureRecordRejectsUnparseableFile(t *testing.T) {
	if _, err := writeFeatureRecord([]byte("statuses: [\n"), tracker.FeatureRecord{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestScanWorkflowFeaturesMergesRecordAndFlags(t *testing.T) {
	raw := "features:\n  installed:\n    - scoring@v0.1.0\n    - status-prompts@v0.1.0\n    - from-the-future@v9.0.0\n  declined:\n    - worktree\n    - parked-status\n" +
		"statuses:\n  - name: idea\n    prompt: think\n    global: true\n"
	res, err := scanWorkflowFeatures("workflow.yaml", []byte(raw), scanOptions{Installed: []string{"types-of-work"}, Declined: []string{"issue-actions"}, Forget: []string{"worktree"}})
	if err != nil {
		t.Fatal(err)
	}
	gotInstalled := strings.Join(res.Record.Installed, ",")
	// status-prompts keeps its recorded version; scoring is recorded but not
	// detected (kept); parked-status is detected, so it leaves declined.
	want := "status-prompts@v0.1.0,parked-status@v0.34.0,types-of-work@v0.34.0,scoring@v0.1.0,from-the-future@v9.0.0"
	if gotInstalled != want {
		t.Errorf("installed = %s\nwant      %s", gotInstalled, want)
	}
	if got := strings.Join(res.Record.Declined, ","); got != "issue-actions" {
		t.Errorf("declined = %s, want issue-actions (worktree forgotten, parked-status installed)", got)
	}
	changes := strings.Join(res.Changes, "\n")
	for _, c := range []string{"+ installed parked-status@v0.34.0", "- declined parked-status (now installed)", "+ installed types-of-work@v0.34.0", "+ declined issue-actions", "- declined worktree (re-opened)"} {
		assertContains(t, changes, c)
	}
	notes := strings.Join(res.Notes, "\n")
	assertContains(t, notes, "scoring is recorded as installed but not detected")
	assertContains(t, notes, "from-the-future is not in this issue-cli's catalog")
}

func TestScanWorkflowFeaturesRejectsBadFlags(t *testing.T) {
	raw := []byte("worktree: true\n")
	cases := map[string]scanOptions{
		"unknown feature id":    {Installed: []string{"nope"}},
		"is present in":         {Declined: []string{"worktree"}},
		"pick one":              {Installed: []string{"scoring"}, Declined: []string{"scoring"}},
		"does not parse as a Y": {},
	}
	for want, opts := range cases {
		in := raw
		if want == "does not parse as a Y" {
			in = []byte("- just a list\n")
		}
		if _, err := scanWorkflowFeatures("workflow.yaml", in, opts); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: err = %v, want %q", opts, err, want)
		}
	}
}

func TestScanWorkflowFeaturesForgetRemovesUndetectedInstall(t *testing.T) {
	raw := "features:\n  installed: [scoring@v0.34.0]\nstatuses:\n  - name: idea\n"
	res, err := scanWorkflowFeatures("workflow.yaml", []byte(raw), scanOptions{Forget: []string{"scoring"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Record.Installed) != 0 || !strings.Contains(strings.Join(res.Changes, ","), "- installed scoring@v0.34.0") {
		t.Fatalf("record = %+v, changes = %v", res.Record, res.Changes)
	}
}

func TestRunWorkflowScanDryRunWritesNothing(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"== Workflow scan ==", "Mode:   dry run", "✓ human-approval-gates", "transitions[0].actions[0].type", "· worktree", "+ installed human-approval-gates@", "Record after this scan:", "features:"} {
		assertContains(t, out, want)
	}
	if got, _ := os.ReadFile(proj.WorkflowFile); string(got) != oldDevelopmentWorkflow {
		t.Fatal("scan --dry-run modified workflow.yaml")
	}
}

func TestRunWorkflowScanWritesRecordAndIsIdempotent(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan", "--declined", "scoring,worktree"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "Mode:   wrote the features: record")
	got, _ := os.ReadFile(proj.WorkflowFile)
	if !strings.HasSuffix(string(got), oldDevelopmentWorkflow) {
		t.Fatalf("scan changed more than the features: block:\n%s", got)
	}
	rec, ok := readFeatureRecord(got)
	if !ok || strings.Join(rec.Declined, ",") != "scoring,worktree" || len(rec.Installed) == 0 {
		t.Fatalf("record = %+v", rec)
	}
	if _, err := tracker.LoadWorkflow(proj.WorkflowFile); err != nil {
		t.Fatalf("LoadWorkflow after scan: %v", err)
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"scan"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "record already up to date, nothing written")
	assertContains(t, stdout.String(), "✗ scoring")
	again, _ := os.ReadFile(proj.WorkflowFile)
	if string(again) != string(got) {
		t.Fatal("second scan changed the file")
	}
}

func TestRunWorkflowScanJSON(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, true)
	if err := runWorkflowScan(ctx, []string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	var res map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"workflow_file", "dry_run", "written", "had_record", "features", "record", "block", "changes", "notes"} {
		if _, ok := res[key]; !ok {
			t.Errorf("JSON missing %q", key)
		}
	}
	if res["written"] != false || res["dry_run"] != true {
		t.Errorf("dry_run/written = %v/%v", res["dry_run"], res["written"])
	}
}

func TestRunWorkflowScanRejectsArgs(t *testing.T) {
	proj := writeUpgradeProject(t, oldDevelopmentWorkflow)
	ctx, _, _ := newTestContext(proj, false)
	if err := runWorkflowScan(ctx, []string{"extra"}); err == nil || !strings.Contains(err.Error(), "takes no arguments") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildUpgradePlanUsesRecordFirst(t *testing.T) {
	// types-of-work is recorded but not in the file: the record wins, so it
	// isn't proposed. worktree is in the file but not recorded: listed as
	// unrecorded, not proposed. scoring is declined: listed, not proposed.
	raw := "features:\n  installed: [types-of-work@v0.34.0]\n  declined: [scoring]\nworktree: true\n" + oldDevelopmentWorkflow
	plan, err := buildUpgradePlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RecordSource != "record" || plan.Record == nil {
		t.Fatalf("record source = %q", plan.RecordSource)
	}
	ids := "," + strings.Join(proposalIDs(plan), ",") + ","
	for _, id := range []string{"types-of-work", "worktree", "scoring"} {
		if strings.Contains(ids, ","+id+",") {
			t.Errorf("%s proposed: %s", id, ids)
		}
	}
	if strings.Join(plan.Declined, ",") != "scoring" || !strings.Contains(","+strings.Join(plan.Unrecorded, ",")+",", ",worktree,") || strings.Join(plan.Present, ",") != "types-of-work" {
		t.Errorf("declined=%v unrecorded=%v present=%v", plan.Declined, plan.Unrecorded, plan.Present)
	}
	if !strings.Contains(ids, ",parked-status,") {
		t.Errorf("parked-status should still be proposed: %s", ids)
	}
}

func TestBuildUpgradePlanAllIncludesDeclinedAndCatalogOnly(t *testing.T) {
	data, err := readWorkflowTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(string(data), "features:\n", "features:\n  declined: [scoring]\n", 1)
	plan, err := buildUpgradePlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != upgradeUpToDate || len(plan.Proposals) != 0 {
		t.Fatalf("fresh template: status=%s proposals=%v", plan.Status, proposalIDs(plan))
	}
	plan, err = buildUpgradePlan("workflow.yaml", []byte(raw), "", true)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]upgradeProposal{}
	for _, p := range plan.Proposals {
		byID[p.ID] = p
	}
	if !byID["scoring"].Declined {
		t.Errorf("--all should list declined scoring with declined=true: %+v", byID["scoring"])
	}
	if _, ok := byID["pr-url-gate"]; !ok {
		t.Errorf("--all should list catalog-only pr-url-gate: %v", proposalIDs(plan))
	}
	if _, ok := byID["types-of-work"]; ok {
		t.Error("--all proposed an installed feature")
	}
}

func TestBuildUpgradePlanProposesUpdatesForOlderInstalls(t *testing.T) {
	raw := "template: development@v0.1.0\nfeatures:\n  installed: [types-of-work@v0.1.0]\n" + oldDevelopmentWorkflow
	plan, err := buildUpgradePlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Proposals {
		if p.ID == "types-of-work" {
			if p.Kind != proposalUpdate || p.Installed != "v0.1.0" {
				t.Fatalf("update proposal = %+v", p)
			}
			return
		}
	}
	t.Fatalf("no update proposal for types-of-work: %v", proposalIDs(plan))
}

func TestRunWorkflowUpgradeListsDeclinedAndRecord(t *testing.T) {
	proj := writeUpgradeProject(t, "features:\n  declined: [scoring]\nworktree: true\n"+oldDevelopmentWorkflow)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "Record:   features: (0 installed, 1 declined)")
	assertContains(t, out, "Declined (not proposed; re-open with `issue-cli workflow scan --forget <id>`): scoring")
	assertContains(t, out, "Detected but not in the features: record (run `issue-cli workflow scan`): ")
	assertContains(t, out, "worktree\n")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runWorkflow(ctx, []string{"upgrade", "--all"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "declined earlier")
}
