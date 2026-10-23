package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// fakeAgentScript stands in for claude and codex: it records its arguments
// (NUL-separated), working directory and environment under $FAKE_AGENT_OUT,
// then prints its last argument, as the agent renders the prompt it was
// launched with.
const fakeAgentScript = `#!/bin/sh
printf '%s\0' "$@" > "$FAKE_AGENT_OUT.args"
pwd -P > "$FAKE_AGENT_OUT.pwd"
env > "$FAKE_AGENT_OUT.env"
for last; do :; done
printf '%s\n' "$last"
`

// installFakeAgents puts fake claude and codex binaries first on PATH and
// returns the prefix they write their records to. HOME points away from the
// developer's shell profile, so a login shell can't put the real agents back
// in front (isolateTmux sets SHELL).
func installFakeAgents(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fakeAgentScript), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	out := filepath.Join(t.TempDir(), "agent")
	t.Setenv("FAKE_AGENT_OUT", out)
	return out
}

// waitForFile returns the contents of path once the fake agent has written it.
func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent never started: %s was not written", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Keys typed into the attached client while a dispatch runs must not reach the
// agent's start command: the agent is the window's own command, and dispatch
// never selects its window. The project's terminal command types into the
// session the way the owner would while the terminal opens.
func TestStartAgentSession_TypingDuringDispatchCannotChangeTheStartCommand(t *testing.T) {
	const typing = "tmux send-keys -t {{session}} junk-typed-"
	cases := []struct {
		name     string
		agent    string
		terminal string
		shared   string
	}{
		{name: "per-session claude, terminal opens", agent: "claude", terminal: typing},
		{name: "per-session codex, terminal opens", agent: "codex", terminal: typing},
		{name: "shared session claude, terminal opens on the shared session", agent: "claude", terminal: typing, shared: "work"},
		{name: "shared session codex, terminal opens on the shared session", agent: "codex", terminal: typing, shared: "work"},
		{name: "terminal none, per-session", agent: "claude", terminal: "none"},
		{name: "terminal none, shared session", agent: "codex", terminal: "none", shared: "work"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := installFakeAgents(t)
			isolateTmux(t)
			withPromptDeliveryTimeout(t, 5*time.Second)
			original := agentrun.SessionAttached
			agentrun.SessionAttached = func(string) bool { return false }
			t.Cleanup(func() { agentrun.SessionAttached = original })

			workDir, _ := filepath.EvalSymlinks(t.TempDir())
			proj := &tracker.Project{Slug: "test", Terminal: tc.terminal, TmuxSession: tc.shared, WorkDir: workDir}
			if tc.shared != "" {
				if out, err := exec.Command("tmux", "new-session", "-d", "-s", tc.shared, "-n", "lead-test").CombinedOutput(); err != nil {
					t.Fatalf("new-session: %v %s", err, out)
				}
			}

			session := "agent-x"
			prompt := "You have been assigned this issue: x\n\nit's \"quoted\" and costs $HOME\nlast line of the briefing\n"
			resp := startAgentSession(proj, session, prompt, "x", tc.agent, "http://viewer.test", nil)
			if resp.Status != "dispatched" {
				t.Fatalf("Status = %q; steps=%+v", resp.Status, resp.Steps)
			}

			args := strings.Split(strings.TrimSuffix(waitForFile(t, out+".args"), "\x00"), "\x00")
			if got, want := args[len(args)-1], strings.TrimRight(prompt, "\n"); got != want {
				t.Errorf("prompt argument = %q, want %q (all args %q)", got, want, args)
			}
			if tc.agent == "claude" && (len(args) < 2 || args[0] != "-n" || args[1] != session) {
				t.Errorf("claude args = %q, want -n %s first", args, session)
			}
			if got := strings.TrimSpace(waitForFile(t, out+".pwd")); got != workDir {
				t.Errorf("agent ran in %q, want %q", got, workDir)
			}
			env := waitForFile(t, out+".env")
			logDir := filepath.Join(workDir, ".agent-logs", session)
			for _, want := range []string{
				"ISSUE_CLI_LOG=" + filepath.Join(logDir, session+".clilog"),
				"ISSUE_VIEWER_ISSUE_SLUG=x",
				"ISSUE_VIEWER_URL=http://viewer.test",
				"ISSUE_VIEWER_PROJECT=test",
				"ISSUE_VIEWER_SERVER_PWD=",
			} {
				if !strings.Contains(env, "\n"+want) && !strings.HasPrefix(env, want) {
					t.Errorf("agent env is missing %q", want)
				}
			}

			for _, step := range resp.Steps {
				if strings.HasPrefix(step.Name, "Select window") {
					t.Errorf("dispatch selected the agent's window: %+v", step)
				}
				if step.Status == "error" {
					t.Errorf("step failed: %+v", step)
				}
			}
			if tc.shared != "" {
				if got := activeWindow(t, tc.shared); got != "lead-test" {
					t.Errorf("active window = %q, want lead-test to stay selected", got)
				}
			}
			if _, err := os.Stat(filepath.Join(logDir, "rawlog")); !os.IsNotExist(err) {
				t.Errorf("dispatch still writes a rawlog (stat err %v)", err)
			}
			// When the agent exits the window keeps a shell, as it always has.
			if !agentrun.IsLive(proj, session) {
				t.Errorf("the agent's window closed after the agent exited")
			}
		})
	}
}

// Ctrl-C on an agent that takes the signal (one still starting, or hung)
// leaves a shell in the window, as when the agent was typed into one. bash,
// like zsh, abandons the rest of a -c command after a child dies of SIGINT;
// dash doesn't, so the test runs under bash.
func TestStartAgentSession_CtrlCOnTheAgentKeepsTheWindow(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	out := installFakeAgents(t)
	isolateTmux(t)
	t.Setenv("SHELL", bash)
	withPromptDeliveryTimeout(t, time.Second)
	fake, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho started > \"$FAKE_AGENT_OUT.args\"\nexec sleep 60\n"
	if err := os.WriteFile(fake, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	proj := &tracker.Project{Slug: "test", Terminal: "none", TmuxSession: "work", WorkDir: t.TempDir()}
	if resp := startAgentSession(proj, "agent-x", "prompt\n", "x", "claude", "", nil); resp.Status != "dispatched" {
		t.Fatalf("Status = %q; steps=%+v", resp.Status, resp.Steps)
	}
	waitForFile(t, out+".args")
	target := agentrun.Target(proj, "agent-x")
	if err := exec.Command("tmux", "send-keys", "-t", target, "C-c").Run(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		cmd, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_current_command}").Output()
		if err != nil {
			t.Fatalf("the window closed after Ctrl-C: %v", err)
		}
		if strings.TrimSpace(string(cmd)) == "bash" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane still runs %q after Ctrl-C, want the shell back", cmd)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
