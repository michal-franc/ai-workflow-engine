package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"gopkg.in/yaml.v3"
)

type WorkflowDesignerData struct {
	Prefix         string
	ProjectName    string
	WorkflowJSON   string
	WorkflowYAML   string
	WorkflowIssues string
	WorkflowSource string
	WorkflowTarget string
	SupportsGitHub bool
}

type WorkflowFlowData struct {
	Prefix         string
	ProjectName    string
	SupportsGitHub bool
}

type RetroEntry struct {
	FileName     string
	FilePath     string
	IssueSlug    string
	IssueTitle   string
	Status       string
	System       string
	Date         string
	BodyHTML     string
	BodyRaw      string
	ModTime      time.Time
	ReviewStatus string
}

type ToolBugReportView struct {
	FileName    string
	FilePath    string
	IssueSlug   string
	Description string
	Tool        string
	Timestamp   string
	Status      string
}

type RetrosData struct {
	Retros         []*RetroEntry
	Bugs           []*ToolBugReportView
	RetroStatus    string
	BugStatus      string
	RetroCounts    map[string]int
	BugCounts      map[string]int
	Prefix         string
	ProjectName    string
	ActiveBots     int
	SupportsGitHub bool
}

type statusUpdateResponse struct {
	Status string `json:"status"`
}


func (s *Server) handleWorkflowDesigner(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	wf := proj.LoadWorkflow()
	workflowJSON, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	workflowYAML, err := yaml.Marshal(wf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type workflowIssue struct {
		Slug   string `json:"slug"`
		Title  string `json:"title"`
		Status string `json:"status"`
		System string `json:"system"`
	}
	issueOptions := make([]workflowIssue, 0, len(issues))
	for _, issue := range issues {
		issueOptions = append(issueOptions, workflowIssue{
			Slug:   issue.Slug,
			Title:  issue.Title,
			Status: issue.Status,
			System: issue.System,
		})
	}
	issuesJSON, err := json.Marshal(issueOptions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	source := "built-in default workflow"
	target := workflowFileTarget(proj)
	switch {
	case proj.WorkflowFile != "":
		source = proj.WorkflowFile
	case fileExists("workflow.yaml"):
		source = "workflow.yaml"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "workflow-designer.html", WorkflowDesignerData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		WorkflowJSON:   string(workflowJSON),
		WorkflowYAML:   string(workflowYAML),
		WorkflowIssues: string(issuesJSON),
		WorkflowSource: source,
		WorkflowTarget: target,
		SupportsGitHub: proj.SupportsGitHub,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleWorkflowFlow(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "workflow-flow.html", WorkflowFlowData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		SupportsGitHub: proj.SupportsGitHub,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleRetros(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	retros, err := loadRetrospectives(proj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bugs, err := loadRelatedToolBugs(issues)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	retroCounts := countRetrosByStatus(retros)
	bugCounts := countBugsByStatus(bugs)

	retroStatus := normalizeRetroReviewStatus(r.URL.Query().Get("retro_status"))
	if retroStatus != "" {
		retros = filterRetrosByStatus(retros, retroStatus)
	}

	bugStatus := normalizeBugStatus(r.URL.Query().Get("bug_status"))
	if bugStatus != "" {
		bugs = filterBugsByStatus(bugs, bugStatus)
	}

	_, activeBots := sessionsByIssueSlug(issues)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "retros.html", RetrosData{
		Retros:         retros,
		Bugs:           bugs,
		RetroStatus:    retroStatus,
		BugStatus:      bugStatus,
		RetroCounts:    retroCounts,
		BugCounts:      bugCounts,
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		SupportsGitHub: proj.SupportsGitHub,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func loadRetrospectives(proj *tracker.Project) ([]*RetroEntry, error) {
	retroDir := filepath.Join(projectRoot(proj), "retros")
	info, err := os.Stat(retroDir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}

	var retros []*RetroEntry
	err = filepath.WalkDir(retroDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		meta, body := parseRetrospective(string(data))
		if strings.TrimSpace(body) == "" {
			body = strings.TrimSpace(string(data))
		}

		page, err := tracker.ParseDocPage(filepath.Base(path), []byte(body))
		if err != nil {
			return err
		}

		fileInfo, err := d.Info()
		if err != nil {
			return err
		}

		retros = append(retros, &RetroEntry{
			FileName:     d.Name(),
			FilePath:     path,
			IssueSlug:    meta["Issue"],
			IssueTitle:   meta["Title"],
			Status:       meta["Status"],
			System:       meta["System"],
			Date:         meta["Date"],
			BodyHTML:     page.BodyHTML,
			BodyRaw:      body,
			ModTime:      fileInfo.ModTime(),
			ReviewStatus: normalizeRetroReviewStatus(meta["ReviewStatus"]),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking retros directory %s: %w", retroDir, err)
	}

	sort.Slice(retros, func(i, j int) bool {
		return retros[i].ModTime.After(retros[j].ModTime)
	})

	return retros, nil
}

func parseRetrospective(raw string) (map[string]string, string) {
	content := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	meta := map[string]string{}

	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[start]), "#") {
		start++
	}
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	idx := start
	for idx < len(lines) {
		line := strings.TrimSpace(lines[idx])
		if line == "" {
			idx++
			break
		}
		switch {
		case strings.HasPrefix(line, "Issue:"):
			meta["Issue"] = strings.TrimSpace(strings.TrimPrefix(line, "Issue:"))
		case strings.HasPrefix(line, "Title:"):
			meta["Title"] = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
		case strings.HasPrefix(line, "Status:"):
			meta["Status"] = strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
		case strings.HasPrefix(line, "System:"):
			meta["System"] = strings.TrimSpace(strings.TrimPrefix(line, "System:"))
		case strings.HasPrefix(line, "Date:"):
			meta["Date"] = strings.TrimSpace(strings.TrimPrefix(line, "Date:"))
		case strings.HasPrefix(line, "ReviewStatus:"):
			meta["ReviewStatus"] = strings.TrimSpace(strings.TrimPrefix(line, "ReviewStatus:"))
		default:
			return meta, strings.TrimSpace(content)
		}
		idx++
	}

	if len(meta) == 0 {
		return meta, strings.TrimSpace(content)
	}
	return meta, strings.TrimSpace(strings.Join(lines[idx:], "\n"))
}

func loadRelatedToolBugs(issues []*tracker.Issue) ([]*ToolBugReportView, error) {
	issueSlugs := map[string]bool{}
	for _, issue := range issues {
		issueSlugs[issue.Slug] = true
	}

	type bugReport struct {
		IssueSlug   string `json:"issue_slug"`
		Description string `json:"description"`
		Tool        string `json:"tool"`
		Timestamp   string `json:"ts"`
		Status      string `json:"status"`
	}

	bugDir := filepath.Join(".", "bugs")
	info, err := os.Stat(bugDir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}

	var bugs []*ToolBugReportView
	err = filepath.WalkDir(bugDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var report bugReport
		if err := json.Unmarshal(data, &report); err != nil {
			return nil
		}
		if report.IssueSlug == "" || !issueSlugs[report.IssueSlug] {
			return nil
		}

		bugs = append(bugs, &ToolBugReportView{
			FileName:    d.Name(),
			FilePath:    path,
			IssueSlug:   report.IssueSlug,
			Description: report.Description,
			Tool:        report.Tool,
			Timestamp:   report.Timestamp,
			Status:      normalizeBugStatus(report.Status),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking bugs directory %s: %w", bugDir, err)
	}

	sort.Slice(bugs, func(i, j int) bool {
		return bugs[i].Timestamp > bugs[j].Timestamp
	})

	return bugs, nil
}

func normalizeRetroReviewStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "open":
		return "open"
	case "processed", "done":
		return "processed"
	default:
		return "open"
	}
}

func normalizeBugStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "open":
		return "open"
	case "fixed", "done":
		return "fixed"
	case "wontfix", "won't fix", "wont-fix":
		return "wontfix"
	default:
		return "open"
	}
}

func filterRetrosByStatus(retros []*RetroEntry, status string) []*RetroEntry {
	var filtered []*RetroEntry
	for _, retro := range retros {
		if normalizeRetroReviewStatus(retro.ReviewStatus) == status {
			filtered = append(filtered, retro)
		}
	}
	return filtered
}

func filterBugsByStatus(bugs []*ToolBugReportView, status string) []*ToolBugReportView {
	var filtered []*ToolBugReportView
	for _, bug := range bugs {
		if normalizeBugStatus(bug.Status) == status {
			filtered = append(filtered, bug)
		}
	}
	return filtered
}

func countRetrosByStatus(retros []*RetroEntry) map[string]int {
	counts := map[string]int{
		"open":      0,
		"processed": 0,
	}
	for _, retro := range retros {
		counts[normalizeRetroReviewStatus(retro.ReviewStatus)]++
	}
	return counts
}

func countBugsByStatus(bugs []*ToolBugReportView) map[string]int {
	counts := map[string]int{
		"open":    0,
		"fixed":   0,
		"wontfix": 0,
	}
	for _, bug := range bugs {
		counts[normalizeBugStatus(bug.Status)]++
	}
	return counts
}

func findRetroByFileName(proj *tracker.Project, fileName string) (*RetroEntry, error) {
	retros, err := loadRetrospectives(proj)
	if err != nil {
		return nil, err
	}
	for _, retro := range retros {
		if retro.FileName == fileName {
			return retro, nil
		}
	}
	return nil, nil
}

func (s *Server) handleUpdateRetroStatus(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	fileName := strings.TrimPrefix(r.URL.Path, prefix+"/retros/retro/")
	fileName = strings.TrimSuffix(fileName, "/status")
	fileName = strings.TrimSpace(fileName)
	if fileName == "" || strings.Contains(fileName, "/") || strings.Contains(fileName, `\`) {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	status := normalizeRetroReviewStatus(body.Status)

	retro, err := findRetroByFileName(proj, fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}
	if retro == nil {
		http.NotFound(w, r)
		return
	}

	if err := writeRetrospectiveStatus(retro, status); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusUpdateResponse{Status: status})
}

func writeRetrospectiveStatus(retro *RetroEntry, status string) error {
	if retro == nil {
		return fmt.Errorf("retro is required")
	}
	content, err := os.ReadFile(retro.FilePath)
	if err != nil {
		return err
	}
	updated := replaceOrInsertRetrospectiveStatus(string(content), status)
	return os.WriteFile(retro.FilePath, []byte(updated), 0644)
}

func replaceOrInsertRetrospectiveStatus(content string, status string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "ReviewStatus:") {
			lines[i] = "ReviewStatus: " + status
			return strings.Join(lines, "\n")
		}
	}

	insertAt := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Date:") {
			insertAt = i + 1
		}
	}
	if insertAt == -1 {
		insertAt = 0
		for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) == "" {
			insertAt++
		}
		if insertAt < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[insertAt]), "#") {
			insertAt++
		}
		for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) != "" {
			insertAt++
		}
	}

	newLines := append([]string{}, lines[:insertAt]...)
	newLines = append(newLines, "ReviewStatus: "+status)
	newLines = append(newLines, lines[insertAt:]...)
	return strings.Join(newLines, "\n")
}

func (s *Server) handleUpdateBugStatus(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	fileName := strings.TrimPrefix(r.URL.Path, prefix+"/retros/bug/")
	fileName = strings.TrimSuffix(fileName, "/status")
	fileName = strings.TrimSpace(fileName)
	if fileName == "" || strings.Contains(fileName, "/") || strings.Contains(fileName, `\`) {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	status := normalizeBugStatus(body.Status)

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}
	bugs, err := loadRelatedToolBugs(issues)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}

	var target *ToolBugReportView
	for _, bug := range bugs {
		if bug.FileName == fileName {
			target = bug
			break
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}

	if err := writeBugStatus(target.FilePath, status); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusUpdateResponse{Status: status})
}

func writeBugStatus(path string, status string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		return err
	}
	data["status"] = status

	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0644)
}

func (s *Server) handleWorkflowDesignerData(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	wf := proj.LoadWorkflow()
	source := "built-in default workflow"
	target := workflowFileTarget(proj)
	switch {
	case proj.WorkflowFile != "":
		source = proj.WorkflowFile
	case fileExists("workflow.yaml"):
		source = "workflow.yaml"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workflow": wf,
		"source":   source,
		"target":   target,
	})
}

func (s *Server) handleWorkflowDesignerPreview(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	var req struct {
		Slug string `json:"slug"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.To) == "" {
		http.Error(w, `{"error":"slug and to are required"}`, http.StatusBadRequest)
		return
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed loading issues: %s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}

	var issue *tracker.Issue
	for _, candidate := range issues {
		if candidate.Slug == req.Slug {
			issue = candidate
			break
		}
	}
	if issue == nil {
		http.Error(w, `{"error":"issue not found"}`, http.StatusNotFound)
		return
	}

	comments, err := tracker.LoadComments(issue.FilePath)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf(`{"error":"failed loading comments: %s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}

	wf := proj.LoadWorkflow().ForSystem(issue.System)
	preview := wf.PreviewTransition(issue, issue.Status, strings.TrimSpace(req.To), issue.System, comments)
	if !wf.IsValidTransition(issue.Status, strings.TrimSpace(req.To)) {
		preview.Allowed = false
		preview.ValidationError = fmt.Sprintf("cannot transition from %q to %q", issue.Status, strings.TrimSpace(req.To))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"issue": map[string]string{
			"slug":   issue.Slug,
			"title":  issue.Title,
			"status": issue.Status,
			"system": issue.System,
		},
		"preview": preview,
	})
}

func (s *Server) handleSaveWorkflowDesigner(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	var body struct {
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(body.YAML) == "" {
		http.Error(w, `{"error":"yaml is required"}`, http.StatusBadRequest)
		return
	}

	var cfg tracker.WorkflowConfig
	if err := yaml.Unmarshal([]byte(body.YAML), &cfg); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid workflow yaml: %s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusBadRequest)
		return
	}

	target := workflowFileTarget(proj)
	if err := os.WriteFile(target, []byte(body.YAML+"\n"), 0644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, strings.ReplaceAll(err.Error(), `"`, `'`)), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"path":   target,
	})
}

// --- Docs ---

type DocsData struct {
	Page           *tracker.DocPage
	Pages          []*tracker.DocPage
	Sections       []tracker.DocSection
	Prefix         string
	ProjectName    string
	SupportsGitHub bool
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	pages, err := tracker.LoadDocs(proj.DocsDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(pages) > 0 {
		http.Redirect(w, r, fmt.Sprintf("%s/docs/%s", prefix, pages[0].Slug), http.StatusFound)
		return
	}

	sections := tracker.GroupDocSections(pages)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "docs.html", DocsData{Pages: pages, Sections: sections, Prefix: prefix, ProjectName: proj.Name, SupportsGitHub: proj.SupportsGitHub}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleDocPage(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/docs/")
	if slug == "" {
		s.handleDocs(w, r, proj, prefix)
		return
	}

	pages, err := tracker.LoadDocs(proj.DocsDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var found *tracker.DocPage
	for _, p := range pages {
		if p.Slug == slug {
			found = p
			break
		}
	}

	if found == nil {
		http.NotFound(w, r)
		return
	}

	sections := tracker.GroupDocSections(pages)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "docs.html", DocsData{Page: found, Pages: pages, Sections: sections, Prefix: prefix, ProjectName: proj.Name, SupportsGitHub: proj.SupportsGitHub}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
