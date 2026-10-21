package main

import (
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestSessionMatchesIssue(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		slug        string
		want        bool
	}{
		{name: "normalized dispatch session", sessionName: "agent-api-integrate-with-claude-session-names-to-show-active-agent-work", slug: "api/integrate-with-claude-session-names-to-show-active-agent-work", want: true},
		{name: "plain slug fragment", sessionName: "claude-watch-bug-in-login", slug: "bug-in-login", want: true},
		{name: "different issue", sessionName: "agent-something-else", slug: "bug-in-login", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionMatchesIssue(tt.sessionName, tt.slug); got != tt.want {
				t.Fatalf("sessionMatchesIssue(%q, %q) = %v, want %v", tt.sessionName, tt.slug, got, tt.want)
			}
		})
	}
}

func TestAgentTmuxTarget(t *testing.T) {
	if got := agentTmuxTarget(&tracker.Project{}, "agent-foo"); got != "agent-foo" {
		t.Fatalf("per-session target = %q, want agent-foo", got)
	}
	if got := agentTmuxTarget(nil, "agent-foo"); got != "agent-foo" {
		t.Fatalf("nil project target = %q, want agent-foo", got)
	}
	shared := &tracker.Project{TmuxSession: "work"}
	if got := agentTmuxTarget(shared, "agent-foo"); got != "=work:=agent-foo" {
		t.Fatalf("shared target = %q, want exact-match =work:=agent-foo", got)
	}
	if got := agentDisplayName(shared, "agent-foo"); got != "work:agent-foo" {
		t.Fatalf("shared display = %q, want work:agent-foo", got)
	}
}

func TestParseSharedAgentWindows(t *testing.T) {
	out := "work\tagent-bug-in-login\n" +
		"work\tzsh\n" + // not an agent window
		"agent-other\tother\n" + // per-agent session, already listed by tmux ls
		"agent-other\tagent-x\n" +
		"\n"
	got := parseSharedAgentWindows(out)
	if len(got) != 1 || got[0].Name != "work:agent-bug-in-login" {
		t.Fatalf("parseSharedAgentWindows = %+v, want only work:agent-bug-in-login", got)
	}
	if !sessionMatchesIssue(got[0].Name, "bug-in-login") {
		t.Fatalf("shared window %q should match its issue", got[0].Name)
	}
}

func TestParseSharedAgentWindows_KeepsLeadWindows(t *testing.T) {
	out := "work\tlead-issue-viewer\nwork\tlead-ui-epic\nwork\tagent-ui-child\nwork\tzsh\nlead-x\tlead-x\n"
	got := parseSharedAgentWindows(out)
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "work:lead-issue-viewer,work:lead-ui-epic,work:agent-ui-child" {
		t.Fatalf("parseSharedAgentWindows = %v", names)
	}
}

func TestSessionMatchesIssue_LeadIsNotAWorker(t *testing.T) {
	for _, name := range []string{"lead-ui-epic", "work:lead-ui-epic"} {
		if sessionMatchesIssue(name, "ui/epic") {
			t.Errorf("%q counted as a worker on its epic", name)
		}
	}
	if !sessionMatchesIssue("work:agent-ui-epic", "ui/epic") {
		t.Errorf("the epic's own worker must still match")
	}
}
