package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const metricsWorkflow = untypedWorkflow + `
workbench:
  metrics:
    probe_results: probe/run.json
  systems:
    Sim:
      metrics: [stuck, full-holds]
`

func issueWithSystem(title, status, system, extra, body string) string {
	return "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\nsystem: \"" + system + "\"\n" + extra + "---\n\n" + body + "\n"
}

func TestSetMetaDependsOn(t *testing.T) {
	proj := typedProject(t, untypedWorkflow, map[string]string{
		"alpha":       issueFile("alpha", "idea", ""),
		"beta":        issueFile("beta", "in progress", ""),
		"gamma-shop":  issueFile("gamma shop", "done", ""),
		"delta-other": issueFile("delta other", "idea", ""),
	})
	ctx, out, _ := newTestContext(proj, false)
	if err := runSetMeta(ctx, []string{"alpha", "--key", "depends_on", "--value", "Beta, #gamma-shop, beta"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `Set depends_on = "beta, gamma-shop"`) || !strings.Contains(out.String(), "show now list this dependency") {
		t.Errorf("out = %s", out)
	}
	alpha := readIssue(t, proj, "alpha")
	if got := tracker.DependsOn(alpha); strings.Join(got, ", ") != "beta, gamma-shop" {
		t.Errorf("depends_on = %q", got)
	}
	if raw, _ := os.ReadFile(filepath.Join(proj.IssueDir, "alpha.md")); !strings.Contains(string(raw), "depends_on:\n  - \"beta\"\n  - \"gamma-shop\"\n") {
		t.Errorf("two slugs not written as a YAML list:\n%s", raw)
	}

	// One slug is a scalar, and it replaces the list without leaving its items.
	ctx, _, _ = newTestContext(proj, false)
	if err := runSetMeta(ctx, []string{"alpha", "--key", "depends_on", "--value", "beta"}); err != nil {
		t.Fatal(err)
	}
	if got := readIssue(t, proj, "alpha").Field("depends_on"); got != "beta" {
		t.Errorf("depends_on = %q, want beta", got)
	}

	ctx, _, _ = newTestContext(proj, false)
	err := runSetMeta(ctx, []string{"alpha", "--key", "depends_on", "--value", "gamma"})
	if err == nil || !strings.Contains(err.Error(), `no issue "gamma"`) || !strings.Contains(err.Error(), "gamma-shop") {
		t.Errorf("unknown slug err = %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	if err := runSetMeta(ctx, []string{"alpha", "--key", "depends_on", "--value", "alpha"}); err == nil || !strings.Contains(err.Error(), "cannot depend on itself") {
		t.Errorf("self err = %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	if err := runSetMeta(ctx, []string{"alpha", "--key", "depends_on", "--clear"}); err != nil {
		t.Errorf("clear: %v", err)
	}
	if got := tracker.DependsOn(readIssue(t, proj, "alpha")); len(got) != 0 {
		t.Errorf("depends_on after clear = %q", got)
	}
}

func TestShowPrintsDependenciesAndGuardMetrics(t *testing.T) {
	proj := typedProject(t, metricsWorkflow, map[string]string{
		"alpha": issueWithSystem("alpha", "idea", "Sim", "depends_on:\n  - beta\n  - nowhere\n", "Built on beta#D-1."),
		"beta":  issueWithSystem("beta", "in progress", "Sim", "", "## Decisions\n- **D-1** · 2026-10-01 · lead chat: Keep it small"),
	})
	root := filepath.Dir(proj.IssueDir)
	if err := os.MkdirAll(filepath.Join(root, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := `{"run":{"date":"2026-10-01"},"metrics":[
	  {"id":"stuck","band":{"max":0},"value":10},
	  {"id":"full-holds","band":{"max":20},"value":30,"verdict":"near"},
	  {"id":"other","value":1}]}`
	if err := os.WriteFile(filepath.Join(root, "probe", "run.json"), []byte(run), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, out, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Depends on: beta (in progress), nowhere (unknown issue)",
		"Decided by: beta#D-1 (active)",
		"Guard metrics for Sim: 1 off (stuck), 1 near. See http://localhost:8080/p/test/metrics?system=Sim",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show is missing %q:\n%s", want, out)
		}
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runShow(ctx, []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Depended on by: alpha (idea)") {
		t.Errorf("beta show:\n%s", out)
	}
}
