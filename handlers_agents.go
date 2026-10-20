package main

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The agents canvas (/p/<project>/agents) shows the human → project lead →
// sub-lead → worker hierarchy built from tmux and the issues' parent links,
// plus the jobs that have work in flight but no live agent.

// AgentIssue is the issue card an agent node links to.
type AgentIssue struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Color  string `json:"color"`
	URL    string `json:"url"`
}

// AgentNode is one lead or worker on the canvas.
type AgentNode struct {
	// Name is the bare session name (lead-x, agent-y).
	Name string `json:"name"`
	// Display is where it lives in tmux (shared:name in shared mode).
	Display string `json:"display"`
	// Kind is project-lead, sub-lead, worker or other (an agent session not
	// bound to an issue, e.g. a project action).
	Kind string `json:"kind"`
	Live bool   `json:"live"`
	// State is working, at-gate, question or stopped for workers; live or
	// not-running for leads.
	State string `json:"state"`
	// Gate is the status awaiting the human's approval when State is at-gate.
	Gate          string      `json:"gate,omitempty"`
	Epic          string      `json:"epic,omitempty"`
	Issue         *AgentIssue `json:"issue,omitempty"`
	RetroNotSwept bool        `json:"retro_not_swept,omitempty"`
	AttachCmd     string      `json:"attach_cmd"`
	StartTime     string      `json:"start_time,omitempty"`
}

// EpicGroup is one epic box: its sub-lead and how its children are doing.
type EpicGroup struct {
	Issue           AgentIssue `json:"issue"`
	Base            string     `json:"base,omitempty"`
	MainAhead       int        `json:"main_ahead"`
	MainAheadKnown  bool       `json:"main_ahead_known"`
	ChildrenRunning int        `json:"children_running"`
	ChildrenTotal   int        `json:"children_total"`
	SubLead         AgentNode  `json:"sub_lead"`
	// HasSubLead is false for epics nobody has spawned a sub-lead for yet.
	HasSubLead bool `json:"has_sub_lead"`
}

// AgentsModel is the whole canvas, also served as /p/<project>/agents.json.
type AgentsModel struct {
	Project     string      `json:"project"`
	Prefix      string      `json:"prefix"`
	ListedAt    string      `json:"listed_at"`
	ProjectLead AgentNode   `json:"project_lead"`
	Epics       []EpicGroup `json:"epics"`
	// Workers are the live workers; each names its epic, if any.
	Workers []AgentNode `json:"workers"`
	// Unfinished are issues with a worktree or an active status and no live
	// agent (the "stopped" workers).
	Unfinished []AgentNode `json:"unfinished"`
	// Others are live agent sessions not bound to an issue.
	Others []AgentNode `json:"others"`
}

// mainAhead counts commits on main not yet in base. A var so tests don't run
// git.
var mainAhead = agentrun.MainAhead

func statusColorOf(status string) string {
	return funcMap["statusColor"].(func(string) string)(status)
}

func agentIssueOf(prefix string, issue *tracker.Issue) *AgentIssue {
	return &AgentIssue{
		Slug:   issue.Slug,
		Title:  issue.Title,
		Status: issue.Status,
		Color:  statusColorOf(issue.Status),
		URL:    prefix + "/issue/" + issue.Slug,
	}
}

// activeStatus reports whether status is one where work is in flight: from
// "in progress" (else the second status) up to, not including, "done" (else
// the last status).
func activeStatus(wf *tracker.WorkflowConfig, status string) bool {
	order := wf.GetStatusOrder()
	idx := wf.GetStatusIndex(status)
	if idx < 0 || len(order) < 3 {
		return false
	}
	start, end := 1, len(order)-1
	if i := wf.GetStatusIndex("in progress"); i >= 0 {
		start = i
	}
	if i := wf.GetStatusIndex("done"); i >= 0 {
		end = i
	}
	return idx >= start && idx < end
}

// pendingGate returns the status the issue waits on the human for: its next
// status, when that transition needs approval not yet given.
func pendingGate(wf *tracker.WorkflowConfig, issue *tracker.Issue) string {
	order := wf.GetStatusOrder()
	idx := wf.GetStatusIndex(issue.Status)
	if idx < 0 || idx+1 >= len(order) {
		return ""
	}
	next := order[idx+1]
	required := wf.RequiredHumanApproval(issue.Status, next)
	if required == "" || strings.EqualFold(issue.HumanApproval, required) {
		return ""
	}
	return required
}

// openQuestion reports whether the issue's newest comment is a question:
// nobody has answered yet.
func openQuestion(comments []tracker.Comment) bool {
	if len(comments) == 0 {
		return false
	}
	last := strings.ToLower(strings.TrimSpace(comments[len(comments)-1].Text))
	return strings.HasPrefix(last, "question:")
}

func (s *Server) buildAgentsModel(proj *tracker.Project, prefix string) (*AgentsModel, error) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Slug < issues[j].Slug })
	workDir := resolveProjectWorkDir(proj)
	live := map[string]AgentSession{}
	for _, sess := range listTmuxSessions() {
		live[agentrun.BareName(sess.Name)] = sess
	}
	retros, _ := loadRetrospectives(proj)
	unswept := map[string]bool{}
	for _, r := range retros {
		if r.IssueSlug != "" && r.ReviewStatus != "processed" && !strings.Contains(filepath.ToSlash(r.FilePath), "/archive/") {
			unswept[strings.ToLower(r.IssueSlug)] = true
		}
	}

	node := func(name, kind string) AgentNode {
		sess, ok := live[name]
		n := AgentNode{Name: name, Display: agentDisplayName(proj, name), Kind: kind, Live: ok, AttachCmd: agentAttachCmd(proj, name), StartTime: sess.StartTime}
		if kind == "project-lead" || kind == "sub-lead" {
			n.State = "not-running"
			if ok {
				n.State = "live"
			}
		}
		return n
	}

	model := &AgentsModel{
		Project:     proj.Name,
		Prefix:      prefix,
		ListedAt:    time.Now().Format(time.RFC3339),
		ProjectLead: node(tracker.LeadSessionName(proj, tracker.LeadScopeProject), "project-lead"),
	}
	claimed := map[string]bool{model.ProjectLead.Name: true}

	for _, issue := range issues {
		if issue.Status == "done" || !tracker.IsEpic(issue, issues) {
			continue
		}
		g := EpicGroup{Issue: *agentIssueOf(prefix, issue)}
		g.SubLead = node(tracker.LeadSessionName(proj, issue.Slug), "sub-lead")
		g.SubLead.Epic = issue.Slug
		g.SubLead.Issue = agentIssueOf(prefix, issue)
		claimed[g.SubLead.Name] = true
		g.HasSubLead = issue.Field("lead") != "" || g.SubLead.Live
		if g.HasSubLead || issue.Field("base") != "" {
			g.Base = tracker.EpicBase(issue)
			g.MainAhead, _, g.MainAheadKnown = mainAhead(workDir, g.Base)
		}
		for _, child := range tracker.EpicChildren(issues, issue.Slug) {
			g.ChildrenTotal++
			if _, ok := live[tracker.AgentSessionName(child.Slug)]; ok {
				g.ChildrenRunning++
			}
		}
		model.Epics = append(model.Epics, g)
	}

	for _, issue := range issues {
		name := tracker.AgentSessionName(issue.Slug)
		claimed[name] = true
		n := node(name, "worker")
		n.Issue = agentIssueOf(prefix, issue)
		if parent := tracker.FindIssue(issues, tracker.ParentSlug(issue)); parent != nil {
			n.Epic = parent.Slug
		} else if tracker.IsEpic(issue, issues) {
			n.Epic = issue.Slug
		}
		wf := proj.LoadWorkflowForIssue(issue)
		if n.Live {
			comments, _ := tracker.LoadComments(issue.FilePath)
			// An open question blocks the agent on the human, so it wins over
			// a gate the issue may also be waiting at.
			switch {
			case openQuestion(comments):
				n.State, n.Gate = "question", pendingGate(wf, issue)
			case pendingGate(wf, issue) != "":
				n.State, n.Gate = "at-gate", pendingGate(wf, issue)
			default:
				n.State = "working"
			}
			model.Workers = append(model.Workers, n)
			continue
		}
		if issue.Status == "done" {
			continue
		}
		wt, _, _ := resolveWorktree(workDir, issue.Slug, wf)
		if activeStatus(wf, issue.Status) || (wt != "" && fileExists(wt)) {
			n.State = "stopped"
			n.RetroNotSwept = unswept[strings.ToLower(issue.Slug)]
			if gate := pendingGate(wf, issue); gate != "" {
				n.Gate = gate
			}
			model.Unfinished = append(model.Unfinished, n)
		}
	}

	var others []string
	for name := range live {
		if !claimed[name] && (strings.HasPrefix(name, "agent-") || strings.HasPrefix(name, "lead-")) {
			others = append(others, name)
		}
	}
	sort.Strings(others)
	for _, name := range others {
		n := node(name, "other")
		n.State = "working"
		model.Others = append(model.Others, n)
	}
	return model, nil
}

type AgentsPageData struct {
	Prefix         string
	ProjectName    string
	SupportsGitHub bool
	ActiveBots     int
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	_, active := sessionsByIssueSlug(nil)
	if err := s.tmpl.ExecuteTemplate(w, "agents.html", AgentsPageData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		SupportsGitHub: proj.SupportsGitHub,
		ActiveBots:     active,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleAgentsJSON(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	model, err := s.buildAgentsModel(proj, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model)
}

// AgentDetail is the selected-agent panel: the pane's last lines, the
// issue's recent comments and the files its branch touches.
type AgentDetail struct {
	Session  string            `json:"session"`
	Pane     string            `json:"pane"`
	Comments []tracker.Comment `json:"comments"`
	Branch   string            `json:"branch,omitempty"`
	Against  string            `json:"against,omitempty"`
	DiffStat string            `json:"diff_stat,omitempty"`
}

// branchDiffStat is `git diff --stat against...branch`. A var for tests.
var branchDiffStat = func(workdir, against, branch string) string {
	out, err := exec.Command("git", "-C", workdir, "diff", "--stat", against+"..."+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

func validAgentName(name string) bool {
	if !strings.HasPrefix(name, "agent-") && !strings.HasPrefix(name, "lead-") {
		return false
	}
	return !strings.ContainsAny(name, " :;'\"`$\\")
}

func (s *Server) handleAgentDetail(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	name := r.URL.Query().Get("session")
	if !validAgentName(name) {
		http.Error(w, "session must be an agent-* or lead-* name", http.StatusBadRequest)
		return
	}
	detail := AgentDetail{Session: name}
	if pane, err := agentrun.CapturePane(agentTmuxTarget(proj, name)); err == nil {
		lines := strings.Split(strings.TrimRight(pane, "\n"), "\n")
		if len(lines) > 20 {
			lines = lines[len(lines)-20:]
		}
		detail.Pane = strings.Join(lines, "\n")
	}
	if slug := r.URL.Query().Get("issue"); slug != "" {
		if issue := s.findIssueBySlug(proj, slug); issue != nil {
			comments, _ := tracker.LoadComments(issue.FilePath)
			if len(comments) > 5 {
				comments = comments[len(comments)-5:]
			}
			detail.Comments = comments
			issues, _ := tracker.LoadIssues(proj.IssueDir)
			workDir := resolveProjectWorkDir(proj)
			wf := proj.LoadWorkflowForIssue(issue)
			if strings.HasPrefix(name, "lead-") {
				detail.Branch = tracker.EpicBase(issue)
				detail.Against = mainBranch(workDir)
			} else if wt := tracker.IssueWorktree(workDir, issue, issues, wf); wt.Branch != "" {
				detail.Branch = wt.Branch
				detail.Against = tracker.ChildBase(issue, issues)
				if detail.Against == "" {
					detail.Against = mainBranch(workDir)
				}
			}
			if detail.Branch != "" && detail.Against != "" {
				detail.DiffStat = branchDiffStat(workDir, detail.Against, detail.Branch)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail)
}

// mainBranch names the project's main branch. A var so tests don't run git.
var mainBranch = agentrun.MainBranch

// handleAgentMessage pastes a message from the human into an agent's or
// lead's live session, signed [from human].
func (s *Server) handleAgentMessage(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	var body struct {
		Session string `json:"session"`
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" || !validAgentName(body.Session) {
		http.Error(w, `{"error":"session (agent-* or lead-*) and text required"}`, http.StatusBadRequest)
		return
	}
	textFile := filepath.Join(resolveProjectWorkDir(proj), ".agent-logs", body.Session, "messages", time.Now().Format("20060102-150405")+".txt")
	resp := agentPaste(proj, body.Session, "[from human] "+strings.TrimSpace(body.Text), textFile, "")
	w.Header().Set("Content-Type", "application/json")
	if resp.Status == "no-session" {
		w.WriteHeader(http.StatusConflict)
	}
	json.NewEncoder(w).Encode(resp)
}

// agentPaste and startLead are seams over agentrun for tests.
var agentPaste = agentrun.Paste
var startLead = agentrun.Start

// handleLeadSpawn starts the project lead, or with {"epic": slug} an epic's
// sub-lead, exactly like `issue-cli lead spawn [--epic]`.
func (s *Server) handleLeadSpawn(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	var body struct {
		Epic  string `json:"epic"`
		Agent string `json:"agent"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var epic *tracker.Issue
	if slug := strings.TrimSpace(body.Epic); slug != "" {
		if epic = tracker.FindIssue(issues, slug); epic == nil {
			http.NotFound(w, r)
			return
		}
	}
	spec, err := agentrun.LeadSpec(proj, proj.LoadWorkflow(), issues, epic, projectRoot(proj), time.Now())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(DispatchResponse{Status: "error", Steps: []DispatchStep{{Name: "Spawn lead", Status: "error", Detail: err.Error()}}})
		return
	}
	spec.AgentType = strings.TrimSpace(body.Agent)
	if spec.AgentType == "" {
		spec.AgentType = "claude"
	}
	spec.ViewerURL = viewerURLFromRequest(r)
	resp := startLead(proj, spec)
	if err := agentrun.RecordLead(epic, resp, spec.Session); err != nil {
		resp.Steps = append(resp.Steps, DispatchStep{Name: "Record lead: on the epic", Status: "error", Detail: err.Error()})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
