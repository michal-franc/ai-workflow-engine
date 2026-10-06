package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// WorkTypeOption is one type of work as the viewer's selects and badges
// show it. Empty slices of these mean the project has no types and every
// type control is hidden.
type WorkTypeOption struct {
	Name        string
	Description string
	Path        string
	Default     bool
}

func workTypeOptions(wf *tracker.WorkflowConfig) []WorkTypeOption {
	if !wf.HasTypes() {
		return nil
	}
	var out []WorkTypeOption
	for _, name := range wf.TypeNames() {
		out = append(out, WorkTypeOption{
			Name:        name,
			Description: wf.Types[name].Description,
			Path:        wf.ForType(name).PathLine(),
			Default:     name == wf.DefaultType,
		})
	}
	return out
}

// resolvedType is the type governing issue ("" when the project has no types).
func resolvedType(wf *tracker.WorkflowConfig, issue *tracker.Issue) string {
	t, _ := wf.ResolveType(issue.Type)
	return t
}

// attachWorkTypes fills IssueView.WorkType with each issue's resolved type.
func attachWorkTypes(views []*IssueView, wf *tracker.WorkflowConfig) {
	if !wf.HasTypes() {
		return
	}
	for _, v := range views {
		if v != nil && v.Issue != nil {
			v.WorkType = resolvedType(wf, v.Issue)
		}
	}
}

// filterByType keeps issues whose resolved type is typ; "" keeps all.
func filterByType(issues []*tracker.Issue, wf *tracker.WorkflowConfig, typ string) []*tracker.Issue {
	if typ == "" || !wf.HasTypes() {
		return issues
	}
	var out []*tracker.Issue
	for _, issue := range issues {
		if resolvedType(wf, issue) == typ {
			out = append(out, issue)
		}
	}
	return out
}

// handleSetIssueType is the human's type change from the detail sidebar.
// Unlike `issue-cli set-type` it works at any status. When the current
// status is not on the new type's path the response is 409 with the
// statuses to pick from; the client re-posts with {"type","status"}.
func (s *Server) handleSetIssueType(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/issue/"), "/type")
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Type) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]interface{}{"error": "type required"})
		return
	}
	wf := proj.LoadWorkflow()
	status, err := wf.RetypeStatus(issue, body.Type, body.Status)
	if err != nil {
		code := http.StatusBadRequest
		resp := map[string]interface{}{"error": err.Error()}
		if _, ok := wf.Types[strings.TrimSpace(body.Type)]; ok && body.Status == "" {
			code = http.StatusConflict
			resp["statuses"] = wf.ForType(body.Type).GetStatusOrder()
		}
		writeJSONStatus(w, code, resp)
		return
	}
	move := ""
	if status != issue.Status {
		move = status
	}
	if err := tracker.SetIssueType(issue.FilePath, strings.TrimSpace(body.Type), move); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]interface{}{"error": "update failed: " + err.Error()})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]interface{}{"status": "ok", "type": strings.TrimSpace(body.Type), "issue_status": status})
}

func writeJSONStatus(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
