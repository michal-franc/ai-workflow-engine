package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

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

