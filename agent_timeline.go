package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
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

	// redacted is the placeholder of a comment's text (clilog.Redact) and
	// end the call's end, so FillRedactedComments can find the comment.
	redacted string
	end      time.Time
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

type cliLogRecord = clilog.Record

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

// LoadAgentTimeline returns the timeline for an issue by merging:
//
//  1. The per-agent clilog at <workDir>/.agent-logs/<assignee>/<assignee>.clilog,
//     which also holds the agent's calls that name no issue.
//  2. The project's clilog at <workDir>/.agent-logs/clilog.jsonl: every call
//     made in the project, filtered to records whose args reference
//     issueSlug, so lead and owner calls on the issue show too.
//  3. The legacy $TMPDIR/issue-cli-logs/actions.jsonl written before v0.54,
//     while it exists, filtered by --project and slug.
//
// Events are merged, deduped on (ts, args), and sorted by timestamp. With an
// empty issueSlug only the per-agent clilog is read. Records written since
// v0.54 carry no free text; FillRedactedComments restores comment text.
func LoadAgentTimeline(workDir, assignee, projSlug, issueSlug string) []TimelineEvent {
	var events []TimelineEvent
	seen := map[string]bool{}

	add := func(rec cliLogRecord) {
		rec.Args = stripGlobalFlags(rec.Args)
		if len(rec.Args) == 0 {
			return
		}
		key := rec.TS + "|" + strings.Join(rec.Args, "\x00")
		if seen[key] {
			return
		}
		seen[key] = true
		events = append(events, timelineEventFromRecord(rec))
	}

	if workDir != "" && assignee != "" {
		readCliLog(clilog.AgentPath(workDir, assignee), add)
	}

	if issueSlug != "" {
		if workDir != "" {
			readCliLog(clilog.ProjectPath(workDir), func(rec cliLogRecord) {
				if cliRecordMatchesIssue(rec, "", issueSlug) {
					add(rec)
				}
			})
		}
		readCliLog(clilog.LegacyPath(), func(rec cliLogRecord) {
			if cliRecordMatchesIssue(rec, projSlug, issueSlug) {
				add(rec)
			}
		})
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

func readCliLog(path string, visit func(cliLogRecord)) {
	clilog.Read(path, visit)
}

// FillRedactedComments gives comment events whose text the clilog redacted
// the text of the issue comment they wrote: the comment stamped within the
// call (comments carry a full timestamp since v0.54), else the one whose
// text has the same placeholder.
func FillRedactedComments(events []TimelineEvent, comments []tracker.Comment) []TimelineEvent {
	used := map[int]bool{}
	for i := range events {
		ev := &events[i]
		if ev.redacted == "" {
			continue
		}
		for _, c := range comments {
			if used[c.ID] || !commentMadeBy(c, ev) {
				continue
			}
			used[c.ID] = true
			setCommentEvent(ev, c.Text)
			break
		}
	}
	return events
}

func commentMadeBy(c tracker.Comment, ev *TimelineEvent) bool {
	if at, ok := tracker.CommentTime(c.Date); ok && !ev.Timestamp.IsZero() {
		if !at.Before(ev.Timestamp.Add(-time.Second)) && !at.After(ev.end.Add(time.Second)) {
			return true
		}
	}
	return clilog.Placeholder(c.Text) == ev.redacted
}

// cliRecordMatchesIssue returns true when the global-log record targets the
// given project (--project flag matches projSlug, or projSlug is empty) AND
// references issueSlug in its positional args. The slug appears as a
// positional arg for every issue-targeting command (start, show, check,
// transition, append, comment, retrospective, checklist, replace, set-meta).
func cliRecordMatchesIssue(rec cliLogRecord, projSlug, issueSlug string) bool {
	if issueSlug == "" {
		return false
	}
	if projSlug != "" {
		flagProj := ""
		for i, a := range rec.Args {
			if a == "--project" && i+1 < len(rec.Args) {
				flagProj = rec.Args[i+1]
				break
			}
		}
		if flagProj != projSlug {
			return false
		}
	}
	stripped := stripGlobalFlags(rec.Args)
	for _, a := range stripped {
		if a == issueSlug {
			return true
		}
	}
	return false
}

func timelineEventFromRecord(rec cliLogRecord) TimelineEvent {
	ts := rec.Start()
	ev := TimelineEvent{Timestamp: ts, end: rec.End()}
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
		if text == "" {
			text = extractFlag(rec.Args, "--body")
		}
		if text == "" && len(rec.Args) > 2 && !strings.HasPrefix(rec.Args[2], "-") {
			text = strings.Join(rec.Args[2:], " ")
		}
		if clilog.IsPlaceholder(text) {
			ev.redacted = text
			ev.Summary = "comment"
			break
		}
		setCommentEvent(&ev, text)
	case "append":
		body := extractFlag(rec.Args, "--body")
		section := extractFlag(rec.Args, "--section")
		switch {
		case section != "":
			ev.Summary = "append to " + section
		case firstHeading(body) != "":
			ev.Summary = firstHeading(body)
		default:
			ev.Summary = "append"
		}
		if !clilog.IsPlaceholder(body) {
			ev.Detail = body
		}
	case "retrospective":
		ev.Summary = "retrospective"
		if body := extractFlag(rec.Args, "--body"); !clilog.IsPlaceholder(body) {
			ev.Detail = body
		}
	default:
		ev.Summary = strings.Join(rec.Args, " ")
	}
	if rec.Exit != nil && *rec.Exit != 0 {
		ev.Summary += " (failed"
		if rec.ErrClass != "" {
			ev.Summary += ": " + strings.ReplaceAll(rec.ErrClass, "_", " ")
		}
		ev.Summary += ")"
	}
	return ev
}

// setCommentEvent fills a comment event from the comment's text: its first
// line as the summary, the rest as detail, and a "comment-<prefix>" kind for
// "tests:", "progress:" and the like.
func setCommentEvent(ev *TimelineEvent, text string) {
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
}

// stripGlobalFlags removes issue-cli's global flags so args[0] is the
// command; a call like `issue-cli --project foo show 42` is logged with
// "--project" as args[0].
func stripGlobalFlags(args []string) []string {
	return clilog.StripGlobal(args)
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
		return truncRunes(s, 117) + "..."
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
