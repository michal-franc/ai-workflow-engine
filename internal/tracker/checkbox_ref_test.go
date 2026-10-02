package tracker

import (
	"strings"
	"testing"
)

const refBody = `- [ ] loose box

## Design
- [x] Approach documented
- [ ] Dependencies identified

## Acceptance Criteria
- [ ] first criterion
- [ ] second criterion

## Testing
- [ ] tests run

## Test Plan
- [ ] plan written

` + "```" + `
- [ ] fenced example, not a box
` + "```" + `

## Documentation
- [ ] docs updated`

func idsOf(items []CheckboxItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestListCheckboxesAssignsIDs(t *testing.T) {
	got := idsOf(ListCheckboxes(refBody))
	want := []string{"", "D1", "D2", "AC1", "AC2", "T1", "TP1", "Do1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestListCheckboxesIDsStableWhenSectionsAppended(t *testing.T) {
	before := idsOf(ListCheckboxes(refBody))
	after := idsOf(ListCheckboxes(refBody + "\n\n## Deployment\n- [ ] shipped\n\n## Implementation\n- [ ] code"))
	for i, id := range before {
		if after[i] != id {
			t.Fatalf("id %d changed from %q to %q after appending sections", i, id, after[i])
		}
	}
	if got := after[len(after)-2:]; got[0] != "De1" || got[1] != "I1" {
		t.Fatalf("appended ids = %v, want [De1 I1]", got)
	}
}

func TestNextSectionAbbrev(t *testing.T) {
	taken := map[string]bool{}
	for _, tc := range []struct{ section, want string }{
		{"Design", "D"},
		{"Documentation", "Do"},
		{"Docs", "Doc"},
		{"Human Testing", "HT"},
		{"Test Plan (2)", "TP"},
		{"", ""},
		{"123", ""},
		{"D", ""}, // "D" is taken and the word cannot be extended
	} {
		if got := nextSectionAbbrev(tc.section, taken); got != tc.want {
			t.Errorf("nextSectionAbbrev(%q) = %q, want %q", tc.section, got, tc.want)
		}
	}
}

func TestResolveCheckboxRef(t *testing.T) {
	items := ListCheckboxes(refBody)
	for _, tc := range []struct {
		ref      string
		status   RefStatus
		wantText string
	}{
		{"D2", RefFound, "Dependencies identified"},
		{"d2", RefFound, "Dependencies identified"},
		{"AC1", RefFound, "first criterion"},
		{"do1", RefFound, "docs updated"},
		{"Design#1", RefFound, "Approach documented"},
		{"acceptance criteria # 2", RefFound, "second criterion"},
		{"D9", RefNoSuchBox, ""},
		{"Design#9", RefNoSuchBox, ""},
		{"phase1", RefNotARef, ""},
		{"Nope#1", RefNotARef, ""},
		{"first criterion", RefNotARef, ""},
	} {
		it, st := ResolveCheckboxRef(items, tc.ref)
		if st != tc.status {
			t.Errorf("ResolveCheckboxRef(%q) status = %v, want %v", tc.ref, st, tc.status)
			continue
		}
		if st == RefFound && it.Text != tc.wantText {
			t.Errorf("ResolveCheckboxRef(%q) = %q, want %q", tc.ref, it.Text, tc.wantText)
		}
	}
}

func TestIsCheckboxRefShaped(t *testing.T) {
	for _, s := range []string{"D3", "ac12", "Design#3", "Acceptance Criteria#2"} {
		if !IsCheckboxRefShaped(s) {
			t.Errorf("IsCheckboxRefShaped(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"make", "D3x", "3", "#3", "make test green"} {
		if IsCheckboxRefShaped(s) {
			t.Errorf("IsCheckboxRefShaped(%q) = true, want false", s)
		}
	}
}

func TestCheckLines(t *testing.T) {
	items := ListCheckboxes(refBody)
	d2, _ := ResolveCheckboxRef(items, "D2")
	ac2, _ := ResolveCheckboxRef(items, "AC2")
	got := CheckLines(refBody, []int{d2.Line, ac2.Line})
	if !strings.Contains(got, "- [x] Dependencies identified") || !strings.Contains(got, "- [x] second criterion") {
		t.Fatalf("boxes not ticked:\n%s", got)
	}
	if !strings.Contains(got, "- [ ] first criterion") {
		t.Fatalf("unrelated box ticked:\n%s", got)
	}
}

func TestCountCheckboxesSkipsFencedBlocks(t *testing.T) {
	total, checked := CountCheckboxes(refBody)
	if total != 8 || checked != 1 {
		t.Fatalf("CountCheckboxes = %d/%d, want 1/8", checked, total)
	}
	total, checked = CountCheckboxesInSection(refBody, "test plan")
	if total != 1 || checked != 0 {
		t.Fatalf("CountCheckboxesInSection(Test Plan) = %d/%d, want 0/1", checked, total)
	}
}

func TestSectionGateMessageListsOpenBoxesWithIDs(t *testing.T) {
	wf := &WorkflowConfig{Statuses: []WorkflowStatus{
		{Name: "testing", Validation: []string{"section_checkboxes_checked: Implementation"}},
	}}
	issue := &Issue{
		Slug: "cli/x",
		BodyRaw: "## Implementation\n- [x] a\n- [ ] b\n- [x] c\n- [ ] d\n\n" +
			"```\n- [ ] fenced, ignored\n```",
	}
	err := wf.Validate(issue, "testing", nil)
	if err == nil {
		t.Fatal("expected gate failure")
	}
	msg := err.Error()
	for _, want := range []string{
		`2 of 4 boxes still open in section "Implementation":`,
		"  I2   b",
		"  I4   d",
		"issue-cli check cli/x I2 I4",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("gate message missing %q:\n%s", want, msg)
		}
	}
}

func TestAllCheckboxesGateMessage(t *testing.T) {
	wf := &WorkflowConfig{Statuses: []WorkflowStatus{
		{Name: "done", Validation: []string{"all_checkboxes_checked"}},
	}}
	issue := &Issue{Slug: "cli/x", BodyRaw: "- [ ] loose\n\n## Shipping\n- [ ] pushed"}
	err := wf.Validate(issue, "done", nil)
	if err == nil {
		t.Fatal("expected gate failure")
	}
	msg := err.Error()
	for _, want := range []string{"2 of 2 boxes still open:", "  #1   loose", "  S1   pushed", "issue-cli check cli/x S1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("gate message missing %q:\n%s", want, msg)
		}
	}
}
