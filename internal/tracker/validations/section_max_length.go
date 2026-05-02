package validations

import (
	"fmt"
	"strings"
)

func init() { register("section_max_length", SectionMaxLength) }

// SectionMaxLength passes when the named section is absent or contains at
// most Max non-whitespace characters. A missing section is a pass — pair
// with has_section when presence matters.
func SectionMaxLength(action Action, issue *IssueView, _ Config) error {
	section := strings.TrimSpace(action.Section)
	if section == "" {
		return fmt.Errorf("section_max_length requires 'section'")
	}
	if action.Max <= 0 {
		return fmt.Errorf("section_max_length requires positive 'max'")
	}
	content, ok := findSectionContent(issue.BodyRaw, section)
	if !ok {
		return nil
	}
	got := len(strings.TrimSpace(content))
	if got > action.Max {
		return fail(action, issue,
			fmt.Sprintf("section \"## %s\" has %d chars, max %d", section, got, action.Max),
			"trim it down (edit the body via the viewer or rewrite the section)")
	}
	return nil
}
