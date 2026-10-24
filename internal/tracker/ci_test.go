package tracker

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestCIConfigParsesStringsAndMaps(t *testing.T) {
	src := `
ci:
  timeout: 20m
  on_red: file_issue
  run:
    - make validate
    - cmd: make ux-test
      tier: slow
      failures: 'FAIL (\S+)'
`
	var wf WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &wf); err != nil {
		t.Fatal(err)
	}
	c := wf.CI
	if !c.Enabled() || len(c.Run) != 2 || c.Run[0].Cmd != "make validate" || c.Run[1].Tier != "slow" {
		t.Fatalf("ci = %+v", c)
	}
	if c.TimeoutDuration() != 20*time.Minute || c.NiceLevel() != 10 || c.OnRedMode() != CIOnRedFileIssue {
		t.Errorf("defaults: timeout %s nice %d on_red %s", c.TimeoutDuration(), c.NiceLevel(), c.OnRedMode())
	}
	if got := c.Commands(""); len(got) != 1 || got[0].Cmd != "make validate" {
		t.Errorf("default tier = %+v", got)
	}
	if got := c.Commands("slow"); len(got) != 2 {
		t.Errorf("slow tier = %+v", got)
	}
	if got := c.Tiers(); len(got) != 1 || got[0] != "slow" {
		t.Errorf("tiers = %v", got)
	}
	if lint := wf.Lint(); len(lint) != 0 {
		t.Errorf("valid block linted: %v", lint)
	}
	if clone := wf.Clone(); clone.CI == nil || clone.CI == wf.CI || len(clone.CI.Run) != 2 {
		t.Errorf("Clone must deep-copy ci: %+v", clone.CI)
	}
}

func TestCIConfigAbsentIsOff(t *testing.T) {
	var wf WorkflowConfig
	if err := yaml.Unmarshal([]byte("statuses: []\n"), &wf); err != nil {
		t.Fatal(err)
	}
	if wf.CI.Enabled() || wf.CI.Validate() != nil || wf.CI.Commands("") != nil {
		t.Errorf("no ci: block must be off and silent")
	}
	if wf.CI.TimeoutDuration() != 10*time.Minute || wf.CI.OnRedMode() != CIOnRedMark {
		t.Error("nil config must still give defaults")
	}
}

func TestCIConfigValidate(t *testing.T) {
	nice := 25
	c := &CIConfig{
		Timeout: "soon",
		Nice:    &nice,
		OnRed:   "revert",
		Run: []CICommand{
			{Cmd: ""},
			{Cmd: "make t", Failures: "FAIL \\S+"},
			{Cmd: "make u", Failures: "("},
		},
	}
	got := strings.Join(c.Validate(), "\n")
	for _, want := range []string{"ci.timeout", "ci.nice 25", "ci.on_red", "ci.run[0]: cmd is empty", "ci.run[1].failures: needs a capture group", "ci.run[2].failures"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	empty := &CIConfig{}
	if got := empty.Validate(); len(got) != 1 || !strings.Contains(got[0], "ci.run is empty") {
		t.Errorf("empty block: %v", got)
	}
	slowOnly := &CIConfig{Run: []CICommand{{Cmd: "x", Tier: "slow"}}}
	if got := strings.Join(slowOnly.Validate(), ""); !strings.Contains(got, "no default-tier command") {
		t.Errorf("slow only: %q", got)
	}
}

func TestCICommandFailureNames(t *testing.T) {
	c := CICommand{Failures: `--- FAIL: (\S+)`}
	out := "--- FAIL: TestA (0.1s)\nok\n--- FAIL: TestB\n--- FAIL: TestA\n--- FAIL: TestC\n"
	if got := c.FailureNames(out, 2); strings.Join(got, ",") != "TestA,TestB" {
		t.Errorf("got %v", got)
	}
	if got := (CICommand{}).FailureNames(out, 5); got != nil {
		t.Errorf("no regex: %v", got)
	}
}

func TestLeadBriefMentionsCIOnlyWhenConfigured(t *testing.T) {
	proj := &Project{Name: "P", Slug: "p"}
	in := LeadBriefInput{Project: proj, Root: t.TempDir()}
	off := LeadBrief(&WorkflowConfig{}, in)
	if strings.Contains(off, "Local CI") {
		t.Error("no ci: block, but the brief mentions local CI")
	}
	on := LeadBrief(&WorkflowConfig{CI: &CIConfig{Run: []CICommand{{Cmd: "make check"}}}}, in)
	for _, want := range []string{"- Local CI:", "CI never messages you: read issue-cli --project p ci status", "before any push", "Never push a release while it is red"} {
		if !strings.Contains(on, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	if strings.Contains(on, "[from ci]") {
		t.Error("the brief still promises a [from ci] message")
	}
}

func TestLeadBriefMentionsReleaseCandidatesOnlyWhenConfigured(t *testing.T) {
	proj := &Project{Name: "P", Slug: "p"}
	in := LeadBriefInput{Project: proj, Root: t.TempDir()}
	if off := LeadBrief(&WorkflowConfig{}, in); strings.Contains(off, "release candidate") {
		t.Error("no release_candidates, but the brief mentions release candidates")
	}
	on := LeadBrief(&WorkflowConfig{Workbench: WorkbenchConfig{ReleaseCandidates: true}}, in)
	for _, want := range []string{"- Releases go through a release candidate on a release branch.", "release cut <version>", "never push a candidate tag", "release pick <slug>", "each pick needs the next candidate", "Never publish the release", "explicit ok on a soaked candidate"} {
		if !strings.Contains(on, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	if strings.Contains(on, "release publish") || strings.Contains(on, "Private tracker") {
		t.Error("no privacy: block, but the brief talks about snapshots or the tracker")
	}
	priv := LeadBrief(&WorkflowConfig{Workbench: WorkbenchConfig{ReleaseCandidates: true}, Privacy: &PrivacyConfig{WorkFiles: "tracker", Code: "snapshots"}}, in)
	for _, want := range []string{"release publish <version> from the release worktree", "never push anything else to public", "tracker sync; never add work files"} {
		if !strings.Contains(priv, want) {
			t.Errorf("privacy brief missing %q", want)
		}
	}
}
