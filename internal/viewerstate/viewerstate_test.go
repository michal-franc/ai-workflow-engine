package viewerstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRead(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := Read(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("before Write: want ErrNotExist, got %v", err)
	}
	want := State{Config: "/w/projects.yaml", URL: "http://127.0.0.1:8080", PID: 7, Started: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)}
	if err := Write(want); err != nil {
		t.Fatal(err)
	}
	got, err := Read()
	if err != nil || got != want {
		t.Fatalf("Read = %+v, %v; want %+v", got, err, want)
	}
	entries, _ := os.ReadDir(Dir())
	if len(entries) != 1 {
		t.Fatalf("Write left temp files behind: %v", entries)
	}
}

func TestPathDefaultsUnderHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/h")
	if got, want := Path(), filepath.Join("/h", ".local", "state", "ai-workflow-engine", "viewer.json"); got != want {
		t.Fatalf("Path = %s, want %s", got, want)
	}
}
