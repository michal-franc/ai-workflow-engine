package main

import (
	"embed"
	"encoding/json"
	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Server struct {
	projects []tracker.Project
	tmpl     *template.Template
}

// AgentSession is one live tmux session (or shared-session window).
type AgentSession = agentrun.Session

type approvalResponse struct {
	Status              string `json:"status"`
	HumanApproval       string `json:"human_approval"`
	QueuedApproval      string `json:"queued_approval,omitempty"`
	NotifiedSession     string `json:"notified_session,omitempty"`
	NotificationMessage string `json:"notification_message,omitempty"`
	NotificationError   string `json:"notification_error,omitempty"`
	Label               string `json:"label,omitempty"`
}

func NewServer(projects []tracker.Project) (*Server, error) {
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	s := &Server{
		projects: projects,
		tmpl:     tmpl,
	}
	tmpl.Funcs(template.FuncMap{"sidebar": s.sidebarNav})
	return s, nil
}

func (s *Server) findProject(slug string) *tracker.Project {
	for i := range s.projects {
		if s.projects[i].Slug == slug {
			return &s.projects[i]
		}
	}
	return nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleProjectList)
	mux.HandleFunc("/p/", s.handleProjectRoutes)
	mux.HandleFunc("/project-for", s.handleProjectFor)
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	return mux
}

// handleProjectRoutes dispatches /p/<slug>/... to the right handler
func (s *Server) handleProjectRoutes(w http.ResponseWriter, r *http.Request) {
	// Parse /p/<slug>/...
	path := strings.TrimPrefix(r.URL.Path, "/p/")
	parts := strings.SplitN(path, "/", 2)
	projectSlug := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = parts[1]
	}

	proj := s.findProject(projectSlug)
	if proj == nil {
		http.NotFound(w, r)
		return
	}

	prefix := "/p/" + projectSlug

	switch {
	case rest == "" || rest == "/":
		s.handleList(w, r, proj, prefix)
	case rest == "board":
		s.handleBoard(w, r, proj, prefix)
	case rest == "graph":
		s.handleGraph(w, r, proj, prefix)
	case rest == "canvas":
		s.handleCanvas(w, r, proj, prefix)
	case rest == "github" && r.Method == http.MethodGet:
		s.handleGitHub(w, r, proj, prefix)
	case rest == "github/fetch" && r.Method == http.MethodPost:
		s.handleGitHubFetch(w, r, proj)
	case rest == "github/import" && r.Method == http.MethodPost:
		s.handleGitHubImport(w, r, proj)
	case rest == "stats":
		s.handleStats(w, r, proj, prefix)
	case rest == "agents" && r.Method == http.MethodGet:
		s.handleAgents(w, r, proj, prefix)
	case rest == "agents.json" && r.Method == http.MethodGet:
		s.handleAgentsJSON(w, r, proj, prefix)
	case rest == "agents/detail" && r.Method == http.MethodGet:
		s.handleAgentDetail(w, r, proj, prefix)
	case rest == "agents/open" && r.Method == http.MethodPost:
		s.handleAgentOpen(w, r, proj, prefix)
	case rest == "lead/spawn" && r.Method == http.MethodPost:
		s.handleLeadSpawn(w, r, proj, prefix)
	case rest == "retros":
		s.handleRetros(w, r, proj, prefix)
	case rest == "retros/review" && r.Method == http.MethodPost:
		s.handleRetrosReviewDispatch(w, r, proj)
	case strings.HasPrefix(rest, "retros/retro/") && strings.HasSuffix(rest, "/status") && r.Method == http.MethodPost:
		s.handleUpdateRetroStatus(w, r, proj, prefix)
	case strings.HasPrefix(rest, "retros/bug/") && strings.HasSuffix(rest, "/status") && r.Method == http.MethodPost:
		s.handleUpdateBugStatus(w, r, proj, prefix)
	case rest == "decisions" && r.Method == http.MethodGet:
		s.handleDecisions(w, r, proj, prefix)
	case rest == "balance" && r.Method == http.MethodGet:
		s.handleBalance(w, r, proj, prefix)
	case (rest == "systems" || strings.HasPrefix(rest, "systems/")) && r.Method == http.MethodGet:
		s.handleSystems(w, r, proj, prefix, rest)
	case rest == "hash":
		s.handleHash(w, r, proj)
	case rest == "issues.json":
		s.handleIssuesJSON(w, r, proj)
	case rest == "docs":
		s.handleDocs(w, r, proj, prefix)
	case strings.HasPrefix(rest, "docs/"):
		s.handleDocPage(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/comments") && r.Method == http.MethodGet:
		s.handleGetComments(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/comments") && r.Method == http.MethodPost:
		s.handleAddComment(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/comments/toggle") && r.Method == http.MethodPost:
		s.handleToggleComment(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/comments/delete") && r.Method == http.MethodPost:
		s.handleDeleteComment(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/data") && r.Method == http.MethodPost:
		s.handleDataAdd(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.Contains(rest, "/data/") && strings.HasSuffix(rest, "/status") && r.Method == http.MethodPost:
		s.handleDataSetStatus(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.Contains(rest, "/data/") && strings.HasSuffix(rest, "/tier") && r.Method == http.MethodPost:
		s.handleDataSetTier(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.Contains(rest, "/data/") && strings.HasSuffix(rest, "/comment") && r.Method == http.MethodPost:
		s.handleDataSetComment(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.Contains(rest, "/data/") && r.Method == http.MethodDelete:
		s.handleDataRemove(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/dispatch") && r.Method == http.MethodPost:
		s.handleDispatchAgent(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/dispatch/reprompt") && r.Method == http.MethodPost:
		s.handleDispatchReprompt(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/dispatch-prompt") && r.Method == http.MethodGet:
		s.handleDispatchPrompt(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.Contains(rest, "/action/") && r.Method == http.MethodPost:
		s.handleCustomAction(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/edit-in-nvim") && r.Method == http.MethodPost:
		s.handleEditIssueInNvim(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/approvals.json") && r.Method == http.MethodGet:
		s.handleApprovalsJSON(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/transition") && r.Method == http.MethodGet:
		s.handleTransitionPreview(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/type") && r.Method == http.MethodPost:
		s.handleSetIssueType(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/approve") && r.Method == http.MethodPost:
		s.handleApproveIssue(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/delete") && r.Method == http.MethodPost:
		s.handleDeleteIssue(w, r, proj, prefix)
	case strings.HasPrefix(rest, "action/") && r.Method == http.MethodPost:
		s.handleProjectAction(w, r, proj, prefix)
	case rest == "issues/create" && r.Method == http.MethodPost:
		s.handleCreateIssue(w, r, proj, prefix)
	case rest == "upload" && r.Method == http.MethodPost:
		s.handleUpload(w, r, proj, prefix)
	case strings.HasPrefix(rest, "files/") && r.Method == http.MethodGet:
		s.handleProjectFile(w, r, proj, prefix)
	case strings.HasPrefix(rest, "attachments/"):
		attachDir := filepath.Join(proj.IssueDir, "attachments")
		http.StripPrefix(prefix+"/attachments/", http.FileServer(http.Dir(attachDir))).ServeHTTP(w, r)
	case strings.HasPrefix(rest, "issue/") && r.Method == http.MethodGet:
		s.handleDetail(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && r.Method == http.MethodPost:
		s.handleUpdateIssue(w, r, proj, prefix)
	default:
		http.NotFound(w, r)
	}
}

// --- Project List ---

type ProjectListData struct {
	Projects []ProjectSummary
}

type ProjectSummary struct {
	Name       string
	Slug       string
	IssueCount int
	DocCount   int
}

func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// If only one project, redirect directly
	if len(s.projects) == 1 {
		http.Redirect(w, r, "/p/"+s.projects[0].Slug+"/", http.StatusFound)
		return
	}

	summaries := make([]ProjectSummary, len(s.projects))
	var wg sync.WaitGroup
	for i, p := range s.projects {
		wg.Add(1)
		go func(i int, p tracker.Project) {
			defer wg.Done()
			issues, _ := tracker.LoadIssues(p.IssueDir)
			docs, _ := tracker.LoadDocs(p.DocsDir)
			summaries[i] = ProjectSummary{
				Name:       p.Name,
				Slug:       p.Slug,
				IssueCount: len(issues),
				DocCount:   len(docs),
			}
		}(i, p)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "projects.html", ProjectListData{Projects: summaries}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleProjectFor is GET /project-for?cwd=<path>: the slug of the project
// whose work directory holds path (the longest match wins), for a client that
// knows its folder but not its project, such as the issue-approve Claude Code
// mod in an agent session started before dispatch exported ISSUE_VIEWER_PROJECT.
func (s *Server) handleProjectFor(w http.ResponseWriter, r *http.Request) {
	cwd := filepath.Clean(r.URL.Query().Get("cwd"))
	best, bestLen := "", -1
	for i := range s.projects {
		root := filepath.Clean(projectRoot(&s.projects[i]))
		if (cwd == root || strings.HasPrefix(cwd, root+string(filepath.Separator))) && len(root) > bestLen {
			best, bestLen = s.projects[i].Slug, len(root)
		}
	}
	if best == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"project": best})
}
