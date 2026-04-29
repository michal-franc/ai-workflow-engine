package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type AddCommentRequest struct {
	Block int    `json:"block"`
	Text  string `json:"text"`
}

type CommentIDRequest struct {
	ID int `json:"id"`
}

func (s *Server) extractCommentSlug(path, prefix, suffix string) string {
	slug := strings.TrimPrefix(path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, suffix)
	return slug
}

func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := s.extractCommentSlug(r.URL.Path, prefix, "/comments")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	comments, err := tracker.LoadComments(issue.FilePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if comments == nil {
		comments = []tracker.Comment{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(comments)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := s.extractCommentSlug(r.URL.Path, prefix, "/comments")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	var req AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	if err := tracker.AddComment(issue.FilePath, req.Block, req.Text, "app"); err != nil {
		http.Error(w, "failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleToggleComment(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := s.extractCommentSlug(r.URL.Path, prefix, "/comments/toggle")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	var req CommentIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := tracker.ToggleComment(issue.FilePath, req.ID); err != nil {
		http.Error(w, "failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := s.extractCommentSlug(r.URL.Path, prefix, "/comments/delete")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	var req CommentIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := tracker.DeleteComment(issue.FilePath, req.ID); err != nil {
		http.Error(w, "failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
