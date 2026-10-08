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
	Session  string `json:"session"`
}

// BuildDispatchPrompt composes the prompt a dispatch would send right now for
// issue, without creating the worktree or tmux session. workDir is the
// project's working directory the worktree path is resolved against. The
// server and issue-cli both call this so their output is byte-identical.
func BuildDispatchPrompt(proj *Project, issue *Issue, wf *WorkflowConfig, workDir string) DispatchPrompt {
	wtPath, wtBranch, _ := ResolveWorktree(workDir, issue.Slug, wf)
	return DispatchPrompt{
		Prompt:   BuildAgentPrompt(proj, issue, wf, wtPath, wtBranch),
		Slug:     issue.Slug,
		Status:   issue.Status,
		Worktree: wtPath,
		Branch:   wtBranch,
		Session:  AgentDisplayName(proj, AgentSessionName(issue.Slug)),
	}
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
		statusReminder = fmt.Sprintf("When the design is complete, tell the human in chat that it needs backlog approval in the issue viewer, then run `issue-cli transition %s --to \"backlog\" --wait --timeout 9m` to block until it is approved (re-run it on exit code 3).", issue.Slug)
	case "backlog":
		statusReminder = fmt.Sprintf("Tell the human in chat that this needs `in progress` approval in the issue viewer, then run `issue-cli start %s --wait --timeout 9m` to block until it is approved (re-run it on exit code 3).", issue.Slug)
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
	if proj != nil && proj.Slug != "" {
		prompt = strings.ReplaceAll(prompt, "issue-cli ", "issue-cli --project "+proj.Slug+" ")
	}

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
