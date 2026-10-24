package workflowupgrade

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestWorkflowFeatureCatalogIsComplete(t *testing.T) {
	entries, err := Catalog()
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
		if ParseVersion(e.Version) == nil {
			t.Errorf("%s: version %q is not vX.Y.Z", e.ID, e.Version)
		}
		if !containsString(Categories, e.Category) {
			t.Errorf("%s: unknown category %q", e.ID, e.Category)
		}
		for field, v := range map[string]string{"title": e.Title, "what": e.What, "why": e.Why, "where": e.Where, "snippet": e.Snippet, "question": e.Question} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is empty", e.ID, field)
			}
		}
		for _, tmpl := range e.Templates {
			if !IsBundled(tmpl) {
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
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	doc := ParseDoc([]byte(raw))
	var ids []string
	for _, e := range entries {
		if Detect(e, doc).Matches > 0 {
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
			name: "a playtest status is not the playtest-sessions convention",
			raw:  "statuses:\n  - name: playtest\n    prompt: the human watches the playtest in-game\n",
			want: []string{"status-prompts"},
			not:  []string{"playtest-sessions"},
		},
		{
			name: "playtest session wording is the playtest-sessions convention",
			raw:  "statuses:\n  - name: testing\n    prompt: write the manual checks as a playtest session\n",
			want: []string{"playtest-sessions"},
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
	entries, _ := Catalog()
	e := catalogIndex(entries)["test-plan-gate"]
	raw := "transitions:\n  - from: a\n    to: b\n    actions:\n      - type: inject_prompt\n      - type: validate\n        rule: has_test_plan\n"
	d := Detect(e, ParseDoc([]byte(raw)))
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
	rec := tracker.FeatureRecord{Installed: []string{"status-prompts@v0.36.0"}, Declined: []string{"scoring"}}
	out, err := WriteRecord([]byte(recordWorkflow), rec)
	if err != nil {
		t.Fatal(err)
	}
	want := "# leading comment\ntemplate: development@v0.1.0\n\n" + featureRecordComment + "\n" + RenderRecord(rec) + "\n# statuses comment\nstatuses:\n  - name: \"idea\"\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestWriteFeatureRecordInsertsBeforeFirstKeyWithoutTemplate(t *testing.T) {
	raw := "# header\n# more\n\nstatuses:\n  - name: idea\n"
	out, err := WriteRecord([]byte(raw), tracker.FeatureRecord{Installed: []string{"x@v1.0.0"}})
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
		rec := tracker.FeatureRecord{Installed: []string{"new@v0.36.0"}}
		out, err := WriteRecord([]byte(raw), rec)
		if err != nil {
			t.Fatal(err)
		}
		want := "template: development@v0.1.0\n" + RenderRecord(rec) + "\n# keep me\nstatuses:\n  - name: idea\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", out, want)
		}
	}
}

func TestWriteFeatureRecordRejectsUnparseableFile(t *testing.T) {
	if _, err := WriteRecord([]byte("statuses: [\n"), tracker.FeatureRecord{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestScanWorkflowFeaturesMergesRecordAndFlags(t *testing.T) {
	raw := "features:\n  installed:\n    - scoring@v0.1.0\n    - status-prompts@v0.1.0\n    - from-the-future@v9.0.0\n  declined:\n    - worktree\n    - parked-status\n" +
		"statuses:\n  - name: idea\n    prompt: think\n    global: true\n"
	res, err := Scan("workflow.yaml", []byte(raw), ScanOptions{Installed: []string{"types-of-work"}, Declined: []string{"issue-actions"}, Forget: []string{"worktree"}})
	if err != nil {
		t.Fatal(err)
	}
	gotInstalled := strings.Join(res.Record.Installed, ",")
	// status-prompts keeps its recorded version; scoring is recorded but not
	// detected (kept); parked-status is detected, so it leaves declined.
	want := "status-prompts@v0.1.0,parked-status@v0.36.0,types-of-work@v0.36.0,scoring@v0.1.0,from-the-future@v9.0.0"
	if gotInstalled != want {
		t.Errorf("installed = %s\nwant      %s", gotInstalled, want)
	}
	if got := strings.Join(res.Record.Declined, ","); got != "issue-actions" {
		t.Errorf("declined = %s, want issue-actions (worktree forgotten, parked-status installed)", got)
	}
	changes := strings.Join(res.Changes, "\n")
	for _, c := range []string{"+ installed parked-status@v0.36.0", "- declined parked-status (now installed)", "+ installed types-of-work@v0.36.0", "+ declined issue-actions", "- declined worktree (re-opened)"} {
		assertContains(t, changes, c)
	}
	notes := strings.Join(res.Notes, "\n")
	assertContains(t, notes, "scoring is recorded as installed but not detected")
	assertContains(t, notes, "from-the-future is not in this issue-cli's catalog")
}

func TestScanWorkflowFeaturesRejectsBadFlags(t *testing.T) {
	raw := []byte("worktree: true\n")
	cases := map[string]ScanOptions{
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
		if _, err := Scan("workflow.yaml", in, opts); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: err = %v, want %q", opts, err, want)
		}
	}
}

func TestScanWorkflowFeaturesForgetRemovesUndetectedInstall(t *testing.T) {
	raw := "features:\n  installed: [scoring@v0.36.0]\nstatuses:\n  - name: idea\n"
	res, err := Scan("workflow.yaml", []byte(raw), ScanOptions{Forget: []string{"scoring"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Record.Installed) != 0 || !strings.Contains(strings.Join(res.Changes, ","), "- installed scoring@v0.36.0") {
		t.Fatalf("record = %+v, changes = %v", res.Record, res.Changes)
	}
}

func TestBuildUpgradePlanUsesRecordFirst(t *testing.T) {
	// types-of-work is recorded but not in the file: the record wins, so it
	// isn't proposed. worktree is in the file but not recorded: listed as
	// unrecorded, not proposed. scoring is declined: listed, not proposed.
	raw := "features:\n  installed: [types-of-work@v0.36.0]\n  declined: [scoring]\nworktree: true\n" + oldDevelopmentWorkflow
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
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
	data, err := ReadTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(string(data), "features:\n", "features:\n  declined: [scoring]\n", 1)
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	// The record decides, not the stamp: a fresh template's stamp is
	// current, yet it is offered the opt-in features it leaves out, so it
	// is undecided, not up to date.
	entries, _ := Catalog()
	inTemplate, _ := templateFeatures("development")
	var want []string
	for _, e := range entries {
		if e.Propose && e.appliesTo("development") && !inTemplate[e.ID] && e.ID != "scoring" && len(e.Requires) == 0 {
			want = append(want, e.ID)
		}
	}
	if plan.Status != StatusUndecided || strings.Join(proposalIDs(plan), ",") != strings.Join(want, ",") {
		t.Fatalf("fresh template: status=%s proposals=%v, want %v", plan.Status, proposalIDs(plan), want)
	}
	plan, err = BuildPlan("workflow.yaml", []byte(raw), "", true)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Proposal{}
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
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Proposals {
		if p.ID == "types-of-work" {
			if p.Kind != ProposalUpdate || p.Installed != "v0.1.0" {
				t.Fatalf("update proposal = %+v", p)
			}
			return
		}
	}
	t.Fatalf("no update proposal for types-of-work: %v", proposalIDs(plan))
}

// Output groups entries by category in catalog order (workflow scan opens a
// heading at each change), so each category's entries sit together, in
// Categories order.
func TestCatalogKeepsEachCategoryTogether(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	rank := map[string]int{}
	for i, c := range Categories {
		rank[c] = i
	}
	for i := 1; i < len(entries); i++ {
		prev, e := entries[i-1], entries[i]
		if rank[e.Category] < rank[prev.Category] {
			t.Errorf("%s (%s) comes after %s (%s): move it into the %s section", e.ID, e.Category, prev.ID, prev.Category, e.Category)
		}
	}
}

// An entry for something the viewer shows anyway says so: the key only adds
// to it. The RogueTech audit read these three as switches.
func TestAlwaysOnFeaturesSayWhatShowsWithoutTheKey(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	idx := catalogIndex(entries)
	for _, c := range []struct {
		id, title string
		what      []string
	}{
		{"system-pages", "System page details", []string{"Without `workbench.systems`", "issue `system:`"}},
		{"metrics-and-knobs", "Metrics and knobs", []string{"Without `workbench.metrics`", "Branches"}},
		{"status-changelog", "Releases on the Status page", []string{"Without `workbench.changelog`", "CHANGELOG.md", "git tags"}},
	} {
		e, ok := idx[c.id]
		if !ok {
			t.Fatalf("no catalog entry %s", c.id)
		}
		if e.Title != c.title {
			t.Errorf("%s: title %q, want %q", c.id, e.Title, c.title)
		}
		for _, s := range c.what {
			if !strings.Contains(e.What, s) {
				t.Errorf("%s: what: lacks %q:\n%s", c.id, s, e.What)
			}
		}
	}
}

// A stamp at the bundled version is not up to date while the record leaves
// a proposal undecided (undecided) or misses a feature the file has
// (stale_record).
func TestBuildUpgradePlanStaleRecordIsNotUpToDate(t *testing.T) {
	data, err := ReadTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	fresh := string(data)
	plan, err := BuildPlan("workflow.yaml", data, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Proposals) == 0 {
		t.Fatal("a fresh development template should leave opt-in features undecided")
	}
	if plan.Status != StatusUndecided {
		t.Errorf("undecided proposals: status %s, want %s", plan.Status, StatusUndecided)
	}
	if !strings.Contains(plan.Message, "issue-cli workflow scan") || strings.Contains(plan.Message, "up to date") {
		t.Errorf("message should name workflow scan and not say up to date: %q", plan.Message)
	}
	if plan.StaleRecord() != "" {
		t.Errorf("undecided only: StaleRecord() = %q, want empty (the owner decides, nothing is stale)", plan.StaleRecord())
	}

	// Decline every proposal: decided, so up to date.
	var ids []string
	for _, p := range plan.Proposals {
		ids = append(ids, p.ID)
	}
	decided := strings.Replace(fresh, "features:\n", "features:\n  declined: ["+strings.Join(ids, ", ")+"]\n", 1)
	plan, err = BuildPlan("workflow.yaml", []byte(decided), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusUpToDate || len(plan.Proposals) != 0 {
		t.Fatalf("all declined: status %s, proposals %v", plan.Status, proposalIDs(plan))
	}
	if plan.StaleRecord() != "" {
		t.Errorf("all declined: StaleRecord() = %q, want empty", plan.StaleRecord())
	}

	// A detected feature the record misses (local-ci): stale again.
	var rest []string
	for _, id := range ids {
		if id != "local-ci" {
			rest = append(rest, id)
		}
	}
	unrecorded := strings.Replace(fresh, "features:\n", "features:\n  declined: ["+strings.Join(rest, ", ")+"]\n", 1) + "\nci:\n  run:\n    - cmd: go test ./...\n"
	plan, err = BuildPlan("workflow.yaml", []byte(unrecorded), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusStaleRecord || !strings.Contains(strings.Join(plan.Unrecorded, ","), "local-ci") {
		t.Fatalf("unrecorded local-ci: status %s, unrecorded %v", plan.Status, plan.Unrecorded)
	}
	if w := plan.StaleRecord(); !strings.Contains(w, "local-ci") || !strings.Contains(w, "issue-cli workflow scan") {
		t.Errorf("StaleRecord() = %q, want local-ci and workflow scan named", w)
	}

	// Behind stays behind, and the record is still called stale.
	behind := strings.Replace(unrecorded, "template: development@"+plan.Latest, "template: development@v0.1.0", 1)
	plan, err = BuildPlan("workflow.yaml", []byte(behind), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusBehind || !strings.Contains(plan.StaleRecord(), "local-ci") {
		t.Errorf("behind with unrecorded local-ci: status %s, StaleRecord() %q", plan.Status, plan.StaleRecord())
	}
}
