package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var listTmuxSessions = defaultListTmuxSessions
var tmuxSendKeys = defaultTmuxSendKeys
var tmuxHasSession = defaultTmuxHasSession
var tmuxSessionAttached = defaultTmuxSessionAttached

func tmuxSessionName(slug string) string {
	return tracker.AgentSessionName(slug)
}

func defaultTmuxHasSession(session string) bool {
	if strings.TrimSpace(session) == "" {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

// sharedTmuxSession returns the project's shared tmux session, or "" when the
// project uses the default one-session-per-agent mode.
func sharedTmuxSession(proj *tracker.Project) string {
	if proj == nil {
		return ""
	}
	return strings.TrimSpace(proj.TmuxSession)
}

// agentTmuxTarget returns the tmux target for the agent (or editor) called
// name: the session itself by default, or the window name inside the
// project's shared session. The "=" prefixes force exact matches so that
// agent-foo never resolves to an existing agent-foo-bar window.
func agentTmuxTarget(proj *tracker.Project, name string) string {
	if shared := sharedTmuxSession(proj); shared != "" {
		return "=" + shared + ":=" + name
	}
	return name
}

// agentDisplayName is the human-facing name of the agent's tmux location,
// e.g. "agent-foo" or "work:agent-foo" in shared mode.
func agentDisplayName(proj *tracker.Project, name string) string {
	return tracker.AgentDisplayName(proj, name)
}

// agentAttachCmd is the command a human runs to reach the agent when the
// project is headless (terminal: none).
func agentAttachCmd(proj *tracker.Project, name string) string {
	if shared := sharedTmuxSession(proj); shared != "" {
		return fmt.Sprintf("tmux attach -t %s \\; select-window -t %s", shared, name)
	}
	return fmt.Sprintf("tmux attach -t %s", name)
}

// createAgentTmux creates the tmux home for the agent called name: its own
// session by default, or a new window in the project's shared session
// (creating that session first if it does not exist yet).
func createAgentTmux(proj *tracker.Project, name string, workDir string) (string, *exec.Cmd) {
	shared := sharedTmuxSession(proj)
	if shared == "" {
		return fmt.Sprintf("Create tmux session in %s", workDir),
			exec.Command("tmux", "new-session", "-d", "-s", name, "-c", workDir)
	}
	if tmuxHasSession("=" + shared) {
		return fmt.Sprintf("Create tmux window %s in session %s (%s)", name, shared, workDir),
			exec.Command("tmux", "new-window", "-d", "-t", "="+shared+":", "-n", name, "-c", workDir)
	}
	return fmt.Sprintf("Create tmux session %s with window %s in %s", shared, name, workDir),
		exec.Command("tmux", "new-session", "-d", "-s", shared, "-n", name, "-c", workDir)
}

// killAgentTmux tears down what createAgentTmux made. In shared mode only the
// agent's window goes; the shared session stays.
func killAgentTmux(proj *tracker.Project, name string) error {
	if sharedTmuxSession(proj) != "" {
		return exec.Command("tmux", "kill-window", "-t", agentTmuxTarget(proj, name)).Run()
	}
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func defaultTmuxSessionAttached(session string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+session+":", "#{session_attached}").Output()
	if err != nil {
		return false
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n > 0
}

func defaultTmuxSendKeys(target string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	content := strings.Join(lines, "\n")
	tmp, err := os.CreateTemp("", "issue-approval-*.txt")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := exec.Command("tmux", "load-buffer", tmpPath).Run(); err != nil {
		return err
	}
	if err := exec.Command("tmux", "paste-buffer", "-t", target).Run(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
}

func defaultListTmuxSessions() []AgentSession {
	out, err := exec.Command("tmux", "ls").Output()
	if err != nil {
		return nil
	}

	lineRE := regexp.MustCompile(`^([^:]+):.*\(created ([^)]+)\)`)
	var sessions []AgentSession
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := lineRE.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}

		session := AgentSession{Name: strings.TrimSpace(matches[1])}
		if created, err := time.Parse("Mon Jan _2 15:04:05 2006", strings.TrimSpace(matches[2])); err == nil {
			session.StartTime = created.Format("2006-01-02 15:04:05")
		} else {
			session.StartTime = strings.TrimSpace(matches[2])
		}
		sessions = append(sessions, session)
	}
	return append(sessions, listSharedAgentWindows()...)
}

// listSharedAgentWindows reports agent windows living inside shared tmux
// sessions (projects with tmux_session set) as "session:window" entries, so
// issue matching and approval notifications find them like regular sessions.
// Windows of per-agent sessions are skipped — those are already listed.
func listSharedAgentWindows() []AgentSession {
	out, err := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}\t#{window_name}").Output()
	if err != nil {
		return nil
	}
	return parseSharedAgentWindows(string(out))
}

func parseSharedAgentWindows(out string) []AgentSession {
	var sessions []AgentSession
	for _, line := range strings.Split(out, "\n") {
		sessionName, windowName, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || strings.HasPrefix(sessionName, "agent-") || !strings.HasPrefix(windowName, "agent-") {
			continue
		}
		sessions = append(sessions, AgentSession{Name: sessionName + ":" + windowName})
	}
	return sessions
}

func sessionsByIssueSlug(issues []*tracker.Issue) (map[string][]AgentSession, int) {
	sessions := listTmuxSessions()
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

func sessionMatchesIssue(sessionName string, slug string) bool {
	sessionName = strings.ToLower(strings.TrimSpace(sessionName))
	slug = strings.ToLower(strings.TrimSpace(slug))
	if sessionName == "" || slug == "" {
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

var tmuxCapturePane = defaultTmuxCapturePane

// promptDeliveryTimeout bounds how long a dispatch or reprompt waits for the
// prompt to show up in the agent's pane before reporting it undelivered.
var promptDeliveryTimeout = 20 * time.Second

const promptDeliveryPoll = 500 * time.Millisecond

func defaultTmuxCapturePane(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-t", target).Output()
	return string(out), err
}

// promptDeliveryStep polls the agent's pane until the prompt's delivery
// marker appears, and records whether it did. It never re-sends: a missing
// prompt is reported as a failed step for the human to act on.
func promptDeliveryStep(steps *[]DispatchStep, target, prompt, repromptURL string) {
	marker := tracker.PromptDeliveryMarker(prompt)
	if marker == "" {
		return
	}
	deadline := time.Now().Add(promptDeliveryTimeout)
	for {
		if pane, err := tmuxCapturePane(target); err == nil && strings.Contains(pane, marker) {
			*steps = append(*steps, DispatchStep{Name: "Prompt delivered", Status: "ok"})
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(promptDeliveryPoll)
	}
	detail := fmt.Sprintf("its last line was not visible in the pane after %s; nothing was re-sent. Check the session, then ", promptDeliveryTimeout)
	if repromptURL != "" {
		detail += "use Re-send prompt or POST " + repromptURL
	} else {
		detail += "paste the prompt from this response by hand"
	}
	*steps = append(*steps, DispatchStep{Name: "Prompt not delivered", Status: "error", Detail: detail})
}
