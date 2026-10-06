package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const viewerTypedWorkflow = `
statuses:
  - name: idea
  - name: discussion
  - name: in progress
  - name: done
transitions:
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
    path: [idea, in progress, done]
    transitions:
      - from: idea
        to: in progress
        actions:
          - type: require_human_approval
            status: in progress
`

func setupTypedProject(t *testing.T) tracker.Project {
	t.Helper()
	root := t.TempDir()
	issueDir := filepath.Join(root, "issues")
	os.MkdirAll(issueDir, 0o755)
	wfPath := filepath.Join(root, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(viewerTypedWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"small-tweak.md":   "---\ntitle: \"Small tweak\"\nstatus: \"idea\"\ntype: \"tweak\"\n---\n\nbody\n",
		"big-feature.md":   "---\ntitle: \"Big feature\"\nstatus: \"idea\"\n---\n\nbody\n",
		"discussed-one.md": "---\ntitle: \"Discussed one\"\nstatus: \"discussion\"\n---\n\nbody\n",
	} {
		if err := os.WriteFile(filepath.Join(issueDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return tracker.Project{Name: "Typed", Slug: "typed", IssueDir: issueDir, WorkflowFile: wfPath}
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func postJSON(t *testing.T, url, body string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestBoard_TypeBadgeAndFilter(t *testing.T) {
	ts := newTestServer(t, []tracker.Project{setupTypedProject(t)})
	defer ts.Close()

	_, html := getBody(t, ts.URL+"/p/typed/board")
	for _, want := range []string{`<select name="type"`, `board-card-type" title="Type of work">tweak<`, `board-card-type" title="Type of work">feature<`, `id="create-type"`} {
		if !strings.Contains(html, want) {
			t.Errorf("board missing %q", want)
		}
	}

	_, html = getBody(t, ts.URL+"/p/typed/board?type=tweak")
	if !strings.Contains(html, "Small tweak") || strings.Contains(html, "Big feature") {
		t.Error("board ?type=tweak should show only the tweak")
	}

	_, html = getBody(t, ts.URL+"/p/typed/?type=feature")
	if strings.Contains(html, "Small tweak") || !strings.Contains(html, "Big feature") || !strings.Contains(html, `class="type-badge"`) {
		t.Errorf("list ?type=feature should show only features, with a type badge: tweak=%v feature=%v badge=%v", strings.Contains(html, "Small tweak"), strings.Contains(html, "Big feature"), strings.Contains(html, `class="type-badge"`))
	}

	_, js := getBody(t, ts.URL+"/p/typed/issues.json")
	if !strings.Contains(js, `"type":"tweak"`) || !strings.Contains(js, `"type":"feature"`) {
		t.Errorf("issues.json missing types: %s", js)
	}

	if code, _ := getBody(t, ts.URL+"/p/typed/graph?type=tweak"); code != http.StatusOK {
		t.Errorf("graph ?type=tweak = %d", code)
	}
}

func TestBoard_UntypedProjectHasNoTypeControls(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	for _, page := range []string{"/board", "/", "/graph"} {
		_, html := getBody(t, ts.URL+"/p/test-project"+page)
		if strings.Contains(html, `name="type"`) || strings.Contains(html, `id="create-type"`) || strings.Contains(html, "board-card-type") {
			t.Errorf("%s shows type controls in a project without types", page)
		}
	}
	_, js := getBody(t, ts.URL+"/p/test-project/issues.json")
	if strings.Contains(js, `"type"`) {
		t.Errorf("issues.json has a type field without types: %s", js)
	}
}

func TestCreate_TypedIssue(t *testing.T) {
	proj := setupTypedProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	// The board's "+" on the discussion column: a tweak never visits it, so
	// the issue starts at the tweak's first status.
	code, out := postJSON(t, ts.URL+"/p/typed/issues/create", `{"title":"New tweak","status":"discussion","type":"tweak","body":"x"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %v", code, out)
	}
	data, _ := os.ReadFile(filepath.Join(proj.IssueDir, "new-tweak.md"))
	if !strings.Contains(string(data), `type: "tweak"`) || !strings.Contains(string(data), `status: "idea"`) {
		t.Errorf("created file:\n%s", data)
	}

	code, _ = postJSON(t, ts.URL+"/p/typed/issues/create", `{"title":"Untyped","body":"x"}`)
	data, _ = os.ReadFile(filepath.Join(proj.IssueDir, "untyped.md"))
	if code != http.StatusCreated || !strings.Contains(string(data), `type: "feature"`) {
		t.Errorf("default type create = %d:\n%s", code, data)
	}

	resp, err := http.Post(ts.URL+"/p/typed/issues/create", "application/json", strings.NewReader(`{"title":"Bad","type":"chore","body":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown type create = %d, want 400", resp.StatusCode)
	}
}

func TestSetIssueTypeEndpoint(t *testing.T) {
	proj := setupTypedProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	// Human retype at any status; on-path status is kept.
	code, out := postJSON(t, ts.URL+"/p/typed/issue/big-feature/type", `{"type":"tweak"}`)
	if code != http.StatusOK || out["issue_status"] != "idea" {
		t.Fatalf("retype = %d %v", code, out)
	}

	// discussion is not on tweak's path: 409 with the statuses to pick from.
	code, out = postJSON(t, ts.URL+"/p/typed/issue/discussed-one/type", `{"type":"tweak"}`)
	if code != http.StatusConflict {
		t.Fatalf("off-path retype = %d %v, want 409", code, out)
	}
	if got, _ := json.Marshal(out["statuses"]); string(got) != `["idea","in progress","done"]` {
		t.Errorf("statuses = %s", got)
	}
	code, out = postJSON(t, ts.URL+"/p/typed/issue/discussed-one/type", `{"type":"tweak","status":"in progress"}`)
	if code != http.StatusOK {
		t.Fatalf("retype with pick = %d %v", code, out)
	}
	data, _ := os.ReadFile(filepath.Join(proj.IssueDir, "discussed-one.md"))
	if !strings.Contains(string(data), `type: "tweak"`) || !strings.Contains(string(data), `status: "in progress"`) {
		t.Errorf("after retype:\n%s", data)
	}

	if code, _ := postJSON(t, ts.URL+"/p/typed/issue/big-feature/type", `{"type":"chore"}`); code != http.StatusBadRequest {
		t.Errorf("unknown type = %d, want 400", code)
	}
}

func TestDetail_ApprovalsAndPreviewFollowType(t *testing.T) {
	ts := newTestServer(t, []tracker.Project{setupTypedProject(t)})
	defer ts.Close()

	_, html := getBody(t, ts.URL+"/p/typed/issue/small-tweak")
	if !strings.Contains(html, `id="approve-in-progress"`) || !strings.Contains(html, `id="type-select"`) {
		t.Error("tweak at idea should show the in progress approval and the Type select")
	}
	_, html = getBody(t, ts.URL+"/p/typed/issue/big-feature")
	if strings.Contains(html, `id="approve-in-progress"`) {
		t.Error("feature at idea should not show the tweak's approval")
	}

	_, preview := getBody(t, ts.URL+"/p/typed/issue/small-tweak/transition?to=discussion")
	if !strings.Contains(preview, `"allowed":false`) {
		t.Errorf("tweak idea → discussion preview: %s", preview)
	}
}

func TestDispatchPrompt_TypeLine(t *testing.T) {
	proj := setupTypedProject(t)
	issue := &tracker.Issue{Title: "T", Slug: "small-tweak", Status: "idea", Type: "tweak"}
	wf := proj.LoadWorkflowForIssue(issue)
	prompt := buildAgentPrompt(&proj, issue, wf, "", "")
	if !strings.Contains(prompt, "  Status: idea\n  Type: tweak (idea → in progress → done)\n") {
		t.Errorf("dispatch prompt missing type line:\n%s", prompt)
	}

	plain, _ := setupTestProject(t)
	untyped := &tracker.Issue{Title: "U", Slug: "u", Status: "idea"}
	if p := buildAgentPrompt(&plain, untyped, plain.LoadWorkflowForIssue(untyped), "", ""); strings.Contains(p, "Type:") {
		t.Error("untyped dispatch prompt has a Type line")
	}
}
