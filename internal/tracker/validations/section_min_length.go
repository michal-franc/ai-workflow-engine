package validations

import (
	"fmt"
	"strings"
)

func init() { register("section_min_length", SectionMinLength) }

// SectionMinLength passes when the named section is present and contains at
// least Min non-whitespace characters.
func SectionMinLength(action Action, issue *IssueView, _ Config) error {
	section := strings.TrimSpace(action.Section)
	if section == "" {
		return fmt.Errorf("section_min_length requires 'section'")
	}
	if action.Min <= 0 {
		return fmt.Errorf("section_min_length requires positive 'min'")
	}
	content, ok := findSectionContent(issue.BodyRaw, section)
	got := len(strings.TrimSpace(content))
	if !ok {
		return fail(action, issue,
			fmt.Sprintf("section \"## %s\" is missing (need ≥%d chars)", section, action.Min),
			fmt.Sprintf("add: issue-cli append %s --section %q --body \"...\"", issue.Slug, section))
	}
	if got < action.Min {
		return fail(action, issue,
			fmt.Sprintf("section \"## %s\" has %d chars, need ≥%d", section, got, action.Min),
			fmt.Sprintf("expand: issue-cli append %s --section %q --body \"...\"", issue.Slug, section))
	}
	return nil
}
