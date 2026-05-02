package validations

import (
	"fmt"
	"regexp"
	"strings"
)

func init() { register("no_todo_markers", NoTodoMarkers) }

// todoMarkerRe matches whole-word case-sensitive TODO and FIXME tokens.
var todoMarkerRe = regexp.MustCompile(`\b(TODO|FIXME)\b`)

// NoTodoMarkers passes when the issue body does not contain a TODO or FIXME
// marker anywhere. Matching is whole-word and case-sensitive; code fences
// are not skipped.
func NoTodoMarkers(action Action, issue *IssueView, _ Config) error {
	for i, line := range strings.Split(issue.BodyRaw, "\n") {
		if loc := todoMarkerRe.FindStringIndex(line); loc != nil {
			excerpt := strings.TrimSpace(line)
			if len(excerpt) > 80 {
				excerpt = excerpt[:80] + "…"
			}
			return fail(action, issue,
				fmt.Sprintf("TODO/FIXME marker on line %d: %q", i+1, excerpt),
				"edit the body to resolve or remove the marker before transitioning")
		}
	}
	return nil
}
