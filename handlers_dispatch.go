package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

// runGitWorktreeAdd is the seam for tests to stub the actual git invocation.
// Passes --no-checkout so the caller can configure sparse-checkout before the
// working tree is populated; when sparse-checkout is skipped, a follow-up
// checkout step materializes the full tree.
var runGitWorktreeAdd = func(workdir, branch, wtPath string) ([]byte, error) {
	return exec.Command("git", "-C", workdir, "worktree", "add", "--no-checkout", "-b", branch, wtPath).CombinedOutput()
}

// runGitSparseCheckout configures sparse-checkout in the new worktree to
// exclude the given paths. Patterns are built from the excludes ('/*' to
// include everything, then '!<path>' per exclude) and passed to
// `sparse-checkout set --no-cone`, which writes the pattern file. Over a
// --no-checkout tree this does NOT materialize files — the caller must run a
// follow-up `git checkout HEAD` to lay down the included paths. Non-cone mode
// is used because we express exclusions rather than inclusions; cone mode is
// include-only.
var runGitSparseCheckout = func(wtPath string, excludes []string) ([]byte, error) {
	args := []string{"-C", wtPath, "sparse-checkout", "set", "--no-cone", "/*"}
	for _, p := range excludes {
		args = append(args, "!"+p)
	}
	return exec.Command("git", args...).CombinedOutput()
}

// runGitCheckoutHead materializes the working tree after a --no-checkout
// worktree add. It always runs (with or without sparse-checkout configured):
// `git worktree add --no-checkout` leaves the tree empty, and the checkout
// honors any sparse-checkout patterns already written. Seam mirrors the others.
var runGitCheckoutHead = func(wtPath string) ([]byte, error) {
	return exec.Command("git", "-C", wtPath, "checkout", "HEAD").CombinedOutput()
}

// runWorktreeSetup is the seam for tests to stub the post-create setup shell
// command. Real implementation runs the user-supplied command via `sh -c` with
// cwd set to the new worktree so relative paths in the command resolve there.
var runWorktreeSetup = func(wtPath, cmd string) ([]byte, error) {
	c := exec.Command("sh", "-c", cmd)
	c.Dir = wtPath
	return c.CombinedOutput()
}

// ensureWorktree creates the per-issue worktree if needed and returns the
// effective working directory for the dispatched session. Returns ok=false
// only when worktree is enabled and creation (or post-create setup) fails —
// disabled or no-slug callers get ok=true with no steps and the original
// workdir. Setup runs only on fresh creation, never on reuse: re-running an
// arbitrary user command on every redispatch would be a footgun.
func ensureWorktree(workdir, slug string, wf *tracker.WorkflowConfig) (string, []DispatchStep, bool) {
	wtPath, branch, enabled := resolveWorktree(workdir, slug, wf)
	if !enabled {
		return workdir, nil, true
	}
	if _, err := os.Stat(wtPath); err == nil {
		return wtPath, []DispatchStep{{Name: fmt.Sprintf("Reuse worktree %s on %s", wtPath, branch), Status: "ok"}}, true
	}
	if out, err := runGitWorktreeAdd(workdir, branch, wtPath); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return workdir, []DispatchStep{{
			Name:   fmt.Sprintf("git worktree add %s on %s", wtPath, branch),
			Status: "error",
			Detail: detail,
		}}, false
	}
	steps := []DispatchStep{{Name: fmt.Sprintf("Created worktree %s on %s", wtPath, branch), Status: "ok"}}

	// Populate the working tree. `git worktree add --no-checkout` leaves the
	// tree empty with a populated index, so the tree must be materialized
	// explicitly. With excludes, lay down the sparse-checkout pattern file
	// first so the subsequent checkout honors it. `sparse-checkout set` only
	// writes the pattern file and reconciles an *already-materialized* tree —
	// over a --no-checkout tree it writes the pattern and leaves the tree
	// empty, which reads as every file staged-for-deletion. So a separate
	// `git checkout HEAD` always runs afterward to write the included files.
	excludes := wf.WorktreeSparseExcludes()
	if len(excludes) > 0 {
		if out, err := runGitSparseCheckout(wtPath, excludes); err != nil {
			detail := strings.TrimSpace(string(out))
			if detail == "" {
				detail = err.Error()
			}
			steps = append(steps, DispatchStep{
				Name:   fmt.Sprintf("Sparse-checkout excludes: %s", strings.Join(excludes, ", ")),
				Status: "error",
				Detail: detail,
			})
			return workdir, steps, false
		}
		steps = append(steps, DispatchStep{
			Name:   fmt.Sprintf("Sparse-checkout excludes: %s", strings.Join(excludes, ", ")),
			Status: "ok",
		})
	}
	if out, err := runGitCheckoutHead(wtPath); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		steps = append(steps, DispatchStep{
			Name:   "git checkout HEAD",
			Status: "error",
			Detail: detail,
		})
		return workdir, steps, false
	}

	if setup := wf.WorktreeSetupCmd(); setup != "" {
		out, err := runWorktreeSetup(wtPath, setup)
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail == "" {
				detail = err.Error()
			}
			steps = append(steps, DispatchStep{
				Name:   fmt.Sprintf("Worktree setup: %s", setup),
				Status: "error",
				Detail: detail,
			})
			return workdir, steps, false
		}
		steps = append(steps, DispatchStep{Name: fmt.Sprintf("Worktree setup: %s", setup), Status: "ok"})
	}
	return wtPath, steps, true
}

type DispatchStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DispatchResponse struct {
	Status    string         `json:"status"`
	Prompt    string         `json:"prompt"`
	Session   string         `json:"session"`
	LogFile   string         `json:"log_file,omitempty"`
	AttachCmd string         `json:"attach_cmd,omitempty"`
	Steps     []DispatchStep `json:"steps"`
}

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

// validModelName limits --model values to characters that are safe to type
// into a tmux shell unquoted.
var validModelName = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]-]+$`)

// agentLaunchCommand builds the shell command typed into tmux. The prompt is
// passed as the agent's positional argument (both claude and codex accept
// one and start interactive with it). A --model flag is added only when the
// project enforces one (agent_model_source: project).
func agentLaunchCommand(proj *tracker.Project, agentType string, promptPath string) string {
	modelFlag := ""
	if m := proj.AgentModel(agentType); m != "" && validModelName.MatchString(m) {
		modelFlag = " --model " + m
	}
	return fmt.Sprintf("%s%s \"$(cat %q)\"", agentType, modelFlag, promptPath)
}

// writeDispatchPrompt persists prompt as <sessionLogDir>/dispatch-prompt.txt
// and returns its path.
func writeDispatchPrompt(sessionLogDir, prompt string) (string, error) {
	if err := os.MkdirAll(sessionLogDir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(sessionLogDir, "dispatch-prompt.txt")
	return path, os.WriteFile(path, []byte(prompt), 0644)
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		*steps = append(*steps, DispatchStep{Name: name, Status: "error", Detail: strings.TrimSpace(string(out))})
		return false
	}
	*steps = append(*steps, DispatchStep{Name: name, Status: "ok"})
	return true
}

// openTerminalStep opens a terminal attached to the given tmux session.
// Uses proj.Terminal if set; falls back to i3+alacritty for backwards compat.
// terminal="none" is headless: appends an info step and returns true.
func openTerminalStep(proj *tracker.Project, session string, steps *[]DispatchStep) bool {
	terminal := ""
	if proj != nil {
		terminal = proj.Terminal
	}

	if terminal == "none" {
		*steps = append(*steps, DispatchStep{Name: "Terminal: headless — attach manually", Status: "ok"})
		return true
	}

	if terminal != "" {
		cmd := strings.ReplaceAll(terminal, "{{session}}", session)
		return runStep(steps, "Open terminal", exec.Command("sh", "-c", cmd))
	}

	// Backwards compat: i3 + alacritty
	if proj != nil && proj.I3Workspace != "" {
		if !runStep(steps, fmt.Sprintf("Switch to workspace %s", proj.I3Workspace),
			exec.Command("i3-msg", "workspace", proj.I3Workspace)) {
			return false
		}
	}
	return runStep(steps, "Open alacritty",
		exec.Command("i3-msg", "exec", fmt.Sprintf("alacritty -e tmux attach -t %s", session)))
}

// attachAgentStep makes the agent visible to the human. By default it opens a
// terminal on the agent's own session. In shared mode it selects the agent's
// window and only opens a terminal when nobody is attached to the shared
// session yet — otherwise the new window just shows up in the open tmux.
func attachAgentStep(proj *tracker.Project, name string, steps *[]DispatchStep) bool {
	shared := sharedTmuxSession(proj)
	if shared == "" {
		return openTerminalStep(proj, name, steps)
	}
	runStep(steps, fmt.Sprintf("Select window %s", name),
		exec.Command("tmux", "select-window", "-t", agentTmuxTarget(proj, name)))
	if tmuxSessionAttached(shared) {
		*steps = append(*steps, DispatchStep{Name: fmt.Sprintf("Session %s already open — no new terminal", shared), Status: "ok"})
		return true
	}
	return openTerminalStep(proj, shared, steps)
}

func startAgentSession(proj *tracker.Project, session string, prompt string, issueSlug string, agentType string, viewerURL string, wf *tracker.WorkflowConfig) DispatchResponse {
	workDir := resolveProjectWorkDir(proj)

	// Create / reuse a per-issue git worktree before opening the tmux session
	// so the agent lands in an isolated working tree. Concurrent issues then
	// cannot stomp on each other's uncommitted state. On failure we abort:
	// silently falling back to the primary checkout would defeat the safety
	// guarantee.
	newDir, worktreeSteps, worktreeOK := ensureWorktree(workDir, issueSlug, wf)
	if !worktreeOK {
		return DispatchResponse{Status: "error", Prompt: prompt, Session: session, Steps: worktreeSteps}
	}
	workDir = newDir

	steps := append([]DispatchStep{}, worktreeSteps...)
	sessionLogDir := filepath.Join(workDir, ".agent-logs", session)
	rawLog := filepath.Join(sessionLogDir, "rawlog")
	cliLog := filepath.Join(sessionLogDir, session+".clilog")

	// target is what every tmux command below addresses: the agent's own
	// session, or its window inside the project's shared session.
	target := agentTmuxTarget(proj, session)

	response := DispatchResponse{
		Status:  "dispatched",
		Prompt:  prompt,
		Session: agentDisplayName(proj, session),
		LogFile: rawLog,
		Steps:   steps,
	}

	if tmuxHasSession(target) {
		// A session with this name already exists — most often the same agent is
		// still running. Re-running new-session/send-keys would clobber whatever
		// the agent is doing and re-paste the prompt, so skip all setup and just
		// open a terminal attached to the existing session.
		steps = append(steps, DispatchStep{Name: "Existing session — attaching", Status: "reattached"})
		if u := repromptURL(proj, session, issueSlug); u != "" {
			steps = append(steps, DispatchStep{Name: "Prompt not re-sent", Status: "reattached", Detail: "to re-send the current briefing use Re-send prompt or POST " + u})
		}
		response.Status = "reattached"
		attachAgentStep(proj, session, &steps)
		if proj != nil && proj.Terminal == "none" {
			response.AttachCmd = agentAttachCmd(proj, session)
		}
		response.Steps = steps
		return response
	}

	createName, createCmd := createAgentTmux(proj, session, workDir)
	if !runStep(&steps, createName, createCmd) {
		response.Status = "error"
		response.Steps = steps
		return response
	}

	// In shared mode the window is already named after the session so it can be
	// matched back to the issue; renaming it to the slug would break that.
	if sharedTmuxSession(proj) == "" {
		windowName := session
		if issueSlug != "" {
			windowName = issueSlug
		}
		exec.Command("tmux", "rename-window", "-t", session, windowName).Run()
	}

	// Persist the exact prompt the bot is briefed with. The agent is launched
	// with this file as its argument, and the timeline view replays it later
	// instead of reconstructing an approximation.
	promptPath, err := writeDispatchPrompt(sessionLogDir, prompt)
	if err != nil {
		steps = append(steps, DispatchStep{Name: "Write prompt file", Status: "error", Detail: err.Error()})
		response.Status = "error"
		response.Steps = steps
		return response
	}

	runStep(&steps, fmt.Sprintf("Log to %s", rawLog),
		exec.Command("tmux", "pipe-pane", "-t", target, "-o", fmt.Sprintf("cat >> %s", rawLog)))
	runStep(&steps, fmt.Sprintf("CLI log to %s", cliLog),
		exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("export ISSUE_CLI_LOG=%q", cliLog), "Enter"))

	serverRoot, _ := os.Getwd()
	runStep(&steps, fmt.Sprintf("Server root env %s", serverRoot),
		exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("export ISSUE_VIEWER_SERVER_PWD=%q", serverRoot), "Enter"))
	if issueSlug != "" {
		runStep(&steps, fmt.Sprintf("Issue slug env %s", issueSlug),
			exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("export ISSUE_VIEWER_ISSUE_SLUG=%q", issueSlug), "Enter"))
	}
	if viewerURL != "" {
		runStep(&steps, fmt.Sprintf("Viewer URL env %s", viewerURL),
			exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("export ISSUE_VIEWER_URL=%q", viewerURL), "Enter"))
	}
	// Propagate the config path the viewer was launched with so the CLI can
	// resolve `--project <slug>` against it without the bot having to know
	// the file name (e.g. projects-mfranc.yaml vs the default projects.yaml).
	if cfg := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_CONFIG")); cfg != "" {
		runStep(&steps, fmt.Sprintf("Viewer config env %s", cfg),
			exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("export ISSUE_VIEWER_CONFIG=%q", cfg), "Enter"))
	}

	runStep(&steps, fmt.Sprintf("cd %s", workDir),
		exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("cd %q", workDir), "Enter"))

	if !attachAgentStep(proj, session, &steps) {
		response.Status = "error"
		response.Steps = steps
		return response
	}
	if proj != nil && proj.Terminal == "none" {
		response.AttachCmd = agentAttachCmd(proj, session)
	}

	time.Sleep(500 * time.Millisecond)

	// The prompt goes in as the agent's launch argument, never pasted:
	// a paste sent before the agent's input is ready is silently dropped.
	// Both agents start interactive, so a human stays in the loop.
	runStep(&steps, fmt.Sprintf("Start %s (interactive, prompt as argument)", agentType),
		exec.Command("tmux", "send-keys", "-t", target, agentLaunchCommand(proj, agentType, promptPath), "Enter"))
	// Only Claude's pane rendering has been checked for the delivery marker.
	if agentType != "codex" {
		promptDeliveryStep(&steps, target, prompt, repromptURL(proj, session, issueSlug))
	}

	response.Steps = steps
	return response
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
	prompt := tracker.BuildDispatchPrompt(proj, issue, wf, resolveProjectWorkDir(proj)).Prompt
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
	dp := tracker.BuildDispatchPrompt(proj, issue, proj.LoadWorkflowForIssue(issue), resolveProjectWorkDir(proj))
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
	dp := tracker.BuildDispatchPrompt(proj, issue, proj.LoadWorkflowForIssue(issue), resolveProjectWorkDir(proj))
	workDir := resolveProjectWorkDir(proj)
	if dp.Worktree != "" && fileExists(dp.Worktree) {
		workDir = dp.Worktree
	}
	resp := repromptAgentSession(proj, tmuxSessionName(issue.Slug), dp.Prompt, issue.Slug, workDir)
	w.Header().Set("Content-Type", "application/json")
	if resp.Status == "no-session" {
		w.WriteHeader(http.StatusConflict)
	}
	json.NewEncoder(w).Encode(resp)
}

// repromptAgentSession pastes prompt into the live agent session and checks
// it arrived. The paste goes through a buffer named after the session
// (deleted after pasting) so concurrent reprompts can't swap prompts, and as
// a bracketed paste so the agent takes multi-line text as one message.
func repromptAgentSession(proj *tracker.Project, session, prompt, issueSlug, workDir string) DispatchResponse {
	target := agentTmuxTarget(proj, session)
	response := DispatchResponse{Status: "reprompted", Prompt: prompt, Session: agentDisplayName(proj, session)}
	if !tmuxHasSession(target) {
		response.Status = "no-session"
		response.Steps = []DispatchStep{{
			Name:   "No live agent session",
			Status: "error",
			Detail: fmt.Sprintf("%s is not running; dispatch the issue instead (POST /p/%s/issue/%s/dispatch or the Claude button)", response.Session, proj.Slug, issueSlug),
		}}
		return response
	}

	var steps []DispatchStep
	promptPath, err := writeDispatchPrompt(filepath.Join(workDir, ".agent-logs", session), prompt)
	if err != nil {
		response.Status = "error"
		response.Steps = []DispatchStep{{Name: "Write prompt file", Status: "error", Detail: err.Error()}}
		return response
	}
	buffer := "issue-viewer-" + session
	ok := runStep(&steps, "Load prompt into tmux buffer "+buffer,
		exec.Command("tmux", "load-buffer", "-b", buffer, promptPath)) &&
		runStep(&steps, "Paste prompt into "+response.Session,
			exec.Command("tmux", "paste-buffer", "-d", "-p", "-b", buffer, "-t", target))
	if ok {
		time.Sleep(200 * time.Millisecond)
		ok = runStep(&steps, "Submit prompt", exec.Command("tmux", "send-keys", "-t", target, "Enter"))
	}
	if ok {
		promptDeliveryStep(&steps, target, prompt, repromptURL(proj, session, issueSlug))
	} else {
		response.Status = "error"
	}
	response.Steps = steps
	return response
}

