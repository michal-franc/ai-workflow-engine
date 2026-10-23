package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// multiLineValues are frontmatter values that span more than one line. A
// writer that edits only the key's own line leaves the rest behind as
// orphan lines, and the frontmatter stops parsing.
var multiLineValues = []struct {
	name  string
	value string // the key's lines, without the key itself
	gone  string // text that must not survive a set or clear
}{
	{"block list", "\n  - a/b\n  - c/d", "c/d"},
	{"compact list", "\n- a/b\n- c/d", "c/d"},
	{"literal block scalar", " |\n  first line\n\n  third line", "third line"},
	{"folded block scalar", " >-\n  folded\n  text", "folded"},
	{"nested map", "\n  owner: alice\n  since: \"2026-10-01\"", "alice"},
}

func writeMultiLineIssue(t *testing.T, value string) string {
	t.Helper()
	fp := filepath.Join(t.TempDir(), "test.md")
	content := "---\ntitle: \"Test\"\nstatus: \"in progress\"\ndepends_on:" + value + "\npriority: \"high\"\n---\n\nBody.\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return fp
}

func readParsed(t *testing.T, fp string) (*Issue, string) {
	t.Helper()
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := ParseIssue("test.md", data)
	if err != nil {
		t.Fatalf("file no longer parses: %v\n%s", err, data)
	}
	if issue.Title != "Test" || issue.Priority != "high" || issue.BodyRaw != "Body." {
		t.Errorf("neighbours corrupted: title=%q priority=%q body=%q\n%s", issue.Title, issue.Priority, issue.BodyRaw, data)
	}
	return issue, string(data)
}

func TestSetFrontmatterField_ReplacesMultiLineValue(t *testing.T) {
	for _, tc := range multiLineValues {
		t.Run(tc.name, func(t *testing.T) {
			fp := writeMultiLineIssue(t, tc.value)
			if err := SetFrontmatterField(fp, "depends_on", "x/y", false); err != nil {
				t.Fatalf("SetFrontmatterField: %v", err)
			}
			issue, content := readParsed(t, fp)
			if strings.Contains(content, tc.gone) {
				t.Errorf("old value left behind:\n%s", content)
			}
			if got := DependsOn(issue); len(got) != 1 || got[0] != "x/y" {
				t.Errorf("DependsOn = %v, want [x/y]\n%s", got, content)
			}
		})
	}
}

func TestSetFrontmatterField_ClearRemovesMultiLineValue(t *testing.T) {
	for _, tc := range multiLineValues {
		t.Run(tc.name, func(t *testing.T) {
			fp := writeMultiLineIssue(t, tc.value)
			if err := SetFrontmatterField(fp, "depends_on", "", true); err != nil {
				t.Fatalf("SetFrontmatterField clear: %v", err)
			}
			_, content := readParsed(t, fp)
			if strings.Contains(content, "depends_on") || strings.Contains(content, tc.gone) {
				t.Errorf("key or its lines left behind:\n%s", content)
			}
		})
	}
}

// The shape set-meta left on disk on 2026-10-23: clearing the key repairs it.
func TestSetFrontmatterField_ClearRemovesOrphanItems(t *testing.T) {
	fp := writeMultiLineIssue(t, " \"a/b\"\n  - a/b")
	if err := SetFrontmatterField(fp, "depends_on", "", true); err != nil {
		t.Fatalf("SetFrontmatterField clear: %v", err)
	}
	_, content := readParsed(t, fp)
	if strings.Contains(content, "a/b") {
		t.Errorf("orphan item left behind:\n%s", content)
	}
}

// The detail page and transitions write extra fields through
// UpdateIssueFrontmatter.
func TestUpdateIssueFrontmatter_ExtraFieldReplacesMultiLineValue(t *testing.T) {
	for _, tc := range multiLineValues {
		t.Run(tc.name, func(t *testing.T) {
			fp := writeMultiLineIssue(t, tc.value)
			if err := UpdateIssueFrontmatter(fp, IssueUpdate{ExtraFields: map[string]string{"depends_on": "x/y"}}); err != nil {
				t.Fatalf("UpdateIssueFrontmatter: %v", err)
			}
			_, content := readParsed(t, fp)
			if strings.Contains(content, tc.gone) {
				t.Errorf("old value left behind:\n%s", content)
			}
		})
	}
}

func TestSetFrontmatterList_WritesYAMLList(t *testing.T) {
	fp := writeMultiLineIssue(t, " \"old/one\"")
	if err := SetFrontmatterList(fp, "depends_on", []string{"a/b", "c/d"}); err != nil {
		t.Fatalf("SetFrontmatterList: %v", err)
	}
	issue, content := readParsed(t, fp)
	if !strings.Contains(content, "depends_on:\n  - \"a/b\"\n  - \"c/d\"\npriority:") {
		t.Errorf("depends_on not written as a block list in place:\n%s", content)
	}
	if got := DependsOn(issue); strings.Join(got, ",") != "a/b,c/d" {
		t.Errorf("DependsOn = %v, want [a/b c/d]", got)
	}

	// Writing it again replaces the list, it doesn't add a second one.
	if err := SetFrontmatterList(fp, "depends_on", []string{"e/f"}); err != nil {
		t.Fatalf("second SetFrontmatterList: %v", err)
	}
	issue, content = readParsed(t, fp)
	if got := DependsOn(issue); strings.Join(got, ",") != "e/f" {
		t.Errorf("DependsOn = %v, want [e/f]\n%s", got, content)
	}
}

func TestSetFrontmatterList_RejectsProtectedKeys(t *testing.T) {
	fp := writeMultiLineIssue(t, " \"a/b\"")
	if err := SetFrontmatterList(fp, "labels", []string{"x"}); err == nil {
		t.Error("labels is managed by the workflow; SetFrontmatterList should refuse it")
	}
}

func TestUpdateIssueFrontmatter_LabelsReplacedInPlace(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "test.md")
	os.WriteFile(fp, []byte("---\ntitle: \"Test\"\nlabels:\n  - bug\n  - ui\npriority: \"high\"\n---\n\nBody.\n"), 0644)
	if err := UpdateIssueFrontmatter(fp, IssueUpdate{Labels: []string{"enhancement"}}); err != nil {
		t.Fatalf("UpdateIssueFrontmatter: %v", err)
	}
	issue, content := readParsed(t, fp)
	if strings.Join(issue.Labels, ",") != "enhancement" {
		t.Errorf("labels = %v, want [enhancement]\n%s", issue.Labels, content)
	}
}

// The safety net: a write whose result doesn't parse is refused, names the
// key, and leaves the file as it was.
func TestFrontmatterWrite_RefusesUnparsableResult(t *testing.T) {
	broken := "---\ntitle: \"Test\"\nnote: [unclosed\n---\n\nBody.\n"
	writers := []struct {
		name  string
		key   string
		write func(fp string) error
	}{
		{"set-meta", "waiting", func(fp string) error { return SetFrontmatterField(fp, "waiting", "x", false) }},
		{"set-meta clear", "waiting", func(fp string) error { return SetFrontmatterField(fp, "waiting", "", true) }},
		{"list", "depends_on", func(fp string) error { return SetFrontmatterList(fp, "depends_on", []string{"a/b"}) }},
		{"update", "status", func(fp string) error {
			s := "testing"
			return UpdateIssueFrontmatter(fp, IssueUpdate{Status: &s})
		}},
	}
	for _, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			fp := filepath.Join(t.TempDir(), "test.md")
			os.WriteFile(fp, []byte(broken), 0644)
			err := w.write(fp)
			if err == nil {
				t.Fatal("write succeeded, want it refused")
			}
			if !strings.Contains(err.Error(), w.key) {
				t.Errorf("error %q does not name the key %q", err, w.key)
			}
			if data, _ := os.ReadFile(fp); string(data) != broken {
				t.Errorf("file changed by a refused write:\n%s", data)
			}
		})
	}
}

func TestCreateIssueFile_RefusesUnparsableFrontmatter(t *testing.T) {
	dir := t.TempDir()
	_, _, err := CreateIssueFileOpts(dir, CreateIssueOpts{Title: "Broken", Version: `0.1" bad: "x`})
	if err == nil {
		t.Fatal("create succeeded, want it refused")
	}
	if !strings.Contains(err.Error(), "frontmatter would not parse") {
		t.Errorf("error %q does not say why it refused", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "broken.md")); !os.IsNotExist(statErr) {
		t.Errorf("issue file written despite the refusal")
	}
}
