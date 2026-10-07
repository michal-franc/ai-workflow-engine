package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// terminalEnv is what detectTerminal looks at. Tests swap it for a fake host.
type terminalEnv struct {
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
}

func hostTerminalEnv() terminalEnv {
	return terminalEnv{goos: runtime.GOOS, getenv: os.Getenv, lookPath: exec.LookPath}
}

func (e terminalEnv) has(bin string) bool {
	_, err := e.lookPath(bin)
	return err == nil
}

// linuxTerminals are tried in order when a display is available. Each command
// is run with `sh -c`, detached so the dispatch request doesn't wait for the
// window to close.
var linuxTerminals = []struct{ bin, cmd string }{
	{"x-terminal-emulator", "x-terminal-emulator -e tmux attach -t {{session}}"}, // Debian/Ubuntu: the user's default
	{"gnome-terminal", "gnome-terminal -- tmux attach -t {{session}}"},
	{"konsole", "konsole -e tmux attach -t {{session}}"},
	{"xfce4-terminal", "xfce4-terminal -x tmux attach -t {{session}}"},
	{"kitty", "kitty tmux attach -t {{session}}"},
	{"wezterm", "wezterm start -- tmux attach -t {{session}}"},
	{"alacritty", "alacritty -e tmux attach -t {{session}}"},
	{"foot", "foot tmux attach -t {{session}}"},
	{"xterm", "xterm -e tmux attach -t {{session}}"},
}

func detached(cmd string) string {
	return "nohup " + cmd + " >/dev/null 2>&1 &"
}

// detectTerminal picks the terminal for a project that leaves `terminal`
// unset. It returns the value to use, in the same form as the projects.yaml
// key ("" keeps the i3 + alacritty path, "none" is headless), and a label
// saying what was detected.
func detectTerminal(env terminalEnv) (terminal, label string) {
	// Keep the original default for setups where it works.
	if env.has("i3-msg") && env.has("alacritty") {
		return "", "i3 + alacritty"
	}
	if env.goos == "darwin" {
		if env.getenv("TERM_PROGRAM") == "iTerm.app" {
			return `osascript -e 'tell app "iTerm2" to create window with default profile command "tmux attach -t {{session}}"'`, "iTerm2"
		}
		return `osascript -e 'tell app "Terminal" to do script "tmux attach -t {{session}}"'`, "Terminal.app"
	}
	if env.getenv("DISPLAY") == "" && env.getenv("WAYLAND_DISPLAY") == "" {
		return "none", "no display"
	}
	for _, t := range linuxTerminals {
		if env.has(t.bin) {
			return detached(t.cmd), t.bin
		}
	}
	return "none", "no known terminal on PATH"
}

// resolveTerminals fills in `terminal` for every project that leaves it unset
// and returns one line per project saying what was picked.
func resolveTerminals(projects []tracker.Project, env terminalEnv) []string {
	var lines []string
	for i := range projects {
		p := &projects[i]
		if p.Terminal != "" {
			continue
		}
		terminal, label := detectTerminal(env)
		p.Terminal = terminal
		if terminal == "none" {
			lines = append(lines, fmt.Sprintf("  %s: terminal not set, %s: agents run headless; the dispatch dialog shows the tmux attach command", p.Name, label))
		} else {
			lines = append(lines, fmt.Sprintf("  %s: terminal not set, using %s (set terminal: in projects.yaml to choose)", p.Name, label))
		}
	}
	return lines
}
