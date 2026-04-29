package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type IssueView struct {
	*tracker.Issue
	ActiveSessions []AgentSession
	Score          *tracker.ScoreBreakdown
}

// HasScore reports whether this view carries a computed score.
func (i *IssueView) HasScore() bool {
	return i != nil && i.Score != nil
}

func (i *IssueView) HasActiveAgent() bool {
	return i != nil && len(i.ActiveSessions) > 0
}

type ListData struct {
	Issues         []*IssueView
	Statuses       []string
	Systems        []string
	Priorities     []string
	Labels         []string
	Assignees      []string
	Filter         FilterParams
	Total          int
	Filtered       int
	Prefix         string
	ProjectName    string
	ActiveBots     int
	SupportsGitHub bool
	ScoringEnabled bool
	Sort           string
}

type FilterParams struct {
	Status   string
	System   string
	Priority string
	Label    string
	Assignee string
	Search   string
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	total := len(issues)
	statuses, systems, priorities, labels, assignees := tracker.CollectFilterValues(issues)
	systems = mergeSubdirSystems(systems, proj.IssueDir)

	filter := FilterParams{
		Status:   r.URL.Query().Get("status"),
		System:   r.URL.Query().Get("system"),
		Priority: r.URL.Query().Get("priority"),
		Label:    r.URL.Query().Get("label"),
		Assignee: r.URL.Query().Get("assignee"),
		Search:   r.URL.Query().Get("search"),
	}

	filtered := filterIssues(issues, filter)
	sessionMap, activeBots := sessionsByIssueSlug(issues)

	wf := proj.LoadWorkflow()
	views := issueViews(filtered, sessionMap)
	scoring := &wf.Scoring
	attachScores(views, scoring)

	sortKey := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortKey == "" && scoring.Enabled && scoring.DefaultSort == "score_desc" {
		sortKey = "score"
	}
	if sortKey == "score" && scoring.Enabled {
		sortViewsByScore(views)
	}

	data := ListData{
		Issues:         views,
		Statuses:       statuses,
		Systems:        systems,
		Priorities:     priorities,
		Labels:         labels,
		Assignees:      assignees,
		Filter:         filter,
		Total:          total,
		Filtered:       len(filtered),
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		SupportsGitHub: proj.SupportsGitHub,
		ScoringEnabled: scoring.Enabled,
		Sort:           sortKey,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleHash(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	sessionMap, activeBots := sessionsByIssueSlug(issues)
	h := sha256.New()
	for _, issue := range issues {
		fmt.Fprintf(h, "%s:%s:%s:%d\n", issue.Slug, issue.Status, issue.Assignee, issue.ModTime.UnixNano())
		for _, session := range sessionMap[issue.Slug] {
			fmt.Fprintf(h, "session:%s:%s:%s\n", issue.Slug, session.Name, session.StartTime)
		}
	}
	fmt.Fprintf(h, "active-bots:%d\n", activeBots)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"hash": fmt.Sprintf("%x", h.Sum(nil))})
}

type issueJSON struct {
	Slug           string         `json:"slug"`
	Title          string         `json:"title"`
	Status         string         `json:"status"`
	System         string         `json:"system"`
	Priority       string         `json:"priority"`
	Assignee       string         `json:"assignee"`
	Version        string         `json:"version"`
	Labels         []string       `json:"labels"`
	ActiveSessions []AgentSession `json:"active_sessions"`
	HasActiveAgent bool           `json:"has_active_agent"`
}

func (s *Server) handleIssuesJSON(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	sessionMap, _ := sessionsByIssueSlug(issues)
	result := make([]issueJSON, len(issues))
	for i, issue := range issues {
		activeSessions := append([]AgentSession(nil), sessionMap[issue.Slug]...)
		result[i] = issueJSON{
			Slug:           issue.Slug,
			Title:          issue.Title,
			Status:         issue.Status,
			System:         issue.System,
			Priority:       issue.Priority,
			Assignee:       issue.Assignee,
			Version:        issue.Version,
			Labels:         issue.Labels,
			ActiveSessions: activeSessions,
			HasActiveAgent: len(activeSessions) > 0,
		}
		if result[i].Labels == nil {
			result[i].Labels = []string{}
		}
		if result[i].ActiveSessions == nil {
			result[i].ActiveSessions = []AgentSession{}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// mergeSubdirSystems merges subdirectory names into the systems list so that
// empty folders still appear as available systems.
func mergeSubdirSystems(systems []string, issueDir string) []string {
	subdirSystems := tracker.CollectSubdirSystems(issueDir)
	seen := map[string]bool{}
	for _, s := range systems {
		seen[s] = true
	}
	for _, s := range subdirSystems {
		if !seen[s] {
			systems = append(systems, s)
			seen[s] = true
		}
	}
	sort.Strings(systems)
	return systems
}

func filterIssues(issues []*tracker.Issue, f FilterParams) []*tracker.Issue {
	var result []*tracker.Issue
	for _, issue := range issues {
		if f.Status != "" && issue.Status != f.Status {
			continue
		}
		if f.System != "" && !strings.EqualFold(issue.System, f.System) {
			continue
		}
		if f.Priority != "" && issue.Priority != f.Priority {
			continue
		}
		if f.Label != "" {
			hasLabel := false
			for _, l := range issue.Labels {
				if strings.EqualFold(l, f.Label) {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				continue
			}
		}
		if f.Assignee == "_claimed" && issue.Assignee == "" {
			continue
		}
		if f.Assignee == "_unclaimed" && issue.Assignee != "" {
			continue
		}
		if f.Assignee != "" && f.Assignee != "_claimed" && f.Assignee != "_unclaimed" && issue.Assignee != f.Assignee {
			continue
		}
		if f.Search != "" {
			search := strings.ToLower(f.Search)
			if !strings.Contains(strings.ToLower(issue.Title), search) &&
				!strings.Contains(strings.ToLower(issue.BodyRaw), search) {
				continue
			}
		}
		result = append(result, issue)
	}
	return result
}

func issueViews(issues []*tracker.Issue, sessionMap map[string][]AgentSession) []*IssueView {
	result := make([]*IssueView, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issueView(issue, sessionMap))
	}
	return result
}

func issueView(issue *tracker.Issue, sessionMap map[string][]AgentSession) *IssueView {
	if issue == nil {
		return nil
	}
	return &IssueView{
		Issue:          issue,
		ActiveSessions: append([]AgentSession(nil), sessionMap[issue.Slug]...),
	}
}

// attachScores computes and attaches ScoreBreakdowns to each view when the
// workflow's scoring block is enabled. No-op otherwise.
func attachScores(views []*IssueView, scoring *tracker.ScoringConfig) {
	if scoring == nil || !scoring.Enabled {
		return
	}
	now := time.Now()
	for _, v := range views {
		if v == nil || v.Issue == nil {
			continue
		}
		v.Score = tracker.ComputeScore(v.Issue, scoring, now)
	}
}

// sortViewsByScore sorts in-place by Score.Total descending. Views with no
// score (nil Score) sort last, stable.
func sortViewsByScore(views []*IssueView) {
	sort.SliceStable(views, func(i, j int) bool {
		si, sj := views[i].Score, views[j].Score
		if si == nil && sj == nil {
			return false
		}
		if si == nil {
			return false
		}
		if sj == nil {
			return true
		}
		return si.Total > sj.Total
	})
}
