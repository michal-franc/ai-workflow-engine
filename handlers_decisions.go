package main

import (
	"html"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Decisions tab is generated from every issue's `## Decisions` D-n lines
// (written by `issue-cli decide`). It is read-only: nothing here
// writes, and the index is rebuilt from the issue files on each request.

type DecisionView struct {
	*tracker.DecisionEntry
	Prefix string
	now    time.Time
}

func (v DecisionView) DateLabel() string { return decisionDateLabel(v.Date, v.now) }

// TextHTML renders the decision text with `**bold**` kept and everything
// else escaped.
func (v DecisionView) TextHTML() template.HTML { return boldOnly(v.Text) }

func (v DecisionView) IssueURL(slug string) string { return v.Prefix + "/issue/" + slug }

func (v DecisionView) ReplacedOn() string {
	if v.ReplacedBy == nil {
		return ""
	}
	return decisionDateLabel(v.ReplacedBy.Date, v.now)
}

func (v DecisionView) CitedCount() int { return len(v.CitedOpen) + len(v.CitedClosed) }

var boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)

func boldOnly(s string) template.HTML {
	return template.HTML(boldRe.ReplaceAllString(html.EscapeString(s), "<strong>$1</strong>"))
}

func decisionDateLabel(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if t.Year() == now.Year() {
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

type DecisionChip struct {
	Name  string
	Count int
}

type DecisionsData struct {
	Prefix         string
	ProjectName    string
	ActiveBots     int
	SupportsGitHub bool
	Hash           string // /hash at render time, the auto-reload baseline

	Rows    []DecisionView
	System  string // filter
	State   string // filter
	Systems []DecisionChip

	Total, Active, Deferred, Replaced, Stale int
}

// RefSlugOf is the issue part of a `<slug>#D-n` ref.
func (d *DecisionsData) RefSlugOf(ref string) string {
	slug, _, _ := strings.Cut(ref, "#")
	return slug
}

// FilterURL links the Decisions tab with the given filters.
func (d *DecisionsData) FilterURL(system, state string) string {
	q := url.Values{}
	if system != "" {
		q.Set("system", system)
	}
	if state != "" {
		q.Set("state", state)
	}
	if len(q) == 0 {
		return d.Prefix + "/decisions"
	}
	return d.Prefix + "/decisions?" + q.Encode()
}

func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessionMap, activeBots := sessionsByIssueSlug(proj, issues)
	index := tracker.BuildDecisionIndex(issues, tracker.ClosedIssueFunc(proj))

	data := DecisionsData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		SupportsGitHub: proj.SupportsGitHub,
		Hash:           issuesHash(issues, sessionMap, activeBots),
		System:         r.URL.Query().Get("system"),
		State:          strings.ToLower(r.URL.Query().Get("state")),
		Total:          len(index.Entries),
	}
	systems := map[string]int{}
	now := time.Now()
	for _, e := range index.Entries {
		switch e.State {
		case tracker.DecisionActive:
			data.Active++
		case tracker.DecisionDeferred:
			data.Deferred++
		case tracker.DecisionStale:
			data.Stale++
		}
		if e.ReplacedBy != nil {
			data.Replaced++
		}
		if e.Issue.System != "" {
			systems[e.Issue.System]++
		}
		if data.System != "" && !strings.EqualFold(e.Issue.System, data.System) {
			continue
		}
		if !matchesDecisionStateFilter(e, data.State) {
			continue
		}
		data.Rows = append(data.Rows, DecisionView{DecisionEntry: e, Prefix: prefix, now: now})
	}
	for name, n := range systems {
		data.Systems = append(data.Systems, DecisionChip{Name: name, Count: n})
	}
	sort.Slice(data.Systems, func(i, j int) bool { return data.Systems[i].Name < data.Systems[j].Name })

	if err := s.tmpl.ExecuteTemplate(w, "decisions.html", &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func matchesDecisionStateFilter(e *tracker.DecisionEntry, state string) bool {
	switch state {
	case "":
		return true
	case tracker.DecisionReplaced:
		return e.ReplacedBy != nil
	default:
		return e.State == state
	}
}
