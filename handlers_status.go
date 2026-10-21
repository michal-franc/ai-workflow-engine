package main

import (
	"net/http"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// loadStatus builds the Status model for a request. A tmux failure only
// drops the agent columns.
func (s *Server) loadStatus(proj *tracker.Project, prefix string) (*StatusModel, error) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil, err
	}
	agents, _ := s.buildAgentsModel(proj, prefix)
	m := buildStatus(proj, issues, agents, time.Now())
	m.Prefix = prefix
	return m, nil
}

// handleStatusMD serves the Status page as markdown: the stand-up the lead
// pastes, and what "Copy as markdown" copies.
func (s *Server) handleStatusMD(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	m, err := s.loadStatus(proj, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(statusMarkdown(m)))
}
