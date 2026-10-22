package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lookupEnv gives a test its own state and config homes and no
// $ISSUE_VIEWER_CONFIG, so the lookup never reaches the real viewer.json or
// ~/.config. Returns (stateHome, configHome).
func lookupEnv(t *testing.T) (string, string) {
	t.Helper()
	state, conf := t.TempDir(), t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CONFIG_HOME", conf)
	t.Setenv("ISSUE_VIEWER_CONFIG", "")
	return state, conf
}

// writeViewerJSON records cfg as the running viewer's config, the way
// issue-viewer --config does at start.
func writeViewerJSON(t *testing.T, stateHome, cfg string) string {
	t.Helper()
	dir := filepath.Join(stateHome, "ai-workflow-engine")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"config": cfg, "url": "http://127.0.0.1:8080", "pid": 4242, "started": "2026-10-22T10:00:00Z"})
	path := filepath.Join(dir, "viewer.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The bug: from a folder with no projects.yaml and no env var, issue-cli
// could not find any config even though the viewer was running with one.
func TestRun_NoFlagNoEnvUsesRunningViewersConfig(t *testing.T) {
	state, _ := lookupEnv(t)
	cfgDir := t.TempDir()
	cfg := writeProjectsConfig(t, cfgDir, "engine")
	writeViewerJSON(t, state, cfg)
	withCwd(t, t.TempDir())

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--project", "engine", "projects"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("projects: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "engine (active)") {
		t.Fatalf("want the viewer's config projects listed, got:\n%s", out)
	}
	if !strings.Contains(out, "config: "+cfg) || !strings.Contains(out, "viewer.json") {
		t.Fatalf("projects should name the config and why it was chosen, got:\n%s", out)
	}
}

func TestFindConfig_Order(t *testing.T) {
	state, conf := lookupEnv(t)
	cwd := t.TempDir()
	withCwd(t, cwd)

	flagCfg := writeProjectsConfig(t, t.TempDir(), "flag")
	envCfg := writeProjectsConfig(t, t.TempDir(), "env")
	viewerCfg := writeProjectsConfig(t, t.TempDir(), "viewer")
	userDir := filepath.Join(conf, "ai-workflow-engine")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatal(err)
	}
	userCfg := writeProjectsConfig(t, userDir, "user")

	// 5. Nothing else: the user config.
	if got := findConfig(""); got.Path != userCfg || !strings.Contains(got.Why, "user config") {
		t.Fatalf("step 5: got %+v, want %s", got, userCfg)
	}
	// 4. The running viewer's config beats the user config.
	writeViewerJSON(t, state, viewerCfg)
	if got := findConfig(""); got.Path != viewerCfg || !strings.Contains(got.Why, "viewer.json") {
		t.Fatalf("step 4: got %+v, want %s", got, viewerCfg)
	}
	// 3. ./projects.yaml, once it exists, beats the viewer.
	localCfg := writeProjectsConfig(t, cwd, "local")
	if got := findConfig(""); got.Path != "projects.yaml" || !strings.Contains(got.Why, "current folder") {
		t.Fatalf("step 3: got %+v, want %s", got, localCfg)
	}
	// 2. $ISSUE_VIEWER_CONFIG beats ./projects.yaml.
	t.Setenv("ISSUE_VIEWER_CONFIG", envCfg)
	if got := findConfig(""); got.Path != envCfg || !strings.Contains(got.Why, "$ISSUE_VIEWER_CONFIG") {
		t.Fatalf("step 2: got %+v, want %s", got, envCfg)
	}
	// 1. --config beats everything.
	if got := findConfig(flagCfg); got.Path != flagCfg || !strings.Contains(got.Why, "--config") {
		t.Fatalf("step 1: got %+v, want %s", got, flagCfg)
	}
}

func TestFindConfig_SkipsViewerJSONWhoseConfigIsGone(t *testing.T) {
	state, _ := lookupEnv(t)
	withCwd(t, t.TempDir())
	writeViewerJSON(t, state, filepath.Join(t.TempDir(), "deleted.yaml"))

	got := findConfig("")
	if got.Path != "" {
		t.Fatalf("a viewer.json naming a missing config must be skipped, got %+v", got)
	}
	if msg := got.notFound().Error(); !strings.Contains(msg, "deleted.yaml") || !strings.Contains(msg, "no longer exists") {
		t.Fatalf("the error should say the recorded config is gone, got:\n%s", msg)
	}
}

func TestFindConfig_ExplicitPathsAreUsedEvenWhenMissing(t *testing.T) {
	// --config and $ISSUE_VIEWER_CONFIG are the user's choice: a missing file
	// is an error to show, not a reason to fall through to another config.
	lookupEnv(t)
	withCwd(t, t.TempDir())
	if got := findConfig("/nope/flag.yaml"); got.Path != "/nope/flag.yaml" {
		t.Fatalf("--config: got %+v", got)
	}
	t.Setenv("ISSUE_VIEWER_CONFIG", "/nope/env.yaml")
	if got := findConfig(""); got.Path != "/nope/env.yaml" {
		t.Fatalf("env: got %+v", got)
	}
}

func TestRun_NoConfigAnywhereListsWhatWasChecked(t *testing.T) {
	state, conf := lookupEnv(t)
	withCwd(t, t.TempDir())

	var stdout, stderr bytes.Buffer
	err := run([]string{"--project", "issue-viewer", "list"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("want an error when no config exists anywhere")
	}
	msg := err.Error()
	for _, want := range []string{
		"--config: not passed",
		"$ISSUE_VIEWER_CONFIG: not set",
		"./projects.yaml: not found",
		filepath.Join(state, "ai-workflow-engine", "viewer.json") + ": not found",
		filepath.Join(conf, "ai-workflow-engine", "projects.yaml") + ": not found",
		"export ISSUE_VIEWER_CONFIG=",
		"issue-viewer --config",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
}

func TestRun_ReportBugWhyNamesTheLookup(t *testing.T) {
	lookupEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withCwd(t, t.TempDir())

	var stdout, stderr bytes.Buffer
	if err := run([]string{"report-bug", "config lookup test"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("report-bug: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "no projects.yaml at projects.yaml") {
		t.Fatalf("old why still printed:\n%s", out)
	}
	for _, want := range []string{"saved locally", "$ISSUE_VIEWER_CONFIG: not set", "export ISSUE_VIEWER_CONFIG="} {
		if !strings.Contains(out, want) {
			t.Errorf("report-bug output missing %q:\n%s", want, out)
		}
	}
}

func TestRun_DebugPrintsChosenConfig(t *testing.T) {
	lookupEnv(t)
	cwd := t.TempDir()
	withCwd(t, cwd)
	writeProjectsConfig(t, cwd, "local")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--debug", "projects"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("projects: %v\nstderr: %s", err, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "debug: config: projects.yaml") || !strings.Contains(got, "current folder") {
		t.Fatalf("--debug should print the config and why on stderr, got:\n%s", got)
	}
}

func TestRun_DebugAfterCommandHintsGlobalFlag(t *testing.T) {
	lookupEnv(t)
	cwd := t.TempDir()
	withCwd(t, cwd)
	writeProjectsConfig(t, cwd, "local")

	var stdout, stderr bytes.Buffer
	err := run([]string{"list", "--debug"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "issue-cli --debug list") {
		t.Fatalf("want the global-flag hint, got %v", err)
	}
}
