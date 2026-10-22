// Package viewerstate records the running viewer for issue-cli: which config
// it serves, its URL, pid and start time. issue-viewer --config writes it at
// start; issue-cli reads it to find the config from any folder without
// --config or $ISSUE_VIEWER_CONFIG.
package viewerstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State is the content of viewer.json.
type State struct {
	Config  string    `json:"config"` // absolute path to projects.yaml
	URL     string    `json:"url"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
}

// Dir is $XDG_STATE_HOME/ai-workflow-engine, default
// ~/.local/state/ai-workflow-engine. Returns "" without a home directory.
func Dir() string {
	base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "ai-workflow-engine")
}

// Path is the viewer.json file, "" when Dir is.
func Path() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "viewer.json")
}

// Write replaces viewer.json with s. The temp-file rename keeps a reader from
// seeing half a file.
func Write(s State) error {
	path := Path()
	if path == "" {
		return errors.New("no home directory for the viewer state")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "viewer-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Read loads viewer.json. A missing file is an error wrapping os.ErrNotExist.
func Read() (State, error) {
	var s State
	path := Path()
	if path == "" {
		return s, errors.New("no home directory for the viewer state")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}
