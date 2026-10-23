package main

import (
	"fmt"
	"os"
	"path/filepath"
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

// navViews in display order. Systems shows whenever an issue has a system or
// workbench.systems is set (ui/workbench-sidebar-and-system-pages#D-2, replacing
// the visibility clause of this issue's D-2); sidebarNav decides, since it has
// the issues.
var navViews = []navView{
	{Key: "status", Label: "Status", Path: "/status"},
	{Key: "systems", Label: "Systems", Path: "/systems"},
	{Key: "workboard", Label: "Board", Path: "/workboard"},
	{Key: "list", Label: "List", Path: "/"},
	{Key: "balance", Label: "Balance", Path: "/balance", Show: func(s *Server, p *tracker.Project) bool {
		return p.LoadWorkflow().Workbench.HasBalance()
	}},
	{Key: "metrics", Label: "Metrics", Path: "/metrics", Show: func(s *Server, p *tracker.Project) bool {
		return hasMetricsOrTuning(p) || isGitCheckout(projectRoot(p))
	}},
	{Key: "decisions", Label: "Decisions", Path: "/decisions"},
	{Key: "docs", Label: "Docs", Path: "/docs"},
	// The classic board keeps drag-and-drop until the retire child removes it.
	{Key: "board", Label: "Classic board", Path: "/board", More: true},
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
	Alert  bool   // Note is a problem count (red), e.g. "3 off"
	// Attention marks Note as things waiting on the owner (amber chip).
	Attention bool
}

// NavCount is a name with an open-issue count (the systems panel on the
// Board and the systems pages).
type NavCount struct {
	Name   string
	Count  int
	Href   string
	Detail string // shown under the name in mono (a path) instead of a count
	Family bool   // a family, listed indented under its system
	Active bool
}

// NavPanel is a page's own screen panel in the sidebar: a label, rows, an
// optional legend and a note. Pages attach it with withPanel.
type NavPanel struct {
	Label  string
	Rows   []NavCount
	Legend bool // the Branches tab's overlap legend
	Note   string
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
	Systems        []NavCount // the screen panel on the Board and the systems pages
	Legend         bool       // the workboard's card legend panel
	Panel          *NavPanel  // another page's screen panel
	// Pages are the workbench.pages entries, indented under Status.
	Pages []NavItem
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
	systemNames := map[string]bool{}
	for _, issue := range issues {
		if name := strings.TrimSpace(issue.System); name != "" {
			systemNames[strings.ToLower(name)] = true
		}
		if issue.Status == "done" {
			continue
		}
		open++
		bySystem[issue.System]++
		openIssues = append(openIssues, issue)
	}

	for name := range proj.LoadWorkflow().Workbench.Systems {
		systemNames[strings.ToLower(name)] = true
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
	for i, p := range proj.LoadWorkflow().Workbench.Pages {
		key := fmt.Sprintf("page/%d", i)
		nav.Pages = append(nav.Pages, NavItem{Key: fmt.Sprint(i), Label: p.Title, Href: fmt.Sprintf("%s/pages/%d", prefix, i), Active: active == key})
	}
	if nav.Version != "" && !strings.HasPrefix(nav.Version, "v") {
		nav.Version = "v" + nav.Version
	}

	for _, v := range navViews {
		if v.Show != nil && !v.Show(s, proj) {
			continue
		}
		if v.Key == "systems" && len(systemNames) == 0 {
			continue
		}
		item := NavItem{Key: v.Key, Label: v.Label, Href: prefix + v.Path, Active: v.Key == active}
		switch v.Key {
		case "status":
			n := len(waitingOnOwner(proj, openIssues))
			if ciRedState(proj) != nil {
				n++
			}
			if unsentBugReports() > 0 {
				n++
			}
			if n > 0 {
				item.Note, item.Attention = fmt.Sprint(n), true
			}
		case "systems":
			item.Active = active == "systems" || strings.HasPrefix(active, "systems/")
			item.Note = fmt.Sprint(len(systemNames))
		case "list":
			item.Note = fmt.Sprint(open)
		case "agents":
			if activeBots > 0 {
				item.Note, item.Live = fmt.Sprintf("%d running", activeBots), true
			}
		case "metrics":
			if !hasMetricsOrTuning(proj) {
				item.Label = "Branches"
			}
			if note := metricsNavNote(proj); note != "" {
				item.Note, item.Alert = note, true
			}
		}
		if v.More {
			nav.More = append(nav.More, item)
			nav.MoreActive = nav.MoreActive || item.Active
		} else {
			nav.Main = append(nav.Main, item)
		}
	}

	switch {
	case active == "board":
		nav.Systems = systemCounts(bySystem, func(name string) string { return prefix + "/board?system=" + name })
	case active == "workboard":
		nav.Systems = systemCounts(bySystem, func(name string) string { return prefix + "/workboard?system=" + name })
		nav.Legend = true
	case active == "systems" || strings.HasPrefix(active, "systems/"):
		nav.Systems = systemsPanel(prefix, active, issues, proj.LoadWorkflow())
	}
	return nav
}

// systemCounts lists systems by open count (most first), each linking to
// href(name), with issues that have no system last as "No system".
func systemCounts(bySystem map[string]int, href func(name string) string) []NavCount {
	var out []NavCount
	for name, n := range bySystem {
		if name == "" {
			continue
		}
		out = append(out, NavCount{Name: name, Count: n, Href: href(name)})
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

func hasMetricsOrTuning(p *tracker.Project) bool {
	wb := p.LoadWorkflow().Workbench
	return wb.Metrics != (tracker.WorkbenchMetrics{}) || len(wb.Tuning) > 0
}

// isGitCheckout reports whether root has a .git directory or file (a
// worktree), without running git.
func isGitCheckout(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

// systemsPanel is the screen panel on the systems pages: every system with
// its open count, linking to its page, and its configured families indented
// below it. active is "systems", "systems/<System>" or
// "systems/<System>/<Family>".
func systemsPanel(prefix, active string, issues []*tracker.Issue, wf *tracker.WorkflowConfig) []NavCount {
	st := newStatusTones(wf)
	groups, none := groupSystems(issues, wf.Workbench)
	segs := splitSystemPath(strings.TrimPrefix(strings.TrimPrefix(active, "systems"), "/"))
	type entry struct {
		row  NavCount
		fams []NavCount
	}
	var entries []entry
	for _, g := range groups {
		e := entry{row: NavCount{Name: g.Name, Count: len(openIssues(st, g.Members)), Href: systemHref(prefix, g.Name)}}
		e.row.Active = len(segs) == 1 && strings.EqualFold(segs[0], g.Name)
		for _, fname := range g.Config.FamilyNames() {
			kids := childrenOf(g.Members, g.Config.Families[fname].Umbrella)
			e.fams = append(e.fams, NavCount{
				Name: fname, Count: len(openIssues(st, kids)), Href: familyHref(prefix, g.Name, fname), Family: true,
				Active: len(segs) == 2 && strings.EqualFold(segs[0], g.Name) && strings.EqualFold(segs[1], fname),
			})
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].row.Count != entries[j].row.Count {
			return entries[i].row.Count > entries[j].row.Count
		}
		return strings.ToLower(entries[i].row.Name) < strings.ToLower(entries[j].row.Name)
	})
	var out []NavCount
	for _, e := range entries {
		out = append(out, e.row)
		out = append(out, e.fams...)
	}
	if n := len(openIssues(st, none)); n > 0 {
		out = append(out, NavCount{Name: "No system", Count: n})
	}
	return out
}
