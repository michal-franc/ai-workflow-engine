package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// updateIssuePayload combines a plain IssueUpdate with the answers collected
// from a workflow transition's declarative fields[]. When Status is set to a
// value different from the issue's current status, the server routes through
// the workflow engine so the web path matches `issue-cli transition`.
type updateIssuePayload struct {
	tracker.IssueUpdate
	Fields map[string]string `json:"fields,omitempty"`
	// BaseBodyHash is tracker.BodyHash of the body the page started from.
	// With a body, the write is refused (409) when the file's body has
	// changed since, so a stale page never overwrites a CLI append.
	BaseBodyHash string `json:"base_body_hash,omitempty"`
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
	// A retitle keeps the slug, as `issue-cli update --title` does: the old
	// slug is pinned with slug: and the file is not renamed. The pin is the
	// server's call, never the client's.
	update.Slug = nil
	if update.Title != nil {
		update.Slug = tracker.RetitleUpdate(found, *update.Title, false).Slug
	}

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

	var bodyHash string
	switch {
	case update.Body != nil && payload.BaseBodyHash != "":
		bodyHash, err = tracker.UpdateIssueFrontmatterIfBody(found.FilePath, payload.BaseBodyHash, update)
		if errors.Is(err, tracker.ErrStaleBody) {
			writeTransitionError(w, errors.New(staleBodyMessage))
			return
		}
		if err != nil {
			http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	case hasNonStatusUpdates(update):
		if err := tracker.UpdateIssueFrontmatter(found.FilePath, update); err != nil {
			http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if payload.Status != nil && *payload.Status == "done" && proj.SupportsGitHub && found.Repo != "" && found.Number > 0 {
		s.bg.Go(fmt.Sprintf("github close %s#%d", found.Repo, found.Number), func() {
			closeGithubIssue(s.bg.hard, found)
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]string{"status": "ok", "slug": found.Slug}
	if bodyHash != "" {
		resp["body_hash"] = bodyHash
	}
	json.NewEncoder(w).Encode(resp)
}

// staleBodyMessage is the 409 text when a page's body is older than the file.
const staleBodyMessage = "This issue changed since the page loaded (an agent, the CLI or another tab wrote it). Your change was not saved: reload to see the current version."

// handleToggleCheckbox flips one task-list checkbox, named by its line in
// the body (the data-line the renderer puts on each box), never by counting
// `[ ]` in the raw markdown, which also matches code and prose. It is
// refused when the body changed since the page loaded (409), when the box
// is already in the asked state (409: the page showed something else) and
// when the line holds no task-list box (400).
func (s *Server) handleToggleCheckbox(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/issue/"), "/checkbox")
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	var payload struct {
		Line         int    `json:"line"`
		Checked      bool   `json:"checked"`
		BaseBodyHash string `json:"base_body_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Line < 1 || payload.BaseBodyHash == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "line and base_body_hash required"})
		return
	}
	body, err := tracker.ToggleIssueTaskBox(issue.FilePath, payload.BaseBodyHash, payload.Line, payload.Checked)
	switch {
	case errors.Is(err, tracker.ErrStaleBody):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": staleBodyMessage})
	case errors.Is(err, tracker.ErrTaskBoxState):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "This box was already in that state in the file. Reload to see the current version."})
	case errors.Is(err, tracker.ErrNoTaskBox):
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case err != nil:
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "update failed: " + err.Error()})
	default:
		writeJSONStatus(w, http.StatusOK, map[string]string{"status": "ok", "body": body, "body_hash": tracker.BodyHash(body)})
	}
}

func hasNonStatusUpdates(u tracker.IssueUpdate) bool {
	return u.Title != nil || u.Priority != nil || u.Version != nil || u.Assignee != nil ||
		u.HumanApproval != nil || u.QueuedApproval != nil || u.StartedAt != nil || u.DoneAt != nil ||
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

// closeGithubIssue runs under ctx, which shutdown cancels once its grace
// period is over; a close it kills is logged, never retried.
func closeGithubIssue(ctx context.Context, issue *tracker.Issue) {
	comment := fmt.Sprintf("Closed via issue-viewer.\n\nImplementation tracked in local issue: %s", issue.Title)
	if issue.BodyRaw != "" {
		comment += "\n\n---\n\n" + issue.BodyRaw
	}
	token, err := exec.CommandContext(ctx, os.ExpandEnv("$HOME/Work/michal-franc-agent/gh-app-token")).Output()
	if err != nil {
		logGithubCloseFailure(ctx, issue, "get token", err, nil)
		return
	}
	args := []string{"issue", "close", fmt.Sprintf("%d", issue.Number),
		"--repo", issue.Repo,
		"--comment", comment,
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+strings.TrimSpace(string(token)))
	if out, err := cmd.CombinedOutput(); err != nil {
		logGithubCloseFailure(ctx, issue, "gh issue close", err, out)
	}
}

func logGithubCloseFailure(ctx context.Context, issue *tracker.Issue, step string, err error, out []byte) {
	if ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "closeGithubIssue: %s#%d not closed: server shut down during %s\n", issue.Repo, issue.Number, step)
		return
	}
	fmt.Fprintf(os.Stderr, "closeGithubIssue: %s#%d: %s: %v\n%s", issue.Repo, issue.Number, step, err, out)
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
		// Then queues a second approval that becomes active once Status is
		// consumed ("approve and start work"). Ignored when un-approving.
		Then string `json:"then"`
		// Words are the owner's, from the words box: saved as an approval:
		// comment and quoted to the agent. Ignored when un-approving.
		Words string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		http.Error(w, `{"error":"status required"}`, http.StatusBadRequest)
		return
	}

	// Toggle: if already approved exactly as asked, clear it. Asking for a
	// follow-up on an approval that has none adds the follow-up instead.
	var humanApproval string
	alreadyApproved := strings.EqualFold(issue.HumanApproval, body.Status)
	if alreadyApproved && body.Then != "" && issue.QueuedApproval == "" {
		alreadyApproved = false
	}
	if !alreadyApproved {
		humanApproval = body.Status
	}

	// The queued approval only exists alongside the approval it follows.
	queued := ""
	if humanApproval != "" && body.Then != "" {
		wf := proj.LoadWorkflowForIssue(issue)
		if !wf.IsValidTransition(body.Status, body.Then) || wf.RequiredHumanApproval(body.Status, body.Then) == "" {
			http.Error(w, fmt.Sprintf(`{"error":"%s → %s is not an approval-gated next step"}`, body.Status, body.Then), http.StatusBadRequest)
			return
		}
		queued = wf.RequiredHumanApproval(body.Status, body.Then)
	}

	update := tracker.IssueUpdate{HumanApproval: &humanApproval, QueuedApproval: &queued}
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, update); err != nil {
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Timestamp the approval so the transition consuming it records exact
	// gate latency.
	if err := tracker.RecordApproval(issue.FilePath, humanApproval, time.Now()); err != nil {
		log.Printf("approve %s: recording approval time: %v", issue.Slug, err)
	}
	// The words go in after the approval, so a failed write never loses it.
	words := ""
	if humanApproval != "" {
		words = strings.TrimSpace(body.Words)
	}
	wordsSaved, wordsErr := false, ""
	if words != "" {
		statuses := []string{humanApproval}
		if queued != "" {
			statuses = append(statuses, queued)
		}
		comment := tracker.ApprovalComment(words, "viewer", time.Now().Format("2006-01-02"), statuses...)
		if err := tracker.AddComment(issue.FilePath, 0, comment, "app"); err != nil {
			log.Printf("approve %s: saving your words: %v", issue.Slug, err)
			wordsErr = err.Error()
		} else {
			wordsSaved = true
		}
	}

	resp := approvalResponse{
		Status:         "ok",
		HumanApproval:  humanApproval,
		QueuedApproval: queued,
		Label:          approvalLabel(body.Status),
		WordsSaved:     wordsSaved,
		WordsError:     wordsErr,
	}
	if humanApproval != "" {
		sessionMap, _ := sessionsByIssueSlug(proj, []*tracker.Issue{issue})
		activeSessions := sessionMap[issue.Slug]
		if len(activeSessions) == 0 {
			resp.NotificationError = "no active agent session matched this issue"
		} else {
			target := activeSessions[0].Name
			messageLines := []string{approvalNudge(issue.Slug, body.Status, queued, words)}
			if err := tmuxSendKeys(target, messageLines); err != nil {
				resp.NotificationError = err.Error()
			} else {
				resp.NotifiedSession = target
				resp.NotificationMessage = "approval notification sent to active agent session"
			}
		}
		if resp.NotificationError != "" {
			log.Printf("approve %s for %q: agent not nudged: %s", issue.Slug, body.Status, resp.NotificationError)
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

// approvalNudge is the line sent to the issue's agent: the owner's words
// when there are any, else a canned line; an approval with a queued follow-up
// always says how to continue into it.
func approvalNudge(slug, status, queued, words string) string {
	if queued != "" {
		msg := fmt.Sprintf("Approved for %s and %s. After the %s transition, run issue-cli start %s to continue straight into %s.", status, queued, status, slug, queued)
		if words != "" {
			msg += fmt.Sprintf(" Owner's words: %q", words)
		}
		return msg
	}
	if words != "" {
		return fmt.Sprintf("Approved for %s: %q", status, words)
	}
	return humanApprovalMessage(status)
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

// editorReachHint is the one-line form of agentrun.AttachHint for edit
// session messages.
func editorReachHint(proj *tracker.Project, session string) string {
	return fmt.Sprintf("inside tmux: %s · from a shell: %s", agentrun.SwitchCmd(proj, session), agentAttachCmd(proj, session))
}

// openEditorTerminal opens a terminal window attached to the given tmux
// session, using project terminal config (or i3+alacritty as backwards-compat
// fallback). Returns ("", nil) on success, a friendly attach hint when the
// project is configured headless ("none"), or a wrapped error.
//
// In shared mode (tmux_session set) the editor's window is selected and a
// terminal is only opened when nobody is attached to the shared session.
func openEditorTerminal(proj *tracker.Project, session string) (string, error) {
	terminal := ""
	if proj != nil {
		terminal = proj.Terminal
	}
	if agentrun.TerminalNone(proj) {
		return editorReachHint(proj, session), nil
	}
	name := session
	if shared := sharedTmuxSession(proj); shared != "" {
		exec.Command("tmux", "select-window", "-t", agentTmuxTarget(proj, session)).Run()
		if agentrun.SessionAttached(shared) {
			return "", nil
		}
		session = shared
	}
	if terminal != "" {
		termCmd := agentrun.ExpandTerminal(proj, terminal, session, name, agentrun.RoleEditor)
		if err := exec.Command("sh", "-c", termCmd).Run(); err != nil {
			return "", fmt.Errorf("open terminal: %w", err)
		}
		return "", nil
	}
	if proj != nil && proj.I3Workspace != "" {
		if err := exec.Command("i3-msg", "workspace", proj.I3Workspace).Run(); err != nil {
			return "", fmt.Errorf("switch i3 workspace: %w", err)
		}
	}
	if err := exec.Command("i3-msg", "exec", fmt.Sprintf("alacritty -e tmux attach -t %s", session)).Run(); err != nil {
		return "", fmt.Errorf("open alacritty: %w", err)
	}
	return "", nil
}

func startIssueBodyEditor(bg *background, proj *tracker.Project, issue *tracker.Issue) (BodyEditResponse, error) {
	if issue == nil {
		return BodyEditResponse{}, fmt.Errorf("issue is required")
	}

	workDir := resolveProjectWorkDir(proj)
	session := tmuxSessionName(issue.Slug) + "-edit"
	waitSignal := session + "-done"
	target := agentTmuxTarget(proj, session)

	if agentrun.HasSession(target) {
		// A previous edit session is still alive (commonly leaked: the wait-for
		// goroutine died before nvim exited). Don't try to reuse its temp
		// body/status files — we don't know them and the previous request owns
		// the save-on-exit goroutine. Just open a terminal pointed at the
		// existing session so the user can finish or abandon their edit.
		attachCmd, err := openEditorTerminal(proj, session)
		if err != nil {
			return BodyEditResponse{}, err
		}
		msg := "Existing edit session — attached without registering save-on-exit. Finish or kill the existing session before editing again."
		if attachCmd != "" {
			msg = fmt.Sprintf("Existing edit session at %q. Reach it %s", agentDisplayName(proj, session), attachCmd)
		}
		return BodyEditResponse{
			Status:     "reattached",
			Session:    agentDisplayName(proj, session),
			Message:    msg,
			Reattached: true,
		}, nil
	}

	baseContent, err := os.ReadFile(issue.FilePath)
	if err != nil {
		return BodyEditResponse{}, fmt.Errorf("read issue file: %w", err)
	}
	baseHash := sha256.Sum256(baseContent)

	editFile, err := os.CreateTemp("", "issue-edit-*.md")
	if err != nil {
		return BodyEditResponse{}, fmt.Errorf("create temp edit file: %w", err)
	}
	editPath := editFile.Name()
	editContent := baseContent
	if len(editContent) > 0 && editContent[len(editContent)-1] != '\n' {
		editContent = append(editContent, '\n')
	}
	if _, err := editFile.Write(editContent); err != nil {
		editFile.Close()
		os.Remove(editPath)
		return BodyEditResponse{}, fmt.Errorf("write temp edit file: %w", err)
	}
	if err := editFile.Close(); err != nil {
		os.Remove(editPath)
		return BodyEditResponse{}, fmt.Errorf("close temp edit file: %w", err)
	}

	statusFile, err := os.CreateTemp("", "issue-edit-status-*.txt")
	if err != nil {
		os.Remove(editPath)
		return BodyEditResponse{}, fmt.Errorf("create temp status file: %w", err)
	}
	statusPath := statusFile.Name()
	statusFile.Close()

	cleanup := func() {
		os.Remove(editPath)
		os.Remove(statusPath)
	}

	_, createCmd := createAgentTmux(proj, session, workDir)
	if err := createCmd.Run(); err != nil {
		cleanup()
		return BodyEditResponse{}, fmt.Errorf("create tmux session: %w", err)
	}

	sessionReady := false
	defer func() {
		if !sessionReady {
			killAgentTmux(proj, session)
			cleanup()
		}
	}()

	if sharedTmuxSession(proj) == "" {
		exec.Command("tmux", "rename-window", "-t", session, issue.Slug).Run()
	}

	if agentrun.TerminalNone(proj) {
		return BodyEditResponse{}, fmt.Errorf("terminal is 'none': %s", editorReachHint(proj, session))
	}
	if _, err := openEditorTerminal(proj, session); err != nil {
		return BodyEditResponse{}, err
	}

	time.Sleep(500 * time.Millisecond)

	editCmd := fmt.Sprintf("nvim %q; code=$?; printf '%%s' \"$code\" > %q; tmux wait-for -S %q; exit $code", editPath, statusPath, waitSignal)
	if err := exec.Command("tmux", "send-keys", "-t", target, editCmd, "Enter").Run(); err != nil {
		return BodyEditResponse{}, fmt.Errorf("start nvim: %w", err)
	}

	edit := bodyEdit{
		proj:       proj,
		session:    session,
		waitSignal: waitSignal,
		editPath:   editPath,
		statusPath: statusPath,
		issuePath:  issue.FilePath,
		baseHash:   baseHash,
		cleanup:    cleanup,
	}
	bg.Go("nvim edit "+issue.Slug, func() { awaitBodyEdit(bg, edit) })

	sessionReady = true
	return BodyEditResponse{
		Status:  "launched",
		Session: agentDisplayName(proj, session),
		Message: "Opened in nvim. The issue file will sync back after the editor exits.",
	}, nil
}

// bodyEdit is one nvim edit session waiting to be written back.
type bodyEdit struct {
	proj                 *tracker.Project
	session, waitSignal  string
	editPath, statusPath string
	issuePath            string
	baseHash             [sha256.Size]byte
	cleanup              func()
}

// awaitBodyEdit blocks until nvim exits, then writes the edit back to the
// issue. Shutdown does not wait for the human: it kills the edit session and
// leaves a comment saying the changes were discarded. A write-back that has
// started runs to the end.
func awaitBodyEdit(bg *background, e bodyEdit) {
	defer e.cleanup()
	if err := exec.CommandContext(bg.ctx, "tmux", "wait-for", e.waitSignal).Run(); err != nil {
		if bg.ctx.Err() != nil {
			killAgentTmux(e.proj, e.session)
			recordEditFailure(e.issuePath, "server shut down; nvim session closed, changes discarded")
		}
		return
	}
	defer killAgentTmux(e.proj, e.session)

	statusBytes, err := os.ReadFile(e.statusPath)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(statusBytes)) != "0" {
		return
	}

	updatedBytes, err := os.ReadFile(e.editPath)
	if err != nil {
		return
	}

	if _, err := tracker.ParseIssue(filepath.Base(e.issuePath), updatedBytes); err != nil {
		recordEditFailure(e.issuePath, fmt.Sprintf("invalid issue file: %v", err))
		return
	}

	currentContent, err := os.ReadFile(e.issuePath)
	if err != nil {
		return
	}
	if sha256.Sum256(currentContent) != e.baseHash {
		recordEditFailure(e.issuePath, "issue file changed during nvim edit; nvim changes discarded")
		return
	}

	if err := tracker.RewriteIssueFile(e.issuePath, updatedBytes); err != nil {
		recordEditFailure(e.issuePath, fmt.Sprintf("write failed: %v", err))
	}
}

// recordEditFailure leaves a comment on the issue describing why a nvim edit
// was discarded so the user discovers the failure on next page load.
func recordEditFailure(filePath, msg string) {
	_ = tracker.AddComment(filePath, 0, "edit-in-nvim aborted: "+msg, "edit-in-nvim")
}

func (s *Server) handleEditIssueInNvim(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/edit-in-nvim")

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	resp, err := launchIssueBodyEditor(s.bg, proj, issue)
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
	Type     string   `json:"type"`
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

	status := req.Status
	workType := ""
	if wf := proj.LoadWorkflow(); wf.HasTypes() {
		if req.Type != "" {
			if _, ok := wf.Types[req.Type]; !ok {
				http.Error(w, fmt.Sprintf("unknown type %q (types: %s)", req.Type, strings.Join(wf.TypeNames(), ", ")), http.StatusBadRequest)
				return
			}
		}
		workType, _ = wf.ResolveType(req.Type)
		// A status the type's path skips (e.g. the board's "+" on a column
		// a tweak never visits) starts the issue at the type's first status.
		if status == "" || wf.ForType(workType).GetStatusIndex(status) == -1 {
			status = wf.FirstStatus(workType)
		}
	}

	_, slug, err := tracker.CreateIssueFileOpts(proj.IssueDir, tracker.CreateIssueOpts{
		Title:    req.Title,
		Type:     workType,
		Status:   status,
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
