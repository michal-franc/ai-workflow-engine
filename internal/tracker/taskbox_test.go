package tracker

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// taskBoxBody quotes the checkbox syntax in inline code, a fenced code
// block, prose and an indented code block above its real boxes: the shape
// that sent a click to the box above.
const taskBoxBody = "Use `[ ]` for a task, and write [x] in prose too.\n" + // 1
	"\n" + // 2
	"```\n" + // 3
	"- [ ] in a fence\n" + // 4
	"```\n" + // 5
	"\n" + // 6
	"    - [ ] indented code\n" + // 7
	"\n" + // 8
	"- [ ] first\n" + // 9
	"- [x] second `[ ]`\n" + // 10
	"  - [ ] nested\n" + // 11
	"1. [ ] ordered\n" // 12

func TestTaskBoxesSkipCodeAndProse(t *testing.T) {
	got := TaskBoxes(taskBoxBody)
	want := []TaskBox{{Line: 9}, {Line: 10, Checked: true}, {Line: 11}, {Line: 12}}
	if len(got) != len(want) {
		t.Fatalf("TaskBoxes = %+v, want lines 9-12", got)
	}
	for i, w := range want {
		if got[i].Line != w.Line || got[i].Checked != w.Checked {
			t.Errorf("box %d = %+v, want line %d checked %v", i, got[i], w.Line, w.Checked)
		}
	}
}

// Each box flips only its own line; every other byte stays.
func TestToggleTaskBoxFlipsExactlyThatLine(t *testing.T) {
	for _, tc := range []struct {
		line    int
		checked bool
		want    string
	}{
		{9, true, "- [x] first"},
		{10, false, "- [ ] second `[ ]`"},
		{11, true, "  - [x] nested"},
		{12, true, "1. [x] ordered"},
	} {
		got, err := ToggleTaskBox(taskBoxBody, tc.line, tc.checked)
		if err != nil {
			t.Fatalf("line %d: %v", tc.line, err)
		}
		before, after := strings.Split(taskBoxBody, "\n"), strings.Split(got, "\n")
		for i := range before {
			if i == tc.line-1 {
				if after[i] != tc.want {
					t.Errorf("line %d = %q, want %q", tc.line, after[i], tc.want)
				}
			} else if after[i] != before[i] {
				t.Errorf("toggling line %d changed line %d: %q → %q", tc.line, i+1, before[i], after[i])
			}
		}
	}
}

func TestToggleTaskBoxRefusesCodeProseAndState(t *testing.T) {
	for _, line := range []int{1, 2, 4, 7, 99} {
		if _, err := ToggleTaskBox(taskBoxBody, line, true); !errors.Is(err, ErrNoTaskBox) {
			t.Errorf("line %d: err = %v, want ErrNoTaskBox", line, err)
		}
	}
	if _, err := ToggleTaskBox(taskBoxBody, 10, true); !errors.Is(err, ErrTaskBoxState) {
		t.Errorf("ticking a ticked box: err = %v, want ErrTaskBoxState", err)
	}
}

// The rendered page tags exactly the parsed boxes with their lines, in order.
func TestParseIssueTagsCheckboxesWithLine(t *testing.T) {
	issue, err := ParseIssue("x.md", []byte("---\ntitle: X\n---\n\n"+taskBoxBody))
	if err != nil {
		t.Fatal(err)
	}
	inputs := regexp.MustCompile(`<input[^>]*type="checkbox"[^>]*>`).FindAllString(issue.BodyHTML, -1)
	lines := regexp.MustCompile(`data-line="(\d+)"`)
	var got []string
	for _, in := range inputs {
		m := lines.FindStringSubmatch(in)
		if m == nil {
			t.Fatalf("checkbox without data-line: %s", in)
		}
		got = append(got, m[1])
	}
	if strings.Join(got, ",") != "9,10,11,12" {
		t.Fatalf("rendered checkbox lines = %v, want 9,10,11,12\n%s", got, issue.BodyHTML)
	}
	if !strings.Contains(issue.BodyHTML, `<input checked="" disabled="" type="checkbox" data-line="10">`) {
		t.Errorf("checked box lost its checked attribute:\n%s", issue.BodyHTML)
	}
}

func TestToggleIssueTaskBox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.md")
	file := "---\ntitle: X\nstatus: idea\n---\n\n" + taskBoxBody
	if err := os.WriteFile(path, []byte(file), 0644); err != nil {
		t.Fatal(err)
	}
	issue, _ := ParseIssue("x.md", []byte(file))
	hash := BodyHash(issue.BodyRaw)

	if _, err := ToggleIssueTaskBox(path, "stale", 9, true); !errors.Is(err, ErrStaleBody) {
		t.Fatalf("stale hash: err = %v, want ErrStaleBody", err)
	}
	next, err := ToggleIssueTaskBox(path, hash, 9, true)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	after, _ := ParseIssue("x.md", data)
	if want := strings.Replace(issue.BodyRaw, "- [ ] first", "- [x] first", 1); after.BodyRaw != want {
		t.Fatalf("body after toggle:\n%s\nwant:\n%s", after.BodyRaw, want)
	}
	if after.Status != "idea" {
		t.Errorf("frontmatter lost: status = %q", after.Status)
	}
	if next != after.BodyRaw {
		t.Errorf("returned body %q, file body %q", next, after.BodyRaw)
	}
	// The old hash is now stale: a second click from an unrefreshed page
	// without the returned hash is refused.
	if _, err := ToggleIssueTaskBox(path, hash, 11, true); !errors.Is(err, ErrStaleBody) {
		t.Fatalf("old hash after a write: err = %v, want ErrStaleBody", err)
	}
	if _, err := ToggleIssueTaskBox(path, BodyHash(next), 11, true); err != nil {
		t.Fatalf("chained hash: %v", err)
	}
}
