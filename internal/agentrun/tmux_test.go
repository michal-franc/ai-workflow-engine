package agentrun

import (
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The attach command names a private tmux server when the board runs on one,
// so a sandbox's or playtest's command reaches its sessions, not the real
// server's.
func TestAttachCmdNamesPrivateServer(t *testing.T) {
	proj := &tracker.Project{Slug: "p"}
	t.Setenv("TMUX_TMPDIR", "")
	if got := AttachCmd(proj, "lead-p"); got != "tmux attach -t lead-p" {
		t.Errorf("default server: %q", got)
	}
	t.Setenv("TMUX_TMPDIR", "/tmp/lsbx-tmux-1000")
	if got := AttachCmd(proj, "lead-p"); got != "TMUX_TMPDIR=/tmp/lsbx-tmux-1000 tmux attach -t lead-p" {
		t.Errorf("private server: %q", got)
	}
	shared := &tracker.Project{Slug: "p", TmuxSession: "work"}
	if got := AttachCmd(shared, "agent-x"); got != `TMUX_TMPDIR=/tmp/lsbx-tmux-1000 tmux attach -t work \; select-window -t agent-x` {
		t.Errorf("private server, shared session: %q", got)
	}
	t.Setenv("TMUX_TMPDIR", "/tmp/my dir")
	if got := AttachCmd(proj, "lead-p"); got != "TMUX_TMPDIR='/tmp/my dir' tmux attach -t lead-p" {
		t.Errorf("quoted dir: %q", got)
	}
}

// The switch command is for a human already inside tmux, where $TMUX names
// the server, so it never carries TMUX_TMPDIR. A shared-session target is
// quoted: zsh expands a leading "=" to a command path.
func TestSwitchCmd(t *testing.T) {
	t.Setenv("TMUX_TMPDIR", "/tmp/lsbx-tmux-1000")
	proj := &tracker.Project{Slug: "p"}
	if got := SwitchCmd(proj, "agent-x"); got != "tmux switch-client -t agent-x" {
		t.Errorf("own session: %q", got)
	}
	shared := &tracker.Project{Slug: "p", TmuxSession: "work"}
	if got := SwitchCmd(shared, "agent-x"); got != "tmux switch-client -t '=work:=agent-x'" {
		t.Errorf("shared session: %q", got)
	}
	want := "Inside tmux:  tmux switch-client -t '=work:=agent-x'\nFrom a shell: TMUX_TMPDIR=/tmp/lsbx-tmux-1000 tmux attach -t work \\; select-window -t agent-x"
	if got := AttachHint(shared, "agent-x"); got != want {
		t.Errorf("hint:\n%s\nwant:\n%s", got, want)
	}
}

func TestTerminalNone(t *testing.T) {
	for _, c := range []struct {
		terminal string
		want     bool
	}{{"none", true}, {" none ", true}, {"", false}, {"alacritty -e tmux attach -t {{session}}", false}} {
		if got := TerminalNone(&tracker.Project{Terminal: c.terminal}); got != c.want {
			t.Errorf("TerminalNone(%q) = %v", c.terminal, got)
		}
	}
	if TerminalNone(nil) {
		t.Error("TerminalNone(nil) = true")
	}
}

// Every project's agents share one tmux server and the agent-<slug> naming,
// so listings are scoped: a shared-session project keeps its own session's
// windows, a per-agent-session project keeps standalone sessions started under
// its root.
func TestForProject(t *testing.T) {
	sessions := []Session{
		{Name: "work:agent-a"},
		{Name: "work:lead-p"},
		{Name: "other:agent-a"},
		{Name: "agent-b", Path: "/proj/.worktrees/b"},
		{Name: "agent-c", Path: "/proj"},
		{Name: "agent-d", Path: "/projector"},
		{Name: "agent-e", Path: "/elsewhere"},
		{Name: "agent-f"},
	}
	names := func(ss []Session) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.Name)
		}
		return out
	}

	shared := &tracker.Project{Slug: "p", TmuxSession: "work"}
	if got := names(ForProject(shared, "/proj", sessions)); strings.Join(got, ",") != "work:agent-a,work:lead-p" {
		t.Errorf("shared session: %v", got)
	}
	perAgent := &tracker.Project{Slug: "p"}
	if got := names(ForProject(perAgent, "/proj", sessions)); strings.Join(got, ",") != "agent-b,agent-c,agent-f" {
		t.Errorf("per-agent sessions: %v", got)
	}
}

func TestParseSessions(t *testing.T) {
	got := ParseSessions("issue-viewer\t1792610000\t/home/u/Work\nbad\n\nagent-x\t1792610100\t/p/.worktrees/x\n")
	if len(got) != 2 || got[0].Name != "issue-viewer" || got[0].Path != "/home/u/Work" || got[1].Path != "/p/.worktrees/x" || got[0].StartTime == "" {
		t.Errorf("ParseSessions = %+v", got)
	}
}

// The agent is created detached as the window's own command, with its env and
// working directory passed to tmux, never typed into a shell.
func TestCreateTmuxRunsCommandDetached(t *testing.T) {
	original := HasSession
	t.Cleanup(func() { HasSession = original })
	HasSession = func(string) bool { return true }
	env := []string{"ISSUE_CLI_LOG=/l/a.clilog", "ISSUE_VIEWER_PROJECT=p"}
	cases := []struct {
		proj *tracker.Project
		want string
	}{
		{&tracker.Project{}, "tmux new-session -d -s agent-a -c /w -e ISSUE_CLI_LOG=/l/a.clilog -e ISSUE_VIEWER_PROJECT=p sh -lic run"},
		{&tracker.Project{TmuxSession: "work"}, "tmux new-window -d -t =work: -n agent-a -c /w -e ISSUE_CLI_LOG=/l/a.clilog -e ISSUE_VIEWER_PROJECT=p sh -lic run"},
	}
	for _, c := range cases {
		_, cmd := CreateTmux(c.proj, "agent-a", "/w", env, "sh", "-lic", "run")
		if got := strings.Join(cmd.Args, " "); got != c.want {
			t.Errorf("CreateTmux = %q\n want %q", got, c.want)
		}
	}
	HasSession = func(string) bool { return false }
	_, cmd := CreateTmux(&tracker.Project{TmuxSession: "work"}, "agent-a", "/w", nil)
	if got, want := strings.Join(cmd.Args, " "), "tmux new-session -d -s work -n agent-a -c /w"; got != want {
		t.Errorf("CreateTmux without a shared session = %q, want %q", got, want)
	}
}

func TestAgentCommandRunsInLoginShellAndKeepsAShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	got := AgentCommand(`claude -n agent-a "$(cat "/p")"`)
	want := []string{"/bin/zsh", "-lic", `trap : INT; claude -n agent-a "$(cat "/p")"; exec /bin/zsh -l`}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("AgentCommand = %q, want %q", got, want)
	}
	t.Setenv("SHELL", "")
	if got := AgentCommand("x"); got[0] != "/bin/sh" {
		t.Fatalf("without $SHELL the shell is %q, want /bin/sh", got[0])
	}
}
