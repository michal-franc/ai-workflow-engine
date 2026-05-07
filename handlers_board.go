package main

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type BoardColumn struct {
	Status      string
	Description string
	Optional    bool
	Global      bool
	Issues      []*IssueView
}

type BoardCardField struct {
	Name   string
	Value  string
	Values []string
	IsList bool
}

type BoardData struct {
	Columns        []*BoardColumn
	Total          int
	Versions       []string
	Version        string
	Systems        []string
	System         string
	Assignees      []string
	Assignee       string
	Priorities     []string
	Labels         []string
	Prefix         string
	ProjectName    string
	ActiveBots     int
	CardFields     []string
	SupportsGitHub bool
	ScoringEnabled bool
	Sort           string
	HideEmpty      bool
}

type GraphStatusNode struct {
	Name            string
	Description     string
	RequireApproval bool
	Optional        bool
	Issues          []*GraphIssueNode
}

type GraphIssueNode struct {
	Slug         string
	Title        string
	System       string
	Priority     string
	Assignee     string
	DaysInStatus int
	IsStale      bool
	IsVeryStale  bool
}

type GraphData struct {
	Prefix         string
	ProjectName    string
	ActiveBots     int
	StatusNodes    []*GraphStatusNode
	Systems        []string
	System         string
	ShowDone       bool
	HideEmpty      bool
	TotalIssues    int
	SupportsGitHub bool
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	versionSet := map[string]bool{}
	systemSet := map[string]bool{}
	assigneeSet := map[string]bool{}
	prioritySet := map[string]bool{}
	labelSet := map[string]bool{}
	for _, issue := range issues {
		if issue.Version != "" {
			versionSet[issue.Version] = true
		}
		if issue.System != "" {
			systemSet[issue.System] = true
		}
		if issue.Assignee != "" {
			assigneeSet[issue.Assignee] = true
		}
		if issue.Priority != "" {
			prioritySet[issue.Priority] = true
		}
		for _, l := range issue.Labels {
			labelSet[l] = true
		}
	}
	var versions []string
	for v := range versionSet {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	var systems []string
	for s := range systemSet {
		systems = append(systems, s)
	}
	systems = mergeSubdirSystems(systems, proj.IssueDir)
	var assignees []string
	for a := range assigneeSet {
		assignees = append(assignees, a)
	}
	sort.Strings(assignees)
	var priorities []string
	for p := range prioritySet {
		priorities = append(priorities, p)
	}
	sort.Strings(priorities)
	var labels []string
	for l := range labelSet {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	versionFilter := r.URL.Query().Get("version")
	systemFilter := r.URL.Query().Get("system")
	assigneeFilter := r.URL.Query().Get("assignee")
	hideEmpty := r.URL.Query().Get("hide_empty") == "1"

	var filtered []*tracker.Issue
	for _, issue := range issues {
		if versionFilter != "" && issue.Version != versionFilter {
			continue
		}
		if systemFilter != "" && !strings.EqualFold(issue.System, systemFilter) {
			continue
		}
		if assigneeFilter == "_claimed" && issue.Assignee == "" {
			continue
		}
		if assigneeFilter == "_unclaimed" && issue.Assignee != "" {
			continue
		}
		if assigneeFilter != "" && assigneeFilter != "_claimed" && assigneeFilter != "_unclaimed" && issue.Assignee != assigneeFilter {
			continue
		}
		filtered = append(filtered, issue)
	}
	issues = filtered

	wf := proj.LoadWorkflow()
	statusOrder := wf.GetBoardColumns()
	statusDescs := wf.GetStatusDescriptions()
	sessionMap, activeBots := sessionsByIssueSlug(issues)
	scoring := &wf.Scoring

	sortKey := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortKey == "" && scoring.Enabled && scoring.DefaultSort == "score_desc" {
		sortKey = "score"
	}

	byStatus := map[string][]*IssueView{}
	seen := map[string]bool{}
	for _, issue := range issues {
		st := issue.Status
		if st == "" {
			st = "none"
		}
		view := issueView(issue, sessionMap)
		if scoring.Enabled {
			view.Score = tracker.ComputeScore(issue, scoring, time.Now())
		}
		byStatus[st] = append(byStatus[st], view)
		seen[st] = true
	}

	if sortKey == "score" && scoring.Enabled {
		for _, list := range byStatus {
			sortViewsByScore(list)
		}
	}

	var columns []*BoardColumn
	added := map[string]bool{}
	for _, st := range statusOrder {
		desc := statusDescs[st]
		optional := false
		global := false
		if ws := wf.GetStatus(st); ws != nil {
			optional = ws.Optional
			global = ws.Global
		}
		columns = append(columns, &BoardColumn{Status: st, Description: desc, Optional: optional, Global: global, Issues: byStatus[st]})
		added[st] = true
	}
	for st := range seen {
		if !added[st] {
			columns = append(columns, &BoardColumn{Status: st, Issues: byStatus[st]})
		}
	}

	if hideEmpty {
		filteredCols := columns[:0]
		for _, c := range columns {
			if len(c.Issues) > 0 {
				filteredCols = append(filteredCols, c)
			}
		}
		columns = filteredCols
	}

	data := BoardData{
		Columns:        columns,
		Total:          len(issues),
		Versions:       versions,
		Version:        versionFilter,
		Systems:        systems,
		System:         systemFilter,
		Assignees:      assignees,
		Assignee:       assigneeFilter,
		Priorities:     priorities,
		Labels:         labels,
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		CardFields:     wf.GetBoardCardFields(),
		SupportsGitHub: proj.SupportsGitHub,
		ScoringEnabled: scoring.Enabled,
		Sort:           sortKey,
		HideEmpty:      hideEmpty,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "board.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wf := proj.LoadWorkflow()

	approvalRequired := map[string]bool{}
	for _, t := range wf.Transitions {
		for _, a := range t.Actions {
			if a.Type == "require_human_approval" {
				target := strings.TrimSpace(a.Status)
				if target == "" {
					target = t.To
				}
				approvalRequired[target] = true
			}
		}
	}

	systemFilter := r.URL.Query().Get("system")
	showDone := r.URL.Query().Get("done") == "1"
	hideEmpty := r.URL.Query().Get("hide_empty") == "1"

	systemSet := map[string]bool{}
	for _, issue := range issues {
		if issue.System != "" {
			systemSet[issue.System] = true
		}
	}
	var systems []string
	for sys := range systemSet {
		systems = append(systems, sys)
	}
	systems = mergeSubdirSystems(systems, proj.IssueDir)

	now := time.Now()
	statusOrder := wf.GetStatusOrder()
	statusDescs := wf.GetStatusDescriptions()

	nodeMap := map[string]*GraphStatusNode{}
	for _, name := range statusOrder {
		optional := false
		if s := wf.GetStatus(name); s != nil {
			optional = s.Optional
		}
		nodeMap[name] = &GraphStatusNode{
			Name:            name,
			Description:     statusDescs[name],
			RequireApproval: approvalRequired[name],
			Optional:        optional,
		}
	}

	totalIssues := 0
	for _, issue := range issues {
		if !showDone && issue.Status == "done" {
			continue
		}
		if systemFilter != "" && !strings.EqualFold(issue.System, systemFilter) {
			continue
		}
		totalIssues++

		days := int(now.Sub(issue.ModTime).Hours() / 24)
		node := &GraphIssueNode{
			Slug:         issue.Slug,
			Title:        issue.Title,
			System:       issue.System,
			Priority:     issue.Priority,
			Assignee:     issue.Assignee,
			DaysInStatus: days,
			IsStale:      days >= 7,
			IsVeryStale:  days >= 14,
		}

		status := issue.Status
		if status == "" {
			status = "none"
		}
		if sn, ok := nodeMap[status]; ok {
			sn.Issues = append(sn.Issues, node)
		} else {
			nodeMap[status] = &GraphStatusNode{Name: status, Issues: []*GraphIssueNode{node}}
		}
	}

	var nodes []*GraphStatusNode
	for _, name := range statusOrder {
		if !showDone && name == "done" {
			continue
		}
		if sn, ok := nodeMap[name]; ok {
			if hideEmpty && len(sn.Issues) == 0 {
				continue
			}
			nodes = append(nodes, sn)
		}
	}

	_, activeBots := sessionsByIssueSlug(issues)

	data := GraphData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		StatusNodes:    nodes,
		Systems:        systems,
		System:         systemFilter,
		ShowDone:       showDone,
		HideEmpty:      hideEmpty,
		TotalIssues:    totalIssues,
		SupportsGitHub: proj.SupportsGitHub,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "graph.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
