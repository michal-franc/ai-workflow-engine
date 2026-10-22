package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives the tests their own state and config homes and drops an
// inherited $ISSUE_VIEWER_CONFIG, so the config lookup never finds this
// machine's viewer.json, ~/.config projects.yaml or the viewer's config.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "issue-cli-test-xdg-")
	if err == nil {
		os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
		os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	}
	os.Unsetenv("ISSUE_VIEWER_CONFIG")
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}
