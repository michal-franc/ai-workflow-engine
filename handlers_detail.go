package main

import (
	"encoding/json"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
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
	NeedsApprovals    []string
	OptionalApprovals []OptionalApproval
	// ApproveThen maps a required approval to the follow-up approval that
	// "Approve <status> and <then>" queues in one click (queued_approval).
	ApproveThen   map[string]string
	RenderedBody  template.HTML
	CustomActions []tracker.CustomAction
	WorkTypes     []WorkTypeOption
	// TypeWarning flags an undefined type: value or an off-path status.
	TypeWarning string
	// CanvasURL opens the issues canvas with this epic highlighted; "" when
	// the issue is not an epic.
	CanvasURL string
	Lead      DetailLead
	// Deps are the depends-on / depended-on-by / decided-by links; Shared
	// is the issue branch's overlap with other open branches (nil: none).
	Deps   []tracker.IssueDep
	Shared *tracker.SharedFiles

	// Tab is the page tab: issue, timeline or evidence (?tab=).
	Tab string
	// TimelineTotal is the Timeline tab's label ("5h 0m", "1h 50m so far").
	TimelineTotal string
	// Timeline is built only on the Timeline tab.
	Timeline        *Timeline
	TimelineFilters []timelineFilterLink
	// IssueTab is built only on the Issue tab.
	IssueTab *IssueTab
	// EvidenceLabel is the Evidence tab's count, on every tab; Evidence is
	// built only on the Evidence tab.
	EvidenceLabel EvidenceLabel
	Evidence      *EvidenceTab
	// Epic is set when the issue is an epic: its children, for the Epic tab.
	Epic *EpicTab
}

// EpicTab is an epic's children, by version, for the first tab (named Epic).
type EpicTab struct {
	Done, Total int
	Versions    []EpicVersion
}

// AllDone: every child closed.
func (e *EpicTab) AllDone() bool { return e.Total > 0 && e.Done == e.Total }

// EpicVersion is one version's children of an epic ("none" for no version).
type EpicVersion struct {
	Version     string
	Done, Total int
	Children    []*tracker.Issue
}

// Pct is the version's closed share, for its bar.
func (v EpicVersion) Pct() int {
	if v.Total == 0 {
		return 0
	}
	return v.Done * 100 / v.Total
}

// buildEpicTab groups the epic's children by version, oldest first; within a
// version the open ones come first, furthest along.
func buildEpicTab(wf *tracker.WorkflowConfig, epic *tracker.Issue, issues []*tracker.Issue) *EpicTab {
	if !tracker.IsEpic(epic, issues) {
		return nil
	}
	cs := newCanvasStatuses(wf)
	tab := &EpicTab{}
	byVersion := map[string]*EpicVersion{}
	for _, c := range tracker.EpicChildren(issues, epic.Slug) {
		v := c.Version
		if v == "" {
			v = "none"
		}
		ev := byVersion[v]
		if ev == nil {
			ev = &EpicVersion{Version: v}
			byVersion[v] = ev
		}
		ev.Children = append(ev.Children, c)
		ev.Total++
		tab.Total++
		if cs.closed(c.Status) {
			ev.Done++
			tab.Done++
		}
	}
	for _, ev := range byVersion {
		sort.SliceStable(ev.Children, func(i, j int) bool {
			ci, cj := cs.closed(ev.Children[i].Status), cs.closed(ev.Children[j].Status)
			if ci != cj {
				return !ci
			}
			if pi, pj := cs.pos(ev.Children[i].Status), cs.pos(ev.Children[j].Status); pi != pj {
				return pi > pj
			}
			return ev.Children[i].Slug < ev.Children[j].Slug
		})
		tab.Versions = append(tab.Versions, *ev)
	}
	sort.Slice(tab.Versions, func(i, j int) bool { return compareVersions(tab.Versions[i].Version, tab.Versions[j].Version) < 0 })
	return tab
}

type timelineFilterLink struct {
	Key, Label, URL string
	On              bool
	Count           int
}

// detailTab normalizes ?tab=: unknown or missing is the Issue tab.
func detailTab(q string) string {
	switch q {
	case "timeline", "evidence":
		return q
	}
	return "issue"
}

// DetailLead is the detail page's Lead section: the project lead the issue
// reports to.
type DetailLead struct {
	Route tracker.LeadRoute
}

func detailLead(proj *tracker.Project, issue *tracker.Issue, issues []*tracker.Issue) DetailLead {
	return DetailLead{Route: tracker.LeadFor(proj, issue, issues, leadLive(proj))}
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

// issueApprovals lists the human approvals the issue's current status offers:
// the targets of its outgoing approval-gated transitions, split into the
// required path and optional side statuses (shown behind a CTA). The detail
// page and approvals.json both read it, so they always offer the same gates.
func issueApprovals(wf *tracker.WorkflowConfig, issue *tracker.Issue) (needs []string, optional []OptionalApproval) {
	for _, t := range wf.Transitions {
		if t.From != issue.Status {
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
			optional = append(optional, OptionalApproval{
				Status:      t.To,
				Description: target.Description,
				CTALabel:    t.CTALabel,
			})
			continue
		}
		needs = append(needs, t.To)
	}
	return needs, optional
}

// ApprovalsJSON is GET /p/<project>/issue/<slug>/approvals.json: the approvals
// the detail page's Human Approval box offers, for clients outside the
// browser (the issue-approve Claude Code mod's buttons). Approving goes
// through the same POST .../approve the page's checkboxes call.
type ApprovalsJSON struct {
	Slug          string         `json:"slug"`
	Title         string         `json:"title"`
	Status        string         `json:"status"`
	HumanApproval string         `json:"human_approval"`
	Approvals     []ApprovalItem `json:"approvals"`
}

type ApprovalItem struct {
	Status   string `json:"status"`
	Approved bool   `json:"approved"`
	Optional bool   `json:"optional,omitempty"`
}

func (s *Server) handleApprovalsJSON(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/issue/"), "/approvals.json")
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	needs, optional := issueApprovals(proj.LoadWorkflowForIssue(issue), issue)
	out := ApprovalsJSON{Slug: issue.Slug, Title: issue.Title, Status: issue.Status, HumanApproval: issue.HumanApproval, Approvals: []ApprovalItem{}}
	for _, st := range needs {
		out.Approvals = append(out.Approvals, ApprovalItem{Status: st, Approved: strings.EqualFold(issue.HumanApproval, st)})
	}
	for _, o := range optional {
		out.Approvals = append(out.Approvals, ApprovalItem{Status: o.Status, Approved: strings.EqualFold(issue.HumanApproval, o.Status), Optional: true})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

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

	sessionMap, _ := sessionsByIssueSlug(proj, issues)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	wf := proj.LoadWorkflowForIssue(found)
	statuses := orderedStatusesForIssue(wf, found.Status)
	detailView := issueView(found, sessionMap)
	if detailView != nil {
		detailView.WorkType = wf.ActiveType
	}
	if wf.Scoring.Enabled && detailView != nil {
		detailView.Score = tracker.ComputeScore(found, &wf.Scoring, time.Now())
	}

	needsApprovals, optionalApprovals := issueApprovals(wf, found)
	approveThen := approveThenFor(wf, needsApprovals)

	tab := detailTab(r.URL.Query().Get("tab"))
	var tl *Timeline
	var filters []timelineFilterLink
	if tab == "timeline" {
		src := loadTimelineSources(proj, found, wf)
		if src.Prompt == "" && len(src.Calls) > 0 {
			// Dispatched before the prompt was saved: rebuild it as the
			// agent saw it, at the status it started from.
			issueCopy := *found
			if len(src.Transitions) > 0 {
				issueCopy.Status = src.Transitions[0].From
			}
			src.Prompt = buildAgentPrompt(proj, &issueCopy, wf, "", "") + "\n\n(reconstructed: no dispatch-prompt.txt was saved)"
		}
		built := buildTimeline(src, timelineInput{
			Status:    found.Status,
			Now:       time.Now(),
			Show:      r.URL.Query().Get("show"),
			OpenGated: len(needsApprovals) > 0,
			WF:        wf,
		})
		tl = &built
		base := prefix + "/issue/" + found.Slug + "?tab=timeline"
		for _, f := range timelineFilters {
			u := base
			if f.Key != showAll {
				u += "&show=" + f.Key
			}
			link := timelineFilterLink{Key: f.Key, Label: f.Label, URL: u, On: f.Key == tl.Show}
			if f.Key == showCLI {
				link.Count = tl.Calls
			}
			filters = append(filters, link)
		}
	}

	var issueTab *IssueTab
	if tab == "issue" {
		issueTab = loadIssueTab(proj, prefix, found, issues, wf, needsApprovals)
	}

	comments, _ := tracker.LoadComments(found.FilePath)
	report := evidenceReport(proj, found, comments)
	var evidence *EvidenceTab
	if tab == "evidence" {
		evidence = loadEvidenceTab(proj, prefix, found, wf, report)
	}

	renderedBody := renderBodyWithDataTable(found, prefix, projectRoot(proj), slugMap)

	closed := tracker.ClosedIssueFunc(proj)
	var canvasURL string
	if isCanvasEpic(found, issues) {
		canvasURL = prefix + "/canvas?hl=" + url.QueryEscape(found.Slug)
	}

	if err := s.tmpl.ExecuteTemplate(w, "detail.html", DetailData{
		Issue:             detailView,
		BackURL:           backURL,
		Prefix:            prefix,
		ProjectName:       proj.Name,
		Statuses:          statuses,
		SlugMap:           slugMap,
		NeedsApprovals:    needsApprovals,
		OptionalApprovals: optionalApprovals,
		ApproveThen:       approveThen,
		RenderedBody:      template.HTML(renderedBody),
		CustomActions:     wf.IssueActionList(),
		WorkTypes:         workTypeOptions(wf),
		TypeWarning:       strings.TrimSpace(wf.UnknownTypeWarning(found.Slug, found.Type) + " " + wf.OffPathWarning()),
		CanvasURL:         canvasURL,
		Lead:              detailLead(proj, found, issues),
		Deps:              tracker.IssueDeps(found, issues, tracker.BuildDecisionIndex(issues, closed), closed),
		Shared:            tracker.IssueSharedFiles(projectRoot(proj), found, issues),
		Tab:               tab,
		TimelineTotal:     timelineTotalLabel(proj, found),
		Timeline:          tl,
		TimelineFilters:   filters,
		IssueTab:          issueTab,
		EvidenceLabel:     evidenceLabel(report, wf.ProofRequired(found)),
		Evidence:          evidence,
		Epic:              buildEpicTab(wf, found, issues),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// loadIssueTab gathers what the Issue tab reads, the same sources as the
// Timeline tab plus the owner's attention and the agent's last call, and
// builds it.
func loadIssueTab(proj *tracker.Project, prefix string, issue *tracker.Issue, issues []*tracker.Issue, wf *tracker.WorkflowConfig, targets []string) *IssueTab {
	now := time.Now()
	src := loadTimelineSources(proj, issue, wf)
	tl := buildTimeline(src, timelineInput{Status: issue.Status, Now: now, WF: wf, OpenGated: len(targets) > 0})
	att := ownerAttention(proj, issue)
	ca := newCardAgents(proj, prefix, now)
	wt, _, _ := resolveWorktree(ca.workDir, issue.Slug, wf)
	call, known := lastCliCall(proj.Slug, agentLogPaths(ca.workDir, wt, tracker.AgentSessionName(issue.Slug), issue.Assignee)...)
	root, mdDir := projectRoot(proj), filepath.Dir(issue.FilePath)
	t := buildIssueTab(issueTabInput{
		Project: proj.Slug,
		Issue:   issue,
		WF:      wf,
		Src:     src,
		TL:      tl,
		Att:     att,
		Targets: targets,
		Parent:  tracker.FindIssue(issues, tracker.ParentSlug(issue)),
		Epic:    epicProgress(prefix, issue, issues),
		Footer:  ca.footer(wf, issue, att),
		Call:    call,
		Known:   known,
		Now:     now,
		Prefix:  prefix,
		ImgSrc: func(path string) string {
			if m := imgSrcRe.FindStringSubmatch(rewriteRelativeImages(`<img src="`+template.HTMLEscapeString(path)+`">`, prefix, mdDir, root)); m != nil {
				return html.UnescapeString(m[2])
			}
			return path
		},
	})
	return &t
}

// approveThenFor finds, for each required approval that lands on a handoff
// status (backlog, human-testing), the approval its own next step needs, so
// the detail page can approve both at once ("approve backlog and in progress").
func approveThenFor(wf *tracker.WorkflowConfig, approvals []string) map[string]string {
	out := map[string]string{}
	for _, status := range approvals {
		if !tracker.IsHandoffStatus(status) {
			continue
		}
		next := wf.NextRequiredStatus(status)
		if next == "" {
			continue
		}
		if then := wf.RequiredHumanApproval(status, next); then != "" {
			out[status] = then
		}
	}
	return out
}

// renderBodyWithDataTable substitutes the first <!-- data --> marker in the
// rendered body HTML with the data table, or appends the table after the
// body if no marker is present. Relative image paths are resolved against
// the issue file's directory, and issue refs are linked afterwards so the
// table itself is not rewritten.
func renderBodyWithDataTable(issue *tracker.Issue, prefix, root string, slugMap map[string]string) string {
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
	body = rewriteRelativeImages(body, prefix, filepath.Dir(issue.FilePath), root)
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
