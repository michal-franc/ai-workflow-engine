package tracker

import (
	"reflect"
	"testing"
)

func TestApprovalComment_Format(t *testing.T) {
	got := ApprovalComment("looks good, go", "viewer", "2026-10-23", "backlog")
	want := `approval: "looks good, go" (viewer, 2026-10-23) → backlog`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = ApprovalComment(`say "hi"`+"\nthen go", "viewer", "2026-10-23", "backlog", "in progress")
	want = `approval: "say \"hi\"\nthen go" (viewer, 2026-10-23) → backlog, in progress`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseApprovalComment(t *testing.T) {
	cases := []struct {
		text string
		want ApprovalWords
		ok   bool
	}{
		{
			text: `approval: "looks good, go" (viewer, 2026-10-23) → backlog`,
			want: ApprovalWords{Words: "looks good, go", How: "viewer", Date: "2026-10-23", Statuses: []string{"backlog"}},
			ok:   true,
		},
		{
			text: `approval: "ship it" (in the lead chat, 2026-10-20) → backlog, in progress — recorded by lead-issue-viewer`,
			want: ApprovalWords{Words: "ship it", How: "in the lead chat", Date: "2026-10-20", Statuses: []string{"backlog", "in progress"}},
			ok:   true,
		},
		{
			// Written before the → suffix.
			text: `approval: "approved" (on a call, 2026-10-01) — recorded by lead-x`,
			want: ApprovalWords{Words: "approved", How: "on a call", Date: "2026-10-01"},
			ok:   true,
		},
		{
			text: `approval: "a \"quoted\"\nline" (viewer, 2026-10-23) → testing`,
			want: ApprovalWords{Words: "a \"quoted\"\nline", How: "viewer", Date: "2026-10-23", Statuses: []string{"testing"}},
			ok:   true,
		},
		{text: "question: ready for backlog?"},
		{text: `approval: not quoted`},
	}
	for _, c := range cases {
		got, ok := ParseApprovalComment(c.text)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseApprovalComment(%q) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
		}
	}
}

func TestApprovalComment_RoundTrip(t *testing.T) {
	text := ApprovalComment("go", "viewer", "2026-10-23", "backlog", "in progress")
	a, ok := ParseApprovalComment(text)
	if !ok || a.Words != "go" || !a.Approves("In Progress") || a.Approves("testing") {
		t.Fatalf("round trip = %+v, %v", a, ok)
	}
	old, _ := ParseApprovalComment(`approval: "go" (viewer, 2026-10-23)`)
	if !old.Approves("anything") {
		t.Fatal("words without statuses must match any status")
	}
}
