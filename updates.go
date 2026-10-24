package main

import (
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

// The Updates tab shows the operator what `issue-cli workflow upgrade` would
// propose: catalog features the project's workflow.yaml doesn't have and
// nobody declined. Decline and Re-open write the features: record (the same
// writer as `workflow scan`); Install with an agent starts a one-shot agent
// that adapts the snippet with the operator. Nothing else in issue-cli or the
// lead mentions upgrades (D-6, D-8).

// planCache keeps each workflow file's upgrade plan until the file changes,
// so the sidebar count costs nothing on most requests.
var planCache sync.Map // path → planCacheEntry

type planCacheEntry struct {
	mod  time.Time
	size int64
	plan *workflowupgrade.Plan
}

// projectUpgradePlan is the project's upgrade plan, or nil when it runs on
// the built-in workflow, its file can't be read, or it is `template: custom`.
func projectUpgradePlan(proj *tracker.Project) *workflowupgrade.Plan {
	path := projectWorkflowPath(proj)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if v, ok := planCache.Load(path); ok {
		if e := v.(planCacheEntry); e.mod.Equal(info.ModTime()) && e.size == info.Size() {
			return e.plan
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	plan, err := workflowupgrade.BuildPlan(path, raw, "", false)
	if err != nil || plan.Status == workflowupgrade.StatusCustom || plan.Status == workflowupgrade.StatusUnknownTemplate {
		plan = nil
	}
	planCache.Store(path, planCacheEntry{mod: info.ModTime(), size: info.Size(), plan: plan})
	return plan
}

// projectWorkflowPath is the file Project.LoadWorkflow reads: the
// configured workflow:, else ./workflow.yaml. "" means the built-in workflow.
func projectWorkflowPath(proj *tracker.Project) string {
	for _, path := range []string{proj.WorkflowFile, "workflow.yaml"} {
		if info, err := os.Stat(path); path != "" && err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// updatesNavNote is the sidebar count: proposals still to decide.
func updatesNavNote(proj *tracker.Project) string {
	if plan := projectUpgradePlan(proj); plan != nil && len(plan.Proposals) > 0 {
		return fmt.Sprint(len(plan.Proposals))
	}
	return ""
}

// installSession is the tmux session an install agent runs in.
func installSession(proj *tracker.Project, id string) string {
	return tmuxSessionName(proj.Slug + "-install-" + id)
}

// UpdateCard is one proposal on the tab.
type UpdateCard struct {
	workflowupgrade.Proposal
	Installing bool
	Session    string // the install agent, when Installing
	AttachCmd  string
	Open       bool // the install panel starts open (?install=<id>)
}

// SummaryHTML is the first sentence of the entry's what, for the folded card.
func (c UpdateCard) SummaryHTML() template.HTML {
	text := unwrap(c.What)
	// "e.g. " and "i.e. " don't end a sentence.
	masked := strings.NewReplacer("e.g. ", "e.g._", "i.e. ", "i.e._").Replace(text)
	if i := strings.Index(masked, ". "); i >= 0 {
		text = text[:i+1]
	}
	return catalogText(text)
}

func (c UpdateCard) WhatHTML() template.HTML { return catalogText(unwrap(c.What)) }
func (c UpdateCard) WhyHTML() template.HTML  { return catalogText(unwrap(c.Why)) }

var backtickRe = regexp.MustCompile("`([^`]+)`")

// catalogText renders catalog prose: escaped, with `code` spans kept.
func catalogText(s string) template.HTML {
	return template.HTML(backtickRe.ReplaceAllString(html.EscapeString(s), "<code>$1</code>"))
}

// unwrap joins the catalog's hard-wrapped lines into one paragraph.
func unwrap(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

type UpdateChip struct {
	Name   string
	Count  int
	Href   string
	Active bool
}

type UpdateRecordRow struct {
	ID, Title string
	Just      bool // declined by the last click (?declined=<id>)
}

type UpdatesData struct {
	Prefix      string
	ProjectName string
	File        string

	Template, Current, Latest string
	State, StateClass         string // behind / N undecided / not recorded / up to date / no stamp
	HasRecord                 bool
	Installed                 []string
	Declined                  []UpdateRecordRow

	Cards    []UpdateCard
	Total    int // proposals before the category filter
	Chips    []UpdateChip
	Category string

	Flash    string
	FlashErr bool
	Undo     string // id the flash's Undo re-opens
}

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	plan := projectUpgradePlan(proj)
	if plan == nil {
		http.Error(w, "this project has no workflow.yaml based on a bundled template", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	data := buildUpdatesData(proj, prefix, plan, q.Get("category"), q.Get("install"))
	switch {
	case q.Get("declined") != "":
		id := q.Get("declined")
		data.Flash, data.Undo = id+" declined. It won't be proposed again; the record is in workflow.yaml (features.declined).", id
		for i := range data.Declined {
			data.Declined[i].Just = data.Declined[i].ID == id
		}
	case q.Get("reopened") != "":
		data.Flash = q.Get("reopened") + " re-opened: it is proposed again."
	case q.Get("installing") != "":
		data.Flash = "Install agent for " + q.Get("installing") + " started. It asks you its question in tmux; the card leaves the list once workflow scan records it."
	case q.Get("error") != "":
		data.Flash, data.FlashErr = q.Get("error"), true
	}
	if err := s.tmpl.ExecuteTemplate(w, "updates.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buildUpdatesData(proj *tracker.Project, prefix string, plan *workflowupgrade.Plan, category, open string) *UpdatesData {
	d := &UpdatesData{
		Prefix:      prefix,
		ProjectName: proj.Name,
		File:        plan.WorkflowFile,
		Template:    plan.Template,
		Current:     plan.Current,
		Latest:      plan.Latest,
		HasRecord:   plan.Record != nil,
		Total:       len(plan.Proposals),
		Category:    category,
	}
	switch {
	case plan.Status == workflowupgrade.StatusUnstamped:
		d.State, d.StateClass = "no stamp", ""
	case len(plan.Proposals) == 0 && plan.Status == workflowupgrade.StatusStaleRecord:
		d.State, d.StateClass = "not recorded", "chip-amber"
	case len(plan.Proposals) == 0:
		d.State, d.StateClass = "up to date", "chip-green"
	case plan.Status == workflowupgrade.StatusBehind:
		d.State, d.StateClass = "behind", "chip-amber"
	default:
		d.State, d.StateClass = fmt.Sprintf("%d undecided", len(plan.Proposals)), "chip-amber"
	}

	entries, _ := workflowupgrade.Catalog()
	titles := map[string]string{}
	order := map[string]int{}
	for i, e := range entries {
		titles[e.ID] = e.Title
		order[e.ID] = i
	}
	if plan.Record != nil {
		for _, ref := range plan.Record.Installed {
			id, _, _ := strings.Cut(ref, "@")
			d.Installed = append(d.Installed, id)
		}
		for _, id := range plan.Record.Declined {
			d.Declined = append(d.Declined, UpdateRecordRow{ID: id, Title: titles[id]})
		}
	} else {
		d.Installed = append(d.Installed, plan.Present...)
	}

	counts := map[string]int{}
	for _, p := range plan.Proposals {
		counts[p.Category]++
	}
	d.Chips = append(d.Chips, UpdateChip{Name: "all", Count: len(plan.Proposals), Href: prefix + "/updates", Active: category == ""})
	for _, c := range workflowupgrade.Categories {
		if counts[c] > 0 {
			d.Chips = append(d.Chips, UpdateChip{Name: c, Count: counts[c], Href: prefix + "/updates?category=" + url.QueryEscape(c), Active: category == c})
		}
	}

	catRank := map[string]int{}
	for i, c := range workflowupgrade.Categories {
		catRank[c] = i
	}
	for _, p := range plan.Proposals {
		if category != "" && p.Category != category {
			continue
		}
		card := UpdateCard{Proposal: p, Open: open == p.ID}
		if session := installSession(proj, p.ID); agentSessionLive(proj, session) {
			card.Installing, card.Session, card.AttachCmd = true, agentDisplayName(proj, session), agentAttachCmd(proj, session)
		}
		d.Cards = append(d.Cards, card)
	}
	// Newest first, then by category, then in catalog order.
	sort.SliceStable(d.Cards, func(i, j int) bool {
		a, b := d.Cards[i], d.Cards[j]
		if c := workflowupgrade.CompareVersions(a.Version, b.Version); c != 0 {
			return c > 0
		}
		if catRank[a.Category] != catRank[b.Category] {
			return catRank[a.Category] < catRank[b.Category]
		}
		return order[a.ID] < order[b.ID]
	})
	return d
}

// updatesRedirect sends the browser back to the tab with a flash key.
func updatesRedirect(w http.ResponseWriter, r *http.Request, prefix, key, value string) {
	http.Redirect(w, r, prefix+"/updates?"+key+"="+url.QueryEscape(value), http.StatusSeeOther)
}

// handleUpdatesRecord declines (decline) or re-opens (reopen) a feature in
// the project's features: record.
func (s *Server) handleUpdatesRecord(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix, verb string) {
	id := strings.TrimSpace(r.FormValue("id"))
	opts := workflowupgrade.ScanOptions{}
	flash := "reopened"
	if verb == "decline" {
		opts.Declined, flash = []string{id}, "declined"
	} else {
		opts.Forget = []string{id}
	}
	if err := writeFeatureRecord(proj, opts); err != nil {
		updatesRedirect(w, r, prefix, "error", err.Error())
		return
	}
	updatesRedirect(w, r, prefix, flash, id)
}

// writeFeatureRecord applies a scan with opts to the project's workflow.yaml,
// changing only its features: block.
func writeFeatureRecord(proj *tracker.Project, opts workflowupgrade.ScanOptions) error {
	path := projectWorkflowPath(proj)
	if path == "" {
		return fmt.Errorf("%s runs on the built-in workflow: no workflow.yaml to record in", proj.Name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	res, err := workflowupgrade.Scan(path, raw, opts)
	if err != nil {
		return err
	}
	out, err := workflowupgrade.WriteRecord(raw, res.Record)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, info.Mode().Perm())
}

// handleUpdatesInstall starts a one-shot agent in the project checkout that
// installs one proposal with the operator.
func (s *Server) handleUpdatesInstall(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	id := strings.TrimSpace(r.FormValue("id"))
	plan := projectUpgradePlan(proj)
	var proposal *workflowupgrade.Proposal
	if plan != nil {
		for i := range plan.Proposals {
			if plan.Proposals[i].ID == id {
				proposal = &plan.Proposals[i]
			}
		}
	}
	if proposal == nil {
		updatesRedirect(w, r, prefix, "error", id+" is not proposed for this project")
		return
	}
	prompt := installPrompt(proj, *proposal, strings.TrimSpace(r.FormValue("words")))
	resp := dispatchAgentSession(proj, installSession(proj, id), prompt, "", "claude", nil)
	if resp.Status == "error" {
		updatesRedirect(w, r, prefix, "error", "could not start the install agent for "+id)
		return
	}
	updatesRedirect(w, r, prefix, "installing", id)
}

// installPrompt is the install agent's brief. The operator's answer to the
// entry's question, when given, goes in so the agent asks only what is left.
func installPrompt(proj *tracker.Project, p workflowupgrade.Proposal, words string) string {
	cli := "issue-cli --project " + proj.Slug
	ask := fmt.Sprintf("Ask the operator the entry's question: %q", p.Question)
	if words != "" {
		ask = fmt.Sprintf("The operator answered its question (%q): %q. Ask only what is still unclear.", p.Question, words)
	}
	setup := ""
	if len(p.Setup) > 0 {
		var b strings.Builder
		b.WriteString("\nThen walk the entry's setup steps in order. Run an agent step yourself; for an owner step, tell the operator exactly what to do and wait until they say it is done, never do it for them:\n")
		for i, st := range p.Setup {
			fmt.Fprintf(&b, "  %d. [%s] %s\n", i+1, st.Who, strings.TrimSpace(st.Do))
		}
		setup = b.String()
	}
	return fmt.Sprintf(`Install workflow feature %s@%s in %s's workflow.yaml (%s). The operator asked for it from the viewer's Updates tab.

1. Read the entry: %s workflow upgrade --all (proposal %s): what, why, where, the snippet and its setup steps.
2. %s
3. Adapt the snippet to the project (its own status names and sections) and add it to workflow.yaml. Keep every existing customisation. Run %s process workflow and fix anything under == Warnings (workflow.yaml) ==.
4. Record it: %s workflow scan --installed %s. Leave the template: stamp alone while other proposals are undecided.
5. Show the operator the diff. Commit only when they say so.
%s`, p.ID, p.Version, proj.Name, projectWorkflowPath(proj), cli, p.ID, ask, cli, cli, p.ID, setup)
}

// UpdatesOverviewRow is one project on /updates.
type UpdatesOverviewRow struct {
	Name, Href      string
	Template, State string
	StateClass      string
	ToDecide        int
	Record          string
	Newest          string
	Builtin         bool // runs on the built-in workflow (no Updates tab)
	stateRank       int
}

type UpdatesOverviewData struct {
	Rows                                              []UpdatesOverviewRow
	Projects, Behind, Stamped, NeverScanned, ToDecide int
	CatalogSize                                       int
}

func (s *Server) handleUpdatesOverview(w http.ResponseWriter, r *http.Request) {
	data := UpdatesOverviewData{Projects: len(s.projects)}
	entries, _ := workflowupgrade.Catalog()
	data.CatalogSize = len(entries)
	for i := range s.projects {
		proj := &s.projects[i]
		row := UpdatesOverviewRow{Name: proj.Name, Href: "/p/" + proj.Slug + "/updates"}
		plan := projectUpgradePlan(proj)
		if plan == nil {
			row.Builtin, row.State, row.Template, row.stateRank = true, "no template", "built-in or custom", 3
			data.Rows = append(data.Rows, row)
			continue
		}
		row.ToDecide = len(plan.Proposals)
		data.ToDecide += row.ToDecide
		row.Template = "no stamp"
		if plan.Current != "" {
			row.Template = plan.Template + "@" + plan.Current
			data.Stamped++
		}
		switch {
		case plan.Status == workflowupgrade.StatusBehind:
			row.State, row.StateClass, row.stateRank = "behind", "chip-amber", 0
			data.Behind++
		case plan.Record == nil:
			row.State, row.stateRank = "never scanned", 1
		case row.ToDecide > 0:
			row.State, row.StateClass, row.stateRank = "undecided", "chip-amber", 0
		case plan.Status == workflowupgrade.StatusStaleRecord:
			row.State, row.StateClass, row.stateRank = "not recorded", "chip-amber", 0
		default:
			row.State, row.StateClass, row.stateRank = "up to date", "chip-green", 2
		}
		if plan.Record == nil {
			data.NeverScanned++
			row.Record = "—"
		} else {
			row.Record = fmt.Sprintf("%d installed · %d declined", len(plan.Record.Installed), len(plan.Record.Declined))
		}
		for _, p := range plan.Proposals {
			if row.Newest == "" || workflowupgrade.CompareVersions(p.Version, strings.SplitN(row.Newest, "@", 2)[1]) > 0 {
				row.Newest = p.ID + "@" + p.Version
			}
		}
		data.Rows = append(data.Rows, row)
	}
	sort.SliceStable(data.Rows, func(i, j int) bool {
		a, b := data.Rows[i], data.Rows[j]
		if a.stateRank != b.stateRank {
			return a.stateRank < b.stateRank
		}
		return a.ToDecide > b.ToDecide
	})
	if err := s.tmpl.ExecuteTemplate(w, "updates_overview.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
