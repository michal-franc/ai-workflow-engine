package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleList_ReturnsIssueList(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content-type, got %s", ct)
	}
}

func TestHandleList_IncludesRetrosTab(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), `href="/p/test-project/retros"`) {
		t.Fatalf("expected list page to link to retros tab\n%s", string(body))
	}
}

func TestHandleList_FiltersWork(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	tests := []struct {
		name  string
		query string
	}{
		{"filter by status", "?status=done"},
		{"filter by system", "?system=Auth"},
		{"filter by priority", "?priority=high"},
		{"filter by label", "?label=bug"},
		{"filter by assignee", "?assignee=alice"},
		{"filter by search", "?search=login"},
		{"combined filters", "?status=in+progress&system=Auth&priority=high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/p/test-project/" + tt.query)
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

func TestHandleList_ShowsActiveBotSummaryAndIssueChip(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{
		{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"},
		{Name: "agent-unrelated-work", StartTime: "2026-04-02 21:58:59"},
	})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	for _, want := range []string{"1 active bot", "1 agent active"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected list view to contain %q\n%s", want, html)
		}
	}
}

func TestHandleIssuesJSON_IncludesActiveSessions(t *testing.T) {
	proj, _ := setupTestProject(t)
	withMockTmuxSessions(t, []AgentSession{{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"}})

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/p/test-project/issues.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var issues []issueJSON
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		t.Fatal(err)
	}

	for _, issue := range issues {
		if issue.Slug == "bug-in-login" {
			if !issue.HasActiveAgent {
				t.Fatal("expected bug-in-login to be marked active")
			}
			if len(issue.ActiveSessions) != 1 || issue.ActiveSessions[0].Name != "agent-bug-in-login" {
				t.Fatalf("unexpected active sessions: %+v", issue.ActiveSessions)
			}
			return
		}
	}
	t.Fatal("bug-in-login missing from issues.json")
}

func TestHandleHash_ChangesWhenActiveSessionsChange(t *testing.T) {
	proj, _ := setupTestProject(t)

	fetchHash := func(sessions []AgentSession) string {
		withMockTmuxSessions(t, sessions)
		ts := newTestServer(t, []tracker.Project{proj})
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/p/test-project/hash")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var payload map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return payload["hash"]
	}

	hashWithout := fetchHash(nil)
	hashWith := fetchHash([]AgentSession{{Name: "agent-bug-in-login", StartTime: "2026-04-02 21:57:59"}})
	if hashWithout == hashWith {
		t.Fatal("expected hash to change when active sessions change")
	}
}

func TestFilterIssues(t *testing.T) {
	issues := []*tracker.Issue{
		{Title: "Bug A", Status: "in progress", System: "Auth", Priority: "high", Labels: []string{"bug"}, Assignee: "alice", BodyRaw: "auth login bug"},
		{Title: "Feature B", Status: "backlog", System: "UI", Priority: "medium", Labels: []string{"enhancement"}, Assignee: "bob", BodyRaw: "dark mode"},
		{Title: "Bug C", Status: "done", System: "Auth", Priority: "low", Labels: []string{"bug", "docs"}, Assignee: "", BodyRaw: "typo fix"},
		{Title: "Feature D", Status: "idea", System: "API", Priority: "critical", Labels: []string{"enhancement", "api"}, Assignee: "alice", BodyRaw: "new endpoint"},
	}

	tests := []struct {
		name     string
		filter   FilterParams
		expected int
	}{
		{
			name:     "no filter returns all",
			filter:   FilterParams{},
			expected: 4,
		},
		{
			name:     "filter by status",
			filter:   FilterParams{Status: "in progress"},
			expected: 1,
		},
		{
			name:     "filter by system (case insensitive)",
			filter:   FilterParams{System: "auth"},
			expected: 2,
		},
		{
			name:     "filter by priority",
			filter:   FilterParams{Priority: "high"},
			expected: 1,
		},
		{
			name:     "filter by label",
			filter:   FilterParams{Label: "bug"},
			expected: 2,
		},
		{
			name:     "filter by label case insensitive",
			filter:   FilterParams{Label: "BUG"},
			expected: 2,
		},
		{
			name:     "filter by assignee",
			filter:   FilterParams{Assignee: "alice"},
			expected: 2,
		},
		{
			name:     "filter by assignee _claimed",
			filter:   FilterParams{Assignee: "_claimed"},
			expected: 3,
		},
		{
			name:     "filter by assignee _unclaimed",
			filter:   FilterParams{Assignee: "_unclaimed"},
			expected: 1,
		},
		{
			name:     "search in title",
			filter:   FilterParams{Search: "Bug"},
			expected: 2,
		},
		{
			name:     "search in body",
			filter:   FilterParams{Search: "dark mode"},
			expected: 1,
		},
		{
			name:     "search case insensitive",
			filter:   FilterParams{Search: "AUTH"},
			expected: 1,
		},
		{
			name:     "combined status and system",
			filter:   FilterParams{Status: "in progress", System: "Auth"},
			expected: 1,
		},
		{
			name:     "combined filters no match",
			filter:   FilterParams{Status: "done", System: "UI"},
			expected: 0,
		},
		{
			name:     "filter by nonexistent label",
			filter:   FilterParams{Label: "nonexistent"},
			expected: 0,
		},
		{
			name:     "combined assignee and priority",
			filter:   FilterParams{Assignee: "alice", Priority: "critical"},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterIssues(issues, tt.filter)
			if len(result) != tt.expected {
				t.Errorf("expected %d issues, got %d", tt.expected, len(result))
			}
		})
	}
}
