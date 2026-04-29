package main

import (
	"encoding/json"
	"net/http"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type GitHubPageData struct {
	Prefix         string
	ProjectName    string
	Repo           string
	ActiveBots     int
	SupportsGitHub bool
}

func (s *Server) handleGitHub(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	if !proj.SupportsGitHub {
		http.NotFound(w, r)
		return
	}
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	_, activeBots := sessionsByIssueSlug(issues)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "github.html", GitHubPageData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		Repo:           proj.Repo,
		ActiveBots:     activeBots,
		SupportsGitHub: proj.SupportsGitHub,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleGitHubFetch(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	if !proj.SupportsGitHub {
		http.NotFound(w, r)
		return
	}
	if proj.Repo == "" {
		http.Error(w, "project has no `repo` configured", http.StatusBadRequest)
		return
	}
	remote, err := FetchGitHubIssues(proj.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	local, err := LocalGitHubURLs(proj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range remote {
		remote[i].Imported = local[RemoteIssueURL(proj.Repo, remote[i].Number)]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"repo":   proj.Repo,
		"issues": remote,
	})
}

func (s *Server) handleGitHubImport(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	if !proj.SupportsGitHub {
		http.NotFound(w, r)
		return
	}
	if proj.Repo == "" {
		http.Error(w, "project has no `repo` configured", http.StatusBadRequest)
		return
	}
	var req struct {
		Numbers []int `json:"numbers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Numbers) == 0 {
		http.Error(w, "no issues selected", http.StatusBadRequest)
		return
	}

	remote, err := FetchGitHubIssues(proj.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	byNum := make(map[int]GitHubIssue, len(remote))
	for _, iss := range remote {
		byNum[iss.Number] = iss
	}

	type result struct {
		Number int    `json:"number"`
		OK     bool   `json:"ok"`
		Path   string `json:"path,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(req.Numbers))
	imported := 0
	for _, n := range req.Numbers {
		iss, ok := byNum[n]
		if !ok {
			results = append(results, result{Number: n, OK: false, Error: "not found on remote"})
			continue
		}
		path, err := ImportGitHubIssue(proj, iss)
		if err != nil {
			results = append(results, result{Number: n, OK: false, Error: err.Error()})
			continue
		}
		imported++
		results = append(results, result{Number: n, OK: true, Path: path})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"imported": imported,
		"results":  results,
	})
}
