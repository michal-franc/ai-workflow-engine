package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleBoard_ReturnsBoardView(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/board")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleBoard_Filters(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	tests := []struct {
		name  string
		query string
	}{
		{"version filter", "?version=1.0"},
		{"system filter", "?system=Auth"},
		{"assignee filter", "?assignee=alice"},
		{"claimed filter", "?assignee=_claimed"},
		{"unclaimed filter", "?assignee=_unclaimed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/p/test-project/board" + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestHandleBoard_ShowsActiveBotSummaryAndIssueChip(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"},
		{Name: "agent-unrelated-work", StartTime: "2026-04-02 21:58:59"},
	})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/board")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{"1 active bot", "board-card-agent-active"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected board view to contain %q\n%s", want, html)
		}
	}
}

func TestHandleGraph_Returns200(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleGraph_ShowsWorkflowStatuses(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, status := range []string{"backlog", "in progress", "testing"} {
		if !strings.Contains(html, status) {
			t.Fatalf("expected graph to contain status %q", status)
		}
	}
}

func TestHandleGraph_HidesDoneByDefault(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if strings.Contains(html, "Fix typo") {
		t.Fatal("expected done issue to be hidden by default")
	}
}

func TestHandleGraph_ShowDoneFilter(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph?done=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Fix typo") {
		t.Fatal("expected done issue to appear with ?done=1")
	}
}

func TestHandleGraph_SystemFilter(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/graph?system=Auth")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Bug in login") {
		t.Fatal("expected Auth issue to appear")
	}
	if strings.Contains(html, "Add dark mode") {
		t.Fatal("expected non-Auth issue to be filtered out")
	}
}

func TestHandleGraph_ShowsGraphNavTab(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	for _, path := range []string{"/p/test-project/", "/p/test-project/board", "/p/test-project/graph"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "/graph") {
			t.Fatalf("expected %q page to contain graph nav link", path)
		}
	}
}

func TestBoardCardFields_RendersExtraFields(t *testing.T) {
	fn, ok := funcMap["boardCardFields"].(func([]string, *IssueView) []BoardCardField)
	if !ok {
		t.Fatal("boardCardFields funcMap entry has unexpected signature")
	}

	issue := &IssueView{
		Issue: &tracker.Issue{
			System: "Repayments",
			ExtraFields: []tracker.ExtraField{
				{Key: "waiting", Value: "team-input"},
				{Key: "team", Value: "payments"},
				{Key: "participants", IsList: true, Values: []string{"alice", "bob"}},
				{Key: "empty", Value: ""},
			},
		},
	}

	fields := []string{"system", "waiting", "team", "participants", "empty", "unknown"}
	got := fn(fields, issue)

	byName := map[string]BoardCardField{}
	for _, f := range got {
		byName[f.Name] = f
	}

	if byName["system"].Value != "Repayments" {
		t.Errorf("system value = %q, want %q", byName["system"].Value, "Repayments")
	}
	if byName["waiting"].Value != "team-input" {
		t.Errorf("waiting value = %q, want %q", byName["waiting"].Value, "team-input")
	}
	if byName["team"].Value != "payments" {
		t.Errorf("team value = %q, want %q", byName["team"].Value, "payments")
	}
	p, ok := byName["participants"]
	if !ok {
		t.Fatal("participants field missing from result")
	}
	if !p.IsList || len(p.Values) != 2 || p.Values[0] != "alice" || p.Values[1] != "bob" {
		t.Errorf("participants field = %+v, want list [alice bob]", p)
	}
	if _, ok := byName["empty"]; ok {
		t.Errorf("empty extra field should be omitted, got %+v", byName["empty"])
	}
	if _, ok := byName["unknown"]; ok {
		t.Errorf("unknown field with no extra match should be omitted")
	}

	if len(got) < 3 {
		t.Fatalf("result length = %d, want at least 3", len(got))
	}
	if got[0].Name != "system" || got[1].Name != "waiting" || got[2].Name != "team" {
		t.Errorf("ordering not preserved: got %q, %q, %q", got[0].Name, got[1].Name, got[2].Name)
	}
}
