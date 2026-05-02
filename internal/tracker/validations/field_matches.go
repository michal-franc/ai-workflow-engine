package validations

import (
	"fmt"
	"regexp"
	"strings"
)

func init() { register("field_matches", FieldMatches) }

// FieldMatches passes when the named frontmatter value matches the supplied
// Go RE2 regex. Note: no backreferences or lookarounds.
func FieldMatches(action Action, issue *IssueView, _ Config) error {
	field := strings.TrimSpace(action.Field)
	if field == "" {
		return fmt.Errorf("field_matches rule requires a 'field'")
	}
	if strings.TrimSpace(action.Pattern) == "" {
		return fmt.Errorf("field_matches rule requires a 'pattern'")
	}
	re, err := regexp.Compile(action.Pattern)
	if err != nil {
		return fmt.Errorf("field_matches: invalid regex %q: %w", action.Pattern, err)
	}
	v := issue.Frontmatter[field]
	if !re.MatchString(v) {
		return fail(action, issue,
			fmt.Sprintf("frontmatter %q=%q does not match /%s/", field, v, action.Pattern),
			fmt.Sprintf("set: issue-cli set-meta %s --key %s --value ...", issue.Slug, field))
	}
	return nil
}
