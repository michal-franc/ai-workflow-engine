package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
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
	// The CLI usage report is read in the background; the page says so
	// until it lands, like a reload a moment later would.
	for i := 0; ; i++ {
		resp, err := http.Get(ts.URL + "/p/test-project/stats")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stats page must not fail because of CLI usage, got %d:\n%s", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "being read in the background") || i == 200 {
			return string(body)
		}
		time.Sleep(5 * time.Millisecond)
	}
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

// The time section: a bugfix done an hour ago, from its stats sidecar and
// per-agent clilog, split per status, with its approval wait and its
// refused transition.
func TestStatsPageTimeSection(t *testing.T) {
	withStubTelemetryReport(t, func(*tracker.Project) ([]byte, error) { return nil, errors.New("off") })
	proj := fixItProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/p/test-project/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	for _, s := range []string{
		`data-testid="stats.time"`,
		"1 issue done",
		`<td>bugfix</td><td class="num">1</td><td class="num">1h 0m / 1h 0m</td>`,
		// human-testing: 50m in it, 30m waiting on you (from the end of the
		// refused call at 20m to the approval at 50m), the rest agent.
		`<td>human-testing</td><td class="num">1</td><td class="num">50m / 50m</td><td class="num">20m 1s / 20m 1s</td><td class="num">30m / 30m</td><td class="num">0s / 0s</td>`,
		`<td>human-testing→done</td><td class="num">1</td><td class="num">30m / 30m</td>`,
		`<td>human-testing→done</td><td><code>approval_missing</code></td><td>transition</td><td class="num">1</td>`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("stats page missing %q", s)
		}
	}
	if t.Failed() {
		i := strings.Index(body, `data-testid="stats.time"`)
		t.Logf("section:\n%s", body[i:min(len(body), i+6000)])
	}
}

// fixItProject is the test project plus fix-it, a bugfix done an hour ago:
// 10m in progress, 50m in human-testing with a refused transition to done
// at 20m and your approval at 50m.
func fixItProject(t *testing.T) tracker.Project {
	t.Helper()
	proj, tmp := setupTestProject(t)
	proj.WorkDir = tmp
	t0 := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	path := filepath.Join(proj.IssueDir, "fix-it.md")
	if err := os.WriteFile(path, []byte("---\ntitle: \"Fix it\"\nstatus: \"done\"\ntype: \"bugfix\"\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	approved := t0.Add(50 * time.Minute)
	stats, _ := json.Marshal(tracker.StatsStore{Transitions: []tracker.TransitionStat{
		{From: "in progress", To: "human-testing", TS: t0.Add(10 * time.Minute)},
		{From: "human-testing", To: "done", TS: t0.Add(60 * time.Minute), ApprovedAt: &approved},
	}})
	if err := os.WriteFile(tracker.StatsSidecarPath(path), stats, 0o644); err != nil {
		t.Fatal(err)
	}
	exit0, exit1, dur := 0, 1, int64(500)
	log := clilog.AgentPath(tmp, tracker.AgentSessionName("fix-it"))
	for _, r := range []clilog.Record{
		{TS: t0.Format(time.RFC3339), Args: []string{"start", "fix-it"}, Exit: &exit0, DurMS: &dur},
		{TS: t0.Add(9 * time.Minute).Format(time.RFC3339), Args: []string{"transition", "fix-it", "--to", "human-testing"}, Exit: &exit0, DurMS: &dur},
		{TS: t0.Add(20 * time.Minute).Format(time.RFC3339), Args: []string{"transition", "fix-it", "--to", "done"}, Exit: &exit1, ErrClass: "approval_missing", DurMS: &dur},
		{TS: t0.Add(55 * time.Minute).Format(time.RFC3339), Args: []string{"transition", "fix-it", "--to", "done"}, Exit: &exit0, DurMS: &dur},
	} {
		if err := clilog.Append(log, r); err != nil {
			t.Fatal(err)
		}
	}

	return proj
}

func TestParseStatsOpts(t *testing.T) {
	for q, want := range map[string]struct {
		days   int
		window time.Duration
		group  string
	}{
		"":                        {30, 60 * 24 * time.Hour, "type"},
		"range=90d&group=system":  {90, 120 * 24 * time.Hour, "system"},
		"range=all":               {0, 3650 * 24 * time.Hour, "type"},
		"range=bogus&group=bogus": {30, 60 * 24 * time.Hour, "type"},
		"days=45":                 {45, 75 * 24 * time.Hour, "type"},
		"days=9999":               {30, 60 * 24 * time.Hour, "type"},
	} {
		v, _ := url.ParseQuery(q)
		o := parseStatsOpts(v)
		if o.days() != want.days || o.window() != want.window || o.Group != want.group {
			t.Errorf("%q: days %d window %v group %q, want %+v", q, o.days(), o.window(), o.Group, want)
		}
	}
	o := parseStatsOpts(url.Values{"range": {"7d"}, "hide": {"none,tweak"}, "p": {"90"}, "drill": {"status:testing"}})
	if !o.hidden("none") || o.hidden("feature") || o.P != "90" {
		t.Errorf("opts = %+v", o)
	}
	if got := o.query(false); got != "group=type&hide=none%2Ctweak&p=90&range=7d" {
		t.Errorf("query = %q", got)
	}
	if got := o.query(true); !strings.Contains(got, "drill=status%3Atesting") {
		t.Errorf("query with drill = %q", got)
	}
}
