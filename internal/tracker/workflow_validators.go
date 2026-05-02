package tracker

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker/validations"
)

// isStructuredRule reports whether the named rule is implemented by the
// validations sub-package. Legacy colon-string rules (body_not_empty,
// has_assignee, …) keep flowing through checkRule.
func isStructuredRule(name string) bool {
	return validations.Has(name)
}

// checkAction delegates a structured-validate action to the validations
// sub-package, after translating the tracker's native types into the narrow
// view the validators expect.
func (w *WorkflowConfig) checkAction(action WorkflowAction, issue *Issue, _ []Comment) error {
	cfg := validations.Config{
		AllowShell: w.AllowShell,
		IssuesRoot: w.IssuesRoot,
	}
	if w.LookupIssue != nil {
		cfg.Lookup = func(slug string) *validations.IssueView {
			linked := w.LookupIssue(slug)
			if linked == nil {
				return nil
			}
			return toIssueView(linked)
		}
	}
	return validations.Check(toAction(action), toIssueView(issue), cfg)
}

func toAction(a WorkflowAction) validations.Action {
	return validations.Action{
		Rule:           a.Rule,
		Field:          a.Field,
		Pattern:        a.Pattern,
		Section:        a.Section,
		Command:        a.Command,
		RefKey:         a.RefKey,
		LinkedStatus:   a.LinkedStatus,
		Hint:           a.Hint,
		Values:         append([]string(nil), a.Values...),
		Min:            a.Min,
		Max:            a.Max,
		TimeoutSeconds: a.TimeoutSeconds,
	}
}

// toIssueView denormalizes the issue's frontmatter into a flat key→value
// map covering both the known Go-typed fields and any custom (extra)
// frontmatter values. List-typed extras are represented by an empty string
// (the key is present so HasKey returns true, but the value is opaque).
func toIssueView(issue *Issue) *validations.IssueView {
	if issue == nil {
		return nil
	}
	fm := map[string]string{}
	if issue.Title != "" {
		fm["title"] = issue.Title
	}
	if issue.Status != "" {
		fm["status"] = issue.Status
	}
	if issue.System != "" {
		fm["system"] = issue.System
	}
	if issue.Version != "" {
		fm["version"] = issue.Version
	}
	if issue.Priority != "" {
		fm["priority"] = issue.Priority
	}
	if issue.Assignee != "" {
		fm["assignee"] = issue.Assignee
	}
	if issue.HumanApproval != "" {
		fm["human_approval"] = issue.HumanApproval
	}
	if issue.StartedAt != "" {
		fm["started_at"] = issue.StartedAt
	}
	if issue.DoneAt != "" {
		fm["done_at"] = issue.DoneAt
	}
	if issue.Created != "" {
		fm["created"] = issue.Created
	}
	if issue.Number != 0 {
		fm["number"] = fmt.Sprintf("%d", issue.Number)
	}
	if issue.Repo != "" {
		fm["repo"] = issue.Repo
	}
	for _, ef := range issue.ExtraFields {
		if ef.IsList {
			fm[ef.Key] = ""
			continue
		}
		fm[ef.Key] = ef.Value
	}
	return &validations.IssueView{
		Slug:        issue.Slug,
		Title:       issue.Title,
		Status:      issue.Status,
		System:      issue.System,
		Priority:    issue.Priority,
		Repo:        issue.Repo,
		Assignee:    issue.Assignee,
		BodyRaw:     issue.BodyRaw,
		Number:      issue.Number,
		Labels:      append([]string(nil), issue.Labels...),
		Frontmatter: fm,
	}
}

// structuredSummary returns a short one-liner for a structured validate
// action, used by DescribeAction in transition previews.
func structuredSummary(action WorkflowAction) string {
	rule := strings.TrimSpace(action.Rule)
	switch rule {
	case "field_present":
		return fmt.Sprintf("Validate frontmatter %q is set", action.Field)
	case "field_not_empty":
		return fmt.Sprintf("Validate frontmatter %q is not empty", action.Field)
	case "field_in":
		return fmt.Sprintf("Validate frontmatter %q ∈ [%s]", action.Field, strings.Join(action.Values, ", "))
	case "field_matches":
		return fmt.Sprintf("Validate frontmatter %q matches /%s/", action.Field, action.Pattern)
	case "has_label":
		return fmt.Sprintf("Validate label %q is set", action.Field)
	case "has_any_label":
		return "Validate at least one label is set"
	case "has_pr_url":
		return "Validate frontmatter \"pr\" is a github PR url"
	case "linked_issue_in_status":
		return fmt.Sprintf("Validate linked issue (frontmatter %q) is in status %q", action.RefKey, action.LinkedStatus)
	case "has_section":
		return fmt.Sprintf("Validate section %q exists", action.Section)
	case "section_min_length":
		return fmt.Sprintf("Validate section %q has ≥%d chars", action.Section, action.Min)
	case "section_max_length":
		return fmt.Sprintf("Validate section %q has ≤%d chars", action.Section, action.Max)
	case "no_todo_markers":
		return "Validate body has no TODO/FIXME markers"
	case "command_succeeds":
		cmd := action.Command
		if len(cmd) > 60 {
			cmd = cmd[:60] + "…"
		}
		return fmt.Sprintf("Validate shell command succeeds: %s", cmd)
	}
	return fmt.Sprintf("Validate %s", rule)
}
