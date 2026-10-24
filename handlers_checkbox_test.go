package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// checkboxBody quotes `[ ]` in inline code, a fenced block and prose above
// its boxes: the old page counted those and saved the box above the click.
const checkboxBody = "Use `[ ]` for a task; prose can say [x] too.\n" + // 1
	"\n" + // 2
	"```\n" + // 3
	"- [ ] make cas\n" + // 4
	"```\n" + // 5
	"\n" + // 6
	"- [ ] first\n" + // 7
	"- [ ] second\n" + // 8
	"- [x] third" // 9

func writeCheckboxIssue(t *testing.T, proj tracker.Project) *tracker.Issue {
	t.Helper()
	file := "---\ntitle: \"Boxes below code\"\nstatus: \"in progress\"\n---\n\n" + checkboxBody + "\n"
	if err := os.WriteFile(filepath.Join(proj.IssueDir, "boxes.md"), []byte(file), 0644); err != nil {
		t.Fatal(err)
	}
	return loadTestIssue(t, proj, "boxes-below-code")
}

func postCheckbox(t *testing.T, ts string, payload map[string]interface{}) (int, map[string]string) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(ts+"/p/test-project/issue/boxes-below-code/checkbox", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&data)
	return resp.StatusCode, data
}

// The page's boxes carry the line the server flips, and clicking each box
// flips exactly that box.
func TestCheckboxToggle_FlipsExactlyTheClickedBox(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	issue := writeCheckboxIssue(t, proj)

	page, err := http.Get(ts.URL + "/p/test-project/issue/boxes-below-code")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(page.Body)
	page.Body.Close()
	var lines []string
	for _, m := range regexp.MustCompile(`type="checkbox" data-line="(\d+)"`).FindAllStringSubmatch(string(html), -1) {
		lines = append(lines, m[1])
	}
	if strings.Join(lines, ",") != "7,8,9" {
		t.Fatalf("page checkbox lines = %v, want 7,8,9", lines)
	}

	hash := tracker.BodyHash(issue.BodyRaw)
	want := strings.Split(checkboxBody, "\n")
	for _, step := range []struct {
		line    int
		checked bool
		text    string
	}{{8, true, "- [x] second"}, {7, true, "- [x] first"}, {9, false, "- [ ] third"}} {
		code, data := postCheckbox(t, ts.URL, map[string]interface{}{"line": step.line, "checked": step.checked, "base_body_hash": hash})
		if code != http.StatusOK {
			t.Fatalf("line %d: %d %v", step.line, code, data)
		}
		want[step.line-1] = step.text
		got := loadTestIssue(t, proj, "boxes-below-code").BodyRaw
		if got != strings.Join(want, "\n") {
			t.Fatalf("after line %d the body is:\n%s\nwant:\n%s", step.line, got, strings.Join(want, "\n"))
		}
		if data["body"] != got || data["body_hash"] != tracker.BodyHash(got) {
			t.Fatalf("response body/body_hash = %q/%q, want the file's", data["body"], data["body_hash"])
		}
		hash = data["body_hash"]
	}
}

func TestCheckboxToggle_Refusals(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	issue := writeCheckboxIssue(t, proj)
	hash := tracker.BodyHash(issue.BodyRaw)

	// A line in code or prose is not a box.
	for _, line := range []int{1, 4} {
		if code, data := postCheckbox(t, ts.URL, map[string]interface{}{"line": line, "checked": true, "base_body_hash": hash}); code != http.StatusBadRequest {
			t.Errorf("line %d: %d %v, want 400", line, code, data)
		}
	}
	// The page showed third unticked, the file has it ticked.
	if code, data := postCheckbox(t, ts.URL, map[string]interface{}{"line": 9, "checked": true, "base_body_hash": hash}); code != http.StatusConflict {
		t.Errorf("state mismatch: %d %v, want 409", code, data)
	}
	if code, _ := postCheckbox(t, ts.URL, map[string]interface{}{"line": 7, "checked": true}); code != http.StatusBadRequest {
		t.Errorf("no base_body_hash: %d, want 400", code)
	}

	// The CLI wrote the file after the page loaded: refused, nothing written.
	if _, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		return body + "\n\n## Testing\n- [ ] Tests pass", true, nil
	}); err != nil {
		t.Fatal(err)
	}
	before := loadTestIssue(t, proj, "boxes-below-code").BodyRaw
	code, data := postCheckbox(t, ts.URL, map[string]interface{}{"line": 7, "checked": true, "base_body_hash": hash})
	if code != http.StatusConflict || !strings.Contains(data["error"], "changed since the page loaded") {
		t.Fatalf("stale page: %d %v, want 409 changed since the page loaded", code, data)
	}
	if after := loadTestIssue(t, proj, "boxes-below-code").BodyRaw; after != before {
		t.Fatalf("stale toggle wrote the file:\n%s", after)
	}
}
