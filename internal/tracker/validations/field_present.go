package validations

import (
	"fmt"
	"strings"
)

func init() { register("field_present", FieldPresent) }

// FieldPresent passes when the named frontmatter key exists, regardless of
// whether the value is empty.
func FieldPresent(action Action, issue *IssueView, _ Config) error {
	field := strings.TrimSpace(action.Field)
	if field == "" {
		return fmt.Errorf("field_present rule requires a 'field'")
	}
	if issue.HasKey(field) {
		return nil
	}
	return fail(action, issue,
		fmt.Sprintf("frontmatter field %q is missing", field),
		fmt.Sprintf("set: issue-cli set-meta %s --key %s --value ...", issue.Slug, field))
}
