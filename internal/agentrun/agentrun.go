// Package agentrun launches agents (workers and leads) in tmux: it creates
// their worktree, their tmux session or shared-session window, exports the
// environment issue-cli reads, and starts the agent with its prompt. The
// viewer's dispatch handlers and `issue-cli lead spawn` both go through it.
package agentrun

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Step is one line of a launch report.
type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Response is the result of a launch, also the JSON body of the viewer's
// dispatch endpoints.
type Response struct {
	Status    string `json:"status"`
	Prompt    string `json:"prompt"`
	Session   string `json:"session"`
	AttachCmd string `json:"attach_cmd,omitempty"`
	// SwitchCmd reaches the agent from inside tmux; set with AttachCmd.
	SwitchCmd string `json:"switch_cmd,omitempty"`
	// Gate is the status awaiting the owner's approval when Status is
	// "waiting-on-gate" (see DispatchSpec).
	Gate  string `json:"gate,omitempty"`
	Steps []Step `json:"steps"`
}

// Spec describes one agent launch.
type Spec struct {
	// Session is the tmux session (or shared-session window) name:
	// agent-<slug> for workers, lead-<project> for leads.
	Session   string
	Prompt    string
	AgentType string
	// IssueSlug is exported as ISSUE_VIEWER_ISSUE_SLUG and names the window
	// in per-session mode. Empty for agents not bound to an issue.
	IssueSlug string
	// LeadScope is exported as ISSUE_VIEWER_LEAD_SCOPE: "project" or the
	// epic slug. Empty for workers.
	LeadScope string
	// WorkDir is the project checkout the agent (or its worktree) starts
	// from; "" means ProjectWorkDir(proj).
	WorkDir string
	// Worktree is where the agent runs; zero means WorkDir itself.
	Worktree tracker.WorktreeSpec
	Workflow *tracker.WorkflowConfig
	// RepromptHint is the command quoted in hints when the prompt can be
	// re-sent (issue-cli dispatch <slug> --reprompt).
	RepromptHint string
	// Headless skips opening a terminal (lead_terminal for leads, terminal
	// for workers); the response carries AttachCmd and SwitchCmd.
	Headless bool
}

// validModelName limits --model values to characters that are safe in a
// shell command unquoted.
var validModelName = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]-]+$`)

// validSessionName limits the -n value to characters that are safe in a shell
// command unquoted; agent-<slug> and lead-<project> names always are.
var validSessionName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// LaunchCommand builds the shell command the agent's window runs. The prompt is
// passed as the agent's positional argument (both claude and codex accept
// one and start interactive with it). Claude is named after its tmux session
// (-n agent-<slug>, -n lead-<project>), so the tmux name is also its address
// for Claude Code's cross-session messaging (ListAgents / SendMessage). A
// --model flag is added only when the project enforces one
// (agent_model_source: project).
func LaunchCommand(proj *tracker.Project, agentType, session, promptPath string) string {
	nameFlag := ""
	if agentType == "claude" && validSessionName.MatchString(session) {
		nameFlag = " -n " + session
	}
	modelFlag := ""
	if m := proj.AgentModel(agentType); m != "" && validModelName.MatchString(m) {
		modelFlag = " --model " + m
	}
	return fmt.Sprintf("%s%s%s \"$(cat %q)\"", agentType, nameFlag, modelFlag, promptPath)
}

// AgentCommand wraps launch for running as a tmux window's own command: in
// the user's login shell, interactive, so the agent is found on the same PATH
// and with the same aliases as when it was typed into a fresh window. When
// the agent exits the window keeps a shell, also after a Ctrl-C: the no-op INT
// trap stops the wrapper shell from abandoning the rest of the command along
// with the agent.
func AgentCommand(launch string) []string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-lic", "trap : INT; " + launch + "; exec " + shellWord(shell) + " -l"}
}

// WritePrompt persists prompt as <sessionLogDir>/dispatch-prompt.txt and
// returns its path.
func WritePrompt(sessionLogDir, prompt string) (string, error) {
	if err := os.MkdirAll(sessionLogDir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(sessionLogDir, "dispatch-prompt.txt")
	return path, os.WriteFile(path, []byte(prompt), 0644)
}

// ProjectWorkDir is the directory agents of proj start from: its configured
// workdir, else the current directory.
func ProjectWorkDir(proj *tracker.Project) string {
	if proj != nil && strings.TrimSpace(proj.WorkDir) != "" {
		return proj.WorkDir
	}
	wd, _ := os.Getwd()
	return wd
}

// Start launches the agent described by spec, or re-attaches when its
// session is already live (never starting a second one).
func Start(proj *tracker.Project, spec Spec) Response {
	session, prompt := spec.Session, spec.Prompt
	workDir := spec.WorkDir
	if workDir == "" {
		workDir = ProjectWorkDir(proj)
	}
	// Create / reuse the worktree before opening the tmux session so the agent
	// lands in an isolated working tree. Concurrent issues then cannot stomp
	// on each other's uncommitted state. On failure we abort: silently falling
	// back to the primary checkout would defeat the safety guarantee.
	newDir, worktreeSteps, worktreeOK := EnsureWorktree(workDir, spec.Worktree, spec.Workflow)
	if !worktreeOK {
		return Response{Status: "error", Prompt: prompt, Session: session, Steps: worktreeSteps}
	}
	// The session's logs (clilog, prompt, messages) stay in the project
	// checkout, not the worktree, so removing a worktree loses no history.
	sessionLogDir := filepath.Join(workDir, ".agent-logs", session)
	cliLog := filepath.Join(sessionLogDir, session+".clilog")
	workDir = newDir

	steps := append([]Step{}, worktreeSteps...)

	// target is what every tmux command below addresses: the agent's own
	// session, or its window inside the project's shared session.
	target := Target(proj, session)

	response := Response{
		Status:  "dispatched",
		Prompt:  prompt,
		Session: tracker.AgentDisplayName(proj, session),
		Steps:   steps,
	}
	// Headless (spec.Headless, or terminal: none) opens no terminal; the
	// response carries the attach hints instead. No launch selects the new
	// window, so whoever is looking at the shared session (usually the lead)
	// keeps it. Leads follow lead_terminal: instead, headless when it is
	// unset.
	lead := spec.LeadScope != ""
	headless := spec.Headless || TerminalNone(proj)
	if lead {
		headless = spec.Headless || LeadHeadless(proj)
	}
	setHints := func() {
		response.AttachCmd = AttachCmd(proj, session)
		response.SwitchCmd = SwitchCmd(proj, session)
	}
	// showLead runs lead_terminal; a tab it could not add leaves the hints.
	showLead := func() bool {
		ok, shown := LeadTerminalStep(proj, session, &steps)
		if !shown {
			setHints()
		}
		return ok
	}

	if HasSession(target) {
		// A session with this name already exists — most often the same agent is
		// still running. Re-running new-session would start a second agent
		// next to it with the prompt again, so skip all setup and just
		// open a terminal attached to the existing session.
		steps = append(steps, Step{Name: "Existing session — attaching", Status: "reattached"})
		if spec.RepromptHint != "" {
			steps = append(steps, Step{Name: "Prompt not re-sent", Status: "reattached", Detail: "to re-send the current briefing use Re-send prompt or run: " + spec.RepromptHint})
		}
		response.Status = "reattached"
		switch {
		case headless:
			steps = append(steps, Step{Name: "Terminal: headless — attach manually", Status: "ok"})
			setHints()
		case lead:
			showLead()
		default:
			AttachStep(proj, session, &steps)
		}
		response.Steps = steps
		return response
	}

	// Persist the exact prompt the bot is briefed with. The agent is launched
	// with this file as its argument, and the timeline view replays it later
	// instead of reconstructing an approximation.
	promptPath, err := WritePrompt(sessionLogDir, prompt)
	if err != nil {
		steps = append(steps, Step{Name: "Write prompt file", Status: "error", Detail: err.Error()})
		response.Status = "error"
		response.Steps = steps
		return response
	}

	serverRoot, _ := os.Getwd()
	env := []string{
		"ISSUE_CLI_LOG=" + cliLog,
		"ISSUE_VIEWER_SERVER_PWD=" + serverRoot,
	}
	if spec.IssueSlug != "" {
		env = append(env, "ISSUE_VIEWER_ISSUE_SLUG="+spec.IssueSlug)
	}
	if spec.LeadScope != "" {
		env = append(env, "ISSUE_VIEWER_LEAD_SCOPE="+spec.LeadScope)
	}
	if proj != nil && proj.Slug != "" {
		env = append(env, "ISSUE_VIEWER_PROJECT="+proj.Slug)
	}
	// Propagate the config path the viewer was launched with so the CLI can
	// resolve `--project <slug>` against it without the bot having to know
	// the file name (e.g. projects-mfranc.yaml vs the default projects.yaml).
	if cfg := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_CONFIG")); cfg != "" {
		env = append(env, "ISSUE_VIEWER_CONFIG="+cfg)
	}

	// The agent is the window's own command, started with its environment and
	// working directory already set. Nothing is typed into a shell, so keys
	// pressed in an attached client while this runs can't reach the command.
	// The prompt goes in as the agent's launch argument, never pasted: a
	// paste sent before the agent's input is ready is silently dropped. Both
	// agents start interactive, so a human stays in the loop.
	createName, createCmd := CreateTmux(proj, session, workDir, env,
		AgentCommand(LaunchCommand(proj, spec.AgentType, session, promptPath))...)
	if !RunStep(&steps, fmt.Sprintf("%s, running %s (interactive, prompt as argument)", createName, spec.AgentType), createCmd) {
		response.Status = "error"
		response.Steps = steps
		return response
	}
	if spec.IssueSlug != "" {
		recordDispatch(proj, spec, &steps)
	}

	// In shared mode the window is already named after the session so it can be
	// matched back to the issue; renaming it to the slug would break that.
	if SharedSession(proj) == "" {
		windowName := session
		if spec.IssueSlug != "" {
			windowName = spec.IssueSlug
		}
		exec.Command("tmux", "rename-window", "-t", session, windowName).Run()
	}

	if headless {
		steps = append(steps, Step{Name: "Terminal: headless — attach manually", Status: "ok"})
		setHints()
	} else if lead && !showLead() || !lead && !ShowStep(proj, session, &steps) {
		response.Status = "error"
		response.Steps = steps
		return response
	}

	// Only Claude's pane rendering has been checked for the delivery marker.
	if spec.AgentType != "codex" {
		PromptDeliveryStep(&steps, target, prompt, spec.RepromptHint)
	}

	response.Steps = steps
	return response
}

// recordDispatch appends this launch to the issue's stats sidecar. A failure
// is a warning step; it never fails the dispatch.
func recordDispatch(proj *tracker.Project, spec Spec, steps *[]Step) {
	if proj == nil || proj.IssueDir == "" {
		return
	}
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err == nil {
		if issue := tracker.FindIssue(issues, spec.IssueSlug); issue != nil {
			err = tracker.RecordDispatch(issue.FilePath, tracker.Dispatch{At: time.Now(), Session: spec.Session, Agent: spec.AgentType})
		}
	}
	if err != nil {
		*steps = append(*steps, Step{Name: "Record dispatch time", Status: "warning", Detail: err.Error()})
	}
}

// Paste delivers text into the live session called session as one bracketed
// paste followed by Enter, and checks it arrived. The paste goes through a
// buffer named after the session (deleted after pasting) so concurrent pastes
// can't swap texts. textFile keeps a copy of what was sent and is what the
// buffer loads. Status is "no-session" when the session is not live.
func Paste(proj *tracker.Project, session, text, textFile, repromptHint string) Response {
	target := Target(proj, session)
	response := Response{Status: "sent", Prompt: text, Session: tracker.AgentDisplayName(proj, session)}
	if !HasSession(target) {
		response.Status = "no-session"
		return response
	}

	var steps []Step
	textPath := textFile
	err := os.MkdirAll(filepath.Dir(textFile), 0755)
	if err == nil {
		err = os.WriteFile(textFile, []byte(text), 0644)
	}
	if err != nil {
		response.Status = "error"
		response.Steps = []Step{{Name: "Write prompt file", Status: "error", Detail: err.Error()}}
		return response
	}
	buffer := "issue-viewer-" + session
	ok := RunStep(&steps, "Load prompt into tmux buffer "+buffer,
		exec.Command("tmux", "load-buffer", "-b", buffer, textPath)) &&
		RunStep(&steps, "Paste prompt into "+response.Session,
			exec.Command("tmux", "paste-buffer", "-d", "-p", "-b", buffer, "-t", target))
	if ok {
		time.Sleep(200 * time.Millisecond)
		ok = RunStep(&steps, "Submit prompt", exec.Command("tmux", "send-keys", "-t", target, "Enter"))
	}
	if ok {
		PromptDeliveryStep(&steps, target, text, repromptHint)
	} else {
		response.Status = "error"
	}
	response.Steps = steps
	return response
}
