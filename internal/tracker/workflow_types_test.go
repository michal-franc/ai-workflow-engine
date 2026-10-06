package tracker

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// loadRaidTypes builds the Raid League fixture: testdata/raid_base.yaml plus
// the types: block from the worked example in docs/Workflow/types.md, so the
// documented example is what the tests exercise.
func loadRaidTypes(t *testing.T) *WorkflowConfig {
	t.Helper()
	base, err := os.ReadFile("testdata/raid_base.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile("../../docs/Workflow/types.md")
	if err != nil {
		t.Fatal(err)
	}
	_, example, ok := strings.Cut(string(doc), "## Worked example")
	if !ok {
		t.Fatal("docs/Workflow/types.md has no worked example section")
	}
	m := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindStringSubmatch(example)
	if m == nil {
		t.Fatal("worked example has no yaml block")
	}
	path := filepath.Join(t.TempDir(), "workflow.yaml")
	if err := os.WriteFile(path, append(base, []byte("\n"+m[1])...), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := LoadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

func actionTitles(actions []WorkflowAction) []string {
	var out []string
	for _, a := range actions {
		if a.Type == "append_section" {
			out = append(out, a.Title)
		}
	}
	return out
}

// Projects without types: ForIssue must return exactly what ForSystem did.
func TestForIssue_NoTypesMatchesForSystem(t *testing.T) {
	for _, file := range []string{"../../workflow.yaml", "../../workflow.yaml.example", "../../demo/workflow.yaml"} {
		wf, err := LoadWorkflow(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if wf.HasTypes() {
			t.Fatalf("%s defines types; this golden test needs a project without them", file)
		}
		if got := wf.Lint(); got != nil {
			t.Errorf("%s: Lint() = %v, want nil without types", file, got)
		}
		systems := []string{"", "NoSuchSystem"}
		for name := range wf.Systems {
			systems = append(systems, name)
		}
		for _, sys := range systems {
			for _, st := range append(wf.GetStatusOrder(), "", "bogus") {
				for _, typ := range []string{"", "tweak"} {
					issue := &Issue{Status: st, System: sys, Type: typ}
					got := wf.ForIssue(issue)
					want := wf.ForSystem(sys)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%s: ForIssue(system=%q status=%q type=%q) differs from ForSystem", file, sys, st, typ)
					}
				}
			}
		}
	}
}

func TestForType_TweakPath(t *testing.T) {
	wf := loadRaidTypes(t)
	tweak := wf.ForType("tweak")

	if got, want := tweak.GetStatusOrder(), []string{"idea", "in progress", "playtest", "shipping", "done", "obsolete"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tweak statuses = %v, want %v", got, want)
	}
	if tweak.ActiveType != "tweak" {
		t.Errorf("ActiveType = %q", tweak.ActiveType)
	}
	if !tweak.IsValidTransition("idea", "in progress") {
		t.Error("tweak: idea → in progress should be valid")
	}
	if tweak.IsValidTransition("idea", "discussion") {
		t.Error("tweak: idea → discussion should be invalid (off path)")
	}
	if got := tweak.RequiredHumanApproval("idea", "in progress"); got != "in progress" {
		t.Errorf("tweak idea → in progress approval = %q, want in progress", got)
	}
	if req, _ := tweak.DefaultNextStatus("idea"); req != "in progress" {
		t.Errorf("tweak next after idea = %q, want in progress", req)
	}
	if req, _ := tweak.DefaultNextStatus("playtest"); req != "shipping" {
		t.Errorf("tweak next after playtest = %q, want shipping", req)
	}
	// Inherited base edges whose ends are both on the path.
	if tweak.GetTransition("shipping", "done") == nil || tweak.GetTransition("idea", "obsolete") == nil {
		t.Error("tweak should inherit shipping → done and idea → obsolete")
	}
	// Dropped base edges.
	if tweak.GetTransition("idea", "discussion") != nil || tweak.GetTransition("playtest", "documentation") != nil {
		t.Error("tweak should not keep edges that leave its path")
	}
	// The type's status prompt overrides the base one.
	if !strings.HasPrefix(tweak.StatusPrompt("in progress"), "Tweak:") {
		t.Errorf("tweak in progress prompt = %q", tweak.StatusPrompt("in progress"))
	}
	if got := tweak.PathLine(); got != "idea → in progress → playtest → shipping → done" {
		t.Errorf("PathLine = %q", got)
	}

	// Feature (the base) keeps the full path and can't take the tweak's shortcut.
	feature := wf.ForType("feature")
	if feature.IsValidTransition("idea", "in progress") {
		t.Error("feature: idea → in progress should be invalid")
	}
	if len(feature.Statuses) != len(wf.Statuses) {
		t.Errorf("feature has %d statuses, base %d", len(feature.Statuses), len(wf.Statuses))
	}
	// Base config is untouched.
	if len(wf.Statuses) != 12 || wf.ActiveType != "" {
		t.Error("ForType mutated the base config")
	}
}

func TestForType_ReplaceDropsBaseActions(t *testing.T) {
	wf := loadRaidTypes(t)
	epic := wf.ForType("epic")

	tr := epic.GetTransition("backlog", "in progress")
	if tr == nil {
		t.Fatal("epic: missing backlog → in progress")
	}
	titles := actionTitles(tr.Actions)
	if len(titles) != 1 || titles[0] != "Implementation" {
		t.Fatalf("epic backlog → in progress appends %v, want one Implementation", titles)
	}
	if !strings.Contains(tr.Actions[len(tr.Actions)-1].Body, "Base branch created") {
		t.Errorf("epic Implementation body = %q, want the epic's checklist", tr.Actions[len(tr.Actions)-1].Body)
	}

	ib := epic.GetTransition("in progress", "balance")
	for _, a := range ib.Actions {
		if a.Rule == "has_comment_prefix: tests:" {
			t.Error("epic in progress → balance still requires the feature's tests: comment")
		}
	}
	// Without replace, the base edge is unchanged for feature.
	feat := wf.ForType("feature").GetTransition("in progress", "balance")
	found := false
	for _, a := range feat.Actions {
		found = found || a.Rule == "has_comment_prefix: tests:"
	}
	if !found {
		t.Error("feature in progress → balance lost the tests: check")
	}
}

func TestForIssue_SystemOverlayRespectsTypePath(t *testing.T) {
	wf := loadRaidTypes(t)
	scoped := wf.ForIssue(&Issue{Type: "tweak", System: "UI", Status: "idea"})

	if scoped.GetStatus("discussion") != nil || scoped.GetStatus("balance") != nil {
		t.Errorf("UI overlay re-added off-path statuses: %v", scoped.GetStatusOrder())
	}
	if scoped.GetTransition("idea", "discussion") != nil || scoped.GetTransition("balance", "playtest") != nil {
		t.Error("UI overlay re-added off-path edges")
	}
	// On-path overlay edges still merge.
	prompts := scoped.TransitionPrompts("in progress", "obsolete")
	if len(prompts) != 1 || !strings.Contains(prompts[0], "superseded") {
		t.Errorf("UI in progress → obsolete prompts = %v", prompts)
	}
	if !scoped.IsValidTransition("idea", "in progress") {
		t.Error("tweak + UI: idea → in progress should be valid")
	}

	// A feature in UI gets the full overlay as before.
	feature := wf.ForIssue(&Issue{Type: "feature", System: "UI", Status: "idea"})
	if got := feature.TransitionPrompts("idea", "discussion"); len(got) != 1 {
		t.Errorf("feature + UI idea → discussion prompts = %v", got)
	}
}

func TestForIssue_OffPathRescue(t *testing.T) {
	wf := loadRaidTypes(t)
	scoped := wf.ForIssue(&Issue{Type: "tweak", Status: "discussion"})

	if scoped.OffPathStatus != "discussion" {
		t.Fatalf("OffPathStatus = %q", scoped.OffPathStatus)
	}
	if got, want := scoped.GetStatusOrder(), []string{"idea", "discussion", "in progress", "playtest", "shipping", "done", "obsolete"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rescued order = %v, want %v", got, want)
	}
	if !scoped.IsValidTransition("discussion", "in progress") || !scoped.IsValidTransition("discussion", "idea") {
		t.Error("rescued status should be a global exit back onto the path")
	}
	if w := scoped.OffPathWarning(); !strings.Contains(w, `"discussion" is not on type "tweak"'s path`) {
		t.Errorf("OffPathWarning = %q", w)
	}
	// On-path issues get no rescue.
	if s := wf.ForIssue(&Issue{Type: "tweak", Status: "playtest"}); s.OffPathStatus != "" || s.OffPathWarning() != "" {
		t.Error("on-path issue should not be rescued")
	}
}

func TestResolveType(t *testing.T) {
	wf := loadRaidTypes(t)
	cases := []struct {
		in, want string
		known    bool
	}{
		{"", "feature", true},
		{"tweak", "tweak", true},
		{" bugfix ", "bugfix", true},
		{"chore", "feature", false},
	}
	for _, c := range cases {
		got, known := wf.ResolveType(c.in)
		if got != c.want || known != c.known {
			t.Errorf("ResolveType(%q) = %q,%v want %q,%v", c.in, got, known, c.want, c.known)
		}
	}
	if got := wf.TypeNames(); !reflect.DeepEqual(got, []string{"feature", "bugfix", "epic", "tweak"}) {
		t.Errorf("TypeNames = %v", got)
	}
	w := wf.UnknownTypeWarning("x", "chore")
	if !strings.Contains(w, `type "chore" is not defined`) || !strings.Contains(w, "issue-cli set-type x <type>") {
		t.Errorf("UnknownTypeWarning = %q", w)
	}
	if wf.UnknownTypeWarning("x", "") != "" || wf.UnknownTypeWarning("x", "tweak") != "" {
		t.Error("no warning expected for empty or known type")
	}
	if wf.ForType("chore").ActiveType != "feature" {
		t.Error("unknown type should resolve to default_type")
	}
}

func TestCheckTransitionOrder_NamesTypePath(t *testing.T) {
	wf := loadRaidTypes(t)
	err := wf.ForType("tweak").CheckTransitionOrder("idea", "discussion")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
	want := `cannot transition from "idea" to "discussion" — type "tweak" goes idea → in progress → playtest → shipping → done (must go to "in progress" next)`
	if err.Error() != want {
		t.Errorf("got  %s\nwant %s", err, want)
	}
}

func TestLint_DocExampleIsClean(t *testing.T) {
	wf := loadRaidTypes(t)
	if got := wf.Lint(); len(got) != 0 {
		t.Errorf("worked example lint warnings:\n%s", strings.Join(got, "\n"))
	}
}

func TestLint_BrokenType(t *testing.T) {
	wf := loadRaidTypes(t)
	wf.DefaultType = "nope"
	wf.Types["broken"] = WorkflowType{
		// No edge into shipping appends Shipping; "review" is not a status;
		// "idea" is listed after "in progress".
		Path:     []string{"in progress", "idea", "review", "shipping", "done"},
		Statuses: []WorkflowStatus{{Name: "balance", Prompt: "x"}},
		Transitions: []WorkflowTransition{
			{From: "idea", To: "discussion"},
		},
	}
	wf.Types["prompty"] = WorkflowType{Statuses: []WorkflowStatus{{Name: "discussion", Prompt: "type prompt"}}}

	got := strings.Join(wf.Lint(), "\n")
	for _, want := range []string{
		`default_type "nope" is not a defined type`,
		`type broken: path entry "review" is not a base status (ignored)`,
		`type broken: path lists "idea" out of base order`,
		`type broken: status override "balance" is not on the path (ignored)`,
		`type broken: transition idea → discussion leaves the path (ignored)`,
		`type broken: shipping → done checks section "Shipping", but nothing on broken's path appends it`,
		`type prompty and system UI both set the "discussion" prompt`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lint missing %q\ngot:\n%s", want, got)
		}
	}
	// A section the base never appends (user-written, like Repro) is not a gap.
	if strings.Contains(got, `"Repro"`) {
		t.Errorf("lint flagged user-authored section Repro:\n%s", got)
	}
}

func TestLint_MissingDefaultType(t *testing.T) {
	wf := loadRaidTypes(t)
	wf.DefaultType = ""
	got := strings.Join(wf.Lint(), "\n")
	if !strings.Contains(got, "default_type is not set") {
		t.Errorf("lint = %s", got)
	}
	if r, known := wf.ResolveType(""); r != "" || !known {
		t.Errorf("no default_type: ResolveType(\"\") = %q,%v want \"\",true", r, known)
	}
}

func TestMerge_NewTransitionKeepsFields(t *testing.T) {
	wf := &WorkflowConfig{Statuses: []WorkflowStatus{{Name: "a"}, {Name: "b"}}}
	wf.Merge(&WorkflowConfig{Transitions: []WorkflowTransition{{
		From: "a", To: "b", Fields: []WorkflowField{{Name: "why", Required: true}},
	}}})
	if got := wf.TransitionFields("a", "b"); len(got) != 1 || got[0].Name != "why" {
		t.Errorf("fields on an overlay-added edge = %v", got)
	}
}

func TestParseIssue_Type(t *testing.T) {
	issue, err := ParseIssue("x.md", []byte("---\ntitle: X\ntype: \" tweak \"\n---\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	if issue.Type != "tweak" {
		t.Errorf("Type = %q", issue.Type)
	}
	// type stays visible as a custom field (projects without types show it).
	found := false
	for _, ef := range issue.ExtraFields {
		found = found || ef.Key == "type"
	}
	if !found {
		t.Error("type should remain in ExtraFields")
	}
	// A non-scalar type: (a custom field in some project) must not fail the parse.
	issue, err = ParseIssue("y.md", []byte("---\ntitle: Y\ntype:\n  - a\n  - b\n---\n"))
	if err != nil {
		t.Fatalf("list-valued type failed the parse: %v", err)
	}
	if issue.Type != "" {
		t.Errorf("list-valued Type = %q, want empty", issue.Type)
	}
}
