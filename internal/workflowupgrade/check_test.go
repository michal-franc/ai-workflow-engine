package workflowupgrade

import (
	"strings"
	"testing"
)

const checkCatalog = `entries:
  - id: health-tuning
    version: v0.53.0
    sources: ["internal/health/**", "internal/tracker/health.go"]
  - id: checklist-gates
    version: v0.36.0
    sources: ["internal/tracker/workflow_validate.go#checkRule"]
  - id: lead-handoff-prompts
    version: v0.36.0
    sources: ["@prompt"]
`

const validateBase = `package tracker

func checkRule() string { return "a" }

func other() string { return "x" }
`

// fakeBranch serves base and head content from maps; head defaults to the
// catalog above.
func fakeBranch(changed []string, base, head map[string]string, trailers ...string) Branch {
	if _, ok := head[CatalogPath]; !ok {
		head[CatalogPath] = checkCatalog
	}
	get := func(m map[string]string) func(string) ([]byte, bool) {
		return func(p string) ([]byte, bool) {
			s, ok := m[p]
			return []byte(s), ok
		}
	}
	return Branch{Changed: changed, Base: get(base), Head: get(head), Trailers: trailers}
}

func entryStates(rep *CheckReport) string {
	var parts []string
	for _, e := range rep.Entries {
		parts = append(parts, e.ID+"="+e.State)
	}
	for _, t := range rep.Templates {
		parts = append(parts, "template:"+t.Name+"="+t.State)
	}
	return strings.Join(parts, ",")
}

func TestCheckBranchAsksForABump(t *testing.T) {
	b := fakeBranch([]string{"internal/health/score.go", "docs/x.md"}, map[string]string{}, map[string]string{})
	rep, err := CheckBranch(b, "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := entryStates(rep); got != "health-tuning=stale" || rep.OK() {
		t.Fatalf("states = %s, ok = %v", got, rep.OK())
	}
	if rep.NextRelease != "v0.54.0" || strings.Join(rep.Entries[0].Files, ",") != "internal/health/score.go" {
		t.Errorf("report = %+v", rep)
	}
}

func TestCheckBranchPassesABumpOrATrailer(t *testing.T) {
	bumped := fakeBranch([]string{"internal/tracker/health.go"}, map[string]string{}, map[string]string{
		CatalogPath: strings.Replace(checkCatalog, "version: v0.53.0", "version: v0.54.0", 1),
	})
	rep, err := CheckBranch(bumped, "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := entryStates(rep); got != "health-tuning=bumped" || !rep.OK() {
		t.Errorf("bumped: %s", got)
	}

	marked := fakeBranch([]string{"internal/tracker/health.go"}, map[string]string{}, map[string]string{}, "n/a health-tuning,checklist-gates rename only", "")
	rep, err = CheckBranch(marked, "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := entryStates(rep); got != "health-tuning=n/a" || !rep.OK() || rep.Entries[0].Why != "rename only" {
		t.Errorf("n/a: %s %+v", got, rep.Entries)
	}
}

// A #symbol source only counts when that symbol's code changed.
func TestCheckBranchComparesSymbols(t *testing.T) {
	path := "internal/tracker/workflow_validate.go"
	otherOnly := strings.Replace(validateBase, `return "x"`, `return "y"`, 1)
	rep, err := CheckBranch(fakeBranch([]string{path}, map[string]string{path: validateBase}, map[string]string{path: otherOnly}), "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Entries) != 0 {
		t.Errorf("a change outside checkRule flagged %s", entryStates(rep))
	}

	withComment := strings.Replace(validateBase, "func checkRule", "// checkRule checks.\nfunc checkRule", 1)
	rep, err = CheckBranch(fakeBranch([]string{path}, map[string]string{path: validateBase}, map[string]string{path: withComment}), "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Entries) != 0 {
		t.Errorf("a doc comment flagged %s", entryStates(rep))
	}

	changed := strings.Replace(validateBase, `return "a"`, `return "b"`, 1)
	rep, err = CheckBranch(fakeBranch([]string{path}, map[string]string{path: validateBase}, map[string]string{path: changed}), "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := entryStates(rep); got != "checklist-gates=stale" {
		t.Errorf("changed checkRule: %s", got)
	}
}

func TestCheckBranchTemplatesNeedAStampBump(t *testing.T) {
	path := TemplatesPath + "/development.yaml"
	stale := fakeBranch([]string{path}, map[string]string{}, map[string]string{path: "template: development@v0.53.0\nstatuses: []\n"})
	rep, err := CheckBranch(stale, "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := entryStates(rep); got != "template:development=stale" || rep.OK() {
		t.Errorf("stale template: %s", got)
	}
	bumped := fakeBranch([]string{path}, map[string]string{}, map[string]string{path: "template: development@v0.54.0\nstatuses: []\n"})
	rep, err = CheckBranch(bumped, "v0.53.0")
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Errorf("bumped template: %s", entryStates(rep))
	}
}

func TestCheckBranchNeedsTheCatalog(t *testing.T) {
	b := Branch{Head: func(string) ([]byte, bool) { return nil, false }, Base: func(string) ([]byte, bool) { return nil, false }}
	if _, err := CheckBranch(b, "v0.53.0"); err == nil || !strings.Contains(err.Error(), "issue-viewer repository") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseHelpers(t *testing.T) {
	last, ok := LastRelease([]byte("# Changelog\n\n## Unreleased\n\n- x\n\n## v0.53.0 — 2026-10-23\n\n## v0.52.0\n"))
	if !ok || last != "v0.53.0" || NextRelease(last) != "v0.54.0" {
		t.Errorf("last = %s %v, next = %s", last, ok, NextRelease(last))
	}
	if _, ok := LastRelease([]byte("## Unreleased\n")); ok {
		t.Error("no release heading should not parse")
	}
}

func TestMatchSource(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"internal/ci/**", "internal/ci/run/x.go", true},
		{"internal/ci/**", "internal/ci.go", false},
		{"ci_watch.go", "ci_watch.go", true},
		{"ci_watch.go", "internal/x/ci_watch.go", false},
		{"cmd/issue-cli/cmd_ci*.go", "cmd/issue-cli/cmd_ci_gotest.go", true},
	}
	for _, c := range cases {
		if got := MatchSource(c.glob, c.path); got != c.want {
			t.Errorf("MatchSource(%s, %s) = %v", c.glob, c.path, got)
		}
	}
}
