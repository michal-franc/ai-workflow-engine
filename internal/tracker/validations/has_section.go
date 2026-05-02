package validations

import (
	"fmt"
	"strings"
)

func init() { register("has_section", HasSection) }

// HasSection passes when the issue body has a "## <Section>" heading
// (case-insensitive title match).
func HasSection(action Action, issue *IssueView, _ Config) error {
	section := strings.TrimSpace(action.Section)
	if section == "" {
		return fmt.Errorf("has_section requires 'section'")
	}
	if _, ok := findSectionContent(issue.BodyRaw, section); ok {
		return nil
	}
	return fail(action, issue,
		fmt.Sprintf("section \"## %s\" is missing", section),
		fmt.Sprintf("add: issue-cli append %s --section %q --body \"...\"", issue.Slug, section))
}
