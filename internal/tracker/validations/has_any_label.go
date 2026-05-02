package validations

import "fmt"

func init() { register("has_any_label", HasAnyLabel) }

// HasAnyLabel passes when the issue has at least one label.
func HasAnyLabel(action Action, issue *IssueView, _ Config) error {
	if len(issue.Labels) > 0 {
		return nil
	}
	return fail(action, issue,
		"no labels set",
		fmt.Sprintf("add: issue-cli set-meta %s --key labels --value <label>", issue.Slug))
}
