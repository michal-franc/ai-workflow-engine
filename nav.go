package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// navView is one entry in the project sidebar. navViews is the only place a
// view is listed: adding a view is one entry here plus its handler, and every
// project template picks it up through the shared sidebar partial.
type navView struct {
	Key   string // active key a template passes to `sidebar`; also the test id suffix
	Label string
	Path  string // appended to the project prefix
	More  bool   // listed under the collapsed More group
	// Show, when set, hides the entry for projects it returns false for.
	Show func(s *Server, proj *tracker.Project) bool
}

// navViews in display order. Systems (D-2) joins the main group when its page
// exists; until then it is not listed (D-7).
var navViews = []navView{
	{Key: "status", Label: "Status", Path: "/status"},
	{Key: "board", Label: "Board", Path: "/board"},
	{Key: "list", Label: "List", Path: "/"},
	{Key: "balance", Label: "Balance", Path: "/balance", Show: func(s *Server, p *tracker.Project) bool {
		return p.LoadWorkflow().Workbench.HasBalance()
	}},
	{Key: "decisions", Label: "Decisions", Path: "/decisions"},
	{Key: "docs", Label: "Docs", Path: "/docs"},
	{Key: "graph", Label: "Graph", Path: "/graph", More: true},
	{Key: "canvas", Label: "Canvas", Path: "/canvas", More: true},
	{Key: "agents", Label: "Agents", Path: "/agents", More: true},
	{Key: "retros", Label: "Retros", Path: "/retros", More: true},
	{Key: "stats", Label: "Stats", Path: "/stats", More: true},
	{Key: "github", Label: "GitHub", Path: "/github", More: true, Show: func(s *Server, p *tracker.Project) bool {
		return p.SupportsGitHub
	}},
}

// NavItem is one rendered sidebar entry.
type NavItem struct {
	Key    string
	Label  string
	Href   string
	Active bool
	Note   string // muted text on the right: a count, "1 running"
	Live   bool   // Note is a running-agents count (green)
	// Attention marks Note as things waiting on the owner (amber chip).
	Attention bool
}

// NavCount is a name with an open-issue count (the Board's systems panel).
type NavCount struct {
	Name  string
	Count int
	Href  string
}

// Nav is everything the sidebar partial renders for one page.
type Nav struct {
	Prefix         string
	ProjectName    string
	Version        string
	Open           int
	Main           []NavItem
	More           []NavItem
	MoreActive     bool // the active view is in More, so the group starts open
	ActiveBots     int
	CreateInPage   bool // the page has the create modal; elsewhere New issue links to the list
	ProjectActions []tracker.CustomAction
	Systems        []NavCount // the Board's screen panel
}

// sidebarNav builds the sidebar for the project behind prefix, with active
// naming the current view (a navViews key, or "" for pages like issue detail).
// Templates call it as {{template "sidebar" (sidebar .Prefix "board")}}.
func (s *Server) sidebarNav(prefix, active string) Nav {
	proj := s.findProject(strings.TrimPrefix(prefix, "/p/"))
	if proj == nil {
		return Nav{Prefix: prefix}
	}
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	_, activeBots := sessionsByIssueSlug(proj, issues)

	open := 0
	bySystem := map[string]int{}
	var openIssues []*tracker.Issue
	for _, issue := range issues {
		if issue.Status == "done" {
			continue
		}
		open++
		bySystem[issue.System]++
		openIssues = append(openIssues, issue)
	}

	nav := Nav{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		Version:        proj.Version,
		Open:           open,
		ActiveBots:     activeBots,
		CreateInPage:   active == "list" || active == "board",
		ProjectActions: proj.LoadWorkflow().ProjectActions,
	}
	if nav.Version != "" && !strings.HasPrefix(nav.Version, "v") {
		nav.Version = "v" + nav.Version
	}

	for _, v := range navViews {
		if v.Show != nil && !v.Show(s, proj) {
			continue
		}
		item := NavItem{Key: v.Key, Label: v.Label, Href: prefix + v.Path, Active: v.Key == active}
		switch v.Key {
		case "status":
			if n := len(waitingOnOwner(proj, openIssues)); n > 0 {
				item.Note, item.Attention = fmt.Sprint(n), true
			}
		case "list":
			item.Note = fmt.Sprint(open)
		case "agents":
			if activeBots > 0 {
				item.Note, item.Live = fmt.Sprintf("%d running", activeBots), true
			}
		}
		if v.More {
			nav.More = append(nav.More, item)
			nav.MoreActive = nav.MoreActive || item.Active
		} else {
			nav.Main = append(nav.Main, item)
		}
	}

	if active == "board" {
		nav.Systems = systemCounts(prefix, bySystem)
	}
	return nav
}

// systemCounts lists systems by open count (most first), with issues that
// have no system last as "No system".
func systemCounts(prefix string, bySystem map[string]int) []NavCount {
	var out []NavCount
	for name, n := range bySystem {
		if name == "" {
			continue
		}
		out = append(out, NavCount{Name: name, Count: n, Href: prefix + "/board?system=" + name})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if n := bySystem[""]; n > 0 {
		out = append(out, NavCount{Name: "No system", Count: n})
	}
	return out
}
