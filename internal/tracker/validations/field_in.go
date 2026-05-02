package validations

import (
	"fmt"
	"strings"
)

func init() { register("field_in", FieldIn) }

// FieldIn passes when the named frontmatter key's value matches one of the
// configured allowed values (exact match, case-sensitive).
func FieldIn(action Action, issue *IssueView, _ Config) error {
	field := strings.TrimSpace(action.Field)
	if field == "" {
		return fmt.Errorf("field_in rule requires a 'field'")
	}
	if len(action.Values) == 0 {
		return fmt.Errorf("field_in rule requires non-empty 'values'")
	}
	v := issue.Frontmatter[field]
	for _, allowed := range action.Values {
		if v == allowed {
			return nil
		}
	}
	return fail(action, issue,
		fmt.Sprintf("frontmatter %q=%q is not one of [%s]", field, v, strings.Join(action.Values, ", ")),
		fmt.Sprintf("set: issue-cli set-meta %s --key %s --value <%s>", issue.Slug, field, strings.Join(action.Values, "|")))
}
