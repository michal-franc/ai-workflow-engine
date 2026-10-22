package main

import (
	"os"
	"testing"
)

// TestMain points XDG_DATA_HOME at an empty directory so no test reads this
// machine's queued report-bug files (Status page and sidebar counts).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "issue-viewer-test-xdg-")
	if err == nil {
		os.Setenv("XDG_DATA_HOME", dir)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}
