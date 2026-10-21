package main

import (
	"os/exec"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The tmux mechanics live in internal/agentrun (shared with issue-cli's
// `lead spawn`). These are the server's seams over them: tests stub
// listTmuxSessions and tmuxSendKeys here, and agentrun.HasSession,
// agentrun.SessionAttached and agentrun.CapturePane there.
var listTmuxSessions = func() []AgentSession { return agentrun.ListSessions() }
var tmuxSendKeys = func(target string, lines []string) error { return agentrun.SendKeys(target, lines) }

func tmuxSessionName(slug string) string {
	return tracker.AgentSessionName(slug)
}

func sharedTmuxSession(proj *tracker.Project) string { return agentrun.SharedSession(proj) }

func agentTmuxTarget(proj *tracker.Project, name string) string { return agentrun.Target(proj, name) }

// agentDisplayName is the human-facing name of the agent's tmux location,
// e.g. "agent-foo" or "work:agent-foo" in shared mode.
func agentDisplayName(proj *tracker.Project, name string) string {
	return tracker.AgentDisplayName(proj, name)
}

func agentAttachCmd(proj *tracker.Project, name string) string { return agentrun.AttachCmd(proj, name) }

func createAgentTmux(proj *tracker.Project, name string, workDir string) (string, *exec.Cmd) {
	return agentrun.CreateTmux(proj, name, workDir)
}

func killAgentTmux(proj *tracker.Project, name string) error { return agentrun.KillTmux(proj, name) }

func listSharedAgentWindows() []AgentSession { return agentrun.ListSharedWindows() }

func parseSharedAgentWindows(out string) []AgentSession { return agentrun.ParseSharedWindows(out) }

// projectSessions lists the live tmux sessions and shared-session windows
// that belong to proj, never another project's agents on the same server.
func projectSessions(proj *tracker.Project) []AgentSession {
	return agentrun.ForProject(proj, projectRoot(proj), listTmuxSessions())
}

func sessionsByIssueSlug(proj *tracker.Project, issues []*tracker.Issue) (map[string][]AgentSession, int) {
	sessions := projectSessions(proj)
	result := make(map[string][]AgentSession, len(issues))
	matchedSessionNames := map[string]bool{}
	for _, issue := range issues {
		for _, session := range sessions {
			if sessionMatchesIssue(session.Name, issue.Slug) {
				result[issue.Slug] = append(result[issue.Slug], session)
				matchedSessionNames[session.Name] = true
			}
		}
	}
	return result, len(matchedSessionNames)
}

// sessionMatchesIssue reports whether a listed session is a worker on the
// issue. Leads (lead-*) are never workers: a sub-lead's lead-<epic> would
// otherwise substring-match its own epic.
func sessionMatchesIssue(sessionName string, slug string) bool {
	sessionName = strings.ToLower(strings.TrimSpace(sessionName))
	slug = strings.ToLower(strings.TrimSpace(slug))
	if sessionName == "" || slug == "" || strings.HasPrefix(agentrun.BareName(sessionName), "lead-") {
		return false
	}

	candidates := []string{
		slug,
		strings.ReplaceAll(slug, "/", "-"),
		tmuxSessionName(slug),
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(sessionName, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func promptDeliveryStep(steps *[]DispatchStep, target, prompt, repromptURL string) {
	agentrun.PromptDeliveryStep(steps, target, prompt, repromptURL)
}
