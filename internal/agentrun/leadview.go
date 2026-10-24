package agentrun

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// LeadTerminalTmuxWindow is the built-in lead_terminal: mode that shows the
// lead as a tab in the human's own tmux session (lead_window_session).
const LeadTerminalTmuxWindow = "tmux-window"

// leadViewOption tags the tab with the lead it shows, so a second spawn finds
// it instead of adding another.
const leadViewOption = "@iv_lead_view"

// LeadTerminal is the project's lead_terminal: setting, trimmed.
func LeadTerminal(proj *tracker.Project) string {
	if proj == nil {
		return ""
	}
	return strings.TrimSpace(proj.LeadTerminal)
}

// LeadHeadless reports whether leads open nothing: lead_terminal unset or
// none. Workers' terminal: does not count.
func LeadHeadless(proj *tracker.Project) bool {
	lt := LeadTerminal(proj)
	return lt == "" || lt == "none"
}

// LeadViewName names both the tab in lead_window_session and the grouped
// session its nested client shows: view-<lead>. It carries no managed prefix,
// so neither is ever taken for an agent.
func LeadViewName(name string) string {
	return "view-" + name
}

// IsLeadView reports whether a tmux session or window name is a lead view.
func IsLeadView(name string) bool {
	return strings.HasPrefix(name, "view-lead-")
}

// LeadTerminalStep makes the lead called name visible the way lead_terminal:
// says, without moving anyone's focus. ok is false only when a configured
// command fails. shown is false when nothing was opened (a misconfigured
// tmux-window); the caller then hands out the attach hints instead.
func LeadTerminalStep(proj *tracker.Project, name string, steps *[]Step) (ok, shown bool) {
	lt := LeadTerminal(proj)
	if lt == LeadTerminalTmuxWindow {
		return true, leadViewStep(proj, name, steps)
	}
	cmd := ExpandTerminal(proj, lt, leadHome(proj, name), name, RoleLead)
	return RunStep(steps, "Open lead terminal", exec.Command("sh", "-c", cmd)), true
}

// leadHome is the session the lead's window lives in.
func leadHome(proj *tracker.Project, name string) string {
	if shared := SharedSession(proj); shared != "" {
		return shared
	}
	return name
}

// leadViewStep adds a detached tab to lead_window_session running a nested
// client on a session grouped with the lead's home and pinned to the lead's
// window: selecting another window in the home session never moves it, and
// closing the tab removes only the grouped session (destroy-unattached).
// A tab already showing this lead is left as it is.
func leadViewStep(proj *tracker.Project, name string, steps *[]Step) bool {
	session := strings.TrimSpace(proj.LeadWindowSession)
	home := leadHome(proj, name)
	switch {
	case session == "":
		*steps = append(*steps, Step{Name: "No lead tab added", Status: "warning",
			Detail: "lead_terminal: tmux-window needs lead_window_session: <your tmux session> in projects.yaml"})
		return false
	case session == home:
		*steps = append(*steps, Step{Name: fmt.Sprintf("Lead already lives in session %s — no tab added", home), Status: "ok"})
		return true
	case !HasSession("=" + session):
		*steps = append(*steps, Step{Name: "No lead tab added", Status: "warning",
			Detail: fmt.Sprintf("session %s (lead_window_session) is not running", session)})
		return false
	}
	if index, found := findLeadView(session, name); found {
		*steps = append(*steps, Step{Name: fmt.Sprintf("Lead tab already at %s:%s — left as is", session, index), Status: "ok"})
		return true
	}

	view := LeadViewName(name)
	newWindow := func(target string) (string, string, error) {
		args := append([]string{"new-window", "-d", "-P", "-F", "#{window_index}", "-t", target, "-n", view}, leadViewCommand(home, name, view)...)
		out, err := exec.Command("tmux", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), target, err
	}
	var index, target string
	var err error
	if proj.LeadWindow != nil {
		slot := strconv.Itoa(*proj.LeadWindow)
		index, target, err = newWindow("=" + session + ":" + slot)
		if err != nil && strings.Contains(index, "in use") {
			index, target, err = newWindow("=" + session + ":")
			if err == nil {
				*steps = append(*steps, Step{Name: "Lead window slot taken", Status: "warning",
					Detail: fmt.Sprintf("window %s in %s is taken (lead_window) — used %s", slot, session, index)})
			}
		}
	} else {
		index, target, err = newWindow("=" + session + ":")
	}
	if err != nil {
		*steps = append(*steps, Step{Name: "No lead tab added", Status: "warning", Detail: fmt.Sprintf("tmux new-window -t %s: %s", target, index)})
		return false
	}
	exec.Command("tmux", "set-option", "-w", "-t", "="+session+":"+index, leadViewOption, name).Run()
	*steps = append(*steps, Step{Name: fmt.Sprintf("Lead tab added at %s:%s — not selected", session, index), Status: "ok"})
	return true
}

// findLeadView returns the index of the window in session tagged as the tab
// for the lead called name.
func findLeadView(session, name string) (string, bool) {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+session, "-F", "#{window_index}\t#{"+leadViewOption+"}").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if index, tag, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && tag == name {
			return index, true
		}
	}
	return "", false
}

// leadViewCommand is the tab's own command: a nested tmux client (TMUX unset,
// a private TMUX_TMPDIR kept) on the view session grouped with home, which
// selects the lead's window. It is argv, not a shell string, so the "="
// targets never meet zsh's =word expansion; the escaped separators reach the
// nested tmux as plain ";" arguments.
func leadViewCommand(home, name, view string) []string {
	args := []string{"env", "-u", "TMUX"}
	if dir := os.Getenv("TMUX_TMPDIR"); dir != "" {
		args = append(args, "TMUX_TMPDIR="+dir)
	}
	return append(args, "tmux", "new-session", "-A", "-s", view, "-t", "="+home,
		`\;`, "set-option", "destroy-unattached", "on",
		`\;`, "select-window", "-t", ":="+name)
}
