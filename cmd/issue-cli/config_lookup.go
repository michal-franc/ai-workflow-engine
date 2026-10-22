package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/viewerstate"
)

// errNoConfig is what resolveFromConfig returns when the lookup found no
// config; runTraced swaps it for configSource.notFound, which says where it
// looked.
var errNoConfig = errors.New("no projects config found")

// configSource is the projects config issue-cli uses: its path ("" when none
// was found), why that one, and what each step of the lookup saw.
type configSource struct {
	Path    string
	Why     string
	Checked []string
}

// findConfig picks the projects config. Order:
//  1. --config (flagValue)
//  2. $ISSUE_VIEWER_CONFIG
//  3. ./projects.yaml, only if it exists
//  4. the config of the running viewer (viewer.json), if that config exists
//  5. ~/.config/ai-workflow-engine/projects.yaml, if it exists
//
// Steps 1 and 2 are taken even when the file is missing: the user named it,
// so loading it fails loud instead of falling through to another config.
func findConfig(flagValue string) configSource {
	var src configSource
	check := func(line string) { src.Checked = append(src.Checked, line) }

	if flagValue != "" {
		return configSource{Path: flagValue, Why: "--config"}
	}
	check("--config: not passed")

	if env := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_CONFIG")); env != "" {
		src.Path, src.Why = env, "$ISSUE_VIEWER_CONFIG"
		return src
	}
	check("$ISSUE_VIEWER_CONFIG: not set")

	if isRegularFile("projects.yaml") {
		src.Path, src.Why = "projects.yaml", "./projects.yaml in the current folder"
		return src
	}
	check("./projects.yaml: not found")

	if path := viewerstate.Path(); path != "" {
		state, err := viewerstate.Read()
		switch {
		case errors.Is(err, os.ErrNotExist):
			check(path + ": not found (issue-viewer --config writes it at start)")
		case err != nil:
			check(fmt.Sprintf("%s: unreadable: %v", path, err))
		case state.Config == "":
			check(path + ": names no config")
		case !isRegularFile(state.Config):
			check(fmt.Sprintf("%s: config %s no longer exists", path, state.Config))
		default:
			src.Path = state.Config
			src.Why = fmt.Sprintf("the running viewer's, %s pid %d, recorded in %s", state.URL, state.PID, path)
			return src
		}
	}

	if path := userConfigPath(); path != "" {
		if isRegularFile(path) {
			src.Path, src.Why = path, "the user config "+path
			return src
		}
		check(path + ": not found")
	}
	return src
}

// userConfigPath is $XDG_CONFIG_HOME/ai-workflow-engine/projects.yaml,
// default ~/.config/ai-workflow-engine/projects.yaml.
func userConfigPath() string {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ai-workflow-engine", "projects.yaml")
}

// notFound is the error for a lookup that found nothing: every place checked
// and the one-line fix.
func (c configSource) notFound() error {
	var b strings.Builder
	for i, line := range c.Checked {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, line)
	}
	return fmt.Errorf("%w. Checked, in order:\n%s\nFix: export ISSUE_VIEWER_CONFIG=/path/to/projects.yaml, or start the viewer (issue-viewer --config /path/to/projects.yaml)", errNoConfig, b.String())
}

// describe is the one line `projects` and --debug print.
func (c configSource) describe() string {
	if c.Path == "" {
		return "config: none found"
	}
	return fmt.Sprintf("config: %s (%s)", c.Path, c.Why)
}
