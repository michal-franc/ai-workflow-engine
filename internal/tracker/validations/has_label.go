package validations

import (
	"fmt"
	"strings"
)

func init() { register("has_label", HasLabel) }

// HasLabel passes when the issue's labels contain the given name (in
// action.Field). The label name is taken from action.Field to keep the
// schema small.
func HasLabel(action Action, issue *IssueView, _ Config) error {
	want := strings.TrimSpace(action.Field)
	if want == "" {
		return fmt.Errorf("has_label rule requires the label name in 'field'")
	}
	for _, l := range issue.Labels {
		if l == want {
			return nil
		}
	}
	combined := append([]string(nil), issue.Labels...)
	combined = append(combined, want)
	return fail(action, issue,
		fmt.Sprintf("missing label %q", want),
		fmt.Sprintf("add: issue-cli set-meta %s --key labels --value %q", issue.Slug, strings.Join(combined, ",")))
}
