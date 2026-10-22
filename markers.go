package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Card markers: the coordination chips the workboard puts on a card. Any view
// that shows issues (the system pages' rows too) builds a markerCtx once per
// request and renders cardMarkers with {{template "markers" .}}, so every view
// shows the same chips.

// Marker is one chip.
type Marker struct {
	// Kind is gate, question, playtest, blocked-on, unblocked, needs-revision,
	// ci-red or parked; it is also the chip's test id suffix.
	Kind  string
	Label string
	// Tone is amber (waiting on you), red (can't move on), grey or green.
	Tone   string
	Href   string // set for a blocked-on chip: the issue it waits for
	Title  string // tooltip
	Struck bool   // a blocked-on whose target is done
}

// Waiting reports whether the chip is something the owner has to do.
func (m Marker) Waiting() bool { return m.Tone == "amber" }

// markerCtx is what cardMarkers needs beyond the issue itself.
type markerCtx struct {
	prefix    string
	now       time.Time
	bySlug    map[string]*tracker.Issue
	attention map[string]Attention
	// ciRed holds, while local CI is red, the issues merged since the last
	// green run (.ci/state.json "blamed").
	ciRed   map[string]bool
	ciState *ci.State
}

// newMarkerCtx reads ownerAttention for every issue once.
func newMarkerCtx(proj *tracker.Project, prefix string, issues []*tracker.Issue, now time.Time) *markerCtx {
	return newMarkerCtxFor(proj, prefix, issues, issues, now)
}

// newMarkerCtxFor reads ownerAttention only for the issues a view shows
// (a system page's rows); blocked_on still resolves against all issues.
func newMarkerCtxFor(proj *tracker.Project, prefix string, all, shown []*tracker.Issue, now time.Time) *markerCtx {
	ctx := &markerCtx{prefix: prefix, now: now, bySlug: map[string]*tracker.Issue{}, attention: map[string]Attention{}}
	for _, issue := range all {
		ctx.bySlug[strings.ToLower(issue.Slug)] = issue
	}
	for _, issue := range shown {
		if issue.Status != "done" {
			ctx.attention[issue.Slug] = ownerAttention(proj, issue)
		}
	}
	if st := ciRedState(proj); st != nil {
		ctx.ciState, ctx.ciRed = st, map[string]bool{}
		for _, slug := range st.Blamed {
			ctx.ciRed[strings.ToLower(slug)] = true
		}
	}
	return ctx
}

// cardMarkers lists the issue's chips: waiting on you first, then what blocks
// it, then parked.
func cardMarkers(ctx *markerCtx, issue *tracker.Issue) []Marker {
	att := ctx.attention[issue.Slug]
	wait := ""
	if !att.Since.IsZero() {
		wait = " · " + agoText(ctx.now.Sub(att.Since))
	}
	var out []Marker
	// A playtest due comes before the approval it leads to: one ask at a time.
	if att.Gate != nil && att.Gate.Ready && !att.PlaytestDue {
		out = append(out, Marker{Kind: "gate", Tone: "amber", Label: "approve " + att.Gate.Target + wait,
			Title: "Every requirement is met except your approval for " + att.Gate.Target})
	}
	if att.PlaytestDue {
		out = append(out, Marker{Kind: "playtest", Tone: "amber", Label: "playtest" + wait,
			Title: "Run: " + strings.Join(att.Playtests, ", ")})
	}
	if att.OpenQuestion {
		out = append(out, Marker{Kind: "question", Tone: "amber", Label: "question",
			Title: "The newest comment is a question: nobody has answered it"})
	}
	for _, slug := range blockedOn(issue) {
		target := ctx.bySlug[strings.ToLower(slug)]
		switch {
		case target == nil:
			out = append(out, Marker{Kind: "blocked-on", Tone: "red", Label: "blocked on " + slug + " (not found)",
				Title: "blocked_on names an issue that does not exist"})
		case strings.EqualFold(target.Status, "done"):
			out = append(out, Marker{Kind: "unblocked", Tone: "grey", Label: target.Slug + " done", Struck: true,
				Href: ctx.prefix + "/issue/" + target.Slug, Title: "blocked_on: " + target.Slug + " is done; remove the field"})
		default:
			out = append(out, Marker{Kind: "blocked-on", Tone: "red", Label: "blocked on " + target.Slug,
				Href: ctx.prefix + "/issue/" + target.Slug, Title: "blocked_on: " + target.Slug + " (" + target.Status + ")"})
		}
	}
	if why, ok := needsRevision(issue); ok {
		label := "needs revision"
		if why != "" {
			label += " · " + why
		}
		out = append(out, Marker{Kind: "needs-revision", Tone: "red", Label: label,
			Title: "Revise the issue before it moves on (needs-revision label or needs_revision field)"})
	}
	out = append(out, ciMarkers(ctx, issue)...)
	if att.Parked {
		label := "parked"
		if att.ParkReason != "" {
			label += " · " + att.ParkReason
		}
		out = append(out, Marker{Kind: "parked", Tone: "grey", Label: label, Title: att.ParkReason})
	}
	return out
}

// ciMarkers is the ci-red chip, while local CI is red and the issue landed
// since the last green run. Done cards get it too: that is where merged
// issues are.
func ciMarkers(ctx *markerCtx, issue *tracker.Issue) []Marker {
	if !ctx.ciRed[strings.ToLower(issue.Slug)] {
		return nil
	}
	st := ctx.ciState
	return []Marker{{Kind: "ci-red", Tone: "red", Label: "CI red · " + clip(st.Failed, 28),
		Title: fmt.Sprintf("Local CI went red on %s at %s; this issue landed since the last green run (%s). issue-cli ci status", st.Branch, ci.Short(st.RedSince), ci.Short(st.GreenSha))}}
}

// blockedOn is the issue's blocked_on frontmatter: one slug or a list.
func blockedOn(issue *tracker.Issue) []string {
	for _, f := range issue.ExtraFields {
		if f.Key != "blocked_on" {
			continue
		}
		vals := f.Values
		if !f.IsList {
			vals = []string{f.Value}
		}
		var out []string
		for _, v := range vals {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

// needsRevision reports a needs-revision label or needs_revision field, with
// the field's text (a reason or decision ref) when there is one.
func needsRevision(issue *tracker.Issue) (why string, ok bool) {
	why = issue.Field("needs_revision")
	if why != "" {
		return why, true
	}
	for _, l := range issue.Labels {
		if strings.EqualFold(strings.TrimSpace(l), "needs-revision") {
			return "", true
		}
	}
	return "", false
}

// CardProgress is a progress line on a card: an epic's own children, or the
// epic a child belongs to.
type CardProgress struct {
	Own   bool   // the card is the epic
	Title string // the epic's title (for a child's card)
	Href  string
	Done  int
	Total int
}

// Pct is the done share for the bar width.
func (p CardProgress) Pct() int {
	if p.Total == 0 {
		return 0
	}
	return p.Done * 100 / p.Total
}

// epicProgress is the issue's own epic progress when it is an epic, else its
// parent epic's; nil when neither applies.
func epicProgress(prefix string, issue *tracker.Issue, issues []*tracker.Issue) *CardProgress {
	count := func(slug string) (done, total int) {
		for _, c := range tracker.EpicChildren(issues, slug) {
			total++
			if strings.EqualFold(c.Status, "done") {
				done++
			}
		}
		return done, total
	}
	if tracker.IsEpic(issue, issues) {
		done, total := count(issue.Slug)
		return &CardProgress{Own: true, Done: done, Total: total}
	}
	if parent := tracker.FindIssue(issues, tracker.ParentSlug(issue)); parent != nil {
		done, total := count(parent.Slug)
		return &CardProgress{Title: parent.Title, Href: prefix + "/issue/" + parent.Slug, Done: done, Total: total}
	}
	return nil
}
