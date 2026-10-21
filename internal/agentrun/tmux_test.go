package agentrun

import (
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
