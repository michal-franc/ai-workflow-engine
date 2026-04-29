package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type dataAddRequest struct {
	Description string `json:"description"`
	Status      string `json:"status"`
}

type dataStatusRequest struct {
	Status string `json:"status"`
}

type dataCommentRequest struct {
	Comment string `json:"comment"`
}

// extractDataSlugAndID parses /<prefix>/issue/<slug>/data[/<id>[/<verb>]]. The
// slug may contain slashes (subdirectory issues). Returns slug, id (-1 when
// the id is not part of the path), and ok. Verb is matched by the caller.
func (s *Server) extractDataSlugAndID(path, prefix string) (slug string, id int, ok bool) {
	rest := strings.TrimPrefix(path, prefix+"/issue/")
	idx := strings.Index(rest, "/data")
	if idx < 0 {
		return "", 0, false
	}
	slug = rest[:idx]
	tail := rest[idx+len("/data"):]
	if tail == "" {
		return slug, -1, true
	}
	tail = strings.TrimPrefix(tail, "/")
	parts := strings.Split(tail, "/")
	parsed, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", 0, false
	}
	return slug, parsed, true
}

func (s *Server) handleDataAdd(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug, _, ok := s.extractDataSlugAndID(r.URL.Path, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	var req dataAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		http.Error(w, "description is required", http.StatusBadRequest)
		return
	}
	id, err := tracker.AddEntry(issue.FilePath, req.Description, req.Status)
	if err != nil {
		http.Error(w, "failed to add: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"id": id})
}

func (s *Server) handleDataSetStatus(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug, id, ok := s.extractDataSlugAndID(r.URL.Path, prefix)
	if !ok || id < 0 {
		http.NotFound(w, r)
		return
	}
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	var req dataStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := tracker.SetEntryStatus(issue.FilePath, id, req.Status); err != nil {
		http.Error(w, "failed: "+err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDataSetComment(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug, id, ok := s.extractDataSlugAndID(r.URL.Path, prefix)
	if !ok || id < 0 {
		http.NotFound(w, r)
		return
	}
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	var req dataCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := tracker.SetEntryComment(issue.FilePath, id, req.Comment); err != nil {
		http.Error(w, "failed: "+err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDataRemove(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug, id, ok := s.extractDataSlugAndID(r.URL.Path, prefix)
	if !ok || id < 0 {
		http.NotFound(w, r)
		return
	}
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	if err := tracker.RemoveEntry(issue.FilePath, id); err != nil {
		http.Error(w, "failed: "+err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
