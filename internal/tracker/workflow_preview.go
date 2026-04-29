package tracker

import (
	"fmt"
	"strings"
)

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
