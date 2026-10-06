package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const typedWorkflow = `
statuses:
  - name: idea
  - name: discussion
  - name: in progress
  - name: testing
  - name: done
transitions:
  - from: idea
    to: discussion
    actions:
      - type: append_section
        title: Discussion
        body: "- [ ] options"
  - from: discussion
    to: in progress
    actions:
      - type: require_human_approval
        status: in progress
default_type: feature
types:
  feature:
    description: "Full path"
  tweak:
    description: "Already decided"
    path: [idea, in progress, testing, done]
    transitions:
      - from: idea
        to: in progress
        actions:
          - type: require_human_approval
            status: in progress
  triaged:
    description: "Starts after triage"
    path: [discussion, in progress, done]
`

const untypedWorkflow = `
statuses:
  - name: idea
  - name: discussion
  - name: in progress
  - name: done
`

func typedProject(t *testing.T, workflow string, issues map[string]string) *tracker.Project {
	t.Helper()
	root := t.TempDir()
	issuesDir := filepath.Join(root, "issues")
	if err := os.MkdirAll(issuesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wfPath := filepath.Join(root, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range issues {
		if err := os.WriteFile(filepath.Join(issuesDir, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: issuesDir, WorkflowFile: wfPath}
}

func issueFile(title, status, typ string) string {
	s := "---\ntitle: \"" + title + "\"\nstatus: \"" + status + "\"\n"
	if typ != "" {
		s += "type: \"" + typ + "\"\n"
	}
	return s + "---\n\nbody\n"
}

func readIssue(t *testing.T, proj *tracker.Project, slug string) *tracker.Issue {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj.IssueDir, slug+".md"))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := tracker.ParseIssue(slug+".md", data)
	if err != nil {
		t.Fatal(err)
	}
	return issue
}

func TestCreate_Type(t *testing.T) {
	proj := typedProject(t, typedWorkflow, nil)

	ctx, out, _ := newTestContext(proj, false)
	if err := runCreate(ctx, []string{"--title", "Bigger cards", "--type", "tweak"}); err != nil {
		t.Fatal(err)
	}
	if got := readIssue(t, proj, "bigger-cards"); got.Type != "tweak" || got.Status != "idea" {
		t.Errorf("created type=%q status=%q", got.Type, got.Status)
	}
	for _, want := range []string{
		"Type: tweak (also: feature, triaged — issue-cli set-type bigger-cards <type> while at idea)",
		"Path: idea → in progress → testing → done",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("create output missing %q:\n%s", want, out)
		}
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runCreate(ctx, []string{"--title", "Plain"}); err != nil {
		t.Fatal(err)
	}
	if got := readIssue(t, proj, "plain"); got.Type != "feature" {
		t.Errorf("default create type = %q, want feature", got.Type)
	}
	if !strings.Contains(out.String(), "Type: feature (default; also: triaged, tweak") {
		t.Errorf("default create output:\n%s", out)
	}

	// A type whose path starts later starts there.
	ctx, _, _ = newTestContext(proj, false)
	if err := runCreate(ctx, []string{"--title", "Triaged one", "--type", "triaged"}); err != nil {
		t.Fatal(err)
	}
	if got := readIssue(t, proj, "triaged-one"); got.Status != "discussion" {
		t.Errorf("triaged starts at %q, want discussion", got.Status)
	}

	ctx, _, _ = newTestContext(proj, false)
	err := runCreate(ctx, []string{"--title", "X", "--type", "chore"})
	if err == nil || !strings.Contains(err.Error(), `unknown type "chore" (types: feature, triaged, tweak)`) {
		t.Errorf("unknown type err = %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	err = runCreate(ctx, []string{"--title", "Y", "--type", "tweak", "--status", "discussion"})
	if err == nil || !strings.Contains(err.Error(), `cannot create a tweak issue with status "discussion" — allowed: "idea"`) {
		t.Errorf("off-path status err = %v", err)
	}
}

func TestCreate_UntypedProjectUnchanged(t *testing.T) {
	proj := typedProject(t, untypedWorkflow, nil)
	ctx, out, _ := newTestContext(proj, false)
	if err := runCreate(ctx, []string{"--title", "Plain"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(proj.IssueDir, "plain.md"))
	if strings.Contains(string(data), "type:") || strings.Contains(out.String(), "Type:") {
		t.Errorf("untyped project got a type:\n%s\n%s", data, out)
	}
	ctx, _, _ = newTestContext(proj, false)
	err := runCreate(ctx, []string{"--title", "Z", "--type", "tweak"})
	if err == nil || !strings.Contains(err.Error(), "this project defines no types") {
		t.Errorf("--type in untyped project err = %v", err)
	}
}

func TestSetType(t *testing.T) {
	proj := typedProject(t, typedWorkflow, map[string]string{
		"fresh":   issueFile("fresh", "idea", ""),
		"started": issueFile("started", "discussion", "feature"),
	})

	ctx, out, _ := newTestContext(proj, false)
	if err := runSetType(ctx, []string{"fresh", "tweak"}); err != nil {
		t.Fatal(err)
	}
	if got := readIssue(t, proj, "fresh"); got.Type != "tweak" || got.Status != "idea" {
		t.Errorf("after set-type: type=%q status=%q", got.Type, got.Status)
	}
	if !strings.Contains(out.String(), "✓ Type: feature (default) → tweak on fresh") {
		t.Errorf("set-type output:\n%s", out)
	}

	// idea is not on triaged's path: the issue moves to triaged's first status.
	ctx, out, _ = newTestContext(proj, false)
	if err := runSetType(ctx, []string{"fresh", "triaged"}); err != nil {
		t.Fatal(err)
	}
	if got := readIssue(t, proj, "fresh"); got.Type != "triaged" || got.Status != "discussion" {
		t.Errorf("retype to triaged: type=%q status=%q", got.Type, got.Status)
	}
	if !strings.Contains(out.String(), "✓ Status: idea → discussion (not on triaged's path)") {
		t.Errorf("set-type output:\n%s", out)
	}

	// Past the first status it's the human's call.
	ctx, _, _ = newTestContext(proj, false)
	err := runSetType(ctx, []string{"started", "tweak"})
	if err == nil || !strings.Contains(err.Error(), `type changes after "idea" are the human's call`) ||
		!strings.Contains(err.Error(), "/p/test/issue/started") {
		t.Errorf("late set-type err = %v", err)
	}

	ctx, _, _ = newTestContext(proj, false)
	if err := runSetType(ctx, []string{"started", "bogus"}); err == nil || !strings.Contains(err.Error(), `unknown type "bogus"`) {
		t.Errorf("unknown type err = %v", err)
	}
}

func TestSetMeta_TypeGuard(t *testing.T) {
	typed := typedProject(t, typedWorkflow, map[string]string{"a": issueFile("a", "idea", "")})
	ctx, _, _ := newTestContext(typed, false)
	err := runSetMeta(ctx, []string{"a", "--key", "type", "--value", "tweak"})
	if err == nil || !strings.Contains(err.Error(), "issue-cli set-type a <type>") {
		t.Errorf("set-meta type in typed project err = %v", err)
	}

	untyped := typedProject(t, untypedWorkflow, map[string]string{"a": issueFile("a", "idea", "")})
	ctx, _, _ = newTestContext(untyped, false)
	if err := runSetMeta(ctx, []string{"a", "--key", "type", "--value", "knowledge"}); err != nil {
		t.Errorf("set-meta type in untyped project: %v", err)
	}
	if got := readIssue(t, untyped, "a"); got.Type != "knowledge" {
		t.Errorf("custom type field = %q", got.Type)
	}
}

func TestTransition_FollowsType(t *testing.T) {
	proj := typedProject(t, typedWorkflow, map[string]string{
		"tw":   issueFile("tw", "idea", "tweak"),
		"feat": issueFile("feat", "idea", ""),
	})

	ctx, _, _ := newTestContext(proj, false)
	err := runTransition(ctx, []string{"tw", "--to", "discussion"})
	if err == nil || !strings.Contains(err.Error(), `type "tweak" goes idea → in progress → testing → done (must go to "in progress" next)`) {
		t.Errorf("tweak idea → discussion err = %v", err)
	}

	ctx, _, _ = newTestContext(proj, false)
	err = runTransition(ctx, []string{"tw", "--to", "in progress"})
	var approval *tracker.ApprovalMissingError
	if !errors.As(err, &approval) {
		t.Errorf("tweak idea → in progress should need approval, got %v", err)
	}

	ctx, _, _ = newTestContext(proj, false)
	err = runTransition(ctx, []string{"feat", "--to", "in progress"})
	if err == nil || !strings.Contains(err.Error(), `must go to "discussion" next`) {
		t.Errorf("feature idea → in progress err = %v", err)
	}

	ctx, out, _ := newTestContext(proj, false)
	if err := runTransition(ctx, []string{"feat", "--to", "discussion"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Status: discussion | Type: feature") {
		t.Errorf("transition output:\n%s", out)
	}
}

func TestShow_Type(t *testing.T) {
	proj := typedProject(t, typedWorkflow, map[string]string{
		"tw":    issueFile("tw", "idea", "tweak"),
		"odd":   issueFile("odd", "idea", "chore"),
		"stray": issueFile("stray", "discussion", "tweak"),
	})

	ctx, out, _ := newTestContext(proj, false)
	if err := runShow(ctx, []string{"tw"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Status: idea | Type: tweak | System:", "Path: idea → in progress → testing → done"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show missing %q:\n%s", want, out)
		}
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runShow(ctx, []string{"odd"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `⚠ type "chore" is not defined (types: feature, triaged, tweak); using default "feature". Fix: issue-cli set-type odd <type>`) {
		t.Errorf("show unknown type:\n%s", out)
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runShow(ctx, []string{"stray"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `⚠ status "discussion" is not on type "tweak"'s path`) {
		t.Errorf("show off-path:\n%s", out)
	}

	ctx, out, _ = newTestContext(proj, true)
	if err := runShow(ctx, []string{"tw"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"type": "tweak"`) || !strings.Contains(out.String(), `"type_path": "idea → in progress → testing → done"`) {
		t.Errorf("show --json:\n%s", out)
	}
}

func TestProcess_Types(t *testing.T) {
	proj := typedProject(t, typedWorkflow, map[string]string{"tw": issueFile("tw", "idea", "tweak")})

	ctx, out, _ := newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"== Types ==",
		"feature (default)",
		"idea → discussion → in progress → testing → done",
		"idea → in progress → testing → done",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("process workflow missing %q:\n%s", want, out)
		}
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow", "--type", "tweak"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `== Status Lifecycle — type "tweak" ==`) || strings.Contains(out.String(), "discussion") {
		t.Errorf("process workflow --type tweak:\n%s", out)
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"transitions", "--type", "tweak"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `== Transition Rules — type "tweak" ==`) {
		t.Errorf("process transitions --type:\n%s", out)
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"transitions", "tw"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `== Transition Rules — type "tweak" (issue tw; no system overlay) ==`) {
		t.Errorf("process transitions <slug>:\n%s", out)
	}

	ctx, out, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"transitions"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "(base rules with no type applied; types: feature, triaged, tweak") {
		t.Errorf("process transitions (no scope):\n%s", out)
	}
}

func TestProcess_LintWarnings(t *testing.T) {
	broken := strings.Replace(typedWorkflow, "path: [discussion, in progress, done]", "path: [discussion, review, done]", 1)
	proj := typedProject(t, broken, nil)
	ctx, out, _ := newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "== Warnings (workflow.yaml) ==") ||
		!strings.Contains(out.String(), `⚠ type triaged: path entry "review" is not a base status (ignored)`) {
		t.Errorf("lint block:\n%s", out)
	}
}

func TestProcess_UntypedProjectUnchanged(t *testing.T) {
	proj := typedProject(t, untypedWorkflow, nil)
	ctx, out, _ := newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Types") || strings.Contains(out.String(), "Warnings") {
		t.Errorf("untyped process workflow changed:\n%s", out)
	}
	ctx, _, _ = newTestContext(proj, false)
	if err := runProcess(ctx, []string{"workflow", "--type", "tweak"}); err == nil {
		t.Error("--type in an untyped project should error")
	}
}

func TestList_TypeFilter(t *testing.T) {
	proj := typedProject(t, typedWorkflow, map[string]string{
		"tw":   issueFile("tw", "idea", "tweak"),
		"feat": issueFile("feat", "idea", ""),
	})
	ctx, out, _ := newTestContext(proj, false)
	if err := runList(ctx, []string{"--type", "feature"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "feat") || strings.Contains(out.String(), " tw ") || !strings.Contains(out.String(), "1 issues") {
		t.Errorf("list --type feature:\n%s", out)
	}
}
