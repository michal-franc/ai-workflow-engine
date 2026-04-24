package tracker

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type WorkflowStatus struct {
	Name        string   `yaml:"name" desc:"Status identifier (e.g. \"in progress\")"`
	Description string   `yaml:"description" desc:"Short one-line description shown in lists and board columns"`
	Prompt      string   `yaml:"prompt,omitempty" desc:"Agent guidance injected when entering this status"`
	Template    string   `yaml:"template" desc:"Legacy body template appended on entry (prefer transitions[].actions[] append_section)"`
	Validation  []string `yaml:"validation" desc:"Legacy shorthand for validation rules (prefer transitions[].actions[] validate)"`
	SideEffects []string `yaml:"side_effects" desc:"Legacy shorthand for side effects (prefer transitions[].actions[] set_fields)"`
	// Optional marks the status as skippable on forward transitions. A transition
	// from A to C is valid if every status strictly between them is Optional.
	Optional bool `yaml:"optional,omitempty" desc:"Skippable on forward transitions; surfaced with a CTA in the viewer"`
	// Global marks the status as an escape hatch: transitions FROM it to any
	// other status are allowed, regardless of the linear lifecycle. Useful for
	// parked states (deferred, blocked, on-hold) that should return to the
	// lifecycle at any point without needing explicit edges for every target.
	Global bool `yaml:"global,omitempty" desc:"Transitions from this status to any status are allowed (no lifecycle constraint)"`
}

type WorkflowAction struct {
	Type   string `yaml:"type" desc:"Action type (see 'Action types' below)"`
	Rule   string `yaml:"rule,omitempty" desc:"Rule spec for type=validate (see 'Validation rules' below)"`
	Status string `yaml:"status,omitempty" desc:"Status for type=require_human_approval"`
	Title  string `yaml:"title,omitempty" desc:"Section title for type=append_section"`
	Body   string `yaml:"body,omitempty" desc:"Section body for type=append_section"`
	Prompt string `yaml:"prompt,omitempty" desc:"Prompt text for type=inject_prompt"`
	Field  string `yaml:"field,omitempty" desc:"Field name for type=set_fields (assignee, priority, status, human_approval)"`
	Value  string `yaml:"value,omitempty" desc:"Field value for type=set_fields (empty string clears)"`
}

type WorkflowField struct {
	Name     string `yaml:"name" json:"name" desc:"Field identifier (also frontmatter key when target=frontmatter)"`
	Prompt   string `yaml:"prompt" json:"prompt" desc:"Human-readable prompt shown in the UI when collecting the answer"`
	Target   string `yaml:"target,omitempty" json:"target,omitempty" desc:"frontmatter (default) or section:<Title> to append under that body section"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty" desc:"Block the transition when the answer is empty"`
	Type     string `yaml:"type,omitempty" json:"type,omitempty" desc:"text (default) or multiline — hint for the UI control"`
}

type WorkflowTransition struct {
	From    string           `yaml:"from" desc:"Source status, or \"*\" to match any source (fallback when no exact edge is defined)"`
	To      string           `yaml:"to" desc:"Target status"`
	Actions []WorkflowAction `yaml:"actions" desc:"Ordered actions run during the transition"`
	Fields  []WorkflowField  `yaml:"fields,omitempty" desc:"Prompts collected from the UI before the transition commits"`
	// CTALabel overrides the default "Divert to <status>" label on the detail-view
	// CTA button that reveals the approval widget for optional-target transitions.
	// Only meaningful when the target status is Optional and the transition has a
	// require_human_approval action.
	CTALabel string `yaml:"cta_label,omitempty" desc:"Override label for the optional-target approval CTA button"`
}

type WorkflowOverlay struct {
	Statuses    []WorkflowStatus     `yaml:"statuses" desc:"Status overrides merged over the base workflow for this system"`
	Transitions []WorkflowTransition `yaml:"transitions" desc:"Transition overrides merged over the base workflow for this system"`
}

type WorkflowBoardConfig struct {
	CardFields []string `yaml:"card_fields" desc:"Frontmatter fields shown on board cards (default: [system, labels])"`
	Columns    []string `yaml:"columns" desc:"Ordered board column names (default: the status lifecycle)"`
}

// ScoringPriority maps priority values (low/medium/high/critical) to weight in points.
type ScoringPriority map[string]float64

// ScoringLabels maps label names to weight in points; unlisted labels contribute 0.
type ScoringLabels map[string]float64

// ScoringDueDate governs the urgency contribution from the `due` frontmatter field.
// Formula: min(overdue_cap, max(0, 30 - days_until_due) * urgency_weight).
type ScoringDueDate struct {
	UrgencyWeight float64 `yaml:"urgency_weight,omitempty" desc:"Points per day under the 30-day horizon"`
	OverdueCap    float64 `yaml:"overdue_cap,omitempty" desc:"Maximum points the due-date term can contribute"`
}

// ScoringAge governs the staleness contribution from the `created` frontmatter field.
type ScoringAge struct {
	StalenessWeight float64 `yaml:"staleness_weight,omitempty" desc:"Points per day since created"`
}

// ScoringFormula holds the per-component weights used by score computation.
type ScoringFormula struct {
	Priority ScoringPriority `yaml:"priority,omitempty" desc:"Priority → points map (e.g. critical: 40, high: 20)"`
	DueDate  ScoringDueDate  `yaml:"due_date,omitempty" desc:"Due-date urgency weights and cap"`
	Age      ScoringAge      `yaml:"age,omitempty" desc:"Staleness weight from created date"`
	Labels   ScoringLabels   `yaml:"labels,omitempty" desc:"Label → points map (summed across issue labels)"`
}

// ScoringConfig controls the ticket scoring system. When Enabled is false (or
// the block is absent) no score is computed or displayed.
type ScoringConfig struct {
	Enabled     bool           `yaml:"enabled,omitempty" desc:"Opt-in: compute and render scores only when true"`
	Formula     ScoringFormula `yaml:"formula,omitempty" desc:"Per-component weights"`
	DefaultSort string         `yaml:"default_sort,omitempty" desc:"Initial sort on list/board: score_desc, created_desc, updated_desc"`
}

type WorkflowConfig struct {
	Statuses    []WorkflowStatus           `yaml:"statuses" desc:"Status lifecycle definitions"`
	Transitions []WorkflowTransition       `yaml:"transitions" desc:"Transition rules between statuses"`
	Systems     map[string]WorkflowOverlay `yaml:"systems" desc:"Per-system overrides keyed by system name"`
	Board       WorkflowBoardConfig        `yaml:"board" desc:"Board display configuration"`
	Scoring     ScoringConfig              `yaml:"scoring,omitempty" desc:"Ticket scoring policy (opt-in)"`
}

var defaultBoardCardFields = []string{"system", "labels"}

func (w *WorkflowConfig) GetBoardCardFields() []string {
	if len(w.Board.CardFields) > 0 {
		return w.Board.CardFields
	}
	return defaultBoardCardFields
}

func (w *WorkflowConfig) GetBoardColumns() []string {
	if len(w.Board.Columns) > 0 {
		return w.Board.Columns
	}
	return w.GetStatusOrder()
}

type TransitionResult struct {
	Update          IssueUpdate `json:"update"`
	BodyChanged     bool        `json:"body_changed"`
	BodyAppended    bool        `json:"body_appended"`
	ClearedApproval bool        `json:"cleared_approval"`
	InjectedPrompts []string    `json:"injected_prompts"`
}

type TransitionPreviewStep struct {
	ActionType string         `json:"action_type"`
	Action     WorkflowAction `json:"action"`
	Outcome    string         `json:"outcome"`
	Summary    string         `json:"summary"`
	Message    string         `json:"message,omitempty"`
}

type TransitionPreview struct {
	From            string                  `json:"from"`
	To              string                  `json:"to"`
	System          string                  `json:"system,omitempty"`
	Allowed         bool                    `json:"allowed"`
	ValidationError string                  `json:"validation_error,omitempty"`
	Steps           []TransitionPreviewStep `json:"steps"`
	Fields          []WorkflowField         `json:"fields,omitempty"`
	Result          TransitionResult        `json:"result"`
}

func stringPtr(s string) *string {
	v := s
	return &v
}

func LoadWorkflow(path string) (*WorkflowConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading workflow %s: %w", path, err)
	}

	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing workflow %s: %w", path, err)
	}

	return &cfg, nil
}

func DefaultWorkflow() *WorkflowConfig {
	return &WorkflowConfig{
		Statuses: []WorkflowStatus{
			{
				Name:        "idea",
				Description: "Raw idea, needs exploration",
				Prompt:      "Clarify the issue with the human before moving into structured design.\nAsk focused questions to remove ambiguity around goals, scope, and success criteria.",
			},
			{
				Name:        "in design",
				Description: "Being designed and specced out",
				Prompt:      "Review the relevant context before proposing changes.\nTurn the problem into explicit checklists, assumptions, and open questions.\nWhen the design is complete, stop and ask for backlog approval in the issue viewer before attempting the transition.",
			},
			{
				Name:        "backlog",
				Description: "Ready to work on",
				Prompt:      "This is a handoff state.\nDo not run `issue-cli start` until a human approves `in progress` in the issue viewer.",
			},
			{
				Name:        "in progress",
				Description: "Actively being implemented",
				Prompt:      "Implement the accepted design and keep the issue body, implementation checklist, and test plan accurate as work progresses.",
			},
			{
				Name:        "testing",
				Description: "Under verification",
				Prompt:      "Build or update the relevant automated coverage, record concrete test evidence, and surface any remaining manual checks clearly.",
			},
			{
				Name:        "human-testing",
				Description: "Manual verification by humans",
				Prompt:      "Stop here and wait for human verification.\nMake sure the manual checks are explicit, minimal, and reproducible.",
			},
			{
				Name:        "documentation",
				Description: "Being documented",
				Prompt:      "Update the relevant docs for the change and record the documentation work clearly.",
			},
			{
				Name:        "shipping",
				Description: "Committing and pushing changes",
				Prompt:      "Commit the changes with a message referencing the issue, push the commit to the remote, and note a PR link if one is opened.\nStop and ask for `done` approval in the issue viewer once the commit is pushed.",
			},
			{
				Name:        "done",
				Description: "Completed",
			},
		},
		Transitions: []WorkflowTransition{
			{
				From: "idea",
				To:   "in design",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "body_not_empty"},
					{Type: "append_section", Title: "Idea", Body: "- [ ] Problem described clearly\n- [ ] Scope defined\n- [ ] Constraints and open questions captured"},
					{Type: "append_section", Title: "Design", Body: "- [ ] Approach documented\n- [ ] Dependencies and risks identified\n- [ ] Human approval requested for backlog"},
					{Type: "append_section", Title: "Acceptance Criteria", Body: "- [ ] First observable requirement\n- [ ] Second observable requirement"},
				},
			},
			{
				From: "in design",
				To:   "backlog",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "section_checkboxes_checked: Design"},
					{Type: "validate", Rule: "section_has_checkboxes: Acceptance Criteria"},
					{Type: "require_human_approval", Status: "backlog"},
					{Type: "set_fields", Field: "assignee", Value: ""},
				},
			},
			{
				From: "backlog",
				To:   "in progress",
				Actions: []WorkflowAction{
					{Type: "require_human_approval", Status: "in progress"},
					{Type: "validate", Rule: "has_assignee"},
					{Type: "append_section", Title: "Implementation", Body: "- [ ] Code changes complete\n- [ ] Automated tests added or updated where practical\n- [ ] Cross-cutting impact reviewed where relevant"},
					{Type: "append_section", Title: "Test Plan", Body: "### Automated\n- [ ] Automated verification recorded\n\n### Manual\n- [ ] Manual verification steps listed if needed"},
					{Type: "inject_prompt", Prompt: "Implement the issue, update the Implementation section as work progresses, and keep the test plan concrete.\nCall out cross-cutting impact early if the change affects shared state, shared config, or behavior outside the immediate subsystem."},
				},
			},
			{
				From: "in progress",
				To:   "testing",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "section_checkboxes_checked: Implementation"},
					{Type: "validate", Rule: "has_test_plan"},
					{Type: "append_section", Title: "Testing", Body: "- [ ] Relevant tests for changed code passing\n- [ ] Known unrelated failures documented if full suite is red\n- [ ] Test results logged with `issue-cli comment <slug> --text \"tests: ...\"`"},
					{Type: "inject_prompt", Prompt: "Verify the implementation, run the relevant tests for the changed code, and record the results in a `tests:` comment before continuing. If unrelated failures block the full suite, document them explicitly."},
				},
			},
			{
				From: "testing",
				To:   "human-testing",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "has_test_plan"},
					{Type: "validate", Rule: "section_checkboxes_checked: Testing"},
					{Type: "validate", Rule: "has_comment_prefix: tests:"},
					{Type: "append_section", Title: "Human Testing", Body: "- [ ] Manual verification completed by a human if required"},
				},
			},
			{
				From: "human-testing",
				To:   "documentation",
				Actions: []WorkflowAction{
					{Type: "require_human_approval", Status: "documentation"},
					{Type: "append_section", Title: "Documentation", Body: "- [ ] User-facing docs updated if behavior changed\n- [ ] `docs:` comment prepared with the documentation changes"},
				},
			},
			{
				From: "documentation",
				To:   "shipping",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "section_checkboxes_checked: Documentation"},
					{Type: "validate", Rule: "has_comment_prefix: docs:"},
					{Type: "append_section", Title: "Shipping", Body: "- [ ] Changes committed with a message referencing the issue\n- [ ] Commit pushed to the remote\n- [ ] PR opened if applicable"},
				},
			},
			{
				From: "shipping",
				To:   "done",
				Actions: []WorkflowAction{
					{Type: "validate", Rule: "section_checkboxes_checked: Shipping"},
					{Type: "require_human_approval", Status: "done"},
				},
			},
		},
		Systems: map[string]WorkflowOverlay{
			"Combat": {
				Transitions: []WorkflowTransition{
					{
						From: "backlog",
						To:   "in progress",
						Actions: []WorkflowAction{
							{Type: "inject_prompt", Prompt: "Check combat edge cases, balance implications, and regression coverage before closing implementation."},
						},
					},
				},
			},
			"UI": {
				Statuses: []WorkflowStatus{
					{
						Name:   "in design",
						Prompt: "Review the relevant context before proposing changes.\nTurn the problem into explicit checklists, assumptions, and open questions.\nFor UI work that launches local tools or editors, state whether to reuse existing tmux/alacritty patterns, whether handlers may block on local processes, and what feedback the user sees after launch.\nWhen the design is complete, stop and ask for backlog approval in the issue viewer before attempting the transition.",
					},
				},
			},
			"API": {
				Statuses: []WorkflowStatus{
					{
						Name:   "in design",
						Prompt: "Review the relevant context before proposing changes.\nTurn the problem into explicit checklists, assumptions, and open questions.\nFor API changes that affect UI-visible state, state the source of truth, whether server-side polling plus `/hash` is the expected refresh path, and whether manual browser verification is required before human-testing.\nWhen the design is complete, stop and ask for backlog approval in the issue viewer before attempting the transition.",
					},
				},
			},
			"CLI": {
				Statuses: []WorkflowStatus{
					{
						Name:   "in design",
						Prompt: "Review the relevant context before proposing changes.\nTurn the problem into explicit checklists, assumptions, and open questions.\nDocument the output contract early: whether text is human-facing or agent-facing, whether script compatibility matters, and whether `--json` or other machine-readable output is in scope for this issue.\nWhen the design is complete, stop and ask for backlog approval in the issue viewer before attempting the transition.",
					},
				},
			},
		},
	}
}

func (w *WorkflowConfig) RequiredHumanApproval(fromStatus, toStatus string) string {
	for _, action := range w.transitionActions(fromStatus, toStatus) {
		if action.Type != "require_human_approval" {
			continue
		}
		status := strings.TrimSpace(action.Status)
		if status == "" {
			return toStatus
		}
		return status
	}
	return ""
}

func (w *WorkflowConfig) GetStatusOrder() []string {
	order := make([]string, len(w.Statuses))
	for i, s := range w.Statuses {
		order[i] = s.Name
	}
	return order
}

func (w *WorkflowConfig) GetStatusDescriptions() map[string]string {
	descs := make(map[string]string, len(w.Statuses))
	for _, s := range w.Statuses {
		descs[s.Name] = s.Description
	}
	return descs
}

func (w *WorkflowConfig) GetStatusIndex(status string) int {
	for i, s := range w.Statuses {
		if s.Name == status {
			return i
		}
	}
	return -1
}

func (w *WorkflowConfig) IsValidTransition(from, to string) bool {
	fi := w.GetStatusIndex(from)
	ti := w.GetStatusIndex(to)
	if fi == -1 || ti == -1 {
		return false
	}
	if w.ResolveTransition(from, to) != nil {
		return true
	}
	// Global source: no lifecycle constraint on transitions away from it.
	if fs := w.GetStatus(from); fs != nil && fs.Global {
		return true
	}
	if ti <= fi {
		return false
	}
	// Forward jumps are valid when every strictly-intermediate status is optional.
	for i := fi + 1; i < ti; i++ {
		if !w.Statuses[i].Optional {
			return false
		}
	}
	return true
}

func (w *WorkflowConfig) GetStatus(name string) *WorkflowStatus {
	for i := range w.Statuses {
		if w.Statuses[i].Name == name {
			return &w.Statuses[i]
		}
	}
	return nil
}

// GetTransition returns the transition with an exact (from, to) match or nil.
// Use ResolveTransition when callers should accept wildcard (from: "*") fallbacks;
// Merge and other structural operations must use GetTransition to avoid
// accidentally mutating a global rule when a specific edge was intended.
func (w *WorkflowConfig) GetTransition(from, to string) *WorkflowTransition {
	for i := range w.Transitions {
		t := &w.Transitions[i]
		if t.From == from && t.To == to {
			return t
		}
	}
	return nil
}

// ResolveTransition returns the transition that governs (from, to): an exact
// (from, to) match if one exists, otherwise the wildcard (from: "*", to)
// fallback. Exact edges win over wildcards, so a specific rule can override a
// global one for a particular source status.
func (w *WorkflowConfig) ResolveTransition(from, to string) *WorkflowTransition {
	if t := w.GetTransition(from, to); t != nil {
		return t
	}
	for i := range w.Transitions {
		t := &w.Transitions[i]
		if t.From == "*" && t.To == to {
			return t
		}
	}
	return nil
}

func (w *WorkflowConfig) Clone() *WorkflowConfig {
	if w == nil {
		return nil
	}

	clone := &WorkflowConfig{
		Statuses:    append([]WorkflowStatus(nil), w.Statuses...),
		Transitions: make([]WorkflowTransition, len(w.Transitions)),
		Systems:     make(map[string]WorkflowOverlay, len(w.Systems)),
		Board:       w.Board,
		Scoring:     w.Scoring,
	}
	for i := range w.Transitions {
		clone.Transitions[i] = WorkflowTransition{
			From:     w.Transitions[i].From,
			To:       w.Transitions[i].To,
			Actions:  append([]WorkflowAction(nil), w.Transitions[i].Actions...),
			Fields:   append([]WorkflowField(nil), w.Transitions[i].Fields...),
			CTALabel: w.Transitions[i].CTALabel,
		}
	}
	for name, overlay := range w.Systems {
		clonedOverlay := WorkflowOverlay{
			Statuses:    append([]WorkflowStatus(nil), overlay.Statuses...),
			Transitions: make([]WorkflowTransition, len(overlay.Transitions)),
		}
		for i := range overlay.Transitions {
			clonedOverlay.Transitions[i] = WorkflowTransition{
				From:     overlay.Transitions[i].From,
				To:       overlay.Transitions[i].To,
				Actions:  append([]WorkflowAction(nil), overlay.Transitions[i].Actions...),
				Fields:   append([]WorkflowField(nil), overlay.Transitions[i].Fields...),
				CTALabel: overlay.Transitions[i].CTALabel,
			}
		}
		clone.Systems[name] = clonedOverlay
	}
	return clone
}

func (w *WorkflowConfig) ForSystem(system string) *WorkflowConfig {
	if w == nil {
		return nil
	}
	if strings.TrimSpace(system) == "" || len(w.Systems) == 0 {
		return w.Clone()
	}

	overlay, ok := w.Systems[system]
	if !ok {
		return w.Clone()
	}

	merged := w.Clone()
	merged.Merge(&WorkflowConfig{
		Statuses:    overlay.Statuses,
		Transitions: overlay.Transitions,
	})
	return merged
}

func (w *WorkflowConfig) TemplateForStatus(name string) string {
	s := w.GetStatus(name)
	if s == nil {
		return ""
	}
	return strings.TrimRight(s.Template, "\n")
}

func (w *WorkflowConfig) StatusPrompt(name string) string {
	s := w.GetStatus(name)
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.Prompt)
}

func (w *WorkflowConfig) transitionActions(from, to string) []WorkflowAction {
	if t := w.ResolveTransition(from, to); t != nil {
		return append([]WorkflowAction(nil), t.Actions...)
	}

	s := w.GetStatus(to)
	if s == nil {
		return nil
	}

	var actions []WorkflowAction
	for _, rule := range s.Validation {
		if strings.HasPrefix(rule, "approved_for: ") || strings.HasPrefix(rule, "human_approval: ") {
			status := strings.TrimSpace(strings.TrimPrefix(rule, "approved_for: "))
			if strings.HasPrefix(rule, "human_approval: ") {
				status = strings.TrimSpace(strings.TrimPrefix(rule, "human_approval: "))
			}
			actions = append(actions, WorkflowAction{
				Type:   "require_human_approval",
				Status: status,
			})
			continue
		}
		actions = append(actions, WorkflowAction{Type: "validate", Rule: rule})
	}
	for _, effect := range s.SideEffects {
		switch effect {
		case "clear_assignee":
			actions = append(actions, WorkflowAction{Type: "set_fields", Field: "assignee", Value: ""})
		}
	}
	return actions
}

func (w *WorkflowConfig) TransitionPrompts(from, to string) []string {
	var prompts []string
	for _, action := range w.transitionActions(from, to) {
		if action.Type == "inject_prompt" && strings.TrimSpace(action.Prompt) != "" {
			prompts = append(prompts, strings.TrimSpace(action.Prompt))
		}
	}
	return prompts
}

func (w *WorkflowConfig) EntryPrompts(from, to string) []string {
	var prompts []string
	if statusPrompt := w.StatusPrompt(to); statusPrompt != "" {
		prompts = append(prompts, statusPrompt)
	}
	prompts = append(prompts, w.TransitionPrompts(from, to)...)
	return prompts
}

// AppendTemplate appends the status template to the body if not already present.
// Returns the new body and whether anything was appended.
func (w *WorkflowConfig) AppendTemplate(body, status string) (string, bool) {
	tmpl := w.TemplateForStatus(status)
	if tmpl == "" {
		return body, false
	}

	// Duplicate guard: check if the first line (section heading) already exists
	firstLine := strings.SplitN(tmpl, "\n", 2)[0]
	if strings.Contains(body, firstLine) {
		return body, false
	}

	body = strings.TrimRight(body, "\n")
	if body != "" {
		body += "\n\n"
	}
	body += tmpl + "\n"
	return body, true
}

func normalizeHeading(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if strings.HasPrefix(title, "#") {
		return title
	}
	return "## " + title
}

type headingMatch struct {
	StartLine int
	EndLine   int
	Level     int
	Line      string
	Key       string
}

func parseHeadingLine(line string) (level int, title string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(trimmed) || trimmed[i] != ' ' {
		return 0, "", false
	}
	title = strings.TrimSpace(trimmed[i+1:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	if title == "" {
		return 0, "", false
	}
	return i, title, true
}

func normalizeHeadingKey(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if _, parsed, ok := parseHeadingLine(title); ok {
		title = parsed
	}
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func findHeadingMatches(body, title string) []headingMatch {
	lines := strings.Split(body, "\n")
	key := normalizeHeadingKey(title)
	if key == "" {
		return nil
	}

	var matches []headingMatch
	for i, line := range lines {
		level, parsedTitle, ok := parseHeadingLine(line)
		if !ok || normalizeHeadingKey(parsedTitle) != key {
			continue
		}

		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			nextLevel, _, ok := parseHeadingLine(lines[j])
			if ok && nextLevel <= level {
				end = j
				break
			}
		}

		matches = append(matches, headingMatch{
			StartLine: i,
			EndLine:   end,
			Level:     level,
			Line:      strings.TrimSpace(line),
			Key:       key,
		})
	}
	return matches
}

func appendContentToMatch(body string, match headingMatch, content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return body, false
	}

	lines := strings.Split(body, "\n")
	sectionLines := append([]string(nil), lines[match.StartLine:match.EndLine]...)
	sectionBody := strings.TrimSpace(strings.Join(sectionLines[1:], "\n"))
	if sectionBody != "" {
		if strings.Contains(sectionBody, content) {
			return body, false
		}
		sectionBody = strings.TrimRight(sectionBody, "\n") + "\n\n" + content
	} else {
		sectionBody = content
	}

	replacement := []string{strings.TrimRight(sectionLines[0], "\n"), sectionBody}
	newLines := append([]string(nil), lines[:match.StartLine]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[match.EndLine:]...)
	return strings.TrimRight(strings.Join(newLines, "\n"), "\n") + "\n", true
}

func appendToSection(body, title, content string) (string, bool) {
	heading := normalizeHeading(title)
	content = strings.TrimSpace(content)
	if heading == "" || content == "" {
		return body, false
	}

	matches := findHeadingMatches(body, title)
	switch len(matches) {
	case 0:
		body = strings.TrimRight(body, "\n")
		if body != "" {
			body += "\n\n"
		}
		body += heading + "\n" + content + "\n"
		return body, true
	default:
		return body, false
	}
}

// ValidateTransition checks whether an issue meets all validation rules for a transition.
// Returns nil if valid, or an error describing what's missing.
func (w *WorkflowConfig) ValidateTransition(issue *Issue, fromStatus, toStatus string, comments []Comment) error {
	if w.GetStatus(toStatus) == nil {
		return fmt.Errorf("unknown status %q", toStatus)
	}

	for _, action := range w.transitionActions(fromStatus, toStatus) {
		switch action.Type {
		case "validate":
			if err := w.checkRule(action.Rule, issue, comments); err != nil {
				return err
			}
		case "require_human_approval":
			status := strings.TrimSpace(action.Status)
			if status == "" {
				status = toStatus
			}
			if !strings.EqualFold(issue.HumanApproval, status) {
				return fmt.Errorf("issue is not human-approved for %q — a human must approve it in the issue viewer first", status)
			}
		}
	}
	return nil
}

// Validate is kept for compatibility with older call sites and tests.
func (w *WorkflowConfig) Validate(issue *Issue, toStatus string, comments []Comment) error {
	fromStatus := ""
	if idx := w.GetStatusIndex(toStatus); idx > 0 {
		fromStatus = w.Statuses[idx-1].Name
	}
	return w.ValidateTransition(issue, fromStatus, toStatus, comments)
}

func (w *WorkflowConfig) PreviewTransition(issue *Issue, fromStatus, toStatus, system string, comments []Comment) TransitionPreview {
	preview := TransitionPreview{
		From:    fromStatus,
		To:      toStatus,
		System:  strings.TrimSpace(system),
		Allowed: true,
	}

	if w.GetStatus(toStatus) == nil {
		preview.Allowed = false
		preview.ValidationError = fmt.Sprintf("unknown status %q", toStatus)
		return preview
	}

	working := *issue
	preview.Fields = w.TransitionFields(fromStatus, toStatus)
	actions := w.transitionActions(fromStatus, toStatus)
	for _, action := range actions {
		step := TransitionPreviewStep{
			ActionType: action.Type,
			Action:     action,
			Outcome:    "info",
			Summary:    action.Type,
		}

		switch action.Type {
		case "validate":
			step.Summary = validationSummary(action.Rule)
			if err := w.checkRule(action.Rule, &working, comments); err != nil {
				step.Outcome = "failed"
				step.Message = err.Error()
				preview.Allowed = false
				preview.ValidationError = err.Error()
				preview.Steps = append(preview.Steps, step)
				return preview
			}
			step.Outcome = "passed"
			step.Message = "Validation passed"
		case "require_human_approval":
			status := strings.TrimSpace(action.Status)
			if status == "" {
				status = toStatus
			}
			step.Summary = fmt.Sprintf("Require approval for %s", status)
			if !strings.EqualFold(working.HumanApproval, status) {
				step.Outcome = "failed"
				step.Message = fmt.Sprintf("Issue is not human-approved for %q", status)
				preview.Allowed = false
				preview.ValidationError = fmt.Sprintf("issue is not human-approved for %q", status)
				preview.Steps = append(preview.Steps, step)
				return preview
			}
			step.Outcome = "passed"
			step.Message = fmt.Sprintf("Issue human-approved for %q", status)
		case "append_section":
			step.Summary = fmt.Sprintf("Append section %s", action.Title)
			newBody, changed := appendToSection(working.BodyRaw, action.Title, action.Body)
			if changed {
				working.BodyRaw = newBody
				step.Outcome = "changed"
				step.Message = "Section would be created"
			} else {
				step.Outcome = "skipped"
				step.Message = "Section already exists or action is empty"
			}
		case "inject_prompt":
			step.Summary = "Inject prompt"
			if strings.TrimSpace(action.Prompt) == "" {
				step.Outcome = "skipped"
				step.Message = "Prompt is empty"
			} else {
				step.Outcome = "changed"
				step.Message = strings.TrimSpace(action.Prompt)
			}
		case "set_fields":
			step.Summary = fmt.Sprintf("Set field %s", action.Field)
			step.Outcome = "changed"
			if action.Value == "" {
				step.Message = fmt.Sprintf("%s would be cleared", action.Field)
			} else {
				step.Message = fmt.Sprintf("%s would be set to %q", action.Field, action.Value)
			}
		default:
			step.Message = "Action type preview not implemented"
		}

		preview.Steps = append(preview.Steps, step)
	}

	result := w.ApplyTransition(&working, fromStatus, toStatus)
	preview.Result = result
	return preview
}

func validationSummary(rule string) string {
	ruleName := rule
	arg := ""
	if idx := strings.Index(rule, ": "); idx != -1 {
		ruleName = rule[:idx]
		arg = rule[idx+2:]
	}

	switch ruleName {
	case "body_not_empty":
		return "Validate issue body is not empty"
	case "has_checkboxes":
		return "Validate issue has checkboxes"
	case "section_has_checkboxes":
		if arg == "" {
			return "Validate section has checkboxes"
		}
		return fmt.Sprintf("Validate section %s has checkboxes", arg)
	case "has_assignee":
		return "Validate issue has assignee"
	case "all_checkboxes_checked":
		return "Validate all checkboxes are checked"
	case "section_checkboxes_checked":
		if arg == "" {
			return "Validate section checkboxes are checked"
		}
		return fmt.Sprintf("Validate section %s checkboxes are checked", arg)
	case "has_test_plan":
		return "Validate test plan is present"
	case "has_comment_prefix":
		if arg == "" {
			return "Validate required comment prefix exists"
		}
		return fmt.Sprintf("Validate comment starts with %s", arg)
	case "approved_for", "human_approval":
		if arg == "" {
			return "Validate issue has human approval"
		}
		return fmt.Sprintf("Validate issue human-approved for %s", arg)
	default:
		return fmt.Sprintf("Validate %s", rule)
	}
}

func (w *WorkflowConfig) checkRule(rule string, issue *Issue, comments []Comment) error {
	// Parse "rule_name: arg" format
	ruleName := rule
	ruleArg := ""
	if idx := strings.Index(rule, ": "); idx != -1 {
		ruleName = rule[:idx]
		ruleArg = rule[idx+2:]
	}

	switch ruleName {
	case "body_not_empty":
		if strings.TrimSpace(issue.BodyRaw) == "" {
			return fmt.Errorf("issue body is empty — add a description first")
		}
	case "has_checkboxes":
		total, _ := CountCheckboxes(issue.BodyRaw)
		if total == 0 {
			return fmt.Errorf("no checkboxes found — add acceptance criteria as checkboxes:\n\n  - [ ] First requirement\n  - [ ] Second requirement")
		}
	case "section_has_checkboxes":
		if ruleArg == "" {
			return fmt.Errorf("section_has_checkboxes rule requires a section name argument")
		}
		total, _ := CountCheckboxesInSection(issue.BodyRaw, ruleArg)
		if total == 0 {
			return fmt.Errorf("no checkboxes found in section %q — add explicit checklist items there", ruleArg)
		}
	case "has_assignee":
		if issue.Assignee == "" {
			return fmt.Errorf("no assignee — claim the issue first:\n\n  issue-cli claim %s --assignee \"your-name\"", issue.Slug)
		}
	case "all_checkboxes_checked":
		total, checked := CountCheckboxes(issue.BodyRaw)
		if total > 0 && checked < total {
			return fmt.Errorf("%d/%d checkboxes incomplete:\n\n  issue-cli checklist %s", checked, total, issue.Slug)
		}
	case "section_checkboxes_checked":
		if ruleArg == "" {
			return fmt.Errorf("section_checkboxes_checked rule requires a section name argument")
		}
		total, checked := CountCheckboxesInSection(issue.BodyRaw, ruleArg)
		if total == 0 {
			// Section missing or has no checkboxes — skip silently.
			// The section may not exist if the issue was created without that template.
			return nil
		}
		if checked < total {
			return fmt.Errorf("%d/%d checkboxes incomplete in section %q:\n\n  issue-cli checklist %s", checked, total, ruleArg, issue.Slug)
		}
	case "has_test_plan":
		hasAuto, hasManual := HasTestPlan(issue.BodyRaw)
		if !hasAuto || !hasManual {
			return fmt.Errorf("missing test plan — add one with:\n  issue-cli append <slug> --body \"## Test Plan\\n\\n### Automated\\n- test description\\n\\n### Manual\\n- verification step\"")
		}
	case "has_comment_prefix":
		if ruleArg == "" {
			return fmt.Errorf("has_comment_prefix rule requires an argument")
		}
		if !HasCommentWithPrefix(comments, ruleArg) {
			return fmt.Errorf("no comment starting with %q — add one:\n\n  issue-cli comment %s --text \"%s ...\"", ruleArg, issue.Slug, ruleArg)
		}
	case "approved_for", "human_approval":
		if ruleArg == "" {
			return fmt.Errorf("human_approval rule requires a status argument")
		}
		if !strings.EqualFold(issue.HumanApproval, ruleArg) {
			return fmt.Errorf("issue is not human-approved for %q — a human must approve it in the issue viewer first", ruleArg)
		}
	default:
		return fmt.Errorf("unknown validation rule: %s", ruleName)
	}
	return nil
}

func (w *WorkflowConfig) ApplyTransition(issue *Issue, fromStatus, toStatus string) TransitionResult {
	return w.ApplyTransitionWithFields(issue, fromStatus, toStatus, nil)
}

func (w *WorkflowConfig) ApplyTransitionWithFields(issue *Issue, fromStatus, toStatus string, fieldValues map[string]string) TransitionResult {
	result := TransitionResult{
		Update: IssueUpdate{
			Status: stringPtr(toStatus),
		},
	}

	for _, action := range w.transitionActions(fromStatus, toStatus) {
		switch action.Type {
		case "append_section":
			newBody, changed := appendToSection(issue.BodyRaw, action.Title, action.Body)
			if changed {
				result.Update.Body = &newBody
				issue.BodyRaw = newBody
				result.BodyChanged = true
				result.BodyAppended = true
			}
		case "inject_prompt":
			if strings.TrimSpace(action.Prompt) != "" {
				result.InjectedPrompts = append(result.InjectedPrompts, strings.TrimSpace(action.Prompt))
			}
		case "set_fields":
			switch action.Field {
			case "assignee":
				result.Update.Assignee = stringPtr(action.Value)
			case "human_approval", "approved_for":
				result.Update.HumanApproval = stringPtr(action.Value)
			case "priority":
				result.Update.Priority = stringPtr(action.Value)
			case "status":
				result.Update.Status = stringPtr(action.Value)
			}
		}
	}

	if tmplBody, appended := w.AppendTemplate(issue.BodyRaw, toStatus); appended {
		result.Update.Body = &tmplBody
		issue.BodyRaw = tmplBody
		result.BodyChanged = true
		result.BodyAppended = true
	}

	for _, field := range w.TransitionFields(fromStatus, toStatus) {
		answer := strings.TrimSpace(fieldValues[field.Name])
		if answer == "" {
			continue
		}
		target := strings.TrimSpace(field.Target)
		switch {
		case target == "" || target == "frontmatter":
			if result.Update.ExtraFields == nil {
				result.Update.ExtraFields = map[string]string{}
			}
			result.Update.ExtraFields[field.Name] = answer
		case strings.HasPrefix(target, "section:"):
			sectionTitle := strings.TrimSpace(strings.TrimPrefix(target, "section:"))
			if sectionTitle == "" {
				continue
			}
			line := fmt.Sprintf("- **%s:** %s", field.Prompt, answer)
			newBody, changed, err := AppendIssueBodyToSection(issue.BodyRaw, sectionTitle, line, true)
			if err != nil {
				continue
			}
			if changed {
				result.Update.Body = &newBody
				issue.BodyRaw = newBody
				result.BodyChanged = true
				result.BodyAppended = true
			}
		}
	}

	if issue.HumanApproval != "" {
		result.Update.HumanApproval = stringPtr("")
		result.ClearedApproval = true
	}

	return result
}

// TransitionFields returns the declarative fields[] for the given transition,
// falling back to a wildcard (from: "*") edge when no exact match exists.
// The caller is responsible for collecting the answers.
func (w *WorkflowConfig) TransitionFields(fromStatus, toStatus string) []WorkflowField {
	t := w.ResolveTransition(fromStatus, toStatus)
	if t == nil {
		return nil
	}
	return append([]WorkflowField(nil), t.Fields...)
}

// ValidateFieldAnswers checks required field answers are present (non-empty
// after trimming). Returns an error naming the first missing required field.
func (w *WorkflowConfig) ValidateFieldAnswers(fromStatus, toStatus string, fieldValues map[string]string) error {
	for _, field := range w.TransitionFields(fromStatus, toStatus) {
		if !field.Required {
			continue
		}
		if strings.TrimSpace(fieldValues[field.Name]) == "" {
			label := field.Prompt
			if label == "" {
				label = field.Name
			}
			return fmt.Errorf("required field %q is missing: %s", field.Name, label)
		}
	}
	return nil
}

func (w *WorkflowConfig) ApplyTransitionToFile(filePath, toStatus string) (string, TransitionResult, error) {
	return w.ApplyTransitionToFileWithFields(filePath, toStatus, nil)
}

func (w *WorkflowConfig) ApplyTransitionToFileWithFields(filePath, toStatus string, fieldValues map[string]string) (string, TransitionResult, error) {
	var fromStatus string
	var result TransitionResult

	err := withIssueLock(filePath, func() error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		issue, err := ParseIssue(filepath.Base(filePath), data)
		if err != nil {
			return err
		}
		issue.FilePath = filePath

		rawBody, err := ParseFrontmatter(string(data), &Issue{})
		if err != nil {
			return fmt.Errorf("parsing current issue state for %s: %w", filePath, err)
		}
		_, comments := ParseComments(rawBody)

		fromStatus = issue.Status
		if !w.IsValidTransition(fromStatus, toStatus) {
			next := w.NextRequiredStatus(fromStatus)
			if next == "" {
				next = w.NextStatus(fromStatus)
			}
			if next != "" {
				return fmt.Errorf("cannot transition from %q to %q — must go to %q next", fromStatus, toStatus, next)
			}
			return fmt.Errorf("cannot transition from %q to %q", fromStatus, toStatus)
		}
		if err := w.ValidateTransition(issue, fromStatus, toStatus, comments); err != nil {
			return err
		}
		if err := w.ValidateFieldAnswers(fromStatus, toStatus, fieldValues); err != nil {
			return err
		}

		result = w.ApplyTransitionWithFields(issue, fromStatus, toStatus, fieldValues)
		return updateIssueFrontmatterLocked(filePath, result.Update)
	})

	return fromStatus, result, err
}

type StartIssueResult struct {
	Issue        *Issue
	Result       TransitionResult
	Claimed      bool
	Transitioned bool
	FromStatus   string
	ToStatus     string
}

// isHandoffStatus returns true when the agent's job at this status is to hand
// off — not to work a checklist. start advances through handoff statuses and
// lands on the next work status.
func isHandoffStatus(status string) bool {
	switch status {
	case "backlog", "human-testing":
		return true
	}
	return false
}

// StartIssueOnce picks up an issue: claims the assignee (if unset) and, when
// the current status is a handoff state, advances to the next work status.
// Preserves the backlog → in progress approval contract while allowing start
// to succeed from any other status as a plain claim.
func (w *WorkflowConfig) StartIssueOnce(filePath, slug, assignee string) (*StartIssueResult, error) {
	var out *StartIssueResult

	err := withIssueLock(filePath, func() error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		issue, err := ParseIssue(filepath.Base(filePath), data)
		if err != nil {
			return err
		}
		issue.FilePath = filePath
		issue.Slug = slug

		fromStatus := issue.Status
		if fromStatus == "done" {
			return fmt.Errorf("issue %s is already done; nothing to start", slug)
		}

		toStatus := ""
		if isHandoffStatus(fromStatus) {
			toStatus = w.NextRequiredStatus(fromStatus)
			if toStatus == "" {
				toStatus = w.NextStatus(fromStatus)
			}
			if toStatus == "" {
				return fmt.Errorf("issue %s cannot be started from status %q: no forward status available", slug, fromStatus)
			}
		}

		rawBody, err := ParseFrontmatter(string(data), &Issue{})
		if err != nil {
			return fmt.Errorf("parsing current issue state for %s: %w", filePath, err)
		}
		_, comments := ParseComments(rawBody)

		update := IssueUpdate{}
		result := TransitionResult{}
		claimed := false
		transitioned := false

		if toStatus != "" {
			candidate := *issue
			if candidate.Assignee == "" {
				candidate.Assignee = assignee
			}

			if err := w.ValidateTransition(&candidate, fromStatus, toStatus, comments); err != nil {
				if required := w.RequiredHumanApproval(fromStatus, toStatus); required != "" && !strings.EqualFold(issue.HumanApproval, required) {
					if fromStatus == "backlog" {
						return fmt.Errorf("cannot start %s: human approval for %q is missing; no changes were made", slug, required)
					}
					return fmt.Errorf("cannot start %s from %q: human approval for %q is missing; no changes were made", slug, fromStatus, required)
				}
				return err
			}

			if issue.Assignee == "" {
				issue.Assignee = assignee
				claimed = true
			}

			result = w.ApplyTransition(issue, fromStatus, toStatus)
			if claimed && result.Update.Assignee == nil {
				result.Update.Assignee = stringPtr(assignee)
			}
			update = result.Update
			transitioned = true
		} else {
			if issue.Assignee == "" {
				update.Assignee = stringPtr(assignee)
				claimed = true
			}
		}

		if issue.StartedAt == "" {
			startedAt := time.Now().UTC().Format(time.RFC3339)
			update.StartedAt = stringPtr(startedAt)
			if transitioned {
				result.Update.StartedAt = update.StartedAt
			}
		}

		if transitioned || update.Assignee != nil || update.StartedAt != nil {
			if err := updateIssueFrontmatterLocked(filePath, update); err != nil {
				return err
			}
		}

		data, err = os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading updated %s: %w", filePath, err)
		}
		updatedIssue, err := ParseIssue(filepath.Base(filePath), data)
		if err != nil {
			return err
		}
		updatedIssue.FilePath = filePath
		updatedIssue.Slug = slug

		finalTo := updatedIssue.Status
		out = &StartIssueResult{
			Issue:        updatedIssue,
			Result:       result,
			Claimed:      claimed,
			Transitioned: transitioned,
			FromStatus:   fromStatus,
			ToStatus:     finalTo,
		}
		return nil
	})

	return out, err
}

func (w *WorkflowConfig) MarkIssueDoneOnce(filePath, slug string) (*Issue, error) {
	var out *Issue

	err := withIssueLock(filePath, func() error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		issue, err := ParseIssue(filepath.Base(filePath), data)
		if err != nil {
			return err
		}
		issue.FilePath = filePath
		issue.Slug = slug

		if issue.DoneAt != "" || issue.Status == "done" {
			if issue.DoneAt != "" {
				return fmt.Errorf("issue %s was already marked done at %s", slug, issue.DoneAt)
			}
			return fmt.Errorf("issue %s is already done", slug)
		}

		rawBody, err := ParseFrontmatter(string(data), &Issue{})
		if err != nil {
			return fmt.Errorf("parsing current issue state for %s: %w", filePath, err)
		}
		_, comments := ParseComments(rawBody)

		statusOrder := w.GetStatusOrder()
		currentIdx := w.GetStatusIndex(issue.Status)
		doneIdx := w.GetStatusIndex("done")
		if doneIdx == -1 {
			return fmt.Errorf("no \"done\" status defined in workflow")
		}
		if currentIdx < doneIdx-1 {
			expected := statusOrder[doneIdx-1]
			return fmt.Errorf("cannot mark as done from %q — issue must be in %q first", issue.Status, expected)
		}

		combined := IssueUpdate{}
		for i := currentIdx + 1; i <= doneIdx; i++ {
			next := statusOrder[i]
			prev := issue.Status
			if err := w.ValidateTransition(issue, prev, next, comments); err != nil {
				return err
			}
			result := w.ApplyTransition(issue, prev, next)
			if result.Update.Status != nil {
				combined.Status = result.Update.Status
				issue.Status = *result.Update.Status
			}
			if result.Update.Body != nil {
				combined.Body = result.Update.Body
				issue.BodyRaw = *result.Update.Body
			}
			if result.Update.Assignee != nil {
				combined.Assignee = result.Update.Assignee
				issue.Assignee = *result.Update.Assignee
			}
			if result.Update.HumanApproval != nil {
				combined.HumanApproval = result.Update.HumanApproval
				issue.HumanApproval = *result.Update.HumanApproval
			}
			if result.Update.Priority != nil {
				combined.Priority = result.Update.Priority
				issue.Priority = *result.Update.Priority
			}
		}

		empty := ""
		doneAt := time.Now().UTC().Format(time.RFC3339)
		combined.Assignee = &empty
		combined.DoneAt = &doneAt
		if combined.Status == nil {
			combined.Status = stringPtr("done")
		}

		if err := updateIssueFrontmatterLocked(filePath, combined); err != nil {
			return err
		}

		data, err = os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading updated %s: %w", filePath, err)
		}
		updatedIssue, err := ParseIssue(filepath.Base(filePath), data)
		if err != nil {
			return err
		}
		updatedIssue.FilePath = filePath
		updatedIssue.Slug = slug
		out = updatedIssue
		return nil
	})

	return out, err
}

// Merge overlays custom workflow config onto this one.
// For matching statuses: appends validation and side_effects (deduped),
// overrides description and template if set in custom.
// New statuses from custom are appended.
func (w *WorkflowConfig) Merge(custom *WorkflowConfig) {
	for _, cs := range custom.Statuses {
		base := w.GetStatus(cs.Name)
		if base == nil {
			w.Statuses = append(w.Statuses, cs)
			continue
		}
		if cs.Description != "" {
			base.Description = cs.Description
		}
		if cs.Prompt != "" {
			base.Prompt = cs.Prompt
		}
		if cs.Template != "" {
			base.Template = cs.Template
		}
		if cs.Optional {
			base.Optional = true
		}
		if cs.Global {
			base.Global = true
		}
		base.Validation = appendUnique(base.Validation, cs.Validation)
		base.SideEffects = appendUnique(base.SideEffects, cs.SideEffects)
	}

	for _, ct := range custom.Transitions {
		base := w.GetTransition(ct.From, ct.To)
		if base == nil {
			w.Transitions = append(w.Transitions, WorkflowTransition{
				From:     ct.From,
				To:       ct.To,
				Actions:  append([]WorkflowAction(nil), ct.Actions...),
				CTALabel: ct.CTALabel,
			})
			continue
		}
		base.Actions = appendUniqueActions(base.Actions, ct.Actions)
		if ct.CTALabel != "" {
			base.CTALabel = ct.CTALabel
		}
	}

	if len(custom.Systems) > 0 {
		if w.Systems == nil {
			w.Systems = make(map[string]WorkflowOverlay, len(custom.Systems))
		}
		for name, overlay := range custom.Systems {
			existing := w.Systems[name]
			cfg := &WorkflowConfig{
				Statuses:    append([]WorkflowStatus(nil), existing.Statuses...),
				Transitions: append([]WorkflowTransition(nil), existing.Transitions...),
			}
			cfg.Merge(&WorkflowConfig{
				Statuses:    overlay.Statuses,
				Transitions: overlay.Transitions,
			})
			w.Systems[name] = WorkflowOverlay{
				Statuses:    cfg.Statuses,
				Transitions: cfg.Transitions,
			}
		}
	}
}

func appendUnique(base, extra []string) []string {
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	for _, v := range extra {
		if !seen[v] {
			base = append(base, v)
			seen[v] = true
		}
	}
	return base
}

func appendUniqueActions(base, extra []WorkflowAction) []WorkflowAction {
	seen := make(map[string]bool, len(base))
	keyFor := func(action WorkflowAction) string {
		return strings.Join([]string{
			action.Type,
			action.Rule,
			action.Status,
			action.Title,
			action.Body,
			action.Prompt,
			action.Field,
			action.Value,
		}, "\x00")
	}
	for _, action := range base {
		seen[keyFor(action)] = true
	}
	for _, action := range extra {
		key := keyFor(action)
		if !seen[key] {
			base = append(base, action)
			seen[key] = true
		}
	}
	return base
}

// NextStatus returns the status name that follows the given one, or empty string.
func (w *WorkflowConfig) NextStatus(current string) string {
	idx := w.GetStatusIndex(current)
	if idx == -1 || idx+1 >= len(w.Statuses) {
		return ""
	}
	return w.Statuses[idx+1].Name
}

// NextRequiredStatus returns the next non-optional status following current, or empty string.
// Used for error messages so the "must go to X next" hint points at a status the caller actually has to pass through.
func (w *WorkflowConfig) NextRequiredStatus(current string) string {
	idx := w.GetStatusIndex(current)
	if idx == -1 {
		return ""
	}
	for i := idx + 1; i < len(w.Statuses); i++ {
		if !w.Statuses[i].Optional {
			return w.Statuses[i].Name
		}
	}
	return ""
}

// DefaultNextStatus returns the default forward suggestion for current, plus any
// optional statuses that sit between current and that default (reachable side-paths).
// The required target is the first non-optional status after current; the optionals
// are the contiguous optional statuses skipped to get there. If every remaining status
// is optional, required is empty and optionals lists them all so callers can render
// alternatives instead of silently picking one.
func (w *WorkflowConfig) DefaultNextStatus(current string) (required string, optionals []string) {
	idx := w.GetStatusIndex(current)
	if idx == -1 {
		return "", nil
	}
	for i := idx + 1; i < len(w.Statuses); i++ {
		s := w.Statuses[i]
		if s.Optional {
			optionals = append(optionals, s.Name)
			continue
		}
		return s.Name, optionals
	}
	return "", optionals
}

// SchemaFieldDoc describes one YAML field in a workflow struct.
type SchemaFieldDoc struct {
	Name        string
	Type        string
	Description string
	Optional    bool
}

// SchemaSection groups fields for a single workflow type.
type SchemaSection struct {
	Path   string // e.g. "statuses[]"
	Title  string // struct name, e.g. "WorkflowStatus"
	Fields []SchemaFieldDoc
}

// SchemaNamedDoc describes a named value (action type or validation rule) that
// lives in a switch statement rather than a struct field.
type SchemaNamedDoc struct {
	Name        string
	Description string
}

// WorkflowActionTypes lists every action.type value honored by
// transitionActions / BuildTransitionPreview. Kept next to the switch so a new
// type must be documented here or reviewers will notice the drift.
var WorkflowActionTypes = []SchemaNamedDoc{
	{Name: "validate", Description: "Run a validation rule (see 'Validation rules'); blocks the transition on failure"},
	{Name: "require_human_approval", Description: "Require the issue to be human-approved for the target status before transitioning"},
	{Name: "append_section", Description: "Append a '## <title>' section with the given body if it does not already exist"},
	{Name: "inject_prompt", Description: "Inject prompt text into the agent's entry guidance on this transition"},
	{Name: "set_fields", Description: "Set or clear a frontmatter field (field=assignee|priority|status|human_approval, value=\"\" to clear)"},
}

// WorkflowValidationRules lists every validation rule recognized by checkRule.
// Keep in sync with the switch in checkRule / validationSummary.
var WorkflowValidationRules = []SchemaNamedDoc{
	{Name: "body_not_empty", Description: "Issue body must contain non-whitespace content"},
	{Name: "has_checkboxes", Description: "Body must contain at least one '- [ ]' or '- [x]' checkbox anywhere"},
	{Name: "section_has_checkboxes: <Title>", Description: "Named '## <Title>' section must contain at least one checkbox"},
	{Name: "has_assignee", Description: "Issue must have a non-empty assignee"},
	{Name: "all_checkboxes_checked", Description: "Every checkbox in the body must be checked"},
	{Name: "section_checkboxes_checked: <Title>", Description: "Every checkbox inside '## <Title>' must be checked (silent pass if the section is missing)"},
	{Name: "has_test_plan", Description: "Body must contain '## Test Plan' with '### Automated' and '### Manual' subsections"},
	{Name: "has_comment_prefix: <prefix>", Description: "At least one comment must start with the given prefix (e.g. 'tests:', 'docs:')"},
	{Name: "approved_for: <status>", Description: "Issue must be human-approved for the given status"},
	{Name: "human_approval: <status>", Description: "Alias for 'approved_for: <status>'"},
}

// WorkflowSchemaSections returns the YAML schema for workflow.yaml, derived by
// reflecting the struct tags on the workflow types. Adding a new `yaml:"..."`
// field to any of these structs surfaces it here automatically, so the CLI
// docs cannot drift from the parser.
func WorkflowSchemaSections() []SchemaSection {
	return []SchemaSection{
		{Path: "(top level)", Title: "WorkflowConfig", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowConfig{}))},
		{Path: "statuses[]", Title: "WorkflowStatus", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowStatus{}))},
		{Path: "transitions[]", Title: "WorkflowTransition", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowTransition{}))},
		{Path: "transitions[].actions[]", Title: "WorkflowAction", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowAction{}))},
		{Path: "transitions[].fields[]", Title: "WorkflowField", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowField{}))},
		{Path: "board", Title: "WorkflowBoardConfig", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowBoardConfig{}))},
		{Path: "systems[<name>]", Title: "WorkflowOverlay", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowOverlay{}))},
		{Path: "scoring", Title: "ScoringConfig", Fields: schemaFieldsOf(reflect.TypeOf(ScoringConfig{}))},
		{Path: "scoring.formula", Title: "ScoringFormula", Fields: schemaFieldsOf(reflect.TypeOf(ScoringFormula{}))},
		{Path: "scoring.formula.due_date", Title: "ScoringDueDate", Fields: schemaFieldsOf(reflect.TypeOf(ScoringDueDate{}))},
		{Path: "scoring.formula.age", Title: "ScoringAge", Fields: schemaFieldsOf(reflect.TypeOf(ScoringAge{}))},
	}
}

func schemaFieldsOf(t reflect.Type) []SchemaFieldDoc {
	var out []SchemaFieldDoc
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		yamlTag := f.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		parts := strings.Split(yamlTag, ",")
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		optional := false
		for _, p := range parts[1:] {
			if strings.TrimSpace(p) == "omitempty" {
				optional = true
			}
		}
		out = append(out, SchemaFieldDoc{
			Name:        name,
			Type:        schemaTypeName(f.Type),
			Description: f.Tag.Get("desc"),
			Optional:    optional,
		})
	}
	return out
}

func schemaTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return schemaTypeName(t.Elem())
	case reflect.Slice, reflect.Array:
		return "list<" + schemaTypeName(t.Elem()) + ">"
	case reflect.Map:
		return "map<" + schemaTypeName(t.Key()) + "," + schemaTypeName(t.Elem()) + ">"
	case reflect.Struct:
		return t.Name()
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	default:
		if t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64 {
			return "int"
		}
		if t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64 {
			return "int"
		}
		if t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64 {
			return "float"
		}
		return t.Kind().String()
	}
}
