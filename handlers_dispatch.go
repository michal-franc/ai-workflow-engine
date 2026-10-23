package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func buildAgentPrompt(proj *tracker.Project, issue *tracker.Issue, wf *tracker.WorkflowConfig, worktreePath, worktreeBranch string) string {
	return tracker.BuildAgentPrompt(proj, issue, wf, worktreePath, worktreeBranch)
}

// resolveWorktree returns the worktree path, branch name, and whether the
// dispatch should create one. Pure: no side effects, safe to call before
// deciding whether to actually run git.
func resolveWorktree(workdir, slug string, wf *tracker.WorkflowConfig) (path, branch string, enabled bool) {
	return tracker.ResolveWorktree(workdir, slug, wf)
}

// ensureWorktree creates the per-issue worktree (work/<slug> from HEAD) if
// needed and returns the effective working directory for the dispatched
// session. See agentrun.EnsureWorktree.
func ensureWorktree(workdir, slug string, wf *tracker.WorkflowConfig) (string, []DispatchStep, bool) {
	wtPath, branch, enabled := resolveWorktree(workdir, slug, wf)
	if !enabled {
		return workdir, nil, true
	}
	return agentrun.EnsureWorktree(workdir, tracker.WorktreeSpec{Path: wtPath, Branch: branch}, wf)
}

// issueWorktreeSpec resolves the worktree a dispatch for issueSlug runs in:
// work/<slug> from HEAD (see tracker.IssueWorktree).
func issueWorktreeSpec(workDir, issueSlug string, wf *tracker.WorkflowConfig) tracker.WorktreeSpec {
	wtPath, branch, enabled := resolveWorktree(workDir, issueSlug, wf)
	if !enabled {
		return tracker.WorktreeSpec{}
	}
	return tracker.WorktreeSpec{Path: wtPath, Branch: branch}
}

// leadLive reports whether proj's agent or lead called name is running. It
// is what dispatch prompts use to say whether the project lead is live.
func leadLive(proj *tracker.Project) func(string) bool {
	return func(name string) bool { return agentrun.IsLive(proj, name) }
}

type DispatchStep = agentrun.Step

type DispatchResponse = agentrun.Response

var dispatchAgentSession = startAgentSession

// viewerURLFromRequest reconstructs the externally-visible base URL of the
// viewer from the inbound request, so dispatched bot sessions can later emit
// deep-link approval hints that point back at the same host the human just
// clicked from. Falls back to ISSUE_VIEWER_URL on the server's environment if
// the request doesn't carry a Host header (rare, but possible behind some
// proxies).
func viewerURLFromRequest(r *http.Request) string {
	if r == nil {
		return strings.TrimSpace(os.Getenv("ISSUE_VIEWER_URL"))
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	if host == "" {
		return strings.TrimSpace(os.Getenv("ISSUE_VIEWER_URL"))
	}
	return scheme + "://" + host
}

func agentLaunchCommand(proj *tracker.Project, agentType, session, promptPath string) string {
	return agentrun.LaunchCommand(proj, agentType, session, promptPath)
}

func writeDispatchPrompt(sessionLogDir, prompt string) (string, error) {
	return agentrun.WritePrompt(sessionLogDir, prompt)
}

// repromptURL is the endpoint that re-sends the briefing into a live agent
// session, quoted in dispatch hints. Empty when session is not the issue's
// main agent session (custom actions, retros reviews), which reprompt can't
// reach.
func repromptURL(proj *tracker.Project, session, issueSlug string) string {
	if issueSlug == "" || session != tmuxSessionName(issueSlug) {
		return ""
	}
	prefix := ""
	if proj != nil && proj.Slug != "" {
		prefix = "/p/" + proj.Slug
	}
	return prefix + "/issue/" + issueSlug + "/dispatch/reprompt"
}

func runStep(steps *[]DispatchStep, name string, cmd *exec.Cmd) bool {
	return agentrun.RunStep(steps, name, cmd)
}

func openTerminalStep(proj *tracker.Project, session string, steps *[]DispatchStep) bool {
	return agentrun.OpenTerminalStep(proj, session, steps)
}

func attachAgentStep(proj *tracker.Project, name string, steps *[]DispatchStep) bool {
	return agentrun.AttachStep(proj, name, steps)
}

// startAgentSession launches (or re-attaches to) the agent called session.
// With wf and an issue slug it runs in the issue's worktree.
func startAgentSession(proj *tracker.Project, session string, prompt string, issueSlug string, agentType string, viewerURL string, wf *tracker.WorkflowConfig) DispatchResponse {
	workDir := resolveProjectWorkDir(proj)
	return agentrun.Start(proj, agentrun.Spec{
		Session:     session,
		Prompt:      prompt,
		AgentType:   agentType,
		IssueSlug:   issueSlug,
		ViewerURL:   viewerURL,
		WorkDir:     workDir,
		Worktree:    issueWorktreeSpec(workDir, issueSlug, wf),
		Workflow:    wf,
		RepromptURL: repromptURL(proj, session, issueSlug),
	})
}

func buildRetrosReviewPrompt(proj *tracker.Project, retros []*RetroEntry, bugs []*ToolBugReportView) string {
	retroLines := make([]string, 0, len(retros))
	for _, retro := range retros {
		title := retro.IssueTitle
		if strings.TrimSpace(title) == "" {
			title = retro.FileName
		}
		retroLines = append(retroLines, fmt.Sprintf("- %s | issue=%s | status=%s | review_status=%s | file=retros/%s",
			title,
			valueOrDash(retro.IssueSlug),
			valueOrDash(retro.Status),
			normalizeRetroReviewStatus(retro.ReviewStatus),
			retro.FileName))
	}
	if len(retroLines) == 0 {
		retroLines = append(retroLines, "- none")
	}

	bugLines := make([]string, 0, len(bugs))
	for _, bug := range bugs {
		bugLines = append(bugLines, fmt.Sprintf("- issue=%s | status=%s | tool=%s | file=bugs/%s | desc=%s",
			valueOrDash(bug.IssueSlug),
			normalizeBugStatus(bug.Status),
			valueOrDash(bug.Tool),
			bug.FileName,
			trimSnippet(bug.Description, 160)))
	}
	if len(bugLines) == 0 {
		bugLines = append(bugLines, "- none")
	}

	return fmt.Sprintf(`Review the project feedback files for %s.

Your job:
- scan the project retrospectives under retros/
- scan related bug reports under bugs/
- decide which reports describe real issues, workflow gaps, duplicated reports, or noise
- suggest concrete fixes, workflow changes, or code changes
- identify items that should become actual tracked issues
- if you are confident a retrospective has been reviewed, mark that file with ReviewStatus: processed
- if you are confident a bug report is resolved or should not be acted on, update its JSON status to fixed or wontfix
- leave uncertain items open

Do not mention issue-cli in your writeup. Focus on the files, the underlying problems, and ideas to fix them.

Files to review:
Retrospectives:
%s

Bug reports:
%s

Expected output:
1. A short triage summary of the real issues and duplicates.
2. Concrete suggestions for fixes or workflow changes.
3. Which files you marked processed, fixed, or wontfix.
`, proj.Name, strings.Join(retroLines, "\n"), strings.Join(bugLines, "\n"))
}

func (s *Server) handleRetrosReviewDispatch(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	retros, err := loadRetrospectives(proj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bugs, err := loadRelatedToolBugs(issues)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	agentType := "claude"
	var body struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil && strings.TrimSpace(body.Agent) != "" {
		agentType = body.Agent
	}

	prompt := buildRetrosReviewPrompt(proj, retros, bugs)
	session := tmuxSessionName(proj.Slug + "-retros-review")
	resp := dispatchAgentSession(proj, session, prompt, "", agentType, viewerURLFromRequest(r), nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// renderActionPrompt substitutes {{slug}}/{{title}}/{{status}}/{{system}}/
// {{priority}}/{{number}} into a custom action's prompt. Plain string
// replacement (not text/template) keeps prompts forgiving: an unrecognized
// {{...}} is left verbatim rather than failing the dispatch.
func renderActionPrompt(prompt string, issue *tracker.Issue) string {
	r := strings.NewReplacer(
		"{{slug}}", issue.Slug,
		"{{title}}", issue.Title,
		"{{status}}", issue.Status,
		"{{system}}", issue.System,
		"{{priority}}", issue.Priority,
		"{{number}}", fmt.Sprintf("%d", issue.Number),
	)
	return r.Replace(prompt)
}

// renderProjectActionPrompt substitutes {{project}} into a project-level custom
// action's prompt. Project actions are not bound to an issue, so only project
// context is available. Plain string replacement keeps prompts forgiving: an
// unrecognized {{...}} is left verbatim rather than failing the dispatch.
func renderProjectActionPrompt(prompt string, proj *tracker.Project) string {
	r := strings.NewReplacer(
		"{{project}}", proj.Name,
	)
	return r.Replace(prompt)
}

// handleProjectAction dispatches a project-level custom action from the list,
// board, or graph views. Unlike handleCustomAction it is not bound to an issue:
// it briefs a fresh tmux agent session in the project checkout with the action's
// prompt (templated with project context) and an empty issue slug.
func (s *Server) handleProjectAction(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	actionID := strings.TrimPrefix(r.URL.Path, prefix+"/action/")
	if actionID == "" || strings.Contains(actionID, "/") {
		http.NotFound(w, r)
		return
	}

	wf := proj.LoadWorkflow()
	action := wf.GetProjectAction(actionID)
	if action == nil {
		http.NotFound(w, r)
		return
	}

	agentType := strings.TrimSpace(action.Agent)
	if agentType == "" {
		agentType = "claude"
	}

	prompt := renderProjectActionPrompt(action.Prompt, proj)
	session := tmuxSessionName(proj.Slug + "-" + actionID)
	resp := dispatchAgentSession(proj, session, prompt, "", agentType, viewerURLFromRequest(r), nil)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleCustomAction dispatches a workflow custom action: it briefs a fresh
// tmux agent session with the action's prompt. Unlike handleDispatchAgent it
// uses the configured prompt verbatim (templated) instead of the status-based
// prompt, and passes wf=nil so the lightweight one-shot runs in the project
// checkout rather than spinning up a per-issue worktree.
func (s *Server) handleCustomAction(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	rest := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	idx := strings.LastIndex(rest, "/action/")
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	slug := rest[:idx]
	actionID := rest[idx+len("/action/"):]

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	wf := proj.LoadWorkflowForIssue(issue)
	action := wf.GetAction(actionID)
	if action == nil {
		http.NotFound(w, r)
		return
	}

	agentType := strings.TrimSpace(action.Agent)
	if agentType == "" {
		agentType = "claude"
	}

	prompt := renderActionPrompt(action.Prompt, issue)
	session := tmuxSessionName(slug + "-" + actionID)
	resp := dispatchAgentSession(proj, session, prompt, issue.Slug, agentType, viewerURLFromRequest(r), nil)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDispatchAgent(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/dispatch")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	// Parse agent type from request body (default: claude)
	agentType := "claude"
	var body struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Agent != "" {
		agentType = body.Agent
	}

	wf := proj.LoadWorkflowForIssue(issue)
	prompt := tracker.BuildDispatchPrompt(proj, issue, wf, resolveProjectWorkDir(proj), leadLive(proj)).Prompt
	session := tmuxSessionName(slug)
	resp := dispatchAgentSession(proj, session, prompt, issue.Slug, agentType, viewerURLFromRequest(r), wf)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleDispatchPrompt serves GET /p/<project>/issue/<slug>/dispatch-prompt:
// the prompt a dispatch would send right now, without creating a worktree,
// session, or file. Plain text by default; ?format=json adds where it would
// run.
func (s *Server) handleDispatchPrompt(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/dispatch-prompt")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	dp := tracker.BuildDispatchPrompt(proj, issue, proj.LoadWorkflowForIssue(issue), resolveProjectWorkDir(proj), leadLive(proj))
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dp)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, dp.Prompt)
}

// handleDispatchReprompt serves POST /p/<project>/issue/<slug>/dispatch/reprompt:
// re-sends the current briefing into the issue's live agent session. It never
// creates a session or worktree; with no live session it answers 409.
func (s *Server) handleDispatchReprompt(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/dispatch/reprompt")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	dp := tracker.BuildDispatchPrompt(proj, issue, proj.LoadWorkflowForIssue(issue), resolveProjectWorkDir(proj), leadLive(proj))
	resp := repromptAgentSession(proj, tmuxSessionName(issue.Slug), dp.Prompt, issue.Slug, resolveProjectWorkDir(proj))
	w.Header().Set("Content-Type", "application/json")
	if resp.Status == "no-session" {
		w.WriteHeader(http.StatusConflict)
	}
	json.NewEncoder(w).Encode(resp)
}

// repromptAgentSession pastes prompt into the live agent session and checks
// it arrived (see agentrun.Paste).
func repromptAgentSession(proj *tracker.Project, session, prompt, issueSlug, workDir string) DispatchResponse {
	textFile := filepath.Join(workDir, ".agent-logs", session, "dispatch-prompt.txt")
	response := agentrun.Paste(proj, session, prompt, textFile, repromptURL(proj, session, issueSlug))
	switch response.Status {
	case "sent":
		response.Status = "reprompted"
	case "no-session":
		response.Steps = []DispatchStep{{
			Name:   "No live agent session",
			Status: "error",
			Detail: fmt.Sprintf("%s is not running; dispatch the issue instead (POST /p/%s/issue/%s/dispatch or the Claude button)", response.Session, proj.Slug, issueSlug),
		}}
	}
	return response
}
