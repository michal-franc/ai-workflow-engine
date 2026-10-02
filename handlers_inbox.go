package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The inbox ("Waiting on you") lists everything blocked on a human: issues
// whose only unmet requirement is a human approval, and open structured
// questions from `issue-cli ask`. It is a read-only view over issue files and
// their stats sidecars; answering and approving write back through the
// tracker package.

const (
	inboxKindApproval = "approval"
	inboxKindQuestion = "question"

	// inboxOldAfter turns a card's wait time amber.
	inboxOldAfter = time.Hour
)

type InboxItem struct {
	Kind         string
	Key          string // "<slug>|<target>", used for ?item= selection
	Issue        *tracker.Issue
	Title        string
	Target       string // approval: status to approve; question: Q-id
	From         string // approval: current status
	WaitSince    time.Time
	Wait         time.Duration
	AgentBlocked bool
	AgentRunning bool
	Session      string
	Question     *tracker.Question

	// ThenStatus is the follow-up approval "Approve and start work" queues
	// ("" when the target has no approval-gated next step).
	ThenStatus string
}

// WaitLabel renders the wait like "10 h 12 m", "41 m" or "<1 m".
func (i InboxItem) WaitLabel() string { return formatWait(i.Wait) }

// IsOld reports whether the wait is long enough to highlight.
func (i InboxItem) IsOld() bool { return i.Wait >= inboxOldAfter }

func formatWait(d time.Duration) string {
	if d < time.Minute {
		return "<1 m"
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if h == 0 {
		return fmt.Sprintf("%d m", m)
	}
	return fmt.Sprintf("%d h %d m", h, m)
}

// buildInbox collects inbox items for every issue, oldest wait first.
func buildInbox(proj *tracker.Project, issues []*tracker.Issue, sessionMap map[string][]AgentSession, now time.Time) []InboxItem {
	workflows := map[string]*tracker.WorkflowConfig{}
	workflowFor := func(issue *tracker.Issue) *tracker.WorkflowConfig {
		if wf, ok := workflows[issue.System]; ok {
			return wf
		}
		wf := proj.LoadWorkflowForIssue(issue)
		workflows[issue.System] = wf
		return wf
	}

	var items []InboxItem
	for _, issue := range issues {
		sessions := sessionMap[issue.Slug]
		running := len(sessions) > 0
		session := ""
		if running {
			session = sessions[0].Name
		}
		var stats *tracker.StatsStore
		loadStats := func() tracker.StatsStore {
			if stats == nil {
				s, err := tracker.LoadStats(issue.FilePath)
				if err != nil {
					log.Printf("inbox %s: %v", issue.Slug, err)
				}
				stats = &s
			}
			return *stats
		}

		if item, ok := approvalItem(workflowFor(issue), issue, loadStats, now); ok {
			item.AgentRunning, item.Session = running, session
			items = append(items, item)
		}

		for _, q := range tracker.OpenQuestions(issue.BodyRaw) {
			q := q
			since := q.AskedAt
			if since.IsZero() {
				since = issue.ModTime
			}
			waitStart, blocked := loadStats().QuestionWaits[q.ID]
			if blocked && waitStart.Before(since) {
				since = waitStart
			}
			items = append(items, InboxItem{
				Kind:         inboxKindQuestion,
				Key:          issue.Slug + "|" + q.ID,
				Issue:        issue,
				Title:        q.Text,
				Target:       q.ID,
				WaitSince:    since,
				Wait:         nonNegative(now.Sub(since)),
				AgentBlocked: blocked,
				AgentRunning: running,
				Session:      session,
				Question:     &q,
			})
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].WaitSince.Equal(items[j].WaitSince) {
			return items[i].WaitSince.Before(items[j].WaitSince)
		}
		return items[i].Key < items[j].Key
	})
	return items
}

// approvalItem reports an issue as waiting on a human approval when the
// approval is the only unmet requirement of its next transition. At handoff
// statuses (backlog, human-testing) nobody is working the issue, so it is only
// listed while an agent is actually blocked on `start --wait`.
func approvalItem(wf *tracker.WorkflowConfig, issue *tracker.Issue, loadStats func() tracker.StatsStore, now time.Time) (InboxItem, bool) {
	from := issue.Status
	to := wf.NextRequiredStatus(from)
	if to == "" {
		to = wf.NextStatus(from)
	}
	if to == "" {
		return InboxItem{}, false
	}
	required := wf.RequiredHumanApproval(from, to)
	if required == "" || strings.EqualFold(issue.HumanApproval, required) {
		return InboxItem{}, false
	}

	store := loadStats()
	pending := store.PendingWait
	blocked := pending != nil && strings.EqualFold(pending.To, to)

	candidate := *issue
	if tracker.IsHandoffStatus(from) {
		if !blocked {
			return InboxItem{}, false
		}
		if candidate.Assignee == "" {
			candidate.Assignee = "agent" // start claims before validating
		}
	}
	comments, _ := tracker.LoadComments(issue.FilePath)
	preview := wf.PreviewTransitionAll(&candidate, from, to, issue.System, comments)
	for _, step := range preview.Steps {
		if step.Outcome == "failed" && step.ActionType != "require_human_approval" {
			return InboxItem{}, false
		}
	}

	since := issue.ModTime
	for i := len(store.Transitions) - 1; i >= 0; i-- {
		if strings.EqualFold(store.Transitions[i].To, from) {
			since = store.Transitions[i].TS
			break
		}
	}
	if blocked {
		since = pending.StartedAt
	}

	item := InboxItem{
		Kind:         inboxKindApproval,
		Key:          issue.Slug + "|" + required,
		Issue:        issue,
		Title:        approvalTitle(from, required),
		Target:       required,
		From:         from,
		WaitSince:    since,
		Wait:         nonNegative(now.Sub(since)),
		AgentBlocked: blocked,
	}
	if tracker.IsHandoffStatus(required) {
		if next := wf.NextRequiredStatus(required); next != "" {
			item.ThenStatus = wf.RequiredHumanApproval(required, next)
		}
	}
	return item, true
}

func approvalTitle(from, target string) string {
	switch {
	case strings.EqualFold(target, "backlog"):
		return "Approve the design for backlog"
	case strings.EqualFold(target, "done"):
		return "Close the issue (done)"
	default:
		return fmt.Sprintf("Approve %s → %s", from, target)
	}
}

func nonNegative(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

type InboxData struct {
	Prefix         string
	ProjectName    string
	ActiveBots     int
	SupportsGitHub bool

	Items     []InboxItem
	Selected  *InboxItem
	Kind      string // filter: "", approval, question
	Total     int
	Approvals int
	Questions int
	Blocked   int
	Oldest    string

	Workbench         tracker.WorkbenchConfig
	WorkbenchSystems  []string
	WorkbenchProblems []string
}

// SelectURL is the inbox link that selects item, keeping the kind filter.
func (d *InboxData) SelectURL(item InboxItem) string {
	q := url.Values{}
	q.Set("item", item.Key)
	if d.Kind != "" {
		q.Set("kind", d.Kind)
	}
	return d.Prefix + "/inbox?" + q.Encode()
}

// KindURL is the inbox link for a kind filter chip.
func (d *InboxData) KindURL(kind string) string {
	if kind == "" {
		return d.Prefix + "/inbox"
	}
	return d.Prefix + "/inbox?kind=" + url.QueryEscape(kind)
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessionMap, activeBots := sessionsByIssueSlug(issues)
	all := buildInbox(proj, issues, sessionMap, time.Now())

	data := InboxData{
		Prefix:         prefix,
		ProjectName:    proj.Name,
		ActiveBots:     activeBots,
		SupportsGitHub: proj.SupportsGitHub,
		Kind:           r.URL.Query().Get("kind"),
		Total:          len(all),
	}
	for _, item := range all {
		switch item.Kind {
		case inboxKindApproval:
			data.Approvals++
		case inboxKindQuestion:
			data.Questions++
		}
		if item.AgentBlocked {
			data.Blocked++
		}
	}
	if len(all) > 0 {
		data.Oldest = all[0].WaitLabel()
	}
	for _, item := range all {
		if data.Kind == "" || item.Kind == data.Kind {
			data.Items = append(data.Items, item)
		}
	}

	wantItem := r.URL.Query().Get("item")
	wantIssue := r.URL.Query().Get("issue")
	for i := range data.Items {
		item := &data.Items[i]
		if (wantItem != "" && item.Key == wantItem) || (wantItem == "" && wantIssue != "" && item.Issue.Slug == wantIssue) {
			data.Selected = item
			break
		}
	}
	if data.Selected == nil && len(data.Items) > 0 {
		data.Selected = &data.Items[0]
	}

	data.Workbench = proj.LoadWorkflow().Workbench
	data.WorkbenchProblems = data.Workbench.Validate()
	for name := range data.Workbench.Systems {
		data.WorkbenchSystems = append(data.WorkbenchSystems, name)
	}
	sort.Strings(data.WorkbenchSystems)

	if err := s.tmpl.ExecuteTemplate(w, "inbox.html", &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type inboxSummary struct {
	Count     int    `json:"count"`
	Approvals int    `json:"approvals"`
	Questions int    `json:"questions"`
	Blocked   int    `json:"blocked"`
	Oldest    string `json:"oldest,omitempty"`
	// Sig changes whenever an item appears, goes, or changes agent state, so
	// the open inbox can reload. Wait times are excluded (they tick).
	Sig string `json:"sig"`
}

// handleInboxJSON feeds the nav badge and the inbox page's refresh check.
func (s *Server) handleInboxJSON(w http.ResponseWriter, r *http.Request, proj *tracker.Project) {
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	sessionMap, _ := sessionsByIssueSlug(issues)
	items := buildInbox(proj, issues, sessionMap, time.Now())

	sum := inboxSummary{Count: len(items)}
	h := sha256.New()
	for _, item := range items {
		switch item.Kind {
		case inboxKindApproval:
			sum.Approvals++
		case inboxKindQuestion:
			sum.Questions++
		}
		if item.AgentBlocked {
			sum.Blocked++
		}
		fmt.Fprintf(h, "%s:%t:%t:%s\n", item.Key, item.AgentBlocked, item.AgentRunning, item.Issue.Status)
	}
	if len(items) > 0 {
		sum.Oldest = items[0].WaitLabel()
	}
	sum.Sig = fmt.Sprintf("%x", h.Sum(nil))[:16]
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sum)
}

type answerResponse struct {
	Status              string `json:"status"`
	QuestionID          string `json:"question_id"`
	DecisionID          string `json:"decision_id"`
	DecisionLine        string `json:"decision_line"`
	NotifiedSession     string `json:"notified_session,omitempty"`
	NotificationMessage string `json:"notification_message,omitempty"`
	NotificationError   string `json:"notification_error,omitempty"`
}

// handleAnswerQuestion records an inbox answer:
// POST /issue/<slug>/questions/<Q-n>/answer {"option":"B","note":"..."}.
func (s *Server) handleAnswerQuestion(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	rest := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	rest = strings.TrimSuffix(rest, "/answer")
	idx := strings.LastIndex(rest, "/questions/")
	if idx == -1 {
		http.NotFound(w, r)
		return
	}
	slug, questionID := rest[:idx], rest[idx+len("/questions/"):]

	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Option string `json:"option"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Option) == "" {
		writeJSONError(w, http.StatusBadRequest, "option required")
		return
	}

	res, err := tracker.AnswerQuestion(issue.FilePath, questionID, strings.TrimSpace(body.Option), body.Note, time.Now())
	switch {
	case errors.Is(err, tracker.ErrQuestionNotOpen):
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	case err != nil && res == nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		log.Printf("answer %s %s: %v", issue.Slug, questionID, err)
	}

	resp := answerResponse{Status: "ok", QuestionID: res.QuestionID, DecisionID: res.DecisionID, DecisionLine: res.DecisionLine}
	sessionMap, _ := sessionsByIssueSlug([]*tracker.Issue{issue})
	if sessions := sessionMap[issue.Slug]; len(sessions) == 0 {
		resp.NotificationError = "no active agent session matched this issue; the answer is saved in ## Decisions"
	} else {
		target := sessions[0].Name
		detail := res.DecisionLine
		if _, after, ok := strings.Cut(detail, "): "); ok {
			detail = after
		}
		msg := fmt.Sprintf("Answered %s in the inbox, recorded as %s under ## Decisions: %s", res.QuestionID, res.DecisionID, detail)
		if err := tmuxSendKeys(target, []string{msg}); err != nil {
			resp.NotificationError = err.Error()
		} else {
			resp.NotifiedSession = target
			resp.NotificationMessage = "answer sent to active agent session"
		}
	}
	if resp.NotificationError != "" {
		log.Printf("answer %s %s: agent not nudged: %s", issue.Slug, res.QuestionID, resp.NotificationError)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
