package tracker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TransitionResult struct {
	Update          IssueUpdate `json:"update"`
	BodyChanged     bool        `json:"body_changed"`
	BodyAppended    bool        `json:"body_appended"`
	ClearedApproval bool        `json:"cleared_approval"`
	InjectedPrompts []string    `json:"injected_prompts"`
}

// ErrApprovalMissing is the sentinel returned when StartIssueOnce or a
// transition refuses to proceed because the workflow requires human approval
// in the issue viewer and the issue is not yet approved for the target status.
//
// Callers should use errors.Is(err, tracker.ErrApprovalMissing) rather than
// matching on the error message.
var ErrApprovalMissing = errors.New("human approval missing")

// ApprovalMissingError describes a missing approval. Its message preserves the
// historical phrasing used by string-matching tests; programmatic callers
// should use errors.Is(err, ErrApprovalMissing).
//
// Verb selects the message form. "" (default) is the StartIssueOnce form
// ("cannot start <slug>..."). "validate" is the form returned by the
// transition validation path ("issue is not human-approved for <status>...").
// The validate form is slug-agnostic because ValidateTransition does not
// always have a meaningful slug to cite.
type ApprovalMissingError struct {
	Slug       string
	FromStatus string
	Required   string
	Verb       string
}

func (e *ApprovalMissingError) Error() string {
	if e.Verb == "validate" {
		return fmt.Sprintf("issue is not human-approved for %q — a human must approve it in the issue viewer first", e.Required)
	}
	if e.FromStatus == "" || e.FromStatus == "backlog" {
		return fmt.Sprintf("cannot start %s: human approval for %q is missing; no changes were made", e.Slug, e.Required)
	}
	return fmt.Sprintf("cannot start %s from %q: human approval for %q is missing; no changes were made", e.Slug, e.FromStatus, e.Required)
}

func (e *ApprovalMissingError) Is(target error) bool {
	return target == ErrApprovalMissing
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

func (w *WorkflowConfig) ApplyTransition(issue *Issue, fromStatus, toStatus string) TransitionResult {
	return w.ApplyTransitionWithFields(issue, fromStatus, toStatus, nil)
}

func (w *WorkflowConfig) ApplyTransitionWithFields(issue *Issue, fromStatus, toStatus string, fieldValues map[string]string) TransitionResult {
	result := TransitionResult{
		Update: IssueUpdate{
			Status: stringPtr(toStatus),
		},
	}

	// Consume any standing approval up front so a `set_fields` action that
	// targets `human_approval` (run below) can override the cleared value.
	if issue.HumanApproval != "" {
		result.Update.HumanApproval = stringPtr("")
		result.ClearedApproval = true
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

// MergeFieldValuesFromFrontmatter returns a copy of fieldValues with any
// `target: frontmatter` field that is empty in the answer map filled in from
// the issue's existing frontmatter. This lets `set-meta <key> <value>` followed
// by `transition` succeed without re-supplying the value: the frontmatter is
// the source of truth for these fields.
//
// Section-targeted fields are not merged — those write a new line into the
// body each time, so re-supplying the answer is the only way to record one.
func (w *WorkflowConfig) MergeFieldValuesFromFrontmatter(issue *Issue, fromStatus, toStatus string, fieldValues map[string]string) map[string]string {
	merged := map[string]string{}
	for k, v := range fieldValues {
		merged[k] = v
	}
	if issue == nil {
		return merged
	}
	for _, field := range w.TransitionFields(fromStatus, toStatus) {
		if strings.TrimSpace(merged[field.Name]) != "" {
			continue
		}
		target := strings.TrimSpace(field.Target)
		if target != "" && target != "frontmatter" {
			continue
		}
		for _, ef := range issue.ExtraFields {
			if ef.Key != field.Name {
				continue
			}
			if ef.IsList {
				if len(ef.Values) > 0 {
					merged[field.Name] = strings.Join(ef.Values, ", ")
				}
			} else if strings.TrimSpace(ef.Value) != "" {
				merged[field.Name] = ef.Value
			}
			break
		}
	}
	return merged
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
		fieldValues = w.MergeFieldValuesFromFrontmatter(issue, fromStatus, toStatus, fieldValues)
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
					return &ApprovalMissingError{Slug: slug, FromStatus: fromStatus, Required: required}
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
