// Package validations holds the structured workflow validators. Each
// validator lives in its own file and registers a Check function that the
// tracker package dispatches via the Registry.
//
// validations is a leaf package: it does not import tracker. The tracker
// translates its native types (WorkflowAction, Issue) into the narrow
// IssueView and Action defined here, then calls Check.
package validations

// IssueView is the read-only snapshot a validator sees. The tracker fills
// this in from the underlying *tracker.Issue. Frontmatter is a flat
// key→value lookup over both the known frontmatter fields and any custom
// (extra) fields; list-typed extra fields are represented by the empty
// string with the key still present, matching the legacy semantics.
type IssueView struct {
	Slug     string
	Title    string
	Status   string
	System   string
	Priority string
	Repo     string
	Assignee string
	BodyRaw  string
	Number   int
	Labels   []string

	// Frontmatter is keyed by the lower-case frontmatter field name.
	// Use Frontmatter[k] for the value and ok-by-presence with HasKey.
	Frontmatter map[string]string
}

// HasKey reports whether the issue has the given frontmatter key, regardless
// of whether the value is empty.
func (v *IssueView) HasKey(key string) bool {
	if v == nil || v.Frontmatter == nil {
		return false
	}
	_, ok := v.Frontmatter[key]
	return ok
}

// Action mirrors the validator-relevant subset of tracker.WorkflowAction.
type Action struct {
	Rule           string
	Field          string
	Pattern        string
	Section        string
	Command        string
	RefKey         string
	LinkedStatus   string
	Hint           string
	Values         []string
	Min            int
	Max            int
	TimeoutSeconds int
}

// LookupFunc resolves another issue by slug, returning nil when not found.
// Used by linked_issue_in_status. The tracker injects this from its own
// issue store.
type LookupFunc func(slug string) *IssueView

// Config holds the cross-validator policy + dependencies that come from the
// loaded workflow plus the runtime callers.
type Config struct {
	AllowShell bool
	IssuesRoot string
	Lookup     LookupFunc
}

// CheckFn is the signature each validator implements.
type CheckFn func(action Action, issue *IssueView, cfg Config) error
