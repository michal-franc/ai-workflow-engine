package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type DetailData struct {
	Issue             *IssueView
	BackURL           string
	Prefix            string
	ProjectName       string
	Statuses          []string
	SlugMap           map[string]string
	NeedsApproval     string
	OptionalApprovals []OptionalApproval
	ActiveBots        int
	Timeline          []TimelineEvent
	RenderedBody      template.HTML
}

// OptionalApproval describes a transition to an Optional status that requires
// human approval. The detail view renders these behind a CTA button so they
// don't compete with the required-path approval as the default next step.
type OptionalApproval struct {
	Status      string
	Description string
	CTALabel    string
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
		if strings.HasPrefix(from, "board") || strings.HasPrefix(from, "docs") {
			backURL = prefix + "/" + from
		}
	}

	slugMap := map[string]string{}
	for _, issue := range issues {
		fname := strings.TrimSuffix(filepath.Base(issue.FilePath), ".md")
		slugMap[fname] = issue.Slug
		slugMap[issue.Slug] = issue.Slug
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
		basePrompt := LoadDispatchPrompt(proj.WorkDir, found.Assignee)
		summary := "dispatch — base prompt"
		if basePrompt == "" {
			briefedStatus := FirstTransitionFromStatus(timeline)
			issueCopy := *found
			if briefedStatus != "" {
				issueCopy.Status = briefedStatus
			}
			basePrompt = buildAgentPrompt(proj, &issueCopy, wf)
			summary = "dispatch — base prompt (reconstructed)"
		}
		dispatchEv := DispatchEvent(basePrompt, timeline[0].Timestamp)
		dispatchEv.Summary = summary
		timeline = append([]TimelineEvent{dispatchEv}, timeline...)
	}

	renderedBody := renderBodyWithDataTable(found, prefix, slugMap)

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
		RenderedBody:      template.HTML(renderedBody),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderBodyWithDataTable substitutes the first <!-- data --> marker in the
// rendered body HTML with the data table, or appends the table after the
// body if no marker is present. Issue refs are linked afterwards so the
// table itself is not rewritten.
func renderBodyWithDataTable(issue *tracker.Issue, prefix string, slugMap map[string]string) string {
	bodyHTML := issue.BodyHTML
	store, err := tracker.LoadData(issue.FilePath)
	if err != nil {
		store = tracker.DataStore{}
	}
	marker := tracker.ParseDataMarker(bodyHTML)
	statuses := tracker.ResolveDataStatuses(marker.Statuses, store.Entries)
	tableHTML := renderDataTable(prefix, issue.Slug, statuses, store.Entries)

	var body string
	if marker.Found {
		body = bodyHTML[:marker.Start] + tableHTML + bodyHTML[marker.Start+len(marker.Raw):]
	} else if len(store.Entries) == 0 && !marker.Found {
		body = bodyHTML
	} else {
		body = bodyHTML + tableHTML
	}
	return linkIssueRefs(body, prefix, slugMap)
}

// renderDataTable produces the inline data-table HTML that replaces a
// <!-- data --> marker in an issue body. statuses is the dropdown menu;
// it should already include any status currently in use on an entry.
func renderDataTable(prefix, slug string, statuses []string, entries []tracker.DataEntry) string {
	var b strings.Builder
	b.WriteString(`<div class="data-table-wrap" data-issue-slug="`)
	b.WriteString(template.HTMLEscapeString(slug))
	b.WriteString(`" data-prefix="`)
	b.WriteString(template.HTMLEscapeString(prefix))
	b.WriteString(`">`)
	b.WriteString(`<table class="data-table"><thead><tr><th>#</th><th>Description</th><th>Status</th><th>Comment</th><th></th></tr></thead><tbody>`)
	if len(entries) == 0 {
		b.WriteString(`<tr class="data-table-empty"><td colspan="5">No entries yet. Add some with <code>issue-cli data add &lt;slug&gt; --description "..."</code>.</td></tr>`)
	}
	for _, e := range entries {
		b.WriteString(`<tr class="data-row" data-id="`)
		b.WriteString(strconv.Itoa(e.ID))
		b.WriteString(`"><td class="data-id">`)
		b.WriteString(strconv.Itoa(e.ID))
		b.WriteString(`</td><td class="data-desc">`)
		b.WriteString(template.HTMLEscapeString(e.Description))
		b.WriteString(`</td><td class="data-status"><select onchange="dataSetStatus(this)">`)
		statusList := append([]string(nil), statuses...)
		seen := false
		for _, s := range statusList {
			if s == e.Status {
				seen = true
				break
			}
		}
		if !seen && e.Status != "" {
			statusList = append(statusList, e.Status)
		}
		for _, s := range statusList {
			selected := ""
			if s == e.Status {
				selected = " selected"
			}
			b.WriteString(`<option value="`)
			b.WriteString(template.HTMLEscapeString(s))
			b.WriteString(`"`)
			b.WriteString(selected)
			b.WriteString(`>`)
			b.WriteString(template.HTMLEscapeString(s))
			b.WriteString(`</option>`)
		}
		b.WriteString(`</select></td><td class="data-comment" contenteditable="true" onblur="dataSetComment(this)" data-original="`)
		b.WriteString(template.HTMLEscapeString(e.Comment))
		b.WriteString(`">`)
		b.WriteString(template.HTMLEscapeString(e.Comment))
		b.WriteString(`</td><td class="data-actions"><button class="data-remove-btn" onclick="dataRemove(this)" title="Remove entry">×</button></td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

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
