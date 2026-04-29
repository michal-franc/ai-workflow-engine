package tracker

import (
	"fmt"
	"os"
	"strings"

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




