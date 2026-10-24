package workflowupgrade

import (
	"strings"
	"testing"
)

// Every entry's own snippet is what a project adds, so its probes must find
// it: an entry whose snippet it can't detect would be proposed forever.
func TestEveryEntryDetectsItsOwnSnippet(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if Detect(e, ParseDoc([]byte(e.Snippet))).Matches == 0 {
			t.Errorf("%s: its probes don't detect its own snippet", e.ID)
		}
	}
}

// The features table from the catalog catch-up: each is an entry, detected
// from its config and absent from a v0.36 file without it.
func TestCatchUpEntriesDetectTheirConfig(t *testing.T) {
	cases := map[string]string{
		"local-ci":             "ci:\n  run:\n    - cmd: make check\n",
		"ci-test-results":      "ci:\n  run:\n    - cmd: make check\n      results:\n        - {kind: unit, format: trx, file: unit.trx}\n",
		"health-tuning":        "health:\n  e2e_sessions: [\"Data/Scenarios/*.yaml\"]\n",
		"status-changelog":     "workbench:\n  changelog: cmd/issue-cli/CHANGELOG.md\n",
		"system-pages":         "workbench:\n  systems:\n    UI:\n      summary: s\n",
		"project-pages":        "workbench:\n  pages:\n    - {title: Report, path: docs/r.html}\n",
		"metrics-and-knobs":    "workbench:\n  tuning: [build/knobs.json]\n",
		"balance-board":        "workbench:\n  balance:\n    comment_prefix: \"balance:\"\n",
		"lead-prompt":          "leads:\n  project: |\n    You are the lead.\n",
		"local-merge-shipping": "statuses:\n  - name: shipping\n    prompt: \"git -C <main checkout> merge --ff-only <branch>\"\n",
	}
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	idx := catalogIndex(entries)
	bare := ParseDoc([]byte(oldDevelopmentWorkflow))
	for id, raw := range cases {
		e, ok := idx[id]
		if !ok {
			t.Errorf("%s: no catalog entry", id)
			continue
		}
		if Detect(e, ParseDoc([]byte(raw))).Matches == 0 {
			t.Errorf("%s: not detected in\n%s", id, raw)
		}
		if Detect(e, bare).Matches > 0 {
			t.Errorf("%s: detected in a workflow without it", id)
		}
	}
	if e := idx["ci-test-results"]; strings.Join(e.Requires, ",") != "local-ci" {
		t.Errorf("ci-test-results requires %v, want [local-ci]", e.Requires)
	}
}

// gpu-runner bumped its stamp to v0.54.0 while proof-links (v0.54.0) was
// undecided, and upgrade stopped proposing it. With a features: record the
// record decides: the stamp hides nothing undecided.
func TestRecordDecidesNotTheStamp(t *testing.T) {
	latest := mustLatest(t, "development")
	raw := "template: development@" + latest + "\nfeatures:\n  installed:\n    - status-prompts@v0.36.0\n  declined:\n    - scoring\n" + oldDevelopmentWorkflow
	plan, err := BuildPlan("workflow.yaml", []byte(raw), "", false)
	if err != nil {
		t.Fatal(err)
	}
	ids := "," + strings.Join(proposalIDs(plan), ",") + ","
	for _, id := range []string{"proof-links", "local-ci", "lead-prompt"} {
		if !strings.Contains(ids, ","+id+",") {
			t.Errorf("%s undecided but not proposed at stamp %s: %s", id, latest, ids)
		}
	}
	if strings.Contains(ids, ",scoring,") {
		t.Errorf("declined scoring proposed: %s", ids)
	}

	// Without a record the stamp still gates: entries at or below it stay quiet.
	plan, err = BuildPlan("workflow.yaml", []byte("template: development@"+latest+"\n"+oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Proposals) != 0 {
		t.Errorf("unrecorded file at the bundled stamp got proposals: %v", proposalIDs(plan))
	}
}

// Step 5 tells the agent to bump the stamp only once nothing is undecided.
func TestUpgradeStepsBumpTheStampLast(t *testing.T) {
	plan, err := BuildPlan("workflow.yaml", []byte("template: development@v0.36.0\n"+oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) < 5 {
		t.Fatalf("steps = %v", plan.Steps)
	}
	assertContains(t, plan.Steps[4], "Only once every proposal is installed or declined, set the stamp")
	assertContains(t, plan.Steps[4], "While any is undecided, leave the stamp as it is")
}

func TestRequiresHidesAnEntryUntilItsDependencyIsThere(t *testing.T) {
	without, err := BuildPlan("workflow.yaml", []byte(oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(","+strings.Join(proposalIDs(without), ",")+",", ",ci-test-results,") {
		t.Errorf("ci-test-results proposed without ci: %v", proposalIDs(without))
	}
	all, err := BuildPlan("workflow.yaml", []byte(oldDevelopmentWorkflow), "", true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range all.Proposals {
		if p.ID == "ci-test-results" {
			found = true
			if strings.Join(p.Needs, ",") != "local-ci" {
				t.Errorf("--all: needs = %v, want [local-ci]", p.Needs)
			}
		}
	}
	if !found {
		t.Error("--all should list ci-test-results")
	}

	withCI := oldDevelopmentWorkflow + "ci:\n  run:\n    - cmd: make check\n"
	with, err := BuildPlan("workflow.yaml", []byte(withCI), "", false)
	if err != nil {
		t.Fatal(err)
	}
	ids := "," + strings.Join(proposalIDs(with), ",") + ","
	if !strings.Contains(ids, ",ci-test-results,") || strings.Contains(ids, ",local-ci,") {
		t.Errorf("with ci: proposals = %s, want ci-test-results and not local-ci", ids)
	}
	for _, p := range with.Proposals {
		if p.ID == "ci-test-results" && len(p.Needs) != 0 {
			t.Errorf("needs = %v with ci present", p.Needs)
		}
	}
}

// Ship by local merge is opt-in: the bundled development template still
// pushes, so a project on it gets push until it installs the entry.
func TestLocalMergeShippingIsOptIn(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	e := catalogIndex(entries)["local-merge-shipping"]
	data, err := ReadTemplate("development")
	if err != nil {
		t.Fatal(err)
	}
	if Detect(e, ParseDoc(data)).Matches > 0 {
		t.Fatal("the bundled development template ships by local merge; it must keep push (opt-in)")
	}
	assertContains(t, string(data), "push the commit to the remote")
	inTemplate, err := templateFeatures("development")
	if err != nil {
		t.Fatal(err)
	}
	if inTemplate["local-merge-shipping"] {
		t.Error("local-merge-shipping is in the development template's record")
	}
	plan, err := BuildPlan("workflow.yaml", []byte(oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Proposals {
		if p.ID == "local-merge-shipping" {
			if p.InTemplate {
				t.Error("local-merge-shipping marked as in the bundled template")
			}
			return
		}
	}
	t.Errorf("local-merge-shipping not offered to an unstamped development workflow: %v", proposalIDs(plan))
}

// A project still on development@v0.36.0 is behind the bundled template.
func TestV036ProjectIsBehind(t *testing.T) {
	plan, err := BuildPlan("workflow.yaml", []byte("template: development@v0.36.0\n"+oldDevelopmentWorkflow), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusBehind || plan.Latest != "v0.54.0" {
		t.Fatalf("status = %s, latest = %s; want behind, v0.54.0", plan.Status, plan.Latest)
	}
}
