package testids

import (
	"reflect"
	"regexp"
	"testing"
)

func TestSegment(t *testing.T) {
	for in, want := range map[string]string{
		"in progress":         "in_progress",
		"human-testing":       "human-testing",
		" Backlog ":           "backlog",
		"ui/dark-mode-toggle": "ui/dark-mode-toggle",
		"v1.2 release":        "v1_2_release",
	} {
		if got := Segment(in); got != want {
			t.Errorf("Segment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegexp(t *testing.T) {
	re := Regexp("board.column.*.card.#")
	for id, want := range map[string]bool{
		"board.column.in_progress.card.0": true,
		"board.column.backlog.card.12":    true,
		"board.column.backlog.card.x":     false,
		"board.column.card.0":             false,
		"board.column.a.b.card.0":         false,
		"board.column.ui/x-y.card.3":      true,
	} {
		if got := re.MatchString(id); got != want {
			t.Errorf("%q matches %s = %v, want %v", id, re, got, want)
		}
	}
}

func TestCompatible(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"board.card.*", "board.card.ui/x", true},
		{"board.card.*", "board.card.*", true},
		{"board.card.*", "board.card.x.title", false},
		{"detail.comment.eye.#", "detail.comment.eye.3", true},
		{"detail.comment.eye.#", "detail.comment.eye.*", true},
		{"detail.comment.eye.#", "detail.comment.eye.x", false},
		{"list.row.*", "board.row.x", false},
	}
	for _, c := range cases {
		if got := Compatible(c.a, c.b); got != c.want {
			t.Errorf("Compatible(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := Compatible(c.b, c.a); got != c.want {
			t.Errorf("Compatible(%q, %q) = %v, want %v (symmetry)", c.b, c.a, got, c.want)
		}
	}
}

func TestKnownAndSuggest(t *testing.T) {
	if !Known("board.card.ui/dark-mode-toggle.title") {
		t.Error("a concrete card title id should be known")
	}
	if Known("board.colum.backlog") {
		t.Error("a misspelt id should not be known")
	}
	if got := Suggest("board.colum.*"); got != "board.column.*" {
		t.Errorf("Suggest = %q, want board.column.*", got)
	}
	if got := Suggest("zzzzzzzzzzzzzzzzzzzzzzzzzz"); got != "" {
		t.Errorf("Suggest for nonsense = %q, want empty", got)
	}
}

func TestScanTemplate(t *testing.T) {
	src := `<div data-testid="board.column.{{tid .Status}}">
<span data-testid="header.count">
html += '<button data-testid="detail.comment.eye.' + idx + '">';
cb.dataset.testid = 'detail.body.checkbox.' + idx;
el.dataset.testid = 'list.empty';`
	want := []string{"board.column.*", "header.count", "detail.comment.eye.*", "detail.body.checkbox.*", "list.empty"}
	if got := ScanTemplate(src); !reflect.DeepEqual(got, want) {
		t.Errorf("ScanTemplate = %v, want %v", got, want)
	}
}

// idFormat: page.area.element, lower snake case, `*` or `#` for a dynamic
// segment.
var idFormat = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.([a-z][a-z0-9_]*|\*|#))+$`)

func TestIDsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range IDs {
		if seen[id] {
			t.Errorf("duplicate id %q", id)
		}
		seen[id] = true
		if !idFormat.MatchString(id) {
			t.Errorf("id %q is not lower snake case with dots", id)
		}
	}
}
