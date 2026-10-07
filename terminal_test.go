package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func fakeTerminalEnv(goos string, vars map[string]string, bins ...string) terminalEnv {
	onPath := map[string]bool{}
	for _, b := range bins {
		onPath[b] = true
	}
	return terminalEnv{
		goos:   goos,
		getenv: func(k string) string { return vars[k] },
		lookPath: func(b string) (string, error) {
			if onPath[b] {
				return "/usr/bin/" + b, nil
			}
			return "", errors.New("not found")
		},
	}
}

func TestDetectTerminal(t *testing.T) {
	x11 := map[string]string{"DISPLAY": ":0"}
	cases := []struct {
		name      string
		env       terminalEnv
		wantTerm  string // exact, or a prefix when prefix is set
		wantLabel string
		prefix    bool
	}{
		{"i3 and alacritty keep the original default", fakeTerminalEnv("linux", nil, "i3-msg", "alacritty"), "", "i3 + alacritty", false},
		{"i3 without alacritty is not the default", fakeTerminalEnv("linux", x11, "i3-msg", "kitty"), "nohup kitty tmux attach -t {{session}}", "kitty", true},
		{"macOS Terminal.app", fakeTerminalEnv("darwin", nil), `osascript -e 'tell app "Terminal"`, "Terminal.app", true},
		{"macOS iTerm2", fakeTerminalEnv("darwin", map[string]string{"TERM_PROGRAM": "iTerm.app"}), `osascript -e 'tell app "iTerm2"`, "iTerm2", true},
		{"no display is headless", fakeTerminalEnv("linux", nil, "gnome-terminal"), "none", "no display", false},
		{"wayland counts as a display", fakeTerminalEnv("linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, "foot"), "nohup foot tmux attach", "foot", true},
		{"the Debian default wins over specific terminals", fakeTerminalEnv("linux", x11, "xterm", "x-terminal-emulator"), "nohup x-terminal-emulator -e", "x-terminal-emulator", true},
		{"gnome-terminal uses --", fakeTerminalEnv("linux", x11, "gnome-terminal"), "nohup gnome-terminal -- tmux attach -t {{session}}", "gnome-terminal", true},
		{"display but no known terminal is headless", fakeTerminalEnv("linux", x11), "none", "no known terminal on PATH", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			term, label := detectTerminal(tc.env)
			if label != tc.wantLabel {
				t.Errorf("label = %q, want %q", label, tc.wantLabel)
			}
			if tc.prefix {
				if !strings.HasPrefix(term, tc.wantTerm) {
					t.Errorf("terminal = %q, want prefix %q", term, tc.wantTerm)
				}
			} else if term != tc.wantTerm {
				t.Errorf("terminal = %q, want %q", term, tc.wantTerm)
			}
			if term != "" && term != "none" && !strings.Contains(term, "{{session}}") {
				t.Errorf("terminal %q has no {{session}} placeholder", term)
			}
		})
	}
}

func TestDetectedLinuxTerminalsRunDetached(t *testing.T) {
	for _, lt := range linuxTerminals {
		env := fakeTerminalEnv("linux", map[string]string{"DISPLAY": ":0"}, lt.bin)
		term, _ := detectTerminal(env)
		if !strings.HasSuffix(term, ">/dev/null 2>&1 &") {
			t.Errorf("%s: %q must run in the background so dispatch doesn't wait for the window", lt.bin, term)
		}
	}
}

func TestResolveTerminalsOnlyFillsUnset(t *testing.T) {
	projects := []tracker.Project{
		{Name: "Explicit", Terminal: "my-term {{session}}"},
		{Name: "Headless", Terminal: "none"},
		{Name: "Unset"},
	}
	lines := resolveTerminals(projects, fakeTerminalEnv("linux", nil))

	if projects[0].Terminal != "my-term {{session}}" || projects[1].Terminal != "none" {
		t.Errorf("explicit values changed: %q, %q", projects[0].Terminal, projects[1].Terminal)
	}
	if projects[2].Terminal != "none" {
		t.Errorf("unset project on a host with no display: terminal = %q, want none", projects[2].Terminal)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "Unset") || !strings.Contains(lines[0], "headless") {
		t.Errorf("lines = %q, want one headless line for Unset", lines)
	}
}
