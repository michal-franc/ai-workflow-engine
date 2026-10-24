package main

import (
	"html/template"
	"strings"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/privacy"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The privacy badges (internal/privacy): work files and code, after the
// Status title, as lines on the Health card, and as markers on the projects
// list and the /stats overview. All local: git ls-files, the tracker's git
// folder and its sync file.

// PrivacyView is one project's badges: work files, then code.
type PrivacyView struct {
	Badges []PrivacyBadge
}

// PrivacyBadge is a privacy.Badge plus what the templates need.
type PrivacyBadge struct {
	privacy.Badge
	Prefix string
}

// Anchor is the Health card line the badge links to.
func (b PrivacyBadge) Anchor() string { return "status-privacy-" + b.Kind }

// Boxed is an amber or red line: a tinted box with the fix.
func (b PrivacyBadge) Boxed() bool { return b.Level == privacy.Amber || b.Level == privacy.Red }

// FixIsCommand is a fix the reader runs (shown as code).
func (b PrivacyBadge) FixIsCommand() bool {
	return strings.HasPrefix(b.Fix, "issue-cli ") || strings.HasPrefix(b.Fix, "git ")
}

// UpdatesURL is the Updates tab, where the privacy feature is enabled.
func (b PrivacyBadge) UpdatesURL() string { return b.Prefix + "/updates" }

// Icon is the badge's inline SVG: a lock for work files (closed when
// private, open otherwise), </> for code, a warning sign when red.
func (b PrivacyBadge) Icon() template.HTML {
	const open = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">`
	switch {
	case b.Level == privacy.Red:
		return template.HTML(open + `<path d="M12 3 2 21h20L12 3z"/><path d="M12 10v5"/><path d="M12 18h.01"/></svg>`)
	case b.Kind == "code":
		return template.HTML(open + `<path d="m8 8-4 4 4 4"/><path d="m16 8 4 4-4 4"/></svg>`)
	case b.Level == privacy.Green:
		return template.HTML(open + `<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/></svg>`)
	}
	return template.HTML(open + `<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.5-2"/></svg>`)
}

// privacyCacheTTL keeps the git calls off every page view.
const privacyCacheTTL = time.Minute

type privacyEntry struct {
	at    time.Time
	badge []privacy.Badge
}

var privacyCache = struct {
	sync.Mutex
	m map[string]privacyEntry
}{m: map[string]privacyEntry{}}

// privacyBadges is proj's badges, cached for a minute per project.
func privacyBadges(proj *tracker.Project, now time.Time) []privacy.Badge {
	key := proj.Slug + "\x00" + proj.WorkDir + "\x00" + proj.IssueDir
	privacyCache.Lock()
	e, ok := privacyCache.m[key]
	privacyCache.Unlock()
	if ok && now.Sub(e.at) < privacyCacheTTL && !now.Before(e.at) {
		return e.badge
	}
	in := privacy.ForProject(proj, proj.LoadWorkflow().Privacy, now)
	badges := []privacy.Badge{privacy.WorkBadge(in), privacy.CodeBadge(in)}
	privacyCache.Lock()
	privacyCache.m[key] = privacyEntry{at: now, badge: badges}
	privacyCache.Unlock()
	return badges
}

// privacyView is the Status page's badges and Health card lines.
func privacyView(proj *tracker.Project, prefix string, now time.Time) *PrivacyView {
	v := &PrivacyView{}
	for _, b := range privacyBadges(proj, now) {
		v.Badges = append(v.Badges, PrivacyBadge{Badge: b, Prefix: prefix})
	}
	return v
}

// Badge is the view's badge of kind, nil without one.
func (v *PrivacyView) Badge(kind string) *PrivacyBadge {
	if v == nil {
		return nil
	}
	for i := range v.Badges {
		if v.Badges[i].Kind == kind {
			return &v.Badges[i]
		}
	}
	return nil
}

// badges is v.Badges, nil-safe.
func (v *PrivacyView) badges() []PrivacyBadge {
	if v == nil {
		return nil
	}
	return v.Badges
}
