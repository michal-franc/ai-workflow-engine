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
	// Lead is the session the agent reports to.
	Lead string `json:"lead,omitempty"`
}

// BuildDispatchPrompt composes the prompt a dispatch would send right now for
// issue, without creating the worktree or tmux session. workDir is the
// project's working directory the worktree path is resolved against. live
// reports whether a lead session is running (nil: none is); the prompt says
// whether the project lead is live. The server and issue-cli both call this so their
// output is byte-identical.
func BuildDispatchPrompt(proj *Project, issue *Issue, wf *WorkflowConfig, workDir string, live func(name string) bool) DispatchPrompt {
	var issues []*Issue
	if proj != nil && proj.IssueDir != "" {
		issues, _ = LoadIssues(proj.IssueDir)
	}
	wt := IssueWorktree(workDir, issue, wf)
	route := LeadFor(proj, issue, issues, live)
	prompt := BuildAgentPrompt(proj, issue, wf, wt.Path, wt.Branch)
	prompt += InjectProject(proj, LeadPromptSection(issue, route))
	prompt += InjectProject(proj, CIPromptSection(workDir, wf))
	return DispatchPrompt{
		Prompt:   prompt,
		Slug:     issue.Slug,
		Status:   issue.Status,
		Worktree: wt.Path,
		Branch:   wt.Branch,
		Session:  AgentDisplayName(proj, AgentSessionName(issue.Slug)),
		Lead:     route.Lead,
	}
}

// CIPromptSection is the "## Local CI" part of a worker's briefing: where
// the integration branch stands now, and that CI runs after the merge
// without the agent. Empty without a ci: block.
func CIPromptSection(workDir string, wf *WorkflowConfig) string {
	if CIStatusLine == nil {
		return ""
	}
	line := CIStatusLine(workDir, wf)
	if line == "" {
		return ""
	}
	return "\n\n## Local CI\n\n" + line + "\n" +
		"- When `make validate` or a test fails on your branch, compare with this line (`issue-cli ci status`): a failure main already has is not yours to fix.\n" +
		"- After you fast-forward main, local CI tests it by itself. Never wait for it: close your issue and stop. If it goes red on your merge, the lead may re-open your issue.\n"
}

// LeadPromptSection is the "## Your lead" part of a worker's briefing. It
// names the project lead the worker reports to and how that lead sees its
// work.
func LeadPromptSection(issue *Issue, route LeadRoute) string {
	state := "not running"
	if route.Live {
		state = "live"
	}
	var b strings.Builder
	b.WriteString("\n\n## Your lead\n\n")
	fmt.Fprintf(&b, "You report to the project lead %s (%s).", route.Lead, state)
	if route.Epic != "" {
		fmt.Fprintf(&b, " Your issue is part of epic %s.", route.Epic)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Your lead follows your issue trail: ask with `issue-cli comment %s \"question: ...\"` and report with `progress:` comments. It relays questions and approvals to the human.\n", issue.Slug)
	fmt.Fprintf(&b, "- Don't message %s or any other agent (SendMessage) unless the human, in your chat, gives you permission or asks you to. Ask the human in your chat, or post a question: comment. Your address is %s, in case the lead messages you.\n", route.Lead, AgentSessionName(issue.Slug))
	b.WriteString("- Only the human decides; a worker never ticks a gate.\n")
	b.WriteString("- `issue-cli lead whoami` shows your lead and whether it is live right now.\n")
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

// BuildAgentPrompt fills AgentDispatchPromptTemplate's placeholders for issue, injects
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

	prompt := renderPromptTemplate(AgentDispatchPromptTemplate, agentPromptFields(issue, currentPrompt, statusReminder, statusField))

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
- Leave the worktree in place after the issue ships: do not remove it, and nobody has to clean it up.
`, worktreePath, worktreeBranch, worktreeBranch)
	}
	return prompt
}

// agentPromptFields is the value of each {{name}} placeholder in
// AgentDispatchPromptTemplate.
func agentPromptFields(issue *Issue, statusGuidance, statusReminder, statusField string) map[string]string {
	return map[string]string{
		"slug":            issue.Slug,
		"system":          issue.System,
		"status_guidance": statusGuidance,
		"status_reminder": statusReminder,
		"title":           issue.Title,
		"status":          statusField,
		"priority":        issue.Priority,
		"body":            issue.BodyRaw,
	}
}

// renderPromptTemplate replaces each {{name}} in tmpl with fields[name]. The
// replacement is one pass over tmpl, so a value that itself contains {{...}}
// (an issue body, a status prompt) is left as written.
func renderPromptTemplate(tmpl string, fields map[string]string) string {
	pairs := make([]string, 0, 2*len(fields))
	for name, value := range fields {
		pairs = append(pairs, "{{"+name+"}}", value)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
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
