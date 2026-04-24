package main

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func canonicalStatusKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func orderedStatusesForIssue(wf *tracker.WorkflowConfig, current string) []string {
	statuses := append([]string{}, wf.GetStatusOrder()...)
	current = strings.TrimSpace(current)
	if current == "" {
		return statuses
	}
	for _, status := range statuses {
		if status == current {
			return statuses
		}
	}
	return append([]string{current}, statuses...)
}

var funcMap = template.FuncMap{
	"statusColor": func(s string) string {
		colors := map[string]string{
			"idea":          "#8b5cf6",
			"in design":     "#3b82f6",
			"backlog":       "#64748b",
			"in progress":   "#eab308",
			"testing":       "#f97316",
			"human-testing": "#ec4899",
			"documentation": "#14b8a6",
			"shipping":      "#0ea5e9",
			"done":          "#22c55e",
		}
		if c, ok := colors[canonicalStatusKey(s)]; ok {
			return c
		}
		return "#6b7280"
	},
	"statusTextColor": func(s string) string {
		dark := map[string]bool{"in progress": true, "testing": true, "human-testing": true}
		if dark[canonicalStatusKey(s)] {
			return "#000000"
		}
		return "#ffffff"
	},
	"priorityColor": func(p string) string {
		colors := map[string]string{
			"low":      "#6b7280",
			"medium":   "#3b82f6",
			"high":     "#f97316",
			"critical": "#ef4444",
		}
		if c, ok := colors[p]; ok {
			return c
		}
		return "#6b7280"
	},
	"joinLabels": func(labels []string) string {
		return strings.Join(labels, ", ")
	},
	"addCounts": func(counts map[string]int, keys ...string) int {
		total := 0
		for _, key := range keys {
			total += counts[key]
		}
		return total
	},
	"safeHTML": func(s string) template.HTML {
		return template.HTML(s)
	},
	"urlEncodeColor": func(s string) string {
		return strings.ReplaceAll(s, "#", "%23")
	},
	"assigneeColor": func(name string) string {
		if name == "" {
			return ""
		}
		colors := []string{
			"#f97316", "#3b82f6", "#22c55e", "#a855f7",
			"#ef4444", "#eab308", "#14b8a6", "#ec4899",
			"#6366f1", "#84cc16",
		}
		h := 0
		for _, c := range name {
			h = h*31 + int(c)
		}
		if h < 0 {
			h = -h
		}
		return colors[h%len(colors)]
	},
	"boardCardFields": func(fields []string, issue *IssueView) []BoardCardField {
		var result []BoardCardField
		for _, f := range fields {
			switch f {
			case "system":
				if issue.System != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.System})
				}
			case "labels":
				if len(issue.Labels) > 0 {
					result = append(result, BoardCardField{Name: f, Values: issue.Labels, IsList: true})
				}
			case "priority":
				if issue.Priority != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.Priority})
				}
			case "assignee":
				if issue.Assignee != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.Assignee})
				}
			case "version":
				if issue.Version != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.Version})
				}
			case "number":
				if issue.Number > 0 {
					result = append(result, BoardCardField{Name: f, Value: fmt.Sprintf("#%d", issue.Number)})
				}
			default:
				for _, ef := range issue.ExtraFields {
					if ef.Key != f {
						continue
					}
					if ef.IsList {
						if len(ef.Values) > 0 {
							result = append(result, BoardCardField{Name: f, Values: ef.Values, IsList: true})
						}
					} else if ef.Value != "" {
						result = append(result, BoardCardField{Name: f, Value: ef.Value})
					}
					break
				}
			}
		}
		return result
	},
	"formatScore": func(score *tracker.ScoreBreakdown) string {
		if score == nil {
			return ""
		}
		// Render whole numbers as integers; fractional scores round to one decimal.
		t := score.Total
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%.1f", t)
	},
	"scoreColor": func(score *tracker.ScoreBreakdown) string {
		if score == nil {
			return "#6b7280"
		}
		switch {
		case score.Total >= 60:
			return "#ef4444" // red — urgent
		case score.Total >= 30:
			return "#f97316" // orange — warm
		case score.Total >= 10:
			return "#eab308" // yellow — mild
		default:
			return "#22c55e" // green — cool
		}
	},
	"linkIssueRefs": func(html string, prefix string, slugMap map[string]string) template.HTML {
		// Match #123, #my-slug, #system/my-slug
		re := regexp.MustCompile(`#([a-zA-Z0-9][\w/.-]*)`)
		result := re.ReplaceAllStringFunc(html, func(match string) string {
			ref := match[1:]
			// Direct slug match (e.g. #combat/fix-heat-bug)
			if slug, ok := slugMap[ref]; ok {
				return fmt.Sprintf(`<a href="%s/issue/%s" class="issue-ref">%s</a>`, prefix, slug, match)
			}
			// Try lowercase
			if slug, ok := slugMap[strings.ToLower(ref)]; ok {
				return fmt.Sprintf(`<a href="%s/issue/%s" class="issue-ref">%s</a>`, prefix, slug, match)
			}
			return match
		})
		return template.HTML(result)
	},
}

type Server struct {
	projects []tracker.Project
	tmpl     *template.Template
}

type AgentSession struct {
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
}

type approvalResponse struct {
	Status              string `json:"status"`
	HumanApproval       string `json:"human_approval"`
	NotifiedSession     string `json:"notified_session,omitempty"`
	NotificationMessage string `json:"notification_message,omitempty"`
	NotificationError   string `json:"notification_error,omitempty"`
	Label               string `json:"label,omitempty"`
}

type IssueView struct {
	*tracker.Issue
	ActiveSessions []AgentSession
	Score          *tracker.ScoreBreakdown
}

// HasScore reports whether this view carries a computed score.
func (i *IssueView) HasScore() bool {
	return i != nil && i.Score != nil
}

func (i *IssueView) HasActiveAgent() bool {
	return i != nil && len(i.ActiveSessions) > 0
}

var listTmuxSessions = defaultListTmuxSessions
var tmuxSendKeys = defaultTmuxSendKeys

func defaultTmuxSendKeys(target string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	content := strings.Join(lines, "\n")
	tmp, err := os.CreateTemp("", "issue-approval-*.txt")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := exec.Command("tmux", "load-buffer", tmpPath).Run(); err != nil {
		return err
	}
	if err := exec.Command("tmux", "paste-buffer", "-t", target).Run(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
}

func NewServer(projects []tracker.Project) (*Server, error) {
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{
		projects: projects,
		tmpl:     tmpl,
	}, nil
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
	case rest == "github" && r.Method == http.MethodGet:
		s.handleGitHub(w, r, proj, prefix)
	case rest == "github/fetch" && r.Method == http.MethodPost:
		s.handleGitHubFetch(w, r, proj)
	case rest == "github/import" && r.Method == http.MethodPost:
		s.handleGitHubImport(w, r, proj)
	case rest == "retros":
		s.handleRetros(w, r, proj, prefix)
	case rest == "retros/review" && r.Method == http.MethodPost:
		s.handleRetrosReviewDispatch(w, r, proj)
	case strings.HasPrefix(rest, "retros/retro/") && strings.HasSuffix(rest, "/status") && r.Method == http.MethodPost:
		s.handleUpdateRetroStatus(w, r, proj, prefix)
	case strings.HasPrefix(rest, "retros/bug/") && strings.HasSuffix(rest, "/status") && r.Method == http.MethodPost:
		s.handleUpdateBugStatus(w, r, proj, prefix)
	case rest == "workflow-flow":
		s.handleWorkflowFlow(w, r, proj, prefix)
	case rest == "workflow-designer" && r.Method == http.MethodGet:
		s.handleWorkflowDesigner(w, r, proj, prefix)
	case rest == "workflow-designer/data" && r.Method == http.MethodGet:
		s.handleWorkflowDesignerData(w, r, proj)
	case rest == "workflow-designer/preview" && r.Method == http.MethodPost:
		s.handleWorkflowDesignerPreview(w, r, proj)
	case rest == "workflow-designer" && r.Method == http.MethodPost:
		s.handleSaveWorkflowDesigner(w, r, proj)
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
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/dispatch") && r.Method == http.MethodPost:
		s.handleDispatchAgent(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/edit-in-nvim") && r.Method == http.MethodPost:
		s.handleEditIssueInNvim(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/transition") && r.Method == http.MethodGet:
		s.handleTransitionPreview(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/approve") && r.Method == http.MethodPost:
		s.handleApproveIssue(w, r, proj, prefix)
	case strings.HasPrefix(rest, "issue/") && strings.HasSuffix(rest, "/delete") && r.Method == http.MethodPost:
		s.handleDeleteIssue(w, r, proj, prefix)
	case rest == "issues/create" && r.Method == http.MethodPost:
		s.handleCreateIssue(w, r, proj, prefix)
	case rest == "upload" && r.Method == http.MethodPost:
		s.handleUpload(w, r, proj, prefix)
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

// --- Issue List ---

type ListData struct {
	Issues         []*IssueView
	Statuses       []string
	Systems        []string
	Priorities     []string
	Labels         []string
	Assignees      []string
	Filter         FilterParams
	Total          int
	Filtered       int
	Prefix         string
	ProjectName    string
	ActiveBots     int
	SupportsGitHub bool
	ScoringEnabled bool
	Sort           string // "score" or "" (default)
}

type FilterParams struct {
	Status   string
	System   string
	Priority string
	Label    string
	Assignee string
	Search   string
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	total := len(issues)
	statuses, systems, priorities, labels, assignees := tracker.CollectFilterValues(issues)
	systems = mergeSubdirSystems(systems, proj.IssueDir)

	filter := FilterParams{
		Status:   r.URL.Query().Get("status"),
		System:   r.URL.Query().Get("system"),
		Priority: r.URL.Query().Get("priority"),
		Label:    r.URL.Query().Get("label"),
		Assignee: r.URL.Query().Get("assignee"),
		Search:   r.URL.Query().Get("search"),
	}

	filtered := filterIssues(issues, filter)
	sessionMap, activeBots := sessionsByIssueSlug(issues)

	wf := proj.LoadWorkflow()
	views := issueViews(filtered, sessionMap)
	scoring := &wf.Scoring
	attachScores(views, scoring)

	sortKey := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortKey == "" && scoring.Enabled && scoring.DefaultSort == "score_desc" {
		sortKey = "score"
	}
	if sortKey == "score" && scoring.Enabled {
		sortViewsByScore(views)
	}

	data := ListData{
		Issues:         views,
		Statuses:       statuses,
		Systems:        systems,
		Priorities:     priorities,
		Labels:         labels,
		Assignees:      assignees,
		Filter:         filter,
		Total:          total,
		Filtered:       len(filtered),
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		SupportsGitHub: proj.SupportsGitHub,
		ScoringEnabled: scoring.Enabled,
		Sort:           sortKey,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- Issue Detail ---

type DetailData struct {
	Issue             *IssueView
	BackURL           string
	Prefix            string
	ProjectName       string
	Statuses          []string
	SlugMap           map[string]string
	NeedsApproval     string             // required-path next status that requires human approval, else empty
	OptionalApprovals []OptionalApproval // optional-path transitions that require human approval, hidden behind CTAs
	ActiveBots        int
	Timeline          []TimelineEvent // agent↔issue-cli interactions parsed from .agent-logs/<assignee>/<assignee>.clilog
}

// OptionalApproval describes a transition to an Optional status that requires
// human approval. The detail view renders these behind a CTA button so they
// don't compete with the required-path approval as the default next step.
type OptionalApproval struct {
	Status      string
	Description string
	CTALabel    string // workflow-configured button label; falls back to a default in the template
}

type BodyEditResponse struct {
	Status  string `json:"status"`
	Session string `json:"session,omitempty"`
	Message string `json:"message,omitempty"`
}

var launchIssueBodyEditor = startIssueBodyEditor

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	path := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug := path
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var found *tracker.Issue
	for _, issue := range issues {
		if issue.Slug == slug {
			found = issue
			break
		}
	}

	if found == nil {
		http.NotFound(w, r)
		return
	}

	backURL := prefix + "/"
	if from := r.URL.Query().Get("from"); from != "" {
		// Only allow known safe relative paths
		if strings.HasPrefix(from, "board") || strings.HasPrefix(from, "docs") {
			backURL = prefix + "/" + from
		}
	}

	// Build ref→slug map for #ref links
	slugMap := map[string]string{}
	for _, issue := range issues {
		// By filename base (e.g. "123" from "123.md") for numeric refs
		fname := strings.TrimSuffix(filepath.Base(issue.FilePath), ".md")
		slugMap[fname] = issue.Slug
		// By slug itself (e.g. "combat/fix-heat-bug") for slug refs
		slugMap[issue.Slug] = issue.Slug
		// By slug without system prefix (e.g. "fix-heat-bug")
		slugMap[filepath.Base(issue.Slug)] = issue.Slug
	}

	sessionMap, activeBots := sessionsByIssueSlug(issues)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	wf := proj.LoadWorkflowForIssue(found)
	statuses := orderedStatusesForIssue(wf, found.Status)
	detailView := issueView(found, sessionMap)
	if wf.Scoring.Enabled && detailView != nil {
		detailView.Score = tracker.ComputeScore(found, &wf.Scoring, time.Now())
	}

	// Classify outgoing transitions that require human approval. The required-path
	// approval (target is non-optional) is shown inline; optional-path approvals are
	// surfaced separately so the template can hide them behind a CTA.
	var needsApproval string
	var optionalApprovals []OptionalApproval
	for _, t := range wf.Transitions {
		if t.From != found.Status {
			continue
		}
		requiresApproval := false
		for _, action := range t.Actions {
			if action.Type == "require_human_approval" {
				requiresApproval = true
				break
			}
		}
		if !requiresApproval {
			continue
		}
		target := wf.GetStatus(t.To)
		if target != nil && target.Optional {
			optionalApprovals = append(optionalApprovals, OptionalApproval{
				Status:      t.To,
				Description: target.Description,
				CTALabel:    t.CTALabel,
			})
			continue
		}
		if needsApproval == "" {
			needsApproval = t.To
		}
	}

	timeline := LoadAgentTimeline(proj.WorkDir, found.Assignee)
	timeline = EnrichTimelineWithWorkflow(timeline, wf, "")
	if len(timeline) > 0 {
		// Prefer the exact prompt persisted at dispatch time; fall back to a
		// reconstruction so older logs (pre-persistence) still render.
		basePrompt := LoadDispatchPrompt(proj.WorkDir, found.Assignee)
		summary := "dispatch — base prompt"
		if basePrompt == "" {
			briefedStatus := FirstTransitionFromStatus(timeline)
			issueCopy := *found
			if briefedStatus != "" {
				issueCopy.Status = briefedStatus
			}
			basePrompt = buildAgentPrompt(&issueCopy, wf)
			summary = "dispatch — base prompt (reconstructed)"
		}
		dispatchEv := DispatchEvent(basePrompt, timeline[0].Timestamp)
		dispatchEv.Summary = summary
		timeline = append([]TimelineEvent{dispatchEv}, timeline...)
	}

	if err := s.tmpl.ExecuteTemplate(w, "detail.html", DetailData{
		Issue:             detailView,
		BackURL:           backURL,
		Prefix:            prefix,
		ProjectName:       proj.Name,
		Statuses:          statuses,
		SlugMap:           slugMap,
		NeedsApproval:     needsApproval,
		OptionalApprovals: optionalApprovals,
		ActiveBots:        activeBots,
		Timeline:          timeline,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- Board ---

type BoardColumn struct {
	Status      string
	Description string
	Optional    bool
	Global      bool
	Issues      []*IssueView
}

type BoardCardField struct {
	Name   string
	Value  string
	Values []string
	IsList bool
}

type BoardData struct {
	Columns        []*BoardColumn
	Total          int
	Versions       []string
	Version        string
	Systems        []string
	System         string
	Assignees      []string
	Assignee       string
	Priorities     []string
	Labels         []string
	Prefix         string
	ProjectName    string
	ActiveBots     int
	CardFields     []string
	SupportsGitHub bool
	ScoringEnabled bool
	Sort           string
}

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

// --- Graph ---

type GraphStatusNode struct {
	Name            string
	Description     string
	RequireApproval bool
	Optional        bool
	Issues          []*GraphIssueNode
}

type GraphIssueNode struct {
	Slug         string
	Title        string
	System       string
	Priority     string
	Assignee     string
	DaysInStatus int
	IsStale      bool
	IsVeryStale  bool
}

type GraphData struct {
	Prefix         string
	ProjectName    string
	ActiveBots     int
	StatusNodes    []*GraphStatusNode
	Systems        []string
	System         string
	ShowDone       bool
	TotalIssues    int
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

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	versionSet := map[string]bool{}
	systemSet := map[string]bool{}
	assigneeSet := map[string]bool{}
	prioritySet := map[string]bool{}
	labelSet := map[string]bool{}
	for _, issue := range issues {
		if issue.Version != "" {
			versionSet[issue.Version] = true
		}
		if issue.System != "" {
			systemSet[issue.System] = true
		}
		if issue.Assignee != "" {
			assigneeSet[issue.Assignee] = true
		}
		if issue.Priority != "" {
			prioritySet[issue.Priority] = true
		}
		for _, l := range issue.Labels {
			labelSet[l] = true
		}
	}
	var versions []string
	for v := range versionSet {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	var systems []string
	for s := range systemSet {
		systems = append(systems, s)
	}
	systems = mergeSubdirSystems(systems, proj.IssueDir)
	var assignees []string
	for a := range assigneeSet {
		assignees = append(assignees, a)
	}
	sort.Strings(assignees)
	var priorities []string
	for p := range prioritySet {
		priorities = append(priorities, p)
	}
	sort.Strings(priorities)
	var labels []string
	for l := range labelSet {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	versionFilter := r.URL.Query().Get("version")
	systemFilter := r.URL.Query().Get("system")
	assigneeFilter := r.URL.Query().Get("assignee")

	var filtered []*tracker.Issue
	for _, issue := range issues {
		if versionFilter != "" && issue.Version != versionFilter {
			continue
		}
		if systemFilter != "" && !strings.EqualFold(issue.System, systemFilter) {
			continue
		}
		if assigneeFilter == "_claimed" && issue.Assignee == "" {
			continue
		}
		if assigneeFilter == "_unclaimed" && issue.Assignee != "" {
			continue
		}
		if assigneeFilter != "" && assigneeFilter != "_claimed" && assigneeFilter != "_unclaimed" && issue.Assignee != assigneeFilter {
			continue
		}
		filtered = append(filtered, issue)
	}
	issues = filtered

	wf := proj.LoadWorkflow()
	statusOrder := wf.GetBoardColumns()
	statusDescs := wf.GetStatusDescriptions()
	sessionMap, activeBots := sessionsByIssueSlug(issues)
	scoring := &wf.Scoring

	sortKey := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortKey == "" && scoring.Enabled && scoring.DefaultSort == "score_desc" {
		sortKey = "score"
	}

	byStatus := map[string][]*IssueView{}
	seen := map[string]bool{}
	for _, issue := range issues {
		st := issue.Status
		if st == "" {
			st = "none"
		}
		view := issueView(issue, sessionMap)
		if scoring.Enabled {
			view.Score = tracker.ComputeScore(issue, scoring, time.Now())
		}
		byStatus[st] = append(byStatus[st], view)
		seen[st] = true
	}

	if sortKey == "score" && scoring.Enabled {
		for _, list := range byStatus {
			sortViewsByScore(list)
		}
	}

	var columns []*BoardColumn
	added := map[string]bool{}
	for _, st := range statusOrder {
		desc := statusDescs[st]
		optional := false
		global := false
		if ws := wf.GetStatus(st); ws != nil {
			optional = ws.Optional
			global = ws.Global
		}
		columns = append(columns, &BoardColumn{Status: st, Description: desc, Optional: optional, Global: global, Issues: byStatus[st]})
		added[st] = true
	}
	for st := range seen {
		if !added[st] {
			columns = append(columns, &BoardColumn{Status: st, Issues: byStatus[st]})
		}
	}

	data := BoardData{
		Columns:        columns,
		Total:          len(issues),
		Versions:       versions,
		Version:        versionFilter,
		Systems:        systems,
		System:         systemFilter,
		Assignees:      assignees,
		Assignee:       assigneeFilter,
		Priorities:     priorities,
		Labels:         labels,
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		CardFields:     wf.GetBoardCardFields(),
		SupportsGitHub: proj.SupportsGitHub,
		ScoringEnabled: scoring.Enabled,
		Sort:           sortKey,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "board.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wf := proj.LoadWorkflow()

	// Which statuses require human approval to enter
	approvalRequired := map[string]bool{}
	for _, t := range wf.Transitions {
		for _, a := range t.Actions {
			if a.Type == "require_human_approval" {
				target := strings.TrimSpace(a.Status)
				if target == "" {
					target = t.To
				}
				approvalRequired[target] = true
			}
		}
	}

	systemFilter := r.URL.Query().Get("system")
	showDone := r.URL.Query().Get("done") == "1"

	systemSet := map[string]bool{}
	for _, issue := range issues {
		if issue.System != "" {
			systemSet[issue.System] = true
		}
	}
	var systems []string
	for sys := range systemSet {
		systems = append(systems, sys)
	}
	systems = mergeSubdirSystems(systems, proj.IssueDir)

	now := time.Now()
	statusOrder := wf.GetStatusOrder()
	statusDescs := wf.GetStatusDescriptions()

	nodeMap := map[string]*GraphStatusNode{}
	for _, name := range statusOrder {
		optional := false
		if s := wf.GetStatus(name); s != nil {
			optional = s.Optional
		}
		nodeMap[name] = &GraphStatusNode{
			Name:            name,
			Description:     statusDescs[name],
			RequireApproval: approvalRequired[name],
			Optional:        optional,
		}
	}

	totalIssues := 0
	for _, issue := range issues {
		if !showDone && issue.Status == "done" {
			continue
		}
		if systemFilter != "" && !strings.EqualFold(issue.System, systemFilter) {
			continue
		}
		totalIssues++

		days := int(now.Sub(issue.ModTime).Hours() / 24)
		node := &GraphIssueNode{
			Slug:         issue.Slug,
			Title:        issue.Title,
			System:       issue.System,
			Priority:     issue.Priority,
			Assignee:     issue.Assignee,
			DaysInStatus: days,
			IsStale:      days >= 7,
			IsVeryStale:  days >= 14,
		}

		status := issue.Status
		if status == "" {
			status = "none"
		}
		if sn, ok := nodeMap[status]; ok {
			sn.Issues = append(sn.Issues, node)
		} else {
			nodeMap[status] = &GraphStatusNode{Name: status, Issues: []*GraphIssueNode{node}}
		}
	}

	var nodes []*GraphStatusNode
	for _, name := range statusOrder {
		if !showDone && name == "done" {
			continue
		}
		if sn, ok := nodeMap[name]; ok {
			nodes = append(nodes, sn)
		}
	}

	_, activeBots := sessionsByIssueSlug(issues)

	data := GraphData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		StatusNodes:    nodes,
		Systems:        systems,
		System:         systemFilter,
		ShowDone:       showDone,
		TotalIssues:    totalIssues,
		SupportsGitHub: proj.SupportsGitHub,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "graph.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

func projectRoot(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	if proj != nil && proj.IssueDir != "" {
		if abs, err := filepath.Abs(proj.IssueDir); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(proj.IssueDir)
	}
	cwd, _ := os.Getwd()
	return cwd
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

func workflowFileTarget(proj *tracker.Project) string {
	if proj.WorkflowFile != "" {
		return proj.WorkflowFile
	}
	return "workflow.yaml"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
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

// --- Update Issue ---

// updateIssuePayload combines a plain IssueUpdate with the answers collected
// from a workflow transition's declarative fields[]. When Status is set to a
// value different from the issue's current status, the server routes through
// the workflow engine so the web path matches `issue-cli transition`.
type updateIssuePayload struct {
	tracker.IssueUpdate
	Fields map[string]string `json:"fields,omitempty"`
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var found *tracker.Issue
	for _, issue := range issues {
		if issue.Slug == slug {
			found = issue
			break
		}
	}

	if found == nil {
		http.NotFound(w, r)
		return
	}

	var payload updateIssuePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	update := payload.IssueUpdate

	// Status changes route through the workflow engine so the board path matches
	// `issue-cli transition` — validation, actions, declarative fields.
	if update.Status != nil && *update.Status != found.Status {
		wf := proj.LoadWorkflowForIssue(found)
		_, _, terr := wf.ApplyTransitionToFileWithFields(found.FilePath, *update.Status, payload.Fields)
		if terr != nil {
			writeTransitionError(w, terr)
			return
		}
		// The engine wrote status/body/fields atomically. Strip Status/Body/
		// ExtraFields from the remaining update so we don't double-write — any
		// remaining fields (title, priority, labels, etc.) still go through the
		// plain writer.
		update.Status = nil
		update.Body = nil
		update.ExtraFields = nil
	}

	if hasNonStatusUpdates(update) {
		if err := tracker.UpdateIssueFrontmatter(found.FilePath, update); err != nil {
			http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if payload.Status != nil && *payload.Status == "done" && proj.SupportsGitHub && found.Repo != "" && found.Number > 0 {
		go closeGithubIssue(found)
	}

	newSlug := found.Slug
	if update.Title != nil {
		newSlug = tracker.Slugify(*update.Title)
		if newSlug != found.Slug {
			dir := filepath.Dir(found.FilePath)
			newPath := filepath.Join(dir, newSlug+".md")
			if err := os.Rename(found.FilePath, newPath); err != nil {
				http.Error(w, "rename failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "slug": newSlug})
}

func hasNonStatusUpdates(u tracker.IssueUpdate) bool {
	return u.Title != nil || u.Priority != nil || u.Version != nil || u.Assignee != nil ||
		u.HumanApproval != nil || u.StartedAt != nil || u.DoneAt != nil ||
		u.Labels != nil || u.Body != nil || len(u.ExtraFields) > 0
}

// writeTransitionError emits an HTTP 409 with a JSON body the board UI can
// render as a toast. 409 is used instead of 500 because these errors are
// user-correctable (approve in viewer, check boxes, fill required fields).
func writeTransitionError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "error",
		"error":  err.Error(),
	})
}

func closeGithubIssue(issue *tracker.Issue) {
	comment := fmt.Sprintf("Closed via issue-viewer.\n\nImplementation tracked in local issue: %s", issue.Title)
	if issue.BodyRaw != "" {
		comment += "\n\n---\n\n" + issue.BodyRaw
	}
	token, err := exec.Command(os.ExpandEnv("$HOME/Work/michal-franc-agent/gh-app-token")).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "closeGithubIssue: get token: %v\n", err)
		return
	}
	args := []string{"issue", "close", fmt.Sprintf("%d", issue.Number),
		"--repo", issue.Repo,
		"--comment", comment,
	}
	cmd := exec.Command("gh", args...)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+strings.TrimSpace(string(token)))
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "closeGithubIssue: gh issue close: %v\n%s\n", err, out)
	}
}

// --- Comments ---

// handleTransitionPreview answers GET /p/<proj>/issue/<slug>/transition?to=<status>
// with the TransitionPreview (steps, allowed/validation_error, declarative
// fields[]). The board uses this to decide whether to open a prompt modal,
// and what to render in it, before POSTing the actual status change.
func (s *Server) handleTransitionPreview(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/issue/"), "/transition")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if to == "" {
		http.Error(w, "missing ?to=<status>", http.StatusBadRequest)
		return
	}

	comments, _ := tracker.LoadComments(issue.FilePath)
	wf := proj.LoadWorkflowForIssue(issue)
	preview := wf.PreviewTransition(issue, issue.Status, to, issue.System, comments)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
}

func (s *Server) findIssueBySlug(proj *tracker.Project, slug string) *tracker.Issue {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil
	}
	for _, issue := range issues {
		if issue.Slug == slug {
			return issue
		}
	}
	return nil
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

type AddCommentRequest struct {
	Block int    `json:"block"`
	Text  string `json:"text"`
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

type CommentIDRequest struct {
	ID int `json:"id"`
}

func (s *Server) extractCommentSlug(path, prefix, suffix string) string {
	slug := strings.TrimPrefix(path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, suffix)
	return slug
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

// --- Content Hash (for auto-refresh) ---

func (s *Server) handleHash(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	sessionMap, activeBots := sessionsByIssueSlug(issues)
	h := sha256.New()
	for _, issue := range issues {
		fmt.Fprintf(h, "%s:%s:%s:%d\n", issue.Slug, issue.Status, issue.Assignee, issue.ModTime.UnixNano())
		for _, session := range sessionMap[issue.Slug] {
			fmt.Fprintf(h, "session:%s:%s:%s\n", issue.Slug, session.Name, session.StartTime)
		}
	}
	fmt.Fprintf(h, "active-bots:%d\n", activeBots)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"hash": fmt.Sprintf("%x", h.Sum(nil))})
}

type issueJSON struct {
	Slug           string         `json:"slug"`
	Title          string         `json:"title"`
	Status         string         `json:"status"`
	System         string         `json:"system"`
	Priority       string         `json:"priority"`
	Assignee       string         `json:"assignee"`
	Version        string         `json:"version"`
	Labels         []string       `json:"labels"`
	ActiveSessions []AgentSession `json:"active_sessions"`
	HasActiveAgent bool           `json:"has_active_agent"`
}

func (s *Server) handleIssuesJSON(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	sessionMap, _ := sessionsByIssueSlug(issues)
	result := make([]issueJSON, len(issues))
	for i, issue := range issues {
		activeSessions := append([]AgentSession(nil), sessionMap[issue.Slug]...)
		result[i] = issueJSON{
			Slug:           issue.Slug,
			Title:          issue.Title,
			Status:         issue.Status,
			System:         issue.System,
			Priority:       issue.Priority,
			Assignee:       issue.Assignee,
			Version:        issue.Version,
			Labels:         issue.Labels,
			ActiveSessions: activeSessions,
			HasActiveAgent: len(activeSessions) > 0,
		}
		if result[i].Labels == nil {
			result[i].Labels = []string{}
		}
		if result[i].ActiveSessions == nil {
			result[i].ActiveSessions = []AgentSession{}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// --- Delete Issue ---

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/delete")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	if err := tracker.DeleteIssue(issue.FilePath); err != nil {
		http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleApproveIssue(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/approve")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		http.Error(w, `{"error":"status required"}`, http.StatusBadRequest)
		return
	}

	// Toggle: if already human-approved for this status, clear it
	var humanApproval string
	if !strings.EqualFold(issue.HumanApproval, body.Status) {
		humanApproval = body.Status
	}

	update := tracker.IssueUpdate{HumanApproval: &humanApproval}
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, update); err != nil {
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := approvalResponse{
		Status:        "ok",
		HumanApproval: humanApproval,
		Label:         approvalLabel(body.Status),
	}
	if humanApproval != "" {
		sessionMap, _ := sessionsByIssueSlug([]*tracker.Issue{issue})
		activeSessions := sessionMap[issue.Slug]
		if len(activeSessions) == 0 {
			resp.NotificationError = "no active agent session matched this issue"
		} else {
			target := activeSessions[0].Name
			messageLines := []string{humanApprovalMessage(body.Status)}
			if err := tmuxSendKeys(target, messageLines); err != nil {
				resp.NotificationError = err.Error()
			} else {
				resp.NotifiedSession = target
				resp.NotificationMessage = "approval notification sent to active agent session"
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

var humanApprovalMessages = []string{
	"Hey, looks good — you're approved to move to %s, go ahead.",
	"Alright, I've reviewed it. You can move to %s now.",
	"Approved for %s. Go for it.",
	"Yeah, this is ready for %s. Carry on.",
	"Checked it out — %s is approved, move it along.",
	"Good to go for %s. No blockers from my end.",
	"I've had a look and I'm happy with it. %s is approved.",
	"All good on my side, you can proceed to %s.",
	"Gave this a read — approved for %s, off you go.",
	"This looks solid. Moving to %s is fine by me.",
}

func humanApprovalMessage(status string) string {
	tmpl := humanApprovalMessages[rand.Intn(len(humanApprovalMessages))]
	return fmt.Sprintf(tmpl, status)
}

func approvalLabel(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	return fmt.Sprintf("Human-approved for %s", status)
}

func resolveProjectWorkDir(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	workDir, _ := os.Getwd()
	return workDir
}

func startIssueBodyEditor(proj *tracker.Project, issue *tracker.Issue) (BodyEditResponse, error) {
	if issue == nil {
		return BodyEditResponse{}, fmt.Errorf("issue is required")
	}

	workDir := resolveProjectWorkDir(proj)
	session := tmuxSessionName(issue.Slug) + "-edit"
	waitSignal := session + "-done"
	baseContent, err := os.ReadFile(issue.FilePath)
	if err != nil {
		return BodyEditResponse{}, fmt.Errorf("read issue file: %w", err)
	}
	baseHash := sha256.Sum256(baseContent)

	bodyFile, err := os.CreateTemp("", "issue-body-*.md")
	if err != nil {
		return BodyEditResponse{}, fmt.Errorf("create temp body file: %w", err)
	}
	bodyPath := bodyFile.Name()
	bodyContent := issue.BodyRaw
	if bodyContent != "" && !strings.HasSuffix(bodyContent, "\n") {
		bodyContent += "\n"
	}
	if _, err := bodyFile.WriteString(bodyContent); err != nil {
		bodyFile.Close()
		os.Remove(bodyPath)
		return BodyEditResponse{}, fmt.Errorf("write temp body file: %w", err)
	}
	if err := bodyFile.Close(); err != nil {
		os.Remove(bodyPath)
		return BodyEditResponse{}, fmt.Errorf("close temp body file: %w", err)
	}

	statusFile, err := os.CreateTemp("", "issue-body-edit-status-*.txt")
	if err != nil {
		os.Remove(bodyPath)
		return BodyEditResponse{}, fmt.Errorf("create temp status file: %w", err)
	}
	statusPath := statusFile.Name()
	statusFile.Close()

	cleanup := func() {
		os.Remove(bodyPath)
		os.Remove(statusPath)
	}

	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-c", workDir).Run(); err != nil {
		cleanup()
		return BodyEditResponse{}, fmt.Errorf("create tmux session: %w", err)
	}

	sessionReady := false
	defer func() {
		if !sessionReady {
			exec.Command("tmux", "kill-session", "-t", session).Run()
			cleanup()
		}
	}()

	exec.Command("tmux", "rename-window", "-t", session, issue.Slug).Run()

	terminal := ""
	if proj != nil {
		terminal = proj.Terminal
	}
	if terminal == "none" {
		return BodyEditResponse{}, fmt.Errorf("terminal is 'none': attach manually with: tmux attach -t %s", session)
	} else if terminal != "" {
		termCmd := strings.ReplaceAll(terminal, "{{session}}", session)
		if err := exec.Command("sh", "-c", termCmd).Run(); err != nil {
			return BodyEditResponse{}, fmt.Errorf("open terminal: %w", err)
		}
	} else {
		// Backwards compat: i3 + alacritty
		if proj != nil && proj.I3Workspace != "" {
			if err := exec.Command("i3-msg", "workspace", proj.I3Workspace).Run(); err != nil {
				return BodyEditResponse{}, fmt.Errorf("switch i3 workspace: %w", err)
			}
		}
		if err := exec.Command("i3-msg", "exec", fmt.Sprintf("alacritty -e tmux attach -t %s", session)).Run(); err != nil {
			return BodyEditResponse{}, fmt.Errorf("open alacritty: %w", err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	editCmd := fmt.Sprintf("nvim %q; code=$?; printf '%%s' \"$code\" > %q; tmux wait-for -S %q; exit $code", bodyPath, statusPath, waitSignal)
	if err := exec.Command("tmux", "send-keys", "-t", session, editCmd, "Enter").Run(); err != nil {
		return BodyEditResponse{}, fmt.Errorf("start nvim: %w", err)
	}

	issueFilePath := issue.FilePath
	go func() {
		defer cleanup()
		if err := exec.Command("tmux", "wait-for", waitSignal).Run(); err != nil {
			return
		}
		defer exec.Command("tmux", "kill-session", "-t", session).Run()

		statusBytes, err := os.ReadFile(statusPath)
		if err != nil {
			return
		}
		if strings.TrimSpace(string(statusBytes)) != "0" {
			return
		}

		updatedBody, err := os.ReadFile(bodyPath)
		if err != nil {
			return
		}
		updated := strings.TrimRight(string(updatedBody), "\n")
		currentContent, err := os.ReadFile(issueFilePath)
		if err != nil {
			return
		}
		if sha256.Sum256(currentContent) != baseHash {
			return
		}
		_ = tracker.UpdateIssueFrontmatter(issueFilePath, tracker.IssueUpdate{Body: &updated})
	}()

	sessionReady = true
	return BodyEditResponse{
		Status:  "launched",
		Session: session,
		Message: "Opened in nvim. The issue body will sync back after the editor exits.",
	}, nil
}

func (s *Server) handleEditIssueInNvim(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/edit-in-nvim")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	resp, err := launchIssueBodyEditor(proj, issue)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- Create Issue ---

type CreateIssueRequest struct {
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	System   string   `json:"system"`
	Version  string   `json:"version"`
	Priority string   `json:"priority"`
	Labels   []string `json:"labels"`
	Body     string   `json:"body"`
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	var req CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	_, slug, err := tracker.CreateIssueFileOpts(proj.IssueDir, tracker.CreateIssueOpts{
		Title:    req.Title,
		Status:   req.Status,
		System:   req.System,
		Version:  req.Version,
		Priority: req.Priority,
		Labels:   req.Labels,
		Body:     req.Body,
	})
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "slug": slug})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "file too large (max 10MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}

	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "only image files allowed", http.StatusBadRequest)
		return
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		switch contentType {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			ext = ".png"
		}
	}

	hash := sha256.Sum256(data)
	filename := fmt.Sprintf("%x%s", hash[:16], ext)

	attachDir := filepath.Join(proj.IssueDir, "attachments")
	os.MkdirAll(attachDir, 0755)

	dest := filepath.Join(attachDir, filename)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if err := os.WriteFile(dest, data, 0644); err != nil {
			http.Error(w, "write error", http.StatusInternalServerError)
			return
		}
	}

	urlPath := prefix + "/attachments/" + filename
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": urlPath})
}

// --- Dispatch to Agent ---

func buildAgentPrompt(issue *tracker.Issue, wf *tracker.WorkflowConfig) string {
	currentPrompt := "Use issue-cli to inspect the current workflow requirements for this status before making changes."
	if wf != nil {
		if prompt := wf.StatusPrompt(issue.Status); strings.TrimSpace(prompt) != "" {
			currentPrompt = prompt
		}
	}

	statusReminder := ""
	switch issue.Status {
	case "in design":
		statusReminder = "When the design is complete, stop and ask the human to approve backlog in the issue viewer before attempting that transition."
	case "backlog":
		statusReminder = "Do not run `issue-cli start` until the issue is human-approved for `in progress` in the issue viewer."
	}

	return fmt.Sprintf(`You have been assigned this issue: %s

## Before you start

Learn the workflow process first:
  issue-cli process workflow      # understand the status lifecycle
  issue-cli process transitions   # understand what each transition requires

## Your goal

Move this issue forward correctly using the configured workflow.
Use issue-cli to inspect the current status requirements, complete the required work, and only transition when the workflow says the issue is ready.
Stop and ask the user whenever clarification, approval, or manual verification is required.

## Current status guidance

%s

%s

## How to work on this issue

1. Run: issue-cli start %s
   If the issue is already approved for the next status, this claims the issue and shows your checklist and next steps.
   If approval is missing, stop and ask the human to approve it in the issue viewer.

2. Run: issue-cli show %s
   Read the full context — body, comments, checklist status.

3. Work through each checkbox in the issue one at a time. After completing each one, mark it:
   issue-cli check %s "<checkbox text>"

4. If you are unsure about something or need clarification, ask the user before proceeding.

5. When the current status checkboxes are done, transition to the next status:
   issue-cli transition %s --to "<next-status>"
   The CLI will tell you what the valid next status is and what it requires.

6. Repeat steps 3-5 for each status. Each transition may add new checkboxes — work through them all.

## issue-cli commands you can use freely

These are safe to run without asking the user:
  issue-cli process workflow          # learn status lifecycle
  issue-cli process transitions       # learn transition requirements
  issue-cli show %s                   # full context dump
  issue-cli checklist %s              # checkbox status
  issue-cli next                      # see available work
  issue-cli start %s                  # claim and begin work
  issue-cli check %s "<text>"         # mark a checkbox done
  issue-cli transition %s --to "<next-status>"  # move forward
  issue-cli append %s --body "content"          # append section to issue body
  issue-cli retrospective %s --body "content"   # save workflow feedback under retros/ in the project

## CRITICAL: NEVER modify issue .md files manually. Always use issue-cli commands.

## If you encounter a bug in issue-cli itself, report it:
  issue-cli report-bug "description of what went wrong"
This saves a bug report under bugs/ in the server root.
The current issue slug is attached automatically for dispatched sessions.

## Workflow review

If you stop because human approval, manual verification, clarification, or tooling/workflow blockage is required, run:
  issue-cli retrospective %s --body "Base workflow: ...\nSubsystem workflow for %s: ...\nMissing system-specific instructions: ...\nTooling/workflow friction: ..."
This saves a retrospective under retros/ in the project.

Then briefly tell the user how the workflow could be improved.
Cover both:
  - the base workflow
  - the relevant subsystem workflow guidance for this issue
  - any missing system-specific instructions that should be added for %s

If you hit friction, ambiguity, or missing guardrails while using issue-cli or the workflow, call that out explicitly.

## Commands that require user approval — DO NOT run without asking

  issue-cli transition <slug> --to "done"       # only humans close issues
  Any transition backwards                       # ask first
  Creating or deleting issues                    # ask first

## Issue metadata
  Title: %s
  Status: %s
  Priority: %s

%s`,
		issue.Slug,
		currentPrompt,
		statusReminder,
		issue.Slug,
		issue.Slug,
		issue.Slug,
		issue.Slug,
		issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.Slug, issue.System, issue.System, issue.Slug,
		issue.Title, issue.Status, issue.Priority,
		issue.BodyRaw)
}

type DispatchStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DispatchResponse struct {
	Status    string         `json:"status"`
	Prompt    string         `json:"prompt"`
	Session   string         `json:"session"`
	LogFile   string         `json:"log_file,omitempty"`
	AttachCmd string         `json:"attach_cmd,omitempty"`
	Steps     []DispatchStep `json:"steps"`
}

var dispatchAgentSession = startAgentSession

func tmuxSessionName(slug string) string {
	r := strings.NewReplacer("/", "-", ".", "-", " ", "-")
	return "agent-" + r.Replace(slug)
}

func agentLaunchCommand(agentType string, promptPath string) string {
	if agentType == "codex" {
		return fmt.Sprintf("codex \"$(cat %q)\"", promptPath)
	}
	return agentType
}

func runStep(steps *[]DispatchStep, name string, cmd *exec.Cmd) bool {
	out, err := cmd.CombinedOutput()
	if err != nil {
		*steps = append(*steps, DispatchStep{Name: name, Status: "error", Detail: strings.TrimSpace(string(out))})
		return false
	}
	*steps = append(*steps, DispatchStep{Name: name, Status: "ok"})
	return true
}

// openTerminalStep opens a terminal attached to the given tmux session.
// Uses proj.Terminal if set; falls back to i3+alacritty for backwards compat.
// terminal="none" is headless: appends an info step and returns true.
func openTerminalStep(proj *tracker.Project, session string, steps *[]DispatchStep) bool {
	terminal := ""
	if proj != nil {
		terminal = proj.Terminal
	}

	if terminal == "none" {
		*steps = append(*steps, DispatchStep{Name: "Terminal: headless — attach manually", Status: "ok"})
		return true
	}

	if terminal != "" {
		cmd := strings.ReplaceAll(terminal, "{{session}}", session)
		return runStep(steps, "Open terminal", exec.Command("sh", "-c", cmd))
	}

	// Backwards compat: i3 + alacritty
	if proj != nil && proj.I3Workspace != "" {
		if !runStep(steps, fmt.Sprintf("Switch to workspace %s", proj.I3Workspace),
			exec.Command("i3-msg", "workspace", proj.I3Workspace)) {
			return false
		}
	}
	return runStep(steps, "Open alacritty",
		exec.Command("i3-msg", "exec", fmt.Sprintf("alacritty -e tmux attach -t %s", session)))
}

func startAgentSession(proj *tracker.Project, session string, prompt string, issueSlug string, agentType string) DispatchResponse {
	workDir := resolveProjectWorkDir(proj)

	promptFile, err := os.CreateTemp("", "agent-prompt-*.txt")
	if err != nil {
		return DispatchResponse{Status: "error", Steps: []DispatchStep{{Name: "Create prompt file", Status: "error", Detail: err.Error()}}}
	}
	promptPath := promptFile.Name()
	if _, err := promptFile.WriteString(prompt); err != nil {
		promptFile.Close()
		os.Remove(promptPath)
		return DispatchResponse{Status: "error", Steps: []DispatchStep{{Name: "Write prompt file", Status: "error", Detail: err.Error()}}}
	}
	if err := promptFile.Close(); err != nil {
		os.Remove(promptPath)
		return DispatchResponse{Status: "error", Steps: []DispatchStep{{Name: "Close prompt file", Status: "error", Detail: err.Error()}}}
	}

	steps := []DispatchStep{}
	sessionLogDir := filepath.Join(workDir, ".agent-logs", session)
	rawLog := filepath.Join(sessionLogDir, "rawlog")
	cliLog := filepath.Join(sessionLogDir, session+".clilog")

	response := DispatchResponse{
		Status:  "dispatched",
		Prompt:  prompt,
		Session: session,
		LogFile: rawLog,
		Steps:   steps,
	}

	if !runStep(&steps, fmt.Sprintf("Create tmux session in %s", workDir),
		exec.Command("tmux", "new-session", "-d", "-s", session, "-c", workDir)) {
		response.Status = "error"
		response.Steps = steps
		return response
	}

	windowName := session
	if issueSlug != "" {
		windowName = issueSlug
	}
	exec.Command("tmux", "rename-window", "-t", session, windowName).Run()

	os.MkdirAll(sessionLogDir, 0755)

	// Persist the exact prompt the bot is briefed with so the timeline view
	// can replay it later instead of reconstructing an approximation.
	dispatchPromptPath := filepath.Join(sessionLogDir, "dispatch-prompt.txt")
	_ = os.WriteFile(dispatchPromptPath, []byte(prompt), 0644)

	runStep(&steps, fmt.Sprintf("Log to %s", rawLog),
		exec.Command("tmux", "pipe-pane", "-t", session, "-o", fmt.Sprintf("cat >> %s", rawLog)))
	runStep(&steps, fmt.Sprintf("CLI log to %s", cliLog),
		exec.Command("tmux", "send-keys", "-t", session, fmt.Sprintf("export ISSUE_CLI_LOG=%q", cliLog), "Enter"))

	serverRoot, _ := os.Getwd()
	runStep(&steps, fmt.Sprintf("Server root env %s", serverRoot),
		exec.Command("tmux", "send-keys", "-t", session, fmt.Sprintf("export ISSUE_VIEWER_SERVER_PWD=%q", serverRoot), "Enter"))
	if issueSlug != "" {
		runStep(&steps, fmt.Sprintf("Issue slug env %s", issueSlug),
			exec.Command("tmux", "send-keys", "-t", session, fmt.Sprintf("export ISSUE_VIEWER_ISSUE_SLUG=%q", issueSlug), "Enter"))
	}

	runStep(&steps, fmt.Sprintf("cd %s", workDir),
		exec.Command("tmux", "send-keys", "-t", session, fmt.Sprintf("cd %q", workDir), "Enter"))

	if !openTerminalStep(proj, session, &steps) {
		response.Status = "error"
		response.Steps = steps
		return response
	}
	if proj != nil && proj.Terminal == "none" {
		response.AttachCmd = fmt.Sprintf("tmux attach -t %s", session)
	}

	time.Sleep(500 * time.Millisecond)

	if agentType == "codex" {
		runStep(&steps, "Start codex with prompt file",
			exec.Command("tmux", "send-keys", "-t", session, agentLaunchCommand(agentType, promptPath), "Enter"))
		// tmux send-keys returns before the shell in the pane expands $(cat ...).
		// Keep the temp file around a bit longer so codex can read it reliably.
		time.AfterFunc(2*time.Minute, func() {
			_ = os.Remove(promptPath)
		})
	} else {
		runStep(&steps, fmt.Sprintf("Start %s (interactive)", agentType),
			exec.Command("tmux", "send-keys", "-t", session, agentLaunchCommand(agentType, promptPath), "Enter"))
		time.Sleep(3 * time.Second)
		runStep(&steps, "Load prompt into tmux buffer",
			exec.Command("tmux", "load-buffer", promptPath))
		runStep(&steps, fmt.Sprintf("Paste prompt to %s", agentType),
			exec.Command("tmux", "paste-buffer", "-t", session))
		time.Sleep(200 * time.Millisecond)
		runStep(&steps, "Submit prompt",
			exec.Command("tmux", "send-keys", "-t", session, "Enter"))
		_ = os.Remove(promptPath)
	}

	response.Steps = steps
	return response
}

func buildRetrosReviewPrompt(proj *tracker.Project, retros []*RetroEntry, bugs []*ToolBugReportView) string {
	retroLines := make([]string, 0, len(retros))
	for _, retro := range retros {
		title := retro.IssueTitle
		if strings.TrimSpace(title) == "" {
			title = retro.FileName
		}
		retroLines = append(retroLines, fmt.Sprintf("- %s | issue=%s | status=%s | review_status=%s | file=retros/%s",
			title,
			valueOrDash(retro.IssueSlug),
			valueOrDash(retro.Status),
			normalizeRetroReviewStatus(retro.ReviewStatus),
			retro.FileName))
	}
	if len(retroLines) == 0 {
		retroLines = append(retroLines, "- none")
	}

	bugLines := make([]string, 0, len(bugs))
	for _, bug := range bugs {
		bugLines = append(bugLines, fmt.Sprintf("- issue=%s | status=%s | tool=%s | file=bugs/%s | desc=%s",
			valueOrDash(bug.IssueSlug),
			normalizeBugStatus(bug.Status),
			valueOrDash(bug.Tool),
			bug.FileName,
			trimSnippet(bug.Description, 160)))
	}
	if len(bugLines) == 0 {
		bugLines = append(bugLines, "- none")
	}

	return fmt.Sprintf(`Review the project feedback files for %s.

Your job:
- scan the project retrospectives under retros/
- scan related bug reports under bugs/
- decide which reports describe real issues, workflow gaps, duplicated reports, or noise
- suggest concrete fixes, workflow changes, or code changes
- identify items that should become actual tracked issues
- if you are confident a retrospective has been reviewed, mark that file with ReviewStatus: processed
- if you are confident a bug report is resolved or should not be acted on, update its JSON status to fixed or wontfix
- leave uncertain items open

Do not mention issue-cli in your writeup. Focus on the files, the underlying problems, and ideas to fix them.

Files to review:
Retrospectives:
%s

Bug reports:
%s

Expected output:
1. A short triage summary of the real issues and duplicates.
2. Concrete suggestions for fixes or workflow changes.
3. Which files you marked processed, fixed, or wontfix.
`, proj.Name, strings.Join(retroLines, "\n"), strings.Join(bugLines, "\n"))
}

func valueOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func trimSnippet(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (s *Server) handleRetrosReviewDispatch(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
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

	agentType := "claude"
	var body struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil && strings.TrimSpace(body.Agent) != "" {
		agentType = body.Agent
	}

	prompt := buildRetrosReviewPrompt(proj, retros, bugs)
	session := tmuxSessionName(proj.Slug + "-retros-review")
	resp := dispatchAgentSession(proj, session, prompt, "", agentType)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDispatchAgent(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/dispatch")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	// Parse agent type from request body (default: claude)
	agentType := "claude"
	var body struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Agent != "" {
		agentType = body.Agent
	}

	wf := proj.LoadWorkflowForIssue(issue)
	prompt := buildAgentPrompt(issue, wf)
	session := tmuxSessionName(slug)
	resp := dispatchAgentSession(proj, session, prompt, issue.Slug, agentType)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// mergeSubdirSystems merges subdirectory names into the systems list so that
// empty folders still appear as available systems.
func mergeSubdirSystems(systems []string, issueDir string) []string {
	subdirSystems := tracker.CollectSubdirSystems(issueDir)
	seen := map[string]bool{}
	for _, s := range systems {
		seen[s] = true
	}
	for _, s := range subdirSystems {
		if !seen[s] {
			systems = append(systems, s)
			seen[s] = true
		}
	}
	sort.Strings(systems)
	return systems
}

// --- Filters ---

func filterIssues(issues []*tracker.Issue, f FilterParams) []*tracker.Issue {
	var result []*tracker.Issue
	for _, issue := range issues {
		if f.Status != "" && issue.Status != f.Status {
			continue
		}
		if f.System != "" && !strings.EqualFold(issue.System, f.System) {
			continue
		}
		if f.Priority != "" && issue.Priority != f.Priority {
			continue
		}
		if f.Label != "" {
			hasLabel := false
			for _, l := range issue.Labels {
				if strings.EqualFold(l, f.Label) {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				continue
			}
		}
		if f.Assignee == "_claimed" && issue.Assignee == "" {
			continue
		}
		if f.Assignee == "_unclaimed" && issue.Assignee != "" {
			continue
		}
		if f.Assignee != "" && f.Assignee != "_claimed" && f.Assignee != "_unclaimed" && issue.Assignee != f.Assignee {
			continue
		}
		if f.Search != "" {
			search := strings.ToLower(f.Search)
			if !strings.Contains(strings.ToLower(issue.Title), search) &&
				!strings.Contains(strings.ToLower(issue.BodyRaw), search) {
				continue
			}
		}
		result = append(result, issue)
	}
	return result
}

func issueViews(issues []*tracker.Issue, sessionMap map[string][]AgentSession) []*IssueView {
	result := make([]*IssueView, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issueView(issue, sessionMap))
	}
	return result
}

func issueView(issue *tracker.Issue, sessionMap map[string][]AgentSession) *IssueView {
	if issue == nil {
		return nil
	}
	return &IssueView{
		Issue:          issue,
		ActiveSessions: append([]AgentSession(nil), sessionMap[issue.Slug]...),
	}
}

// attachScores computes and attaches ScoreBreakdowns to each view when the
// workflow's scoring block is enabled. No-op otherwise.
func attachScores(views []*IssueView, scoring *tracker.ScoringConfig) {
	if scoring == nil || !scoring.Enabled {
		return
	}
	now := time.Now()
	for _, v := range views {
		if v == nil || v.Issue == nil {
			continue
		}
		v.Score = tracker.ComputeScore(v.Issue, scoring, now)
	}
}

// sortViewsByScore sorts in-place by Score.Total descending. Views with no
// score (nil Score) sort last, stable.
func sortViewsByScore(views []*IssueView) {
	sort.SliceStable(views, func(i, j int) bool {
		si, sj := views[i].Score, views[j].Score
		if si == nil && sj == nil {
			return false
		}
		if si == nil {
			return false
		}
		if sj == nil {
			return true
		}
		return si.Total > sj.Total
	})
}

func sessionsByIssueSlug(issues []*tracker.Issue) (map[string][]AgentSession, int) {
	sessions := listTmuxSessions()
	result := make(map[string][]AgentSession, len(issues))
	matchedSessionNames := map[string]bool{}
	for _, issue := range issues {
		for _, session := range sessions {
			if sessionMatchesIssue(session.Name, issue.Slug) {
				result[issue.Slug] = append(result[issue.Slug], session)
				matchedSessionNames[session.Name] = true
			}
		}
	}
	return result, len(matchedSessionNames)
}

func sessionMatchesIssue(sessionName string, slug string) bool {
	sessionName = strings.ToLower(strings.TrimSpace(sessionName))
	slug = strings.ToLower(strings.TrimSpace(slug))
	if sessionName == "" || slug == "" {
		return false
	}

	candidates := []string{
		slug,
		strings.ReplaceAll(slug, "/", "-"),
		tmuxSessionName(slug),
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(sessionName, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func defaultListTmuxSessions() []AgentSession {
	out, err := exec.Command("tmux", "ls").Output()
	if err != nil {
		return nil
	}

	lineRE := regexp.MustCompile(`^([^:]+):.*\(created ([^)]+)\)`)
	var sessions []AgentSession
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := lineRE.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}

		session := AgentSession{Name: strings.TrimSpace(matches[1])}
		if created, err := time.Parse("Mon Jan _2 15:04:05 2006", strings.TrimSpace(matches[2])); err == nil {
			session.StartTime = created.Format("2006-01-02 15:04:05")
		} else {
			session.StartTime = strings.TrimSpace(matches[2])
		}
		sessions = append(sessions, session)
	}
	return sessions
}

// --- GitHub integration ---

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
