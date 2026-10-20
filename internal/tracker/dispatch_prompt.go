package tracker

import (
	"fmt"
	"path/filepath"
	"strings"
)

// DispatchPrompt is the briefing an agent dispatch would send for an issue,
// plus where the agent would run. It is the shape of both
// GET /p/<project>/issue/<slug>/dispatch-prompt?format=json and
// `issue-cli dispatch-prompt --json`.
type DispatchPrompt struct {
	Prompt   string `json:"prompt"`
	Slug     string `json:"slug"`
	Status   string `json:"status"`
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
	// Base is the epic base branch the worktree is created from and the
	// agent ships into ("" outside an epic with a base branch).
	Base    string `json:"base,omitempty"`
	Session string `json:"session"`
	// Lead is the session the agent reports to.
	Lead string `json:"lead,omitempty"`
}

// BuildDispatchPrompt composes the prompt a dispatch would send right now for
// issue, without creating the worktree or tmux session. workDir is the
// project's working directory the worktree path is resolved against. live
// reports whether a lead session is running (nil: none is); it decides which
// lead the prompt names. The server and issue-cli both call this so their
// output is byte-identical.
func BuildDispatchPrompt(proj *Project, issue *Issue, wf *WorkflowConfig, workDir string, live func(name string) bool) DispatchPrompt {
	var issues []*Issue
	if proj != nil && proj.IssueDir != "" {
		issues, _ = LoadIssues(proj.IssueDir)
	}
	wt := IssueWorktree(workDir, issue, issues, wf)
	route := LeadFor(proj, issue, issues, live)
	base := ChildBase(issue, issues)
	prompt := BuildAgentPrompt(proj, issue, wf, wt.Path, wt.Branch)
	prompt += InjectProject(proj, LeadPromptSection(issue, route, base))
	return DispatchPrompt{
		Prompt:   prompt,
		Slug:     issue.Slug,
		Status:   issue.Status,
		Worktree: wt.Path,
		Branch:   wt.Branch,
		Base:     base,
		Session:  AgentDisplayName(proj, AgentSessionName(issue.Slug)),
		Lead:     route.Lead,
	}
}

// LeadPromptSection is the "## Your lead" part of a worker's briefing, plus
// "## Base branch" for children of an epic with a base branch. It names the
// lead the worker reports to now and how that lead sees its work.
func LeadPromptSection(issue *Issue, route LeadRoute, base string) string {
	state := func(live bool) string {
		if live {
			return "live"
		}
		return "not running"
	}
	var b strings.Builder
	b.WriteString("\n\n## Your lead\n\n")
	switch {
	case route.Epic != "" && route.SubLeadLive:
		fmt.Fprintf(&b, "You report to %s, the sub-lead of epic %s (live).\n", route.SubLead, route.Epic)
	case route.Epic != "":
		fmt.Fprintf(&b, "Your epic %s has no live sub-lead (%s), so you report to the project lead %s (%s).\n", route.Epic, route.SubLead, route.ProjectLead, state(route.Live))
	default:
		fmt.Fprintf(&b, "You report to the project lead %s (%s).\n", route.ProjectLead, state(route.Live))
	}
	fmt.Fprintf(&b, "- Your lead follows your issue trail: ask with `issue-cli comment %s \"question: ...\"` and report with `progress:` comments. It relays questions and approvals to the human.\n", issue.Slug)
	fmt.Fprintf(&b, "- In Claude Code you can also message %s, or another agent on this project, with ListAgents / SendMessage. An agent's address is its tmux name (lead-<scope>, agent-<slug>); yours is %s.\n", route.Lead, AgentSessionName(issue.Slug))
	b.WriteString("- Only the human decides; a worker never ticks a gate.\n")
	b.WriteString("- `issue-cli lead whoami` shows your lead and whether it is live right now.\n")
	if base != "" {
		fmt.Fprintf(&b, "\n## Base branch\n\nThis issue is part of epic %s, which integrates on base branch %s.\n", route.Epic, base)
		fmt.Fprintf(&b, "- Build on %s: your branch was created from it. To catch up, merge %s (never main) into your branch.\n", base, base)
		fmt.Fprintf(&b, "- Ship into %s (open the PR against %s, not main). The sub-lead takes the base branch to main when the human decides.\n", base, base)
	}
	return b.String()
}

// AgentSessionName is the tmux session (or shared-session window) name a
// dispatched agent for slug runs in.
func AgentSessionName(slug string) string {
	r := strings.NewReplacer("/", "-", ".", "-", " ", "-")
	return "agent-" + r.Replace(slug)
}

// AgentDisplayName is the human-facing name of the agent's tmux location,
// e.g. "agent-foo" or "work:agent-foo" when the project uses a shared session.
func AgentDisplayName(proj *Project, name string) string {
	if proj != nil {
		if shared := strings.TrimSpace(proj.TmuxSession); shared != "" {
			return shared + ":" + name
		}
	}
	return name
}

// ResolveWorktree returns the worktree path, branch name, and whether the
// dispatch should create one. Pure: no side effects, safe to call before
// deciding whether to actually run git.
func ResolveWorktree(workdir, slug string, wf *WorkflowConfig) (path, branch string, enabled bool) {
	if wf == nil || !wf.WorktreeEnabled() || strings.TrimSpace(slug) == "" || strings.TrimSpace(workdir) == "" {
		return "", "", false
	}
	return filepath.Join(workdir, ".worktrees", slug), "work/" + slug, true
}

// BuildAgentPrompt formats AgentDispatchPromptTemplate for issue, injects
// --project into every issue-cli command, and appends the worktree section
// when the agent runs in one.
func BuildAgentPrompt(proj *Project, issue *Issue, wf *WorkflowConfig, worktreePath, worktreeBranch string) string {
	currentPrompt := "Use issue-cli to inspect the current workflow requirements for this status before making changes."
	if wf != nil {
		if prompt := wf.StatusPrompt(issue.Status); strings.TrimSpace(prompt) != "" {
			currentPrompt = prompt
		}
	}

	statusReminder := ""
	switch issue.Status {
	case "in design":
		statusReminder = "When the design is complete, tell the human in chat which approval it needs in the issue viewer (see the status guidance), then stop and end your turn. Continue when the human messages you that it is approved."
	case "backlog":
		statusReminder = "Tell the human in chat that this needs `in progress` approval in the issue viewer, then stop and end your turn. Continue when the human messages you that it is approved."
	}

	// Typed issues get a Type line under Status in the metadata block, so the
	// agent knows its path before reading any workflow output.
	statusField := issue.Status
	if wf != nil && wf.ActiveType != "" {
		statusField += fmt.Sprintf("\n  Type: %s (%s)", wf.ActiveType, wf.PathLine())
	}

	prompt := fmt.Sprintf(AgentDispatchPromptTemplate,
		issue.Slug,
		currentPrompt,
		statusReminder,
		issue.Slug,
		issue.Slug,
		issue.Slug,
		issue.Slug,
		issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.System, issue.System, issue.Slug,
		issue.Title, statusField, issue.Priority,
		issue.BodyRaw)

	// Inject --project so dispatched bots in a multi-project projects.yaml
	// setup don't silently run against the default project. The web app
	// already knows the project; the bot would otherwise have to discover or
	// guess it. Replace bare `issue-cli ` (trailing space) so we don't break
	// adjacent tokens, and skip when the project has no slug (bootstrap mode).
	prompt = InjectProject(proj, prompt)

	if worktreePath != "" {
		prompt += fmt.Sprintf(`

## Worktree

You are already in an isolated git worktree at %s on branch %s.
- Do your code work and make commits here on %s. Do not switch this checkout to another branch.
- Issue management is NOT code work. The issues/ tree is excluded from this worktree on purpose — it lives on the primary checkout (master). Run every issue-cli command exactly as written: --project already points it at the primary checkout, so it manages the issue on master regardless of your worktree branch. Do not cd into the primary checkout, and do not try to create or edit issue files inside this worktree.
- The human handles cleanup (running git worktree remove) after the issue is shipped — you do not need to remove the worktree yourself.
`, worktreePath, worktreeBranch, worktreeBranch)
	}
	return prompt
}

// InjectProject adds --project <slug> to every issue-cli command in text, so
// agents in a multi-project projects.yaml setup don't silently run against
// the default project. Bare `issue-cli ` (trailing space) is replaced so
// adjacent tokens aren't broken; a project without a slug (bootstrap mode) is
// left alone.
func InjectProject(proj *Project, text string) string {
	if proj == nil || proj.Slug == "" {
		return text
	}
	return strings.ReplaceAll(text, "issue-cli ", "issue-cli --project "+proj.Slug+" ")
}

// PromptDeliveryMarker is the text whose appearance in the agent's pane shows
// the prompt reached it: the start of the prompt's last non-empty line. The
// last line is used because Claude redraws the screen and a long prompt's
// first lines scroll out of capture-pane's reach, while the tail stays visible
// right after the prompt renders. Truncated so terminal wrapping can't split
// it. Returns "" for a blank prompt.
func PromptDeliveryMarker(prompt string) string {
	lines := strings.Split(prompt, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > 40 {
			line = strings.TrimSpace(string(r[:40]))
		}
		return line
	}
	return ""
}
