package validations

import (
	"fmt"
	"strings"
)

func init() { register("field_not_empty", FieldNotEmpty) }

// FieldNotEmpty passes when the named frontmatter key exists and has a
// non-blank value (whitespace is treated as blank).
func FieldNotEmpty(action Action, issue *IssueView, _ Config) error {
	field := strings.TrimSpace(action.Field)
	if field == "" {
		return fmt.Errorf("field_not_empty rule requires a 'field'")
	}
	v, ok := issue.Frontmatter[field]
	if !ok || strings.TrimSpace(v) == "" {
		return fail(action, issue,
			fmt.Sprintf("frontmatter field %q is blank", field),
			fmt.Sprintf("set: issue-cli set-meta %s --key %s --value ...", issue.Slug, field))
	}
	return nil
}
