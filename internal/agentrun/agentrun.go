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
	LogFile   string `json:"log_file,omitempty"`
	AttachCmd string `json:"attach_cmd,omitempty"`
	Steps     []Step `json:"steps"`
}

// Spec describes one agent launch.
type Spec struct {
	// Session is the tmux session (or shared-session window) name:
	// agent-<slug> for workers, lead-<scope> for leads.
	Session   string
	Prompt    string
	AgentType string
	// IssueSlug is exported as ISSUE_VIEWER_ISSUE_SLUG and names the window
	// in per-session mode. Empty for agents not bound to an issue.
	IssueSlug string
	// LeadScope is exported as ISSUE_VIEWER_LEAD_SCOPE: "project" or the
	// epic slug. Empty for workers.
	LeadScope string
	ViewerURL string
	// WorkDir is the project checkout the agent (or its worktree) starts
	// from; "" means ProjectWorkDir(proj).
	WorkDir string
	// Worktree is where the agent runs; zero means WorkDir itself.
	Worktree tracker.WorktreeSpec
	Workflow *tracker.WorkflowConfig
	// RepromptURL is quoted in hints when the prompt can be re-sent.
	RepromptURL string
	// Headless skips opening a terminal; the response carries AttachCmd.
	Headless bool
}

// validModelName limits --model values to characters that are safe to type
// into a tmux shell unquoted.
var validModelName = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]-]+$`)

// validSessionName limits the -n value to characters that are safe to type
// into a tmux shell unquoted; agent-<slug> and lead-<scope> names always are.
var validSessionName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// LaunchCommand builds the shell command typed into tmux. The prompt is
// passed as the agent's positional argument (both claude and codex accept
// one and start interactive with it). Claude is named after its tmux session
// (-n agent-<slug>, -n lead-<scope>), so the tmux name is also its address
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
	workDir = newDir

	steps := append([]Step{}, worktreeSteps...)
	sessionLogDir := filepath.Join(workDir, ".agent-logs", session)
	rawLog := filepath.Join(sessionLogDir, "rawlog")
	cliLog := filepath.Join(sessionLogDir, session+".clilog")

	// target is what every tmux command below addresses: the agent's own
	// session, or its window inside the project's shared session.
	target := Target(proj, session)

	response := Response{
		Status:  "dispatched",
		Prompt:  prompt,
		Session: tracker.AgentDisplayName(proj, session),
		LogFile: rawLog,
		Steps:   steps,
	}
	headless := spec.Headless || (proj != nil && proj.Terminal == "none")

	if HasSession(target) {
		// A session with this name already exists — most often the same agent is
		// still running. Re-running new-session/send-keys would clobber whatever
		// the agent is doing and re-paste the prompt, so skip all setup and just
		// open a terminal attached to the existing session.
		steps = append(steps, Step{Name: "Existing session — attaching", Status: "reattached"})
		if spec.RepromptURL != "" {
			steps = append(steps, Step{Name: "Prompt not re-sent", Status: "reattached", Detail: "to re-send the current briefing use Re-send prompt or POST " + spec.RepromptURL})
		}
		response.Status = "reattached"
		if !spec.Headless {
			AttachStep(proj, session, &steps)
		}
		if headless {
			response.AttachCmd = AttachCmd(proj, session)
		}
		response.Steps = steps
		return response
	}

	createName, createCmd := CreateTmux(proj, session, workDir)
	if !RunStep(&steps, createName, createCmd) {
		response.Status = "error"
		response.Steps = steps
		return response
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

	RunStep(&steps, fmt.Sprintf("Log to %s", rawLog),
		exec.Command("tmux", "pipe-pane", "-t", target, "-o", fmt.Sprintf("cat >> %s", rawLog)))
	exportEnv := func(label, key, value string) {
		RunStep(&steps, fmt.Sprintf("%s %s", label, value),
			exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("export %s=%q", key, value), "Enter"))
	}
	exportEnv("CLI log to", "ISSUE_CLI_LOG", cliLog)

	serverRoot, _ := os.Getwd()
	exportEnv("Server root env", "ISSUE_VIEWER_SERVER_PWD", serverRoot)
	if spec.IssueSlug != "" {
		exportEnv("Issue slug env", "ISSUE_VIEWER_ISSUE_SLUG", spec.IssueSlug)
	}
	if spec.LeadScope != "" {
		exportEnv("Lead scope env", "ISSUE_VIEWER_LEAD_SCOPE", spec.LeadScope)
	}
	if spec.ViewerURL != "" {
		exportEnv("Viewer URL env", "ISSUE_VIEWER_URL", spec.ViewerURL)
	}
	// Propagate the config path the viewer was launched with so the CLI can
	// resolve `--project <slug>` against it without the bot having to know
	// the file name (e.g. projects-mfranc.yaml vs the default projects.yaml).
	if cfg := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_CONFIG")); cfg != "" {
		exportEnv("Viewer config env", "ISSUE_VIEWER_CONFIG", cfg)
	}

	RunStep(&steps, fmt.Sprintf("cd %s", workDir),
		exec.Command("tmux", "send-keys", "-t", target, fmt.Sprintf("cd %q", workDir), "Enter"))

	if spec.Headless {
		steps = append(steps, Step{Name: "Terminal: headless — attach manually", Status: "ok"})
	} else if !AttachStep(proj, session, &steps) {
		response.Status = "error"
		response.Steps = steps
		return response
	}
	if headless {
		response.AttachCmd = AttachCmd(proj, session)
	}

	time.Sleep(500 * time.Millisecond)

	// The prompt goes in as the agent's launch argument, never pasted:
	// a paste sent before the agent's input is ready is silently dropped.
	// Both agents start interactive, so a human stays in the loop.
	RunStep(&steps, fmt.Sprintf("Start %s (interactive, prompt as argument)", spec.AgentType),
		exec.Command("tmux", "send-keys", "-t", target, LaunchCommand(proj, spec.AgentType, session, promptPath), "Enter"))
	// Only Claude's pane rendering has been checked for the delivery marker.
	if spec.AgentType != "codex" {
		PromptDeliveryStep(&steps, target, prompt, spec.RepromptURL)
	}

	response.Steps = steps
	return response
}

// Paste delivers text into the live session called session as one bracketed
// paste followed by Enter, and checks it arrived. The paste goes through a
// buffer named after the session (deleted after pasting) so concurrent pastes
// can't swap texts. textFile keeps a copy of what was sent and is what the
// buffer loads. Status is "no-session" when the session is not live.
func Paste(proj *tracker.Project, session, text, textFile, repromptURL string) Response {
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
		PromptDeliveryStep(&steps, target, text, repromptURL)
	} else {
		response.Status = "error"
	}
	response.Steps = steps
	return response
}
