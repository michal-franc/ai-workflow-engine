package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// TimelineEvent is one `issue-cli` invocation during an agent's run on an
// issue, surfaced on the detail view as a summary line with optional
// click-to-expand detail (for append/comment/retrospective bodies).
type TimelineEvent struct {
	Timestamp  time.Time
	TimeLabel  string
	Kind       string
	Summary    string
	Detail     string
	FromStatus string           // transition events only, after enrichment
	ToStatus   string           // transition events only
	Actions    []TimelineAction // populated from workflow.yaml for transition events
}

// TimelineAction describes one action that ran as part of a transition —
// a validation rule, an appended section, an injected prompt, a field set,
// or a human-approval gate. The label is a one-liner; body carries the
// verbatim text the bot saw (prompt, section body, rule spec).
type TimelineAction struct {
	Type  string
	Label string
	Body  string
}

type cliLogRecord struct {
	Args []string `json:"args"`
	TS   string   `json:"ts"`
}

// LoadDispatchPrompt returns the exact prompt the bot was briefed with at
// dispatch time, persisted at <workDir>/.agent-logs/<assignee>/dispatch-prompt.txt
// by startAgentSession. Returns empty string when the file is missing (older
// dispatches, or issues that weren't dispatched through the viewer).
func LoadDispatchPrompt(workDir, assignee string) string {
	if workDir == "" || assignee == "" {
		return ""
	}
	path := filepath.Join(workDir, ".agent-logs", assignee, "dispatch-prompt.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// LoadAgentTimeline reads the per-agent clilog at
// <workDir>/.agent-logs/<assignee>/<assignee>.clilog and returns semantic
// events in file order (oldest first). Returns nil when the log is missing
// or either input is empty.
func LoadAgentTimeline(workDir, assignee string) []TimelineEvent {
	if workDir == "" || assignee == "" {
		return nil
	}
	logPath := filepath.Join(workDir, ".agent-logs", assignee, assignee+".clilog")
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var events []TimelineEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var rec cliLogRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		rec.Args = stripGlobalFlags(rec.Args)
		if len(rec.Args) == 0 {
			continue
		}
		events = append(events, timelineEventFromRecord(rec))
	}
	return events
}

func timelineEventFromRecord(rec cliLogRecord) TimelineEvent {
	ts, _ := time.Parse(time.RFC3339, rec.TS)
	ev := TimelineEvent{Timestamp: ts}
	if ts.IsZero() {
		ev.TimeLabel = rec.TS
	} else {
		ev.TimeLabel = ts.Format("2006-01-02 15:04:05")
	}

	cmd := rec.Args[0]
	ev.Kind = cmd
	switch cmd {
	case "start":
		ev.Summary = "start"
	case "show":
		ev.Summary = "show"
	case "process":
		topic := ""
		if len(rec.Args) > 1 {
			topic = rec.Args[1]
		}
		ev.Summary = "process " + topic
	case "check":
		target := ""
		if len(rec.Args) > 2 {
			target = rec.Args[2]
		}
		ev.Summary = "check: " + target
	case "transition":
		to := extractFlag(rec.Args, "--to")
		ev.ToStatus = to
		ev.Summary = "transition → " + to
	case "comment":
		text := extractFlag(rec.Args, "--text")
		ev.Summary = firstLine(text)
		if strings.Contains(text, "\n") || len(text) > 120 {
			ev.Detail = text
		}
		if idx := strings.Index(text, ":"); idx > 0 && idx < 20 {
			prefix := strings.ToLower(strings.TrimSpace(text[:idx]))
			if isSimpleWord(prefix) {
				ev.Kind = "comment-" + prefix
			}
		}
	case "append":
		body := extractFlag(rec.Args, "--body")
		section := extractFlag(rec.Args, "--section")
		if section != "" {
			ev.Summary = "append to " + section
		} else {
			ev.Summary = firstHeading(body)
			if ev.Summary == "" {
				ev.Summary = "append"
			}
		}
		ev.Detail = body
	case "retrospective":
		ev.Summary = "retrospective"
		ev.Detail = extractFlag(rec.Args, "--body")
	default:
		ev.Summary = strings.Join(rec.Args, " ")
	}
	return ev
}

// stripGlobalFlags removes issue-cli's global flags (--json, --config <val>,
// --project <val>) so the remaining args[0] is the subcommand. logAction
// records os.Args[1:] verbatim, so a call like `issue-cli --project foo show 42`
// lands in the clilog with "--project" as args[0] and the timeline would
// otherwise render that as the command.
func stripGlobalFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			continue
		case "--config", "--project":
			if i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func extractFlag(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

func firstHeading(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			return "append: " + strings.TrimSpace(strings.TrimPrefix(line, "##"))
		}
	}
	return ""
}

func isSimpleWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// EnrichTimelineWithWorkflow attaches workflow-derived action details to
// transition events so the detail view can show the validations, injected
// prompts, and appended section bodies the bot actually saw. initialStatus
// seeds the from-chain for the first transition; each subsequent transition's
// `from` defaults to the previous transition's target. The target status's
// `prompt` (status-level guidance) is appended as an extra action so the
// timeline also surfaces what the agent reads on entering that status.
func EnrichTimelineWithWorkflow(events []TimelineEvent, wf *tracker.WorkflowConfig, initialStatus string) []TimelineEvent {
	if wf == nil {
		return events
	}
	prev := initialStatus
	ruleDocs := ruleDescriptions(wf)
	for i := range events {
		switch events[i].Kind {
		case "transition":
			to := events[i].ToStatus
			if to == "" {
				continue
			}
			tr := matchTransition(wf, prev, to)
			if tr != nil {
				events[i].FromStatus = tr.From
				events[i].Actions = actionsToTimeline(tr.Actions, ruleDocs)
			}
			if prompt := wf.StatusPrompt(to); prompt != "" {
				events[i].Actions = append(events[i].Actions, TimelineAction{
					Type:  "status_prompt",
					Label: "status prompt: " + to,
					Body:  prompt,
				})
			}
			prev = to
		case "start":
			// `issue-cli start` runs the canonical claim-to-work transition.
			// Look it up by conventional target ("in progress") so the
			// timeline can show the same workflow actions the bot received.
			tr := findStartTransition(wf)
			if tr == nil {
				continue
			}
			events[i].FromStatus = tr.From
			events[i].ToStatus = tr.To
			events[i].Summary = "start → " + tr.To
			events[i].Actions = actionsToTimeline(tr.Actions, ruleDocs)
			if prompt := wf.StatusPrompt(tr.To); prompt != "" {
				events[i].Actions = append(events[i].Actions, TimelineAction{
					Type:  "status_prompt",
					Label: "status prompt: " + tr.To,
					Body:  prompt,
				})
			}
			// Don't advance prev — subsequent real transitions drive it.
		}
	}
	return events
}

// findStartTransition returns the workflow transition that `issue-cli start`
// canonically runs. It prefers a transition targeting "in progress" and
// falls back to the first transition whose action set includes a
// `set_fields: assignee` (the claim pattern).
func findStartTransition(wf *tracker.WorkflowConfig) *tracker.WorkflowTransition {
	for i := range wf.Transitions {
		if wf.Transitions[i].To == "in progress" {
			return &wf.Transitions[i]
		}
	}
	for i := range wf.Transitions {
		for _, a := range wf.Transitions[i].Actions {
			if a.Type == "set_fields" && a.Field == "assignee" {
				return &wf.Transitions[i]
			}
		}
	}
	return nil
}

// FirstTransitionFromStatus returns the `from` status of the first transition
// event in the timeline, or empty if there are no transition events. Useful
// for reconstructing the state the agent was in when dispatched.
func FirstTransitionFromStatus(events []TimelineEvent) string {
	for _, ev := range events {
		if ev.Kind == "transition" && ev.FromStatus != "" {
			return ev.FromStatus
		}
	}
	return ""
}

// DispatchEvent synthesizes a "dispatch" event at the top of the timeline
// that shows the base prompt the agent received when it was first briefed.
// basePrompt is whatever the caller reconstructs via buildAgentPrompt or
// similar; the event is skipped if the prompt is empty.
func DispatchEvent(basePrompt string, ts time.Time) TimelineEvent {
	ev := TimelineEvent{
		Timestamp: ts,
		Kind:      "dispatch",
		Summary:   "dispatch — base prompt",
		Detail:    basePrompt,
	}
	if ts.IsZero() {
		ev.TimeLabel = ""
	} else {
		ev.TimeLabel = ts.Format("2006-01-02 15:04:05")
	}
	return ev
}

// matchTransition finds the transition whose target is `to`. Preference order:
// (1) exact from→to match, (2) first transition with the given `to`. Returns
// nil when no transition matches, which happens if the workflow is out of
// sync with the log (e.g. a transition that's since been renamed).
func matchTransition(wf *tracker.WorkflowConfig, from, to string) *tracker.WorkflowTransition {
	var fallback *tracker.WorkflowTransition
	for i := range wf.Transitions {
		t := &wf.Transitions[i]
		if t.To != to {
			continue
		}
		if from != "" && t.From == from {
			return t
		}
		if fallback == nil {
			fallback = t
		}
	}
	return fallback
}

// ruleDescriptions returns a lookup from validation rule name (the part
// before the first colon) to its human description, sourced from the
// drift-guarded `tracker.WorkflowValidationRules` registry.
func ruleDescriptions(wf *tracker.WorkflowConfig) map[string]string {
	m := make(map[string]string, len(tracker.WorkflowValidationRules))
	for _, r := range tracker.WorkflowValidationRules {
		m[r.Name] = r.Description
	}
	return m
}

func actionsToTimeline(actions []tracker.WorkflowAction, ruleDocs map[string]string) []TimelineAction {
	out := make([]TimelineAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, actionToTimeline(a, ruleDocs))
	}
	return out
}

func actionToTimeline(a tracker.WorkflowAction, ruleDocs map[string]string) TimelineAction {
	ta := TimelineAction{Type: a.Type}
	switch a.Type {
	case "validate":
		name, arg := splitRule(a.Rule)
		ta.Label = "validate: " + a.Rule
		if desc := ruleDocs[name]; desc != "" {
			if arg != "" {
				ta.Body = desc + "\n\nArgument: " + arg
			} else {
				ta.Body = desc
			}
		}
	case "require_human_approval":
		ta.Label = "require human approval → " + a.Status
		ta.Body = "Blocks the transition until a human ticks the approval gate for status '" + a.Status + "' in the viewer."
	case "append_section":
		ta.Label = "append section: " + a.Title
		ta.Body = a.Body
	case "inject_prompt":
		ta.Label = "inject prompt"
		ta.Body = a.Prompt
	case "set_fields":
		val := a.Value
		if val == "" {
			val = "(cleared)"
		}
		ta.Label = "set field: " + a.Field + " = " + val
	default:
		ta.Label = a.Type
	}
	return ta
}

func splitRule(rule string) (name, arg string) {
	if i := strings.Index(rule, ":"); i >= 0 {
		return strings.TrimSpace(rule[:i]), strings.TrimSpace(rule[i+1:])
	}
	return strings.TrimSpace(rule), ""
}
