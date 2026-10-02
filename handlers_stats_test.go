package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func withStubTelemetryReport(t *testing.T, fn func(*tracker.Project) ([]byte, error)) {
	t.Helper()
	orig := runTelemetryReport
	runTelemetryReport = fn
	t.Cleanup(func() { runTelemetryReport = orig })
}

func getStatsPage(t *testing.T) string {
	t.Helper()
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/p/test-project/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats page must not fail because of CLI usage, got %d:\n%s", resp.StatusCode, body)
	}
	return string(body)
}

func TestStatsPageRendersCLIUsageSection(t *testing.T) {
	report := telemetry.Report{
		V: 1, Enabled: true, Source: "/x/telemetry.jsonl",
		Summary:  telemetry.Summary{Events: 7, Failures: 2, ErrorPct: 28.6, Callers: map[string]int{"agent": 7}},
		Commands: []telemetry.CommandStat{{Command: "transition", Calls: 4, Failures: 2, ErrorPct: 50, P50MS: 12}},
		NeverUsed: telemetry.NeverUsed{
			Commands: []string{"search"}, Topics: []string{"schema"},
			Flags: map[string][]string{"list": {"sort"}},
		},
		Errors:         []telemetry.ErrorStat{{Class: "validation", Command: "transition", Count: 2}},
		Unknown:        []telemetry.UnknownStat{{Class: "unknown_command", Token: "add-comment", Count: 1}},
		RetrySequences: []telemetry.RetryStat{{Failed: "transition (validation)", Next: "same args again", Count: 1}},
	}
	var gotSlug string
	withStubTelemetryReport(t, func(p *tracker.Project) ([]byte, error) {
		gotSlug = p.Slug
		return json.Marshal(report)
	})
	body := getStatsPage(t)
	if gotSlug != "test-project" {
		t.Fatalf("report requested for %q", gotSlug)
	}
	for _, s := range []string{"CLI usage", "7 calls, 2 failed", "<code>transition</code>", "<code>search</code>", "<code>--sort</code>", "add-comment", "same args again", "<code>schema</code>"} {
		if !strings.Contains(body, s) {
			t.Errorf("stats page missing %q", s)
		}
	}
}

func TestStatsPageCLIUsageNotices(t *testing.T) {
	cases := []struct {
		name string
		fn   func(*tracker.Project) ([]byte, error)
		want string
	}{
		{"cli missing", func(*tracker.Project) ([]byte, error) {
			return nil, &exec.Error{Name: "issue-cli", Err: exec.ErrNotFound}
		}, "not on the server&#39;s PATH"},
		{"cli error", func(*tracker.Project) ([]byte, error) {
			return nil, errors.New("exit status 1: boom")
		}, "Could not load CLI usage: exit status 1: boom"},
		{"bad json", func(*tracker.Project) ([]byte, error) { return []byte("nope"), nil }, "Could not parse"},
		{"newer report", func(*tracker.Project) ([]byte, error) { return []byte(`{"v":99}`), nil }, "newer than this viewer"},
		{"disabled", func(*tracker.Project) ([]byte, error) {
			return []byte(`{"v":1,"enabled":false,"disabled":"telemetry: false is set"}`), nil
		}, "CLI usage telemetry is disabled for this project (telemetry: false is set)."},
		{"empty", func(*tracker.Project) ([]byte, error) {
			return []byte(`{"v":1,"enabled":true,"summary":{"events":0}}`), nil
		}, "No CLI calls recorded in the last 30 days."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withStubTelemetryReport(t, c.fn)
			body := getStatsPage(t)
			if !strings.Contains(body, c.want) {
				t.Fatalf("missing %q in CLI usage section:\n%s", c.want, sectionAfter(body, "cli-usage"))
			}
		})
	}
}

func sectionAfter(body, id string) string {
	if i := strings.Index(body, fmt.Sprintf("id=%q", id)); i >= 0 {
		return body[i:]
	}
	return body
}
