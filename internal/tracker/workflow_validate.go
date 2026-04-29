package tracker

import (
	"fmt"
	"strings"
)

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

// ValidationSummary returns a human-readable one-line description of the
// validation rule (the same text shown in transition previews).
func ValidationSummary(rule string) string {
	return validationSummary(rule)
}

// DescribeAction returns a short human-readable description of a workflow
// action, suitable for listing the rules that govern a transition.
func DescribeAction(action WorkflowAction, defaultStatus string) string {
	switch action.Type {
	case "validate":
		return ValidationSummary(action.Rule)
	case "require_human_approval":
		status := strings.TrimSpace(action.Status)
		if status == "" {
			status = defaultStatus
		}
		if status == "" {
			return "Must be human-approved in the issue viewer"
		}
		return fmt.Sprintf("Must be human-approved for %q in the issue viewer", status)
	case "append_section":
		title := strings.TrimSpace(action.Title)
		if title == "" {
			return "Side-effect: appends a body section"
		}
		return fmt.Sprintf("Side-effect: appends ## %s section", title)
	case "inject_prompt":
		return "Side-effect: injects entry guidance prompt"
	case "set_fields":
		field := strings.TrimSpace(action.Field)
		if field == "" {
			return "Side-effect: sets a frontmatter field"
		}
		if action.Value == "" {
			return fmt.Sprintf("Side-effect: clears %s", field)
		}
		return fmt.Sprintf("Side-effect: sets %s = %q", field, action.Value)
	default:
		return action.Type
	}
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
