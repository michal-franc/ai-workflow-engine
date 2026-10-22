package tracker

import (
	"strings"
)

// LeadScopeProject is the project lead's scope, exported to its session as
// ISSUE_VIEWER_LEAD_SCOPE.
const LeadScopeProject = "project"

// WorktreeSpec is where an agent runs: a git worktree at Path on Branch,
// created from StartPoint ("" = the primary checkout's HEAD) when the branch
// is new. The zero value means "no worktree, run in the project checkout".
type WorktreeSpec struct {
	Path       string
	Branch     string
	StartPoint string
}

// Field returns the value of a custom (non-core) frontmatter field, or "".
func (i *Issue) Field(key string) string {
	if i == nil {
		return ""
	}
	for _, f := range i.ExtraFields {
		if f.Key == key && !f.IsList {
			return strings.TrimSpace(f.Value)
		}
	}
	return ""
}

// ParentSlug is the epic an issue belongs to: its `parent:` field, or the
// legacy `umbrella:` alias.
func ParentSlug(issue *Issue) string {
	if p := issue.Field("parent"); p != "" {
		return p
	}
	return issue.Field("umbrella")
}

// UsesLegacyUmbrella reports whether the issue links its epic with the
// legacy `umbrella:` key instead of `parent:`.
func UsesLegacyUmbrella(issue *Issue) bool {
	return issue.Field("parent") == "" && issue.Field("umbrella") != ""
}

// FindIssue returns the issue whose slug is slug (case-insensitive), or nil.
func FindIssue(issues []*Issue, slug string) *Issue {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil
	}
	for _, issue := range issues {
		if strings.ToLower(issue.Slug) == slug {
			return issue
		}
	}
	return nil
}

// EpicChildren returns the issues whose parent is the epic slug.
func EpicChildren(issues []*Issue, slug string) []*Issue {
	var children []*Issue
	for _, issue := range issues {
		if p := ParentSlug(issue); p != "" && strings.EqualFold(p, slug) {
			children = append(children, issue)
		}
	}
	return children
}

// IsEpic reports whether issue is an epic: typed `epic`, or named as parent
// by another issue.
func IsEpic(issue *Issue, issues []*Issue) bool {
	if issue == nil {
		return false
	}
	if strings.EqualFold(issue.Type, "epic") {
		return true
	}
	return len(EpicChildren(issues, issue.Slug)) > 0
}

// sessionSafe flattens a slug into a tmux-safe name segment.
func sessionSafe(slug string) string {
	return strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(slug)
}

// LeadSessionName is the tmux session (or shared-session window) name of the
// project lead: lead-<project-slug>. The name is the lead's identity, like
// agent-<slug> for workers. There is one lead per project.
func LeadSessionName(proj *Project) string {
	name := "project"
	if proj != nil && proj.Slug != "" {
		name = proj.Slug
	}
	return "lead-" + sessionSafe(name)
}

// IssueWorktree resolves where a dispatched agent for issue runs: work/<slug>
// created from HEAD. Epics and their children get no special branch; an old
// `base:` on an epic is a plain custom field.
//
// Pure: no git or filesystem access.
func IssueWorktree(workdir string, issue *Issue, wf *WorkflowConfig) WorktreeSpec {
	path, branch, enabled := ResolveWorktree(workdir, issue.Slug, wf)
	if !enabled {
		return WorktreeSpec{}
	}
	return WorktreeSpec{Path: path, Branch: branch}
}

// LeadRole is who a session is in the lead hierarchy.
type LeadRole string

const (
	RoleProjectLead LeadRole = "project lead"
	RoleWorker      LeadRole = "worker"
	RoleUnmanaged   LeadRole = "unmanaged"
)

// LeadRoute names who an issue's worker reports to: always the project lead.
// Routing is derived from the parent link and the live sessions, never
// stored.
type LeadRoute struct {
	// Lead is the project lead's session name.
	Lead string
	// Live reports whether Lead's session is running.
	Live bool
	// Epic is the issue's epic ("" when it has none).
	Epic string
}

// LeadFor routes issue to the project lead and names its epic. live reports
// whether a session name is running; nil means nothing is.
func LeadFor(proj *Project, issue *Issue, issues []*Issue, live func(name string) bool) LeadRoute {
	route := LeadRoute{Lead: LeadSessionName(proj)}
	route.Live = live != nil && live(route.Lead)
	if issue != nil {
		if epic := ParentSlug(issue); epic != "" {
			if e := FindIssue(issues, epic); e != nil {
				epic = e.Slug
			}
			route.Epic = epic
		}
	}
	return route
}
