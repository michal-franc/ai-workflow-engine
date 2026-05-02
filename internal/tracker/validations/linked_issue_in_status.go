package validations

import (
	"fmt"
	"strings"
)

func init() { register("linked_issue_in_status", LinkedIssueInStatus) }

// LinkedIssueInStatus passes when the issue referenced by the given
// frontmatter ref-key has the expected status. Requires cfg.Lookup to be set
// — the tracker injects this from its issue store.
func LinkedIssueInStatus(action Action, issue *IssueView, cfg Config) error {
	refKey := strings.TrimSpace(action.RefKey)
	want := strings.TrimSpace(action.LinkedStatus)
	if refKey == "" {
		return fmt.Errorf("linked_issue_in_status requires 'ref_key'")
	}
	if want == "" {
		return fmt.Errorf("linked_issue_in_status requires 'linked_status'")
	}
	v, ok := issue.Frontmatter[refKey]
	v = strings.TrimSpace(v)
	if !ok || v == "" {
		return fail(action, issue,
			fmt.Sprintf("frontmatter %q is empty (linked-issue ref-key)", refKey),
			fmt.Sprintf("set: issue-cli set-meta %s --key %s --value <slug>", issue.Slug, refKey))
	}
	if cfg.Lookup == nil {
		return fail(action, issue,
			"linked_issue_in_status requires an issue lookup, but none is configured",
			"run validation via the issue-cli or server (sets the lookup automatically)")
	}
	linked := cfg.Lookup(v)
	if linked == nil {
		return fail(action, issue,
			fmt.Sprintf("linked issue %q (from %q) not found", v, refKey),
			fmt.Sprintf("verify the slug or update the ref: issue-cli show %s", v))
	}
	if !strings.EqualFold(linked.Status, want) {
		return fail(action, issue,
			fmt.Sprintf("linked issue %q is in status %q, expected %q", v, linked.Status, want),
			fmt.Sprintf("inspect or progress it: issue-cli show %s", v))
	}
	return nil
}
