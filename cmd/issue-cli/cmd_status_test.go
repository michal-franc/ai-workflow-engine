package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// status prints the viewer's status.md / status.json body untouched, from
// the project's viewer_url.
func TestStatus_PrintsTheViewersPage(t *testing.T) {
	const md = "# Proj status, 2026-10-23 10:00\n\n## Waiting on you (0)\n- Nothing\n"
	const js = `{"project":"Proj","open":3}` + "\n"
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/p/proj/status.md":
			w.Write([]byte(md))
		case "/p/proj/status.json":
			w.Write([]byte(js))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	proj := makeLeadProject(t)
	proj.ViewerURL = ts.URL

	for _, tc := range []struct {
		args []string
		json bool
		want string
	}{
		{nil, false, md},
		{[]string{"--md"}, false, md},
		{nil, true, js},
	} {
		ctx, stdout, _ := newTestContext(proj, tc.json)
		if err := runStatus(ctx, tc.args); err != nil {
			t.Fatalf("%v json=%v: %v", tc.args, tc.json, err)
		}
		if stdout.String() != tc.want {
			t.Errorf("%v json=%v: stdout = %q, want %q", tc.args, tc.json, stdout.String(), tc.want)
		}
	}
	if want := "GET /p/proj/status.md GET /p/proj/status.md GET /p/proj/status.json"; strings.Join(paths, " ") != want {
		t.Errorf("requests = %v, want %s", paths, want)
	}
}

func TestStatus_ViewerUnreachable(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close()
	proj := makeLeadProject(t)
	proj.ViewerURL = url
	ctx, stdout, _ := newTestContext(proj, false)
	err := runStatus(ctx, nil)
	if exitOf(err) != 1 || !strings.Contains(err.Error(), "viewer not reachable at "+url+"/p/proj/status.md") || !strings.Contains(err.Error(), "viewer_url") {
		t.Fatalf("err = %v (exit %d)", err, exitOf(err))
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}

	// A viewer that answers with an error is not a stand-up either.
	ts = httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	proj.ViewerURL = ts.URL
	ctx, _, _ = newTestContext(proj, false)
	if err := runStatus(ctx, nil); exitOf(err) != 1 || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: err = %v", err)
	}
}

func TestStatus_UsageErrors(t *testing.T) {
	proj := makeLeadProject(t)
	ctx, _, _ := newTestContext(proj, false)
	err := runStatus(ctx, []string{"ui/child"})
	if exitOf(err) != 2 || !strings.Contains(err.Error(), "issue-cli --project proj show ui/child") {
		t.Fatalf("slug: err = %v (exit %d)", err, exitOf(err))
	}
	ctx, _, _ = newTestContext(proj, true)
	if err := runStatus(ctx, []string{"--md"}); exitOf(err) != 2 {
		t.Fatalf("--md --json: err = %v", err)
	}
}

// projects --json names each project's viewer address (the issue-approve mod
// reads it instead of an environment variable).
func TestProjectsJSON_ViewerURL(t *testing.T) {
	proj := makeLeadProject(t)
	proj.ViewerURL = "https://issues.example.com"
	ctx, stdout, _ := newTestContext(proj, true)
	ctx.AllProjects = []tracker.Project{*proj}
	if err := runProjects(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var got []projectListEntry
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || len(got) != 1 || got[0].ViewerURL != "https://issues.example.com" {
		t.Fatalf("projects --json = %s (%v)", stdout.String(), err)
	}
}
