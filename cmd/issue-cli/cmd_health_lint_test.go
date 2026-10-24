package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lintTestProject is makeSimpleProject (one undated issue) with a
// workflow.yaml; extra is appended to a valid one.
func lintTestProject(t *testing.T, extra string) *Context {
	t.Helper()
	proj, _, _ := makeSimpleProject(t, "idea")
	wf := filepath.Join(filepath.Dir(proj.IssueDir), "workflow.yaml")
	content := "template: custom\nstatuses:\n  - name: idea\n    description: raw\ntransitions:\n  - from: idea\n    to: idea\n    actions: []\n" + extra
	if err := os.WriteFile(wf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	proj.WorkflowFile = wf
	ctx, _, _ := newTestContext(proj, false)
	return ctx
}

func stdout(ctx *Context) string { return ctx.Stdout.(fmt.Stringer).String() }

func TestHealthLintPrintsWarningsWithFixes(t *testing.T) {
	ctx := lintTestProject(t, "bord:\n  columns: [idea]\n")
	if err := runHealth(ctx, []string{"lint"}); err != nil {
		t.Fatalf("findings are warnings: err = %v, want exit 0", err)
	}
	out := stdout(ctx)
	assertContains(t, out, "== health lint: ")
	assertContains(t, out, "undated cli/sample: no created: date")
	assertContains(t, out, "  fix: issue-cli set-meta cli/sample --key created --value ")
	assertContains(t, out, `schema workflow.yaml:9: unknown key "bord" at the top level`)
	assertContains(t, out, `  fix: did you mean "board"?`)
	assertContains(t, out, "2 warnings; the health score's lint component counts them")
}

func TestHealthLintCheckFlag(t *testing.T) {
	ctx := lintTestProject(t, "bord:\n  columns: [idea]\n")
	if err := runHealth(ctx, []string{"lint", "--check", "schema"}); err != nil {
		t.Fatal(err)
	}
	if out := stdout(ctx); strings.Contains(out, "undated") || !strings.Contains(out, "1 warning;") {
		t.Errorf("--check schema ran other checks:\n%s", out)
	}
}

func TestHealthLintUnknownCheckIsUsage(t *testing.T) {
	ctx := lintTestProject(t, "")
	err := runHealth(ctx, []string{"lint", "--check", "schema,nope"})
	var ue *usageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "checks: undated, workflow") {
		t.Fatalf("err = %v, want a usage error listing the checks", err)
	}
}

func TestHealthLintList(t *testing.T) {
	ctx := lintTestProject(t, "")
	if err := runHealth(ctx, []string{"lint", "--list"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"undated", "workflow", "schema", "template", "placeholders", "sections"} {
		assertContains(t, stdout(ctx), "  "+name+" ")
	}
}

func TestHealthLintJSON(t *testing.T) {
	ctx := lintTestProject(t, "")
	ctx.JSONOutput = true
	if err := runHealth(ctx, []string{"lint"}); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Findings []struct{ Check, Where, Message, Fix string }
		Warnings int
	}
	if err := json.Unmarshal([]byte(stdout(ctx)), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Warnings != 1 || len(rep.Findings) != 1 || rep.Findings[0].Check != "undated" || rep.Findings[0].Where != "cli/sample" {
		t.Errorf("report = %+v", rep)
	}
}

func TestHealthLintCapsWarningsPerCheck(t *testing.T) {
	ctx := lintTestProject(t, "")
	for i := 0; i < lintWarnCap+3; i++ {
		p := filepath.Join(ctx.Project.IssueDir, "CLI", fmt.Sprintf("extra-%d.md", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("---\ntitle: \"extra %d\"\nstatus: idea\n---\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runHealth(ctx, []string{"lint"}); err != nil {
		t.Fatal(err)
	}
	out := stdout(ctx)
	if n := strings.Count(out, "\nundated "); n != lintWarnCap {
		t.Errorf("printed %d undated warnings, want %d", n, lintWarnCap)
	}
	assertContains(t, out, "… 4 more undated warnings: issue-cli health lint --check undated --all")
	assertContains(t, out, "24 warnings;")
}

func TestTopLevelLintIsGone(t *testing.T) {
	if _, ok := commandRegistry["lint"]; ok {
		t.Error("issue-cli lint is registered; lint lives under issue-cli health lint")
	}
}
