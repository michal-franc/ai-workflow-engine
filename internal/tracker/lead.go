package tracker

import (
	"path/filepath"
	"strings"
)

// Lead scopes. A project lead's scope is LeadScopeProject; a sub-lead's
// scope is its epic's slug.
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

// IsEpic reports whether issue is an epic: typed `epic`, already given a
// sub-lead (`lead:`), or named as parent by another issue. `base:` alone does
// not make an epic — children of an epic often carry it too.
func IsEpic(issue *Issue, issues []*Issue) bool {
	if issue == nil {
		return false
	}
	if strings.EqualFold(issue.Type, "epic") || issue.Field("lead") != "" {
		return true
	}
	return len(EpicChildren(issues, issue.Slug)) > 0
}

// sessionSafe flattens a slug into a tmux-safe name segment.
func sessionSafe(slug string) string {
	return strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(slug)
}

// LeadSessionName is the tmux session (or shared-session window) name of a
// lead: lead-<project-slug> for the project lead, lead-<epic-slug> for a
// sub-lead. The name is the lead's identity, like agent-<slug> for workers.
func LeadSessionName(proj *Project, scope string) string {
	if scope == "" || scope == LeadScopeProject {
		name := "project"
		if proj != nil && proj.Slug != "" {
			name = proj.Slug
		}
		return "lead-" + sessionSafe(name)
	}
	return "lead-" + sessionSafe(scope)
}

// DefaultBaseBranch is the base branch an epic gets when it has no `base:`.
func DefaultBaseBranch(epicSlug string) string {
	return "base/" + epicSlug
}

// EpicBase returns the epic's base branch: its `base:`, else the default.
func EpicBase(epic *Issue) string {
	if b := epic.Field("base"); b != "" {
		return b
	}
	return DefaultBaseBranch(epic.Slug)
}

// IssueWorktree resolves where a dispatched agent for issue runs:
//   - an epic with a `base:` runs in the base-branch worktree its sub-lead
//     uses (.worktrees/<epic-slug> on the base branch);
//   - a child of an epic with a base branch gets work/<slug> created from that
//     base, so it builds on the epic's integrated work;
//   - everything else gets work/<slug> from HEAD, as before.
//
// Pure: no git or filesystem access.
func IssueWorktree(workdir string, issue *Issue, issues []*Issue, wf *WorkflowConfig) WorktreeSpec {
	path, branch, enabled := ResolveWorktree(workdir, issue.Slug, wf)
	if !enabled {
		return WorktreeSpec{}
	}
	spec := WorktreeSpec{Path: path, Branch: branch}
	if base := issue.Field("base"); base != "" && IsEpic(issue, issues) {
		spec.Branch = base
		return spec
	}
	spec.StartPoint = ChildBase(issue, issues)
	return spec
}

// ChildBase is the base branch a child issue builds on and ships into: its
// epic's `base:`, else its own `base:` (the older convention of copying it to
// each child). "" when the issue is not part of an epic with a base branch.
func ChildBase(issue *Issue, issues []*Issue) string {
	if parent := FindIssue(issues, ParentSlug(issue)); parent != nil {
		if b := parent.Field("base"); b != "" {
			return b
		}
	}
	if ParentSlug(issue) != "" {
		return issue.Field("base")
	}
	return ""
}

// EpicWorktree is the worktree a sub-lead runs in: .worktrees/<epic-slug> on
// the epic's base branch, created from HEAD when the branch is new. Unlike
// IssueWorktree it does not depend on the workflow's `worktree:` switch — a
// sub-lead always guards its own base branch.
func EpicWorktree(workdir string, epic *Issue) WorktreeSpec {
	return WorktreeSpec{
		Path:   filepath.Join(workdir, ".worktrees", epic.Slug),
		Branch: EpicBase(epic),
	}
}

// LeadRole is who a session is in the lead hierarchy.
type LeadRole string

const (
	RoleProjectLead LeadRole = "project lead"
	RoleSubLead     LeadRole = "sub-lead"
	RoleWorker      LeadRole = "worker"
	RoleUnmanaged   LeadRole = "unmanaged"
)

// LeadRoute names who an issue's worker (or an epic's sub-lead) reports to.
// Routing is derived from the parent link and the live sessions, never
// stored.
type LeadRoute struct {
	// Lead is the session the agent reports to right now.
	Lead string
	// Live reports whether Lead's session is running.
	Live bool
	// Epic is the issue's epic ("" when it has none).
	Epic string
	// SubLead is the epic's sub-lead session name ("" without an epic).
	SubLead string
	// SubLeadLive reports whether the sub-lead is running.
	SubLeadLive bool
	// ProjectLead is the project lead's session name.
	ProjectLead string
}

// LeadFor routes issue to its lead: the sub-lead of its epic when that
// sub-lead is live, otherwise the project lead. An epic itself reports to the
// project lead. live reports whether a session name is running; nil means
// nothing is.
func LeadFor(proj *Project, issue *Issue, issues []*Issue, live func(name string) bool) LeadRoute {
	isLive := func(name string) bool { return live != nil && live(name) }
	route := LeadRoute{ProjectLead: LeadSessionName(proj, LeadScopeProject)}
	if issue != nil {
		if epic := ParentSlug(issue); epic != "" {
			if e := FindIssue(issues, epic); e != nil {
				epic = e.Slug
			}
			route.Epic = epic
			route.SubLead = LeadSessionName(proj, epic)
			route.SubLeadLive = isLive(route.SubLead)
		}
	}
	if route.SubLeadLive {
		route.Lead, route.Live = route.SubLead, true
		return route
	}
	route.Lead, route.Live = route.ProjectLead, isLive(route.ProjectLead)
	return route
}
