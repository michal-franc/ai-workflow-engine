package validations

import (
	"fmt"
	"strings"
)

func init() { register("children_closed", ChildrenClosed) }

// closedByDefault are the statuses that close a child when the rule names
// none in values:.
var closedByDefault = []string{"done", "parked", "obsolete"}

// ChildrenClosed passes when every child of the issue (the issues naming it
// in parent:) is in a closing status: values: when set, else done, parked or
// obsolete. An epic with no children passes. Requires cfg.Children, which the
// tracker injects from its issue store.
func ChildrenClosed(action Action, issue *IssueView, cfg Config) error {
	if cfg.Children == nil {
		return fail(action, issue,
			"children_closed requires the issue store, but none is configured",
			"run validation via the issue-cli or server (sets it automatically)")
	}
	closed := action.Values
	if len(closed) == 0 {
		closed = closedByDefault
	}
	var open []string
	for _, c := range cfg.Children(issue.Slug) {
		if !inFold(c.Status, closed) {
			open = append(open, c.Slug+" ("+c.Status+")")
		}
	}
	if len(open) == 0 {
		return nil
	}
	return fail(action, issue,
		fmt.Sprintf("%d of its children are still open: %s", len(open), strings.Join(open, ", ")),
		fmt.Sprintf("finish, park or obsolete them first (issue-cli list --parent %s)", issue.Slug))
}

func inFold(s string, list []string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}
