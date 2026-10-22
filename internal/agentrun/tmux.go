package agentrun

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Session is one live tmux session, or a "session:window" entry for an agent
// window inside a shared session.
type Session struct {
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	// Path is the directory a standalone session was started in; empty for
	// shared-session windows and when tmux didn't report one.
	Path string `json:"path,omitempty"`
}

// The exec-backed tmux calls are package vars so tests (here and in the
// server) can stub them.
var (
	HasSession      = defaultHasSession
	SessionAttached = defaultSessionAttached
	FocusTerminal   = defaultFocusTerminal
	SendKeys        = defaultSendKeys
	ListSessions    = defaultListSessions
	CapturePane     = defaultCapturePane
)

// PromptDeliveryTimeout bounds how long a dispatch or reprompt waits for the
// prompt to show up in the agent's pane before reporting it undelivered.
var PromptDeliveryTimeout = 20 * time.Second

const promptDeliveryPoll = 500 * time.Millisecond

// managedPrefixes are the window names that are agents (or leads) when they
// live inside a shared session.
var managedPrefixes = []string{"agent-", "lead-"}

func hasManagedPrefix(name string) bool {
	for _, p := range managedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func defaultHasSession(session string) bool {
	if strings.TrimSpace(session) == "" {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

// SharedSession returns the project's shared tmux session, or "" when the
// project uses the default one-session-per-agent mode.
func SharedSession(proj *tracker.Project) string {
	if proj == nil {
		return ""
	}
	return strings.TrimSpace(proj.TmuxSession)
}

// Target returns the tmux target for the agent (or editor) called name: the
// session itself by default, or the window name inside the project's shared
// session. The "=" prefixes force exact matches so that agent-foo never
// resolves to an existing agent-foo-bar window.
func Target(proj *tracker.Project, name string) string {
	if shared := SharedSession(proj); shared != "" {
		return "=" + shared + ":=" + name
	}
	return name
}

// IsLive reports whether the agent or lead called name has a live tmux home.
func IsLive(proj *tracker.Project, name string) bool {
	return HasSession(Target(proj, name))
}

// AttachCmd is the command a human runs to reach the agent when the project
// is headless (terminal: none). When this process talks to a private tmux
// server (TMUX_TMPDIR set, as in a sandbox or playtest scenario), the command
// names it, so pasting it into any shell reaches the same server.
func AttachCmd(proj *tracker.Project, name string) string {
	if shared := SharedSession(proj); shared != "" {
		return fmt.Sprintf("%stmux attach -t %s \\; select-window -t %s", tmuxEnvPrefix(), shared, name)
	}
	return fmt.Sprintf("%stmux attach -t %s", tmuxEnvPrefix(), name)
}

// SwitchCmd is the command a human already inside tmux runs to reach the
// agent: a plain `tmux attach` refuses to nest there. It switches the client
// to the agent's session, or in shared mode to its window in the shared
// session. It carries no TMUX_TMPDIR: inside tmux, $TMUX already names the
// server, including a sandbox's private one.
func SwitchCmd(proj *tracker.Project, name string) string {
	return "tmux switch-client -t " + shellWord(Target(proj, name))
}

// AttachHint is the two ways to reach the agent, one per line, for text
// output: from inside tmux, and from a plain shell.
func AttachHint(proj *tracker.Project, name string) string {
	return "Inside tmux:  " + SwitchCmd(proj, name) + "\nFrom a shell: " + AttachCmd(proj, name)
}

// TerminalNone reports whether proj is headless (terminal: none): agents get
// their tmux home and nothing on screen changes.
func TerminalNone(proj *tracker.Project) bool {
	return proj != nil && strings.TrimSpace(proj.Terminal) == "none"
}

// shellWord single-quotes s unless it is safe to type unquoted. A leading
// "=" is not: zsh expands =word to a command's path.
func shellWord(s string) string {
	if safeShellWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tmuxEnvPrefix is "TMUX_TMPDIR=<dir> " when this process uses a private tmux
// server, else "". A dir that isn't safe to type unquoted is single-quoted.
func tmuxEnvPrefix() string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		return ""
	}
	return "TMUX_TMPDIR=" + shellWord(dir) + " "
}

var safeShellWord = regexp.MustCompile(`^[A-Za-z0-9._/:-]+$`)

// CreateTmux creates the tmux home for the agent called name: its own
// session by default, or a new window in the project's shared session
// (creating that session first if it does not exist yet).
func CreateTmux(proj *tracker.Project, name string, workDir string) (string, *exec.Cmd) {
	shared := SharedSession(proj)
	if shared == "" {
		return fmt.Sprintf("Create tmux session in %s", workDir),
			exec.Command("tmux", "new-session", "-d", "-s", name, "-c", workDir)
	}
	if HasSession("=" + shared) {
		return fmt.Sprintf("Create tmux window %s in session %s (%s)", name, shared, workDir),
			exec.Command("tmux", "new-window", "-d", "-t", "="+shared+":", "-n", name, "-c", workDir)
	}
	return fmt.Sprintf("Create tmux session %s with window %s in %s", shared, name, workDir),
		exec.Command("tmux", "new-session", "-d", "-s", shared, "-n", name, "-c", workDir)
}

// KillTmux tears down what CreateTmux made. In shared mode only the agent's
// window goes; the shared session stays.
func KillTmux(proj *tracker.Project, name string) error {
	if SharedSession(proj) != "" {
		return exec.Command("tmux", "kill-window", "-t", Target(proj, name)).Run()
	}
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func defaultSessionAttached(session string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+session+":", "#{session_attached}").Output()
	if err != nil {
		return false
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n > 0
}

// defaultFocusTerminal brings the open terminal for session to the front and
// reports whether it found one. Only i3 can do this: it focuses the window
// titled exactly session (switching workspace if needed), which is how the
// default i3 + alacritty terminal titles the windows it opens.
func defaultFocusTerminal(session string) bool {
	if _, err := exec.LookPath("i3-msg"); err != nil {
		return false
	}
	out, err := exec.Command("i3-msg", fmt.Sprintf(`[title="^%s$"] focus`, regexp.QuoteMeta(session))).CombinedOutput()
	return err == nil && strings.Contains(string(out), `"success":true`)
}

func defaultSendKeys(target string, lines []string) error {
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

func defaultListSessions() []Session {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\t#{session_created}\t#{session_path}").Output()
	if err != nil {
		return nil
	}
	return append(ParseSessions(string(out)), ListSharedWindows()...)
}

// ParseSessions reads `tmux list-sessions -F
// '#{session_name}\t#{session_created}\t#{session_path}'` output.
func ParseSessions(out string) []Session {
	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		session := Session{Name: fields[0]}
		if secs, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			session.StartTime = time.Unix(secs, 0).Format("2006-01-02 15:04:05")
		}
		if len(fields) > 2 {
			session.Path = fields[2]
		}
		sessions = append(sessions, session)
	}
	return sessions
}

// ForProject keeps the listed sessions that belong to proj. Every project's
// agents share one tmux server and the same agent-<slug> / lead-<slug> names,
// so an unscoped list shows other projects' agents in this one (and an equal
// slug in two projects looks like the same agent).
//
// With a shared tmux_session, only that session's windows belong to the
// project. Without one, agents are standalone sessions started in the project
// root or one of its worktrees: a session whose path is known and lies outside
// root is dropped, and shared-session windows (other projects') are too.
func ForProject(proj *tracker.Project, root string, sessions []Session) []Session {
	var kept []Session
	if shared := SharedSession(proj); shared != "" {
		for _, s := range sessions {
			if session, _, ok := strings.Cut(s.Name, ":"); ok && session == shared {
				kept = append(kept, s)
			}
		}
		return kept
	}
	for _, s := range sessions {
		if strings.Contains(s.Name, ":") {
			continue
		}
		if s.Path != "" && root != "" && !underDir(s.Path, root) {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

// underDir reports whether path is dir or inside it.
func underDir(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// ListSharedWindows reports agent and lead windows living inside shared tmux
// sessions (projects with tmux_session set) as "session:window" entries, so
// issue matching and approval notifications find them like regular sessions.
// Windows of per-agent sessions are skipped — those are already listed.
func ListSharedWindows() []Session {
	out, err := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}\t#{window_name}").Output()
	if err != nil {
		return nil
	}
	return ParseSharedWindows(string(out))
}

func ParseSharedWindows(out string) []Session {
	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		sessionName, windowName, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || hasManagedPrefix(sessionName) || !hasManagedPrefix(windowName) {
			continue
		}
		sessions = append(sessions, Session{Name: sessionName + ":" + windowName})
	}
	return sessions
}

// BareName strips a shared session's "session:" prefix from a listed name,
// leaving the agent or lead name.
func BareName(listed string) string {
	if _, window, ok := strings.Cut(listed, ":"); ok {
		return window
	}
	return listed
}

func defaultCapturePane(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-t", target).Output()
	return string(out), err
}

// PromptDeliveryStep polls the agent's pane until the prompt's delivery
// marker appears, and records whether it did. It never re-sends: a missing
// prompt is reported as a failed step for the human to act on.
func PromptDeliveryStep(steps *[]Step, target, prompt, repromptURL string) {
	marker := tracker.PromptDeliveryMarker(prompt)
	if marker == "" {
		return
	}
	deadline := time.Now().Add(PromptDeliveryTimeout)
	for {
		if pane, err := CapturePane(target); err == nil && strings.Contains(pane, marker) {
			*steps = append(*steps, Step{Name: "Prompt delivered", Status: "ok"})
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(promptDeliveryPoll)
	}
	detail := fmt.Sprintf("its last line was not visible in the pane after %s; nothing was re-sent. Check the session, then ", PromptDeliveryTimeout)
	if repromptURL != "" {
		detail += "use Re-send prompt or POST " + repromptURL
	} else {
		detail += "paste the prompt from this response by hand"
	}
	*steps = append(*steps, Step{Name: "Prompt not delivered", Status: "error", Detail: detail})
}

// OpenTerminalStep opens a terminal attached to the given tmux session.
// Uses proj.Terminal if set; falls back to i3+alacritty for backwards compat.
// terminal="none" is headless: appends an info step and returns true.
func OpenTerminalStep(proj *tracker.Project, session string, steps *[]Step) bool {
	terminal := ""
	if proj != nil {
		terminal = proj.Terminal
	}

	if TerminalNone(proj) {
		*steps = append(*steps, Step{Name: "Terminal: headless — attach manually", Status: "ok"})
		return true
	}

	if terminal != "" {
		cmd := strings.ReplaceAll(terminal, "{{session}}", session)
		return RunStep(steps, "Open terminal", exec.Command("sh", "-c", cmd))
	}

	// Backwards compat: i3 + alacritty
	if proj != nil && proj.I3Workspace != "" {
		if !RunStep(steps, fmt.Sprintf("Switch to workspace %s", proj.I3Workspace),
			exec.Command("i3-msg", "workspace", proj.I3Workspace)) {
			return false
		}
	}
	// i3 starts the terminal with its own environment, so a private
	// TMUX_TMPDIR has to be passed on explicitly.
	envFlag := ""
	if p := tmuxEnvPrefix(); p != "" {
		envFlag = "env " + p
	}
	// The window is titled after the session, so a later click can focus it
	// (FocusTerminal) instead of opening another.
	return RunStep(steps, "Open alacritty",
		exec.Command("i3-msg", "exec", fmt.Sprintf("alacritty -T %s -e %stmux attach -t %s", session, envFlag, session)))
}

// AttachStep makes the agent visible to the human. By default it opens a
// terminal on the agent's own session. In shared mode it selects the agent's
// window and only opens a terminal when nobody is attached to the shared
// session yet — otherwise the new window just shows up in the open tmux.
func AttachStep(proj *tracker.Project, name string, steps *[]Step) bool {
	shared := SharedSession(proj)
	if shared == "" {
		return OpenTerminalStep(proj, name, steps)
	}
	RunStep(steps, fmt.Sprintf("Select window %s", name),
		exec.Command("tmux", "select-window", "-t", Target(proj, name)))
	if SessionAttached(shared) {
		*steps = append(*steps, Step{Name: fmt.Sprintf("Session %s already open — no new terminal", shared), Status: "ok"})
		return true
	}
	return OpenTerminalStep(proj, shared, steps)
}

func RunStep(steps *[]Step, name string, cmd *exec.Cmd) bool {
	out, err := cmd.CombinedOutput()
	if err != nil {
		*steps = append(*steps, Step{Name: name, Status: "error", Detail: strings.TrimSpace(string(out))})
		return false
	}
	*steps = append(*steps, Step{Name: name, Status: "ok"})
	return true
}
