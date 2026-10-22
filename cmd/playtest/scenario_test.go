package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// TestScenarioEnvIsolated: a scenario process never sees the caller's tmux
// server, config, HOME or agent identity.
func TestScenarioEnvIsolated(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"TMUX=/tmp/tmux-1000/default,123,0",
		"TMUX_PANE=%3",
		"HOME=/home/someone",
		"XDG_STATE_HOME=/home/someone/.local/state",
		"ISSUE_VIEWER_CONFIG=/home/someone/Work/projects.yaml",
		"ISSUE_VIEWER_ISSUE_SLUG=real/issue",
		"CLAUDECODE=1",
		"AGENT_NAME=agent-real",
		"GIT_AUTHOR_NAME=Someone",
		"LANG=C.UTF-8",
	}
	s := &Scenario{Root: "/tmp/pt-x", Base: "http://127.0.0.1:1234", bins: &Binaries{Dir: "/tmp/bin"}}
	m := envMap(scenarioEnv(base, s))
	for _, k := range []string{"TMUX", "TMUX_PANE", "ISSUE_VIEWER_ISSUE_SLUG", "CLAUDECODE", "AGENT_NAME"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s leaked into the scenario env", k)
		}
	}
	want := map[string]string{
		"HOME":                "/tmp/pt-x/home",
		"XDG_STATE_HOME":      "/tmp/pt-x/state",
		"TMUX_TMPDIR":         "/tmp/pt-x/tmux",
		"ISSUE_VIEWER_CONFIG": "/tmp/pt-x/projects.yaml",
		"ISSUE_VIEWER_URL":    "http://127.0.0.1:1234",
		"GIT_AUTHOR_NAME":     "playtest",
		"LANG":                "C.UTF-8",
		"PATH":                "/tmp/bin:/usr/bin",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	for k, v := range m {
		if strings.Contains(v, "/home/someone") {
			t.Errorf("%s=%s points at the real home", k, v)
		}
	}
}

func TestGuardTmuxNeedsMarker(t *testing.T) {
	root := t.TempDir()
	s := &Scenario{Root: root}
	if err := os.MkdirAll(s.TmuxDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.guardTmux(); err == nil {
		t.Fatal("guardTmux allowed a dir without the marker")
	}
	if _, _, err := s.Tmux(context.Background(), "kill-server"); err == nil {
		t.Fatal("Tmux ran without the marker")
	}
	if err := os.WriteFile(filepath.Join(s.TmuxDir(), markerName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.guardTmux(); err != nil {
		t.Fatalf("guardTmux refused a marked dir: %v", err)
	}
}

var (
	testBinsOnce sync.Once
	testBins     *Binaries
	testBinsErr  error
)

// buildTestBinaries builds issue-viewer and issue-cli once per test run.
func buildTestBinaries(t *testing.T) *Binaries {
	t.Helper()
	if testing.Short() {
		t.Skip("builds binaries; skipped with -short")
	}
	testBinsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pt-bins-")
		if err != nil {
			testBinsErr = err
			return
		}
		testBins, testBinsErr = BuildBinaries(repoDir(t), dir, io.Discard)
	})
	if testBinsErr != nil {
		t.Fatal(testBinsErr)
	}
	return testBins
}

// TestStartScenarioIsolated starts the basic scenario for real: the board
// serves the fixtures, issue-cli reads the scenario's config, tmux runs on
// the private socket, and Stop removes everything.
func TestStartScenarioIsolated(t *testing.T) {
	bins := buildTestBinaries(t)
	sc, err := StartScenario(filepath.Join(repoDir(t), "playtest", "scenarios"), "basic", bins, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			sc.Stop(false)
		}
	}()

	resp, err := http.Get(sc.Base + "/p/demo/board")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Export issues as CSV") {
		t.Fatalf("board: %d, fixture missing", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, code, err := sc.CLI(ctx, "list")
	if err != nil || code != 0 || !strings.Contains(out, "export-issues-as-csv") {
		t.Fatalf("issue-cli list: exit %d err %v\n%s", code, err, out)
	}
	cfg, _ := os.ReadFile(sc.ConfigPath())
	if strings.Contains(string(cfg), "/Work/projects.yaml") || !strings.Contains(string(cfg), sc.Proj) {
		t.Errorf("scenario config doesn't point at the scenario project:\n%s", cfg)
	}
	if !strings.Contains(string(cfg), `terminal: "none"`) {
		t.Errorf("scenario config must be headless (terminal: none), or dispatch opens a real terminal:\n%s", cfg)
	}

	if _, err := exec.LookPath("tmux"); err == nil {
		if out, code, err := sc.Tmux(ctx, "new-session", "-d", "-s", "pt-probe"); err != nil || code != 0 {
			t.Fatalf("tmux new-session: %v %d %s", err, code, out)
		}
		out, _, _ := sc.Tmux(ctx, "display-message", "-p", "-t", "pt-probe", "#{socket_path}")
		if !strings.HasPrefix(strings.TrimSpace(out), sc.TmuxDir()+"/") {
			t.Errorf("tmux socket %q is not under the scenario's %s", strings.TrimSpace(out), sc.TmuxDir())
		}
	}

	root := sc.Root
	sc.Stop(false)
	stopped = true
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("Stop left %s behind", root)
	}
}

func TestStopKeepsUnmarkedDirs(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "precious")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	(&Scenario{Root: root}).Stop(false)
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("Stop removed a dir without the scenario marker")
	}
}

func TestPinIssueTimes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	os.WriteFile(a, []byte("---\ntitle: a\ncreated: \"2026-01-05\"\n---\n"), 0o644)
	os.WriteFile(b, []byte("---\ntitle: b\n---\n"), 0o644)
	if err := pinIssueTimes(dir); err != nil {
		t.Fatal(err)
	}
	ia, _ := os.Stat(a)
	ib, _ := os.Stat(b)
	if got := ia.ModTime().UTC().Format("2006-01-02"); got != "2026-01-05" {
		t.Errorf("a mtime = %s, want its created date", got)
	}
	if got := ib.ModTime().UTC().Format("2006-01-02"); got != "2026-01-01" {
		t.Errorf("b mtime = %s, want the fixed default", got)
	}
}

// TestGenerateIssues: a generate entry writes count issues with the slug the
// viewer derives from the title, in the system folder, with its frontmatter.
func TestGenerateIssues(t *testing.T) {
	dir := t.TempDir()
	err := generateIssues(dir, []ScenarioGenerate{
		{Count: 3, Dir: "Sim", Title: "Idea %03d", Status: "idea", Created: "2026-09-01", Extra: "priority: \"low\"\n", Body: "Someday."},
		{Count: 2, Title: "Chore #%d: tidy", Status: "backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Sim", "idea-003.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`title: "Idea 003"`, `status: "idea"`, `system: "Sim"`, `created: "2026-09-01"`, `priority: "low"`, "Someday."} {
		if !strings.Contains(string(data), want) {
			t.Errorf("idea-003.md lacks %s:\n%s", want, data)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "chore-2-tidy.md")); err != nil {
		t.Errorf("no system: written at the issues root as chore-2-tidy.md: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "Sim", "*.md"))
	if len(files) != 3 {
		t.Errorf("Sim holds %d issues, want 3", len(files))
	}
}

func TestLoadScenarioSpec_RejectsBadGenerate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "scenario.yaml"), []byte("generate:\n  - count: 5\n    status: idea\n    title: No number\n"), 0o644)
	if _, err := LoadScenarioSpec(dir); err == nil || !strings.Contains(err.Error(), "generate[0]") {
		t.Errorf("err = %v, want a generate[0] error", err)
	}
}
