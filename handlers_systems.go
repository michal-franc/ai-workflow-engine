package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// handleSystems serves /systems (the index) and /systems/<System>[/<Family>].
// rest is the route after the project prefix.
func (s *Server) handleSystems(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix, rest string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	segs := splitSystemPath(strings.TrimPrefix(strings.TrimPrefix(rest, "systems"), "/"))
	if len(segs) == 0 {
		data := buildSystemsIndex(proj, issues, prefix)
		data.Hash = s.systemsHash(proj, issues)
		s.renderSystems(w, http.StatusOK, &data)
		return
	}

	wb := proj.LoadWorkflow().Workbench
	groups, _ := groupSystems(issues, wb)
	g := findSystem(groups, segs[0])
	var famKey string
	var fam *tracker.WorkbenchFamily
	if g != nil && len(segs) == 2 {
		if key, f, ok := g.Config.Family(segs[1]); ok {
			famKey, fam = key, &f
		}
	}
	if g == nil || len(segs) > 2 || (len(segs) == 2 && fam == nil) {
		data := buildSystemsIndex(proj, issues, prefix)
		data.Hash = s.systemsHash(proj, issues)
		data.NotFound = strings.Join(segs, " / ")
		s.renderSystems(w, http.StatusNotFound, &data)
		return
	}
	data := s.buildSystemPage(proj, issues, prefix, g, famKey, fam)
	if err := s.tmpl.ExecuteTemplate(w, "system.html", &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) systemsHash(proj *tracker.Project, issues []*tracker.Issue) string {
	sessionMap, activeBots := sessionsByIssueSlug(proj, issues)
	return issuesHash(issues, sessionMap, activeBots)
}

func (s *Server) renderSystems(w http.ResponseWriter, status int, data *SystemsIndexData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, "systems.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// splitSystemPath splits "AI/Play%20calls" into its unescaped segments.
func splitSystemPath(p string) []string {
	var out []string
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if seg == "" {
			continue
		}
		if u, err := url.PathUnescape(seg); err == nil {
			seg = u
		}
		out = append(out, seg)
	}
	return out
}
