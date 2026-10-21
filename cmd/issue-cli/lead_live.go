package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// sessionLive reports whether proj's agent or lead session called name is
// running. A var so tests never consult the real tmux server.
var sessionLive = func(proj *tracker.Project) func(string) bool {
	return func(name string) bool { return agentrun.IsLive(proj, name) }
}

// liveSessionNames lists the bare names (agent-x, lead-y) of proj's live tmux
// sessions and shared-session windows; other projects' agents on the same
// tmux server are left out. A var for tests.
var liveSessionNames = func(proj *tracker.Project) map[string]bool {
	names := map[string]bool{}
	for _, s := range agentrun.ForProject(proj, projectRoot(proj), agentrun.ListSessions()) {
		names[agentrun.BareName(s.Name)] = true
	}
	return names
}

// startAgent launches an agent through the shared launcher. A var so tests
// can capture the spec instead of starting tmux.
var startAgent = agentrun.Start

// pasteIntoSession delivers text into a live session. A var for tests.
var pasteIntoSession = agentrun.Paste

// currentTmuxNames returns the tmux session and window this process runs in,
// or "", "" outside tmux. A var for tests.
var currentTmuxNames = func() (session, window string) {
	if os.Getenv("TMUX") == "" {
		return "", ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#S\t#W").Output()
	if err != nil {
		return "", ""
	}
	session, window, _ = strings.Cut(strings.TrimSpace(string(out)), "\t")
	return session, window
}

// commitsBehind counts the commits on the main branch that base lacks. A var
// for tests.
var commitsBehind = agentrun.MainAhead

// workingDir is os.Getwd, a var so tests don't depend on where they run.
var workingDir = os.Getwd

// Identity is who the current session is in the lead hierarchy.
type Identity struct {
	Role tracker.LeadRole
	// Name is the session name (lead-x, agent-y); "" when unmanaged or
	// unknown.
	Name string
	// Scope is "project" or the epic slug for leads.
	Scope string
	// Slug is the worker's issue ("" when unknown).
	Slug string
	// Signal says how the role was detected.
	Signal string
}

// whoAmI detects the current session's role, in order: the lead scope a
// spawned lead exports, the issue slug a dispatched worker exports, the tmux
// session or window name, and a cwd under .worktrees/. Anything else is
// unmanaged — a human, or a lead started by hand.
func whoAmI(proj *tracker.Project, issues []*tracker.Issue) Identity {
	if scope := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_LEAD_SCOPE")); scope != "" {
		return leadIdentity(proj, scope, "ISSUE_VIEWER_LEAD_SCOPE")
	}
	if slug := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_ISSUE_SLUG")); slug != "" {
		return Identity{Role: tracker.RoleWorker, Name: tracker.AgentSessionName(slug), Slug: slug, Signal: "ISSUE_VIEWER_ISSUE_SLUG"}
	}
	session, window := currentTmuxNames()
	for _, name := range []string{session, window} {
		switch {
		case strings.HasPrefix(name, "lead-"):
			if name == tracker.LeadSessionName(proj, tracker.LeadScopeProject) {
				return leadIdentity(proj, tracker.LeadScopeProject, "tmux name "+name)
			}
			for _, issue := range issues {
				if tracker.LeadSessionName(proj, issue.Slug) == name {
					return leadIdentity(proj, issue.Slug, "tmux name "+name)
				}
			}
		case strings.HasPrefix(name, "agent-"):
			id := Identity{Role: tracker.RoleWorker, Name: name, Signal: "tmux name " + name}
			for _, issue := range issues {
				if tracker.AgentSessionName(issue.Slug) == name {
					id.Slug = issue.Slug
				}
			}
			return id
		}
	}
	if wd, err := workingDir(); err == nil && strings.Contains(filepath.ToSlash(wd), "/.worktrees/") {
		return Identity{Role: tracker.RoleWorker, Signal: "working directory under .worktrees/"}
	}
	return Identity{Role: tracker.RoleUnmanaged, Signal: "no lead or worker signal"}
}

func leadIdentity(proj *tracker.Project, scope, signal string) Identity {
	role := tracker.RoleSubLead
	if scope == tracker.LeadScopeProject {
		role = tracker.RoleProjectLead
	}
	return Identity{Role: role, Name: tracker.LeadSessionName(proj, scope), Scope: scope, Signal: signal}
}

// sender is how a message from this identity is signed.
func (id Identity) sender() string {
	if id.Name != "" {
		return id.Name
	}
	return "human"
}
