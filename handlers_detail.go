package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
	"regexp"
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
	Status     string `json:"status"`
	Session    string `json:"session,omitempty"`
	Message    string `json:"message,omitempty"`
	Reattached bool   `json:"reattached,omitempty"`
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
			basePrompt = buildAgentPrompt(proj, &issueCopy, wf, "", "")
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
	tiers := tracker.ResolveDataTiers(marker.Tiers, store.Entries)
	tableHTML := renderDataTable(prefix, issue.Slug, statuses, tiers, store.Entries)

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
// <!-- data --> marker in an issue body. statuses is the status dropdown
// menu; tiers is the optional second-axis dropdown.
//
// When tiers is non-empty the Status and Tier selects share a single
// "Status / Tier" column with the two selects stacked vertically, freeing
// horizontal space for description/comment. When tiers is empty the layout
// reverts to a plain Status column so workflows without a tiers= marker
// render exactly as before.
func renderDataTable(prefix, slug string, statuses, tiers []string, entries []tracker.DataEntry) string {
	hasTier := len(tiers) > 0

	var b strings.Builder
	b.WriteString(`<div class="data-table-wrap`)
	if hasTier {
		b.WriteString(` has-tier`)
	}
	b.WriteString(`" data-issue-slug="`)
	b.WriteString(template.HTMLEscapeString(slug))
	b.WriteString(`" data-prefix="`)
	b.WriteString(template.HTMLEscapeString(prefix))
	b.WriteString(`">`)
	b.WriteString(`<div class="data-table-toolbar">`)
	b.WriteString(`<select class="data-table-filter" onchange="dataTableFilter(this)" title="Filter by status"><option value="">All statuses</option></select>`)
	if hasTier {
		b.WriteString(`<select class="data-table-tier-filter" onchange="dataTableTierFilter(this)" title="Filter by tier"><option value="">All tiers</option></select>`)
	}
	b.WriteString(`<button type="button" class="data-table-tb-btn data-table-wide-btn" onclick="dataTableToggleWide()" title="Expand / shrink the table (spill under sidebar)">↔ Expand</button>`)
	b.WriteString(`</div>`)
	// Explicit colgroup pins the small fixed-width columns so browsers don't
	// redistribute leftover space into them under table-layout: fixed.
	// Description and Comment share the remaining width via percentages.
	b.WriteString(`<table class="data-table">`)
	b.WriteString(`<colgroup>`)
	b.WriteString(`<col class="col-id">`)
	b.WriteString(`<col class="col-desc">`)
	if hasTier {
		b.WriteString(`<col class="col-status-tier">`)
	} else {
		b.WriteString(`<col class="col-status">`)
	}
	b.WriteString(`<col class="col-comment">`)
	b.WriteString(`<col class="col-actions">`)
	b.WriteString(`</colgroup>`)
	b.WriteString(`<thead><tr><th>#</th><th>Description</th>`)
	if hasTier {
		b.WriteString(`<th>Status / Tier</th>`)
	} else {
		b.WriteString(`<th>Status</th>`)
	}
	b.WriteString(`<th>Comment</th><th></th></tr></thead><tbody>`)
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
		b.WriteString(`</td>`)
		if hasTier {
			b.WriteString(`<td class="data-status-tier"><div class="data-status"><select onchange="dataSetStatus(this)">`)
			writeOptions(&b, statuses, e.Status)
			b.WriteString(`</select></div><div class="data-tier"><select onchange="dataSetTier(this)"><option value=""`)
			if e.Tier == "" {
				b.WriteString(` selected`)
			}
			b.WriteString(`>—</option>`)
			writeOptions(&b, tiers, e.Tier)
			b.WriteString(`</select></div></td>`)
		} else {
			b.WriteString(`<td class="data-status"><select onchange="dataSetStatus(this)">`)
			writeOptions(&b, statuses, e.Status)
			b.WriteString(`</select></td>`)
		}
		b.WriteString(`<td class="data-comment" contenteditable="true" onblur="dataSetComment(this)" onclick="dataCommentClick(event)" title="`)
		b.WriteString(template.HTMLEscapeString(e.Comment))
		b.WriteString(`" data-original="`)
		b.WriteString(template.HTMLEscapeString(e.Comment))
		b.WriteString(`">`)
		b.WriteString(linkifyComment(e.Comment))
		b.WriteString(`</td><td class="data-actions"><button class="data-remove-btn" onclick="dataRemove(this)" title="Remove entry">×</button></td></tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String()
}

// commentURLRe matches a URL token at the start of a non-whitespace run:
// either an explicit http(s) scheme, or a bare host.domain/path form
// (at least one dot in the host, then a slash, e.g. github.com/owner/repo).
var commentURLRe = regexp.MustCompile(`^(?:https?://\S+|[A-Za-z0-9][A-Za-z0-9-]*(?:\.[A-Za-z0-9-]+)+/\S*)`)

// linkifyComment escapes a comment string for safe HTML and wraps any URL-ish
// tokens in <a> tags. Trailing punctuation (.,;:!?)]) is kept outside the link
// so "see github.com/x/y." doesn't trap the period inside the href.
func linkifyComment(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			b.WriteString(template.HTMLEscapeString(string(c)))
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '\n' && s[j] != '\r' {
			j++
		}
		token := s[i:j]
		if m := commentURLRe.FindString(token); m != "" {
			stripped := strings.TrimRight(m, ".,;:!?)]")
			rest := token[len(stripped):]
			href := stripped
			if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
				href = "https://" + href
			}
			b.WriteString(`<a href="`)
			b.WriteString(template.HTMLEscapeString(href))
			b.WriteString(`" target="_blank" rel="noopener noreferrer">`)
			b.WriteString(template.HTMLEscapeString(stripped))
			b.WriteString(`</a>`)
			b.WriteString(template.HTMLEscapeString(rest))
		} else {
			b.WriteString(template.HTMLEscapeString(token))
		}
		i = j
	}
	return b.String()
}

// writeOptions renders <option> tags for a select, ensuring `current` is
// present (appended if not declared) so an out-of-band value stays visible
// until the user picks something else.
func writeOptions(b *strings.Builder, choices []string, current string) {
	list := append([]string(nil), choices...)
	seen := false
	for _, s := range list {
		if s == current {
			seen = true
			break
		}
	}
	if !seen && current != "" {
		list = append(list, current)
	}
	for _, s := range list {
		selected := ""
		if s == current {
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
