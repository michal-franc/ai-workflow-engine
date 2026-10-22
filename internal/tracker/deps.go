package tracker

import (
	"sort"
	"strings"
)

// DependsOnField is the frontmatter key that lists the issues an issue waits
// on: a YAML list, or a comma-separated string as `issue-cli set-meta`
// writes it.
const DependsOnField = "depends_on"

// DependsOn returns the slugs in the issue's depends_on field, lower-cased,
// in the order written, without duplicates.
func DependsOn(issue *Issue) []string {
	if issue == nil {
		return nil
	}
	var raw []string
	for _, f := range issue.ExtraFields {
		if f.Key != DependsOnField {
			continue
		}
		if f.IsList {
			raw = append(raw, f.Values...)
		} else {
			raw = append(raw, strings.Split(f.Value, ",")...)
		}
	}
	return ParseSlugList(strings.Join(raw, ","))
}

// ParseSlugList splits "a/b, c/d" into normalised slugs.
func ParseSlugList(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		slug := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "#")))
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	return out
}

// DependedOnBy lists the issues whose depends_on names slug, by slug.
func DependedOnBy(issues []*Issue, slug string) []*Issue {
	slug = strings.ToLower(slug)
	var out []*Issue
	for _, issue := range issues {
		for _, d := range DependsOn(issue) {
			if d == slug {
				out = append(out, issue)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// IssueDep is one dependency or decision link with the state it shows.
type IssueDep struct {
	Kind   string // "depends on", "depended on by", "decided by"
	Target string // slug, or slug#D-n
	Slug   string // the issue to link to ("" when unknown)
	State  string // the issue's status, or the decision's state; "unknown issue"
	Closed bool
}

// IssueDeps returns an issue's depends-on, depended-on-by and decided-by
// links. index may be nil (no decided-by).
func IssueDeps(issue *Issue, issues []*Issue, index *DecisionIndex, isClosed func(*Issue) bool) []IssueDep {
	if issue == nil {
		return nil
	}
	closed := func(i *Issue) bool {
		if isClosed != nil {
			return isClosed(i)
		}
		return isClosedStatus(i.Status)
	}
	var out []IssueDep
	for _, slug := range DependsOn(issue) {
		dep := IssueDep{Kind: "depends on", Target: slug, State: "unknown issue"}
		if target := FindIssue(issues, slug); target != nil {
			dep.Slug, dep.State, dep.Closed = target.Slug, target.Status, closed(target)
		}
		out = append(out, dep)
	}
	for _, by := range DependedOnBy(issues, issue.Slug) {
		out = append(out, IssueDep{Kind: "depended on by", Target: by.Slug, Slug: by.Slug, State: by.Status, Closed: closed(by)})
	}
	if index != nil {
		for _, e := range index.Cited(issue.Slug) {
			if e.Slug == issue.Slug {
				continue // its own decisions are on the page already
			}
			state := e.State
			if e.ReplacedBy != nil {
				state = "replaced by " + e.ReplacedBy.ID
				if e.ReplacedBy.Slug != e.Slug {
					state = "replaced by " + e.ReplacedBy.Ref
				}
			}
			out = append(out, IssueDep{Kind: "decided by", Target: e.Ref, Slug: e.Slug, State: state, Closed: e.ReplacedBy != nil})
		}
	}
	return out
}

// BranchFileCount is how many files b changes against its merge-base.
func BranchFileCount(root string, b OpenBranch) int {
	stats, err := branchNumstat(root, b)
	if err != nil {
		return 0
	}
	return len(stats)
}

// BranchOf returns the open branch whose issue is slug, or nil.
func BranchOf(branches []OpenBranch, slug string) *OpenBranch {
	for i := range branches {
		if branches[i].IssueSlug != "" && strings.EqualFold(branches[i].IssueSlug, slug) {
			return &branches[i]
		}
	}
	return nil
}

// HotFilesFor keeps the hot files branch takes part in (unshared), with the
// other branches that change them.
func HotFilesFor(hot []HotFile, branch string) []HotFile {
	var out []HotFile
	for _, h := range hot {
		for _, c := range h.Changes {
			if c.Branch == branch && !c.Shared {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

// SharedFiles summarises an issue's branch against the other open branches:
// the hot files it takes part in and how many other branches change them.
type SharedFiles struct {
	Branch string
	Files  []HotFile
	Others int
}

// IssueSharedFiles looks for the issue's open branch (work/<slug>) and the
// hot files it shares. It runs no branch scan when that branch is absent.
func IssueSharedFiles(root string, issue *Issue, issues []*Issue) *SharedFiles {
	if issue == nil {
		return nil
	}
	name := "work/" + issue.Slug
	if !refExists(root, name) {
		return nil
	}
	branches := OpenBranches(root, "", issues)
	if BranchOf(branches, issue.Slug) == nil {
		return nil
	}
	files := HotFilesFor(HotFiles(root, branches, true), name)
	if len(files) == 0 {
		return nil
	}
	others := map[string]bool{}
	for _, h := range files {
		for _, c := range h.Changes {
			if c.Branch != name && !c.Shared {
				others[c.Branch] = true
			}
		}
	}
	return &SharedFiles{Branch: name, Files: files, Others: len(others)}
}
