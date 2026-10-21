package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func issueWith(slug string, fields map[string]string) *Issue {
	issue := &Issue{Slug: slug, Title: slug, Status: "in progress"}
	for k, v := range fields {
		issue.ExtraFields = append(issue.ExtraFields, ExtraField{Key: k, Value: v})
	}
	return issue
}

func TestParentSlug_ParentWinsUmbrellaIsAlias(t *testing.T) {
	both := issueWith("a", map[string]string{"parent": "epic-p", "umbrella": "epic-u"})
	if got := ParentSlug(both); got != "epic-p" || UsesLegacyUmbrella(both) {
		t.Fatalf("parent+umbrella: ParentSlug = %q, legacy = %v", got, UsesLegacyUmbrella(both))
	}
	legacy := issueWith("b", map[string]string{"umbrella": "epic-u"})
	if got := ParentSlug(legacy); got != "epic-u" || !UsesLegacyUmbrella(legacy) {
		t.Fatalf("umbrella only: ParentSlug = %q, legacy = %v", got, UsesLegacyUmbrella(legacy))
	}
	if got := ParentSlug(issueWith("c", nil)); got != "" {
		t.Fatalf("no link: ParentSlug = %q", got)
	}
}

func TestIsEpic(t *testing.T) {
	epic := issueWith("ui/epic", nil)
	child := issueWith("ui/child", map[string]string{"parent": "ui/epic", "base": "base/ui/epic"})
	typed := issueWith("typed", nil)
	typed.Type = "epic"
	led := issueWith("led", map[string]string{"lead": "lead-led"})
	issues := []*Issue{epic, child, typed, led}

	for _, c := range []struct {
		issue *Issue
		want  bool
	}{{epic, true}, {child, false}, {typed, true}, {led, true}} {
		if got := IsEpic(c.issue, issues); got != c.want {
			t.Errorf("IsEpic(%s) = %v, want %v", c.issue.Slug, got, c.want)
		}
	}
	if kids := EpicChildren(issues, "UI/Epic"); len(kids) != 1 || kids[0] != child {
		t.Errorf("EpicChildren = %+v", kids)
	}
}

func TestLeadSessionName(t *testing.T) {
	proj := &Project{Slug: "issue-viewer"}
	if got := LeadSessionName(proj, LeadScopeProject); got != "lead-issue-viewer" {
		t.Errorf("project lead = %q", got)
	}
	if got := LeadSessionName(proj, "ui/workbench.v2 redesign"); got != "lead-ui-workbench-v2-redesign" {
		t.Errorf("sub-lead = %q", got)
	}
	if got := LeadSessionName(nil, ""); got != "lead-project" {
		t.Errorf("no project = %q", got)
	}
}

func TestIssueWorktree(t *testing.T) {
	enabled := true
	wf := &WorkflowConfig{Worktree: &enabled}
	epic := issueWith("ui/epic", map[string]string{"base": "base/ui/epic", "lead": "lead-ui-epic"})
	child := issueWith("ui/child", map[string]string{"parent": "ui/epic"})
	legacyChild := issueWith("ui/old", map[string]string{"umbrella": "ui/epic"})
	loner := issueWith("loner", nil)
	issues := []*Issue{epic, child, legacyChild, loner}

	if got := IssueWorktree("/w", epic, issues, wf); got != (WorktreeSpec{Path: "/w/.worktrees/ui/epic", Branch: "base/ui/epic"}) {
		t.Errorf("epic = %+v", got)
	}
	want := WorktreeSpec{Path: "/w/.worktrees/ui/child", Branch: "work/ui/child", StartPoint: "base/ui/epic"}
	if got := IssueWorktree("/w", child, issues, wf); got != want {
		t.Errorf("child = %+v, want %+v", got, want)
	}
	if got := IssueWorktree("/w", legacyChild, issues, wf); got.StartPoint != "base/ui/epic" {
		t.Errorf("umbrella child = %+v", got)
	}
	if got := IssueWorktree("/w", loner, issues, wf); got != (WorktreeSpec{Path: "/w/.worktrees/loner", Branch: "work/loner"}) {
		t.Errorf("loner = %+v", got)
	}
	if got := IssueWorktree("/w", child, issues, nil); got != (WorktreeSpec{}) {
		t.Errorf("worktrees disabled = %+v", got)
	}
}

func TestChildBase_FallsBackToOwnBase(t *testing.T) {
	epic := issueWith("epic", nil)
	child := issueWith("child", map[string]string{"parent": "epic", "base": "experiment"})
	if got := ChildBase(child, []*Issue{epic, child}); got != "experiment" {
		t.Fatalf("ChildBase = %q, want the child's own base", got)
	}
	orphanBase := issueWith("x", map[string]string{"base": "experiment"})
	if got := ChildBase(orphanBase, nil); got != "" {
		t.Fatalf("issue without parent: ChildBase = %q", got)
	}
}

func TestLeadFor(t *testing.T) {
	proj := &Project{Slug: "p"}
	epic := issueWith("ui/epic", nil)
	child := issueWith("ui/child", map[string]string{"parent": "ui/epic"})
	issues := []*Issue{epic, child}
	liveSet := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}

	r := LeadFor(proj, child, issues, liveSet("lead-ui-epic", "lead-p"))
	if r.Lead != "lead-ui-epic" || !r.Live || r.Epic != "ui/epic" {
		t.Errorf("live sub-lead: %+v", r)
	}
	r = LeadFor(proj, child, issues, liveSet("lead-p"))
	if r.Lead != "lead-p" || !r.Live || r.SubLead != "lead-ui-epic" || r.SubLeadLive {
		t.Errorf("sub-lead down: %+v", r)
	}
	r = LeadFor(proj, epic, issues, nil)
	if r.Lead != "lead-p" || r.Live || r.Epic != "" {
		t.Errorf("epic reports to project lead: %+v", r)
	}
}

func TestBuildDispatchPrompt_ChildOfEpic(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("UI/epic.md", "---\ntitle: \"Epic\"\nstatus: \"in progress\"\nbase: \"base/ui/epic\"\n---\n\nEpic body.\n")
	write("UI/child.md", "---\ntitle: \"Child\"\nstatus: \"in progress\"\numbrella: \"ui/epic\"\n---\n\nChild body.\n")
	proj := &Project{Slug: "p", IssueDir: dir}
	issues, err := LoadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	child := FindIssue(issues, "ui/child")
	enabled := true
	wf := &WorkflowConfig{Worktree: &enabled}

	dp := BuildDispatchPrompt(proj, child, wf, "/w", func(n string) bool { return n == "lead-ui-epic" })
	if dp.Base != "base/ui/epic" || dp.Lead != "lead-ui-epic" || dp.Branch != "work/ui/child" {
		t.Fatalf("got %+v", dp)
	}
	for _, want := range []string{
		"You report to lead-ui-epic, the sub-lead of epic ui/epic (live).",
		"`issue-cli --project p comment ui/child \"question: ...\"`",
		"## Base branch",
		"merge base/ui/epic (never main) into your branch",
		"open the PR against base/ui/epic, not main",
	} {
		if !strings.Contains(dp.Prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	dp = BuildDispatchPrompt(proj, child, wf, "/w", nil)
	if !strings.Contains(dp.Prompt, "Your epic ui/epic has no live sub-lead (lead-ui-epic), so you report to the project lead lead-p (not running).") {
		t.Errorf("fallback lead line missing:\n%s", dp.Prompt)
	}
}
