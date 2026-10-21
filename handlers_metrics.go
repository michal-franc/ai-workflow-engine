package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Metrics view (/metrics) shows, read-only, the guard metrics and tuning
// knobs a project exports as JSON (workbench.metrics, workbench.tuning;
// docs/workbench-files.md) and the open branches with the files several of
// them change. A git project without metrics or tuning gets the Branches tab
// only (labelled Branches in the sidebar).

const (
	metricsTabMetrics  = "metrics"
	metricsTabKnobs    = "knobs"
	metricsTabBranches = "branches"
)

// MetricsData is the page model, and (as JSON) /metrics.json.
type MetricsData struct {
	Prefix      string `json:"-"`
	ProjectName string `json:"project"`
	Hash        string `json:"-"`
	Sig         string `json:"sig"`

	Tab        string `json:"tab"`
	HasMetrics bool   `json:"has_metrics"`
	HasKnobs   bool   `json:"has_knobs"`
	GitProblem string `json:"git_problem,omitempty"`

	// Guard metrics tab.
	Report       tracker.MetricsReport `json:"metrics"`
	OK           int                   `json:"ok"`
	Near         int                   `json:"near"`
	Off          int                   `json:"off"`
	RunSince     int                   `json:"run_commits_since,omitempty"`
	RunSinceDirs string                `json:"-"`
	Groups       []MetricGroupView     `json:"-"`
	Systems      []string              `json:"-"`

	// Knobs tab.
	Knobs        []tracker.Knob  `json:"knobs,omitempty"`
	KnobProblems []string        `json:"knob_problems,omitempty"`
	KnobGroups   []KnobGroupView `json:"-"`
	KnobFiles    []string        `json:"-"`
	EditedKnobs  int             `json:"edited_knobs"`

	// Branches tab.
	Branches   []BranchView      `json:"branches,omitempty"`
	Idle       []BranchView      `json:"idle_branches,omitempty"`
	HotFiles   []tracker.HotFile `json:"hot_files,omitempty"`
	Hot3, Hot2 int               `json:"-"`
	OpenCount  int               `json:"open_branches"`
	Base       string            `json:"-"` // shared base of the idle branches, for the collapsed line

	Filter MetricsFilter `json:"-"`
	Panel  NavPanel      `json:"-"`
}

// MetricsFilter is the query string of the page.
type MetricsFilter struct {
	System, Group, Q, File string
	Only                   bool // near or off only
	Edited, Recent         bool // knobs
	Two                    bool // hot files on exactly two branches
	ShowShared             bool
}

// MetricGroupView is one table section of the Guard metrics tab.
type MetricGroupView struct {
	Name  string
	Count int
	Rows  []tracker.GuardMetric
}

// KnobGroupView is one export file's knobs, grouped by source file.
type KnobGroupView struct {
	File  string
	Count int
	Knobs []tracker.Knob
}

// BranchView is one row of the Branches tab.
type BranchView struct {
	tracker.OpenBranch
	Title  string           `json:"title,omitempty"`
	Status string           `json:"status,omitempty"`
	Files  int              `json:"files"`
	Deps   []tracker.IssueDep `json:"deps,omitempty"`
	Shares string           `json:"-"`
	BaseOf int              `json:"base_of,omitempty"` // open branches built on this one
	Hints  []BranchHint     `json:"hints,omitempty"`
}

// BranchHint is a note on a branch row, with an optional command to fix it.
type BranchHint struct {
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string, asJSON bool) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessionMap, activeBots := sessionsByIssueSlug(proj, issues)
	data := buildMetricsData(proj, issues, prefix, r.URL.Query())
	data.Hash = issuesHash(issues, sessionMap, activeBots)

	if asJSON {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("sig") != "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"sig": data.Sig})
			return
		}
		_ = json.NewEncoder(w).Encode(data)
		return
	}
	if err := s.tmpl.ExecuteTemplate(w, "metrics.html", &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buildMetricsData(proj *tracker.Project, issues []*tracker.Issue, prefix string, q url.Values) MetricsData {
	root := projectRoot(proj)
	wb := proj.LoadWorkflow().Workbench
	data := MetricsData{
		Prefix:      prefix,
		ProjectName: proj.Name,
		HasMetrics:  wb.Metrics != (tracker.WorkbenchMetrics{}),
		HasKnobs:    len(wb.Tuning) > 0,
		Filter: MetricsFilter{
			System: q.Get("system"), Group: q.Get("group"), Q: strings.TrimSpace(q.Get("q")), File: q.Get("file"),
			Only: q.Get("only") == "off", Edited: q.Get("edited") == "1", Recent: q.Get("recent") == "1",
			Two: q.Get("hot") == "2", ShowShared: q.Get("shared") == "1",
		},
	}
	data.Tab = q.Get("tab")
	switch {
	case data.Tab == metricsTabKnobs && data.HasKnobs, data.Tab == metricsTabBranches:
	case data.HasMetrics:
		data.Tab = metricsTabMetrics
	case data.HasKnobs:
		data.Tab = metricsTabKnobs
	default:
		data.Tab = metricsTabBranches
	}

	branches, gitErr := tracker.OpenBranchesErr(root, "", issues)
	if gitErr != nil {
		data.GitProblem = gitProblemText(gitErr)
	}
	data.OpenCount = len(branches)

	sig := fnv.New64a()
	for _, b := range branches {
		fmt.Fprint(sig, b.Name, b.Head)
	}

	if data.HasMetrics {
		data.Report = tracker.LoadMetrics(root, wb, branches)
		data.OK, data.Near, data.Off = data.Report.Counts()
		for _, m := range data.Report.Metrics {
			data.Systems = appendUnique(data.Systems, m.Systems...)
		}
		sort.Strings(data.Systems)
		data.Groups = metricGroups(data.Report, data.Filter)
		addFileSig(sig, root, wb.Metrics.ProbeResults)
		addFileSig(sig, root, wb.Metrics.Targets)
		for _, b := range branches {
			if b.Worktree != "" {
				addFileSig(sig, b.Worktree, wb.Metrics.ProbeResults)
			}
		}
	}
	if data.HasKnobs {
		data.Knobs, data.KnobProblems = tracker.LoadKnobs(root, wb, branches)
		for _, k := range data.Knobs {
			if len(k.Edits) > 0 {
				data.EditedKnobs++
			}
		}
		data.KnobGroups, data.KnobFiles = knobGroups(data.Knobs, data.Filter)
		for _, t := range wb.Tuning {
			addFileSig(sig, root, t)
		}
	}
	if data.Report.Run != nil && data.Report.Run.Commit != "" && gitErr == nil {
		dirs := knobDirs(data.Knobs)
		data.RunSince = tracker.CommitsSince(root, data.Report.Run.Commit, dirs)
		data.RunSinceDirs = strings.Join(dirs, ", ")
		if len(dirs) == 0 {
			data.RunSinceDirs = "the repo"
		}
	}

	if data.Tab == metricsTabBranches {
		data.HotFiles = tracker.HotFiles(root, branches, !data.Filter.ShowShared)
		var shown []tracker.HotFile
		for _, h := range data.HotFiles {
			n := h.Branches()
			if data.Filter.ShowShared {
				n = len(h.Changes)
			}
			if n >= 3 {
				data.Hot3++
			} else {
				data.Hot2++
			}
			if (n >= 3) != data.Filter.Two {
				shown = append(shown, h)
			}
		}
		data.HotFiles = shown
		index := tracker.BuildDecisionIndex(issues, tracker.ClosedIssueFunc(proj))
		closed := tracker.ClosedIssueFunc(proj)
		for _, b := range branches {
			view := branchView(root, b, branches, issues, index, closed)
			if b.Ahead == 0 {
				data.Idle = append(data.Idle, view)
				data.Base = b.Base
				continue
			}
			data.Branches = append(data.Branches, view)
		}
	} else if gitErr == nil {
		// The tab count needs the hot-file total; HotFiles is cached by sha.
		for _, h := range tracker.HotFiles(root, branches, true) {
			if h.Branches() >= 3 {
				data.Hot3++
			} else {
				data.Hot2++
			}
		}
	}

	data.Sig = fmt.Sprintf("%x", sig.Sum64())
	data.Panel = metricsPanel(&data, wb)
	return data
}

// metricGroups filters the report and splits it into its groups.
func metricGroups(rep tracker.MetricsReport, f MetricsFilter) []MetricGroupView {
	var out []MetricGroupView
	for _, g := range rep.Groups() {
		if f.Group != "" && !strings.EqualFold(f.Group, g) {
			continue
		}
		view := MetricGroupView{Name: g}
		for _, m := range rep.Metrics {
			if m.Group != g {
				continue
			}
			view.Count++
			if f.System != "" && !containsFold(m.Systems, f.System) {
				continue
			}
			if f.Only {
				if v := m.Verdict(); v != tracker.VerdictNear && v != tracker.VerdictOff {
					continue
				}
			}
			if f.Q != "" && !containsQuery(f.Q, m.ID, m.Name) {
				continue
			}
			view.Rows = append(view.Rows, m)
		}
		if view.Name == "" {
			view.Name = "Other"
		}
		out = append(out, view)
	}
	return out
}

func knobGroups(knobs []tracker.Knob, f MetricsFilter) ([]KnobGroupView, []string) {
	byFile := map[string]*KnobGroupView{}
	var files []string
	cutoff := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	for _, k := range knobs {
		file := k.File
		if file == "" {
			file = k.Export
		}
		g := byFile[file]
		if g == nil {
			g = &KnobGroupView{File: file}
			byFile[file] = g
			files = append(files, file)
		}
		g.Count++
		if f.File != "" && f.File != file {
			continue
		}
		if f.System != "" && !containsFold(k.Systems, f.System) {
			continue
		}
		if f.Edited && len(k.Edits) == 0 {
			continue
		}
		if f.Recent && (k.Last() == nil || k.Last().Date < cutoff) {
			continue
		}
		if f.Q != "" && !containsQuery(f.Q, k.Name, k.Comment) {
			continue
		}
		g.Knobs = append(g.Knobs, k)
	}
	var out []KnobGroupView
	for _, file := range files {
		if g := byFile[file]; len(g.Knobs) > 0 {
			out = append(out, *g)
		}
	}
	return out, files
}

func branchView(root string, b tracker.OpenBranch, all []tracker.OpenBranch, issues []*tracker.Issue, index *tracker.DecisionIndex, closed func(*tracker.Issue) bool) BranchView {
	v := BranchView{OpenBranch: b, Files: tracker.BranchFileCount(root, b)}
	for _, o := range all {
		if o.Base == b.Name {
			v.BaseOf++
		}
	}
	issue := tracker.FindIssue(issues, b.IssueSlug)
	switch {
	case issue != nil:
		v.Title, v.Status = issue.Title, issue.Status
		for _, d := range tracker.IssueDeps(issue, issues, index, closed) {
			if d.Kind == "depended on by" {
				continue
			}
			v.Deps = append(v.Deps, d)
			if strings.HasPrefix(d.State, "replaced") {
				v.Hints = appendHint(v.Hints, BranchHint{Text: "Cites a replaced decision: see the Decisions index"})
			}
		}
	case v.BaseOf == 0:
		v.Hints = appendHint(v.Hints, BranchHint{Text: "No issue: name the branch work/<slug> of an issue to link it"})
	}
	var shares []string
	for other, n := range b.SharesWith {
		shares = append(shares, fmt.Sprintf("shares %d commit%s with %s", n, pluralS(n), shortBranch(other)))
	}
	sort.Strings(shares)
	v.Shares = strings.Join(shares, " · ")
	if issue != nil {
		linked := map[string]bool{}
		for _, d := range tracker.DependsOn(issue) {
			linked["work/"+d] = true
		}
		for _, o := range all {
			n := b.SharesWith[o.Name]
			// Suggest the link on the branch forked later (more commits).
			if n == 0 || linked[o.Name] || o.IssueSlug == "" || o.Ahead > b.Ahead || (o.Ahead == b.Ahead && o.Name > b.Name) {
				continue
			}
			v.Hints = appendHint(v.Hints, BranchHint{
				Text:    fmt.Sprintf("Shares commits with %s, and no depends_on links them:", shortBranch(o.Name)),
				Command: fmt.Sprintf("issue-cli set-meta %s --key depends_on --value %s", issue.Slug, o.IssueSlug),
			})
		}
	}
	return v
}

func appendHint(list []BranchHint, h BranchHint) []BranchHint {
	for _, l := range list {
		if l == h {
			return list
		}
	}
	return append(list, h)
}

func metricsPanel(d *MetricsData, wb tracker.WorkbenchConfig) NavPanel {
	switch d.Tab {
	case metricsTabKnobs:
		p := NavPanel{Label: "Knob files", Note: "listed in " + strings.Join(wb.Tuning, ", ")}
		counts := map[string]int{}
		for _, k := range d.Knobs {
			file := k.File
			if file == "" {
				file = k.Export
			}
			counts[file]++
		}
		for _, f := range d.KnobFiles {
			p.Rows = append(p.Rows, NavCount{Name: f, Count: counts[f], Href: d.URL(map[string]string{"file": f})})
		}
		return p
	case metricsTabBranches:
		return NavPanel{Label: "Legend", Legend: true, Note: "Each branch is compared with its base: its epic's base, the open branch it was forked from, or main."}
	}
	var p NavPanel
	p.Label = "Sources"
	if wb.Metrics.ProbeResults != "" {
		p.Rows = append(p.Rows, NavCount{Name: "metrics.probe_results", Detail: wb.Metrics.ProbeResults})
	}
	if wb.Metrics.Targets != "" {
		p.Rows = append(p.Rows, NavCount{Name: "metrics.targets", Detail: wb.Metrics.Targets})
	}
	for _, t := range wb.Tuning {
		p.Rows = append(p.Rows, NavCount{Name: "tuning", Detail: t})
	}
	p.Note = "from workflow.yaml › workbench. Read only."
	return p
}

// URL is this page with the given query values changed ("" removes one).
// The tab is kept unless set.
func (d *MetricsData) URL(set map[string]string) string {
	q := url.Values{}
	f := d.Filter
	put := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	put("tab", d.Tab)
	put("system", f.System)
	put("group", f.Group)
	put("q", f.Q)
	put("file", f.File)
	if f.Only {
		q.Set("only", "off")
	}
	if f.Edited {
		q.Set("edited", "1")
	}
	if f.Recent {
		q.Set("recent", "1")
	}
	if f.Two {
		q.Set("hot", "2")
	}
	if f.ShowShared {
		q.Set("shared", "1")
	}
	for k, v := range set {
		if v == "" {
			q.Del(k)
		} else {
			q.Set(k, v)
		}
	}
	if s := q.Encode(); s != "" {
		return d.Prefix + "/metrics?" + s
	}
	return d.Prefix + "/metrics"
}

// TabURL opens a tab with no filters.
func (d *MetricsData) TabURL(tab string) string { return d.Prefix + "/metrics?tab=" + tab }

// MetricsNavNote is the sidebar note for the Metrics entry: "N off".
func metricsNavNote(proj *tracker.Project) string {
	wb := proj.LoadWorkflow().Workbench
	if wb.Metrics == (tracker.WorkbenchMetrics{}) {
		return ""
	}
	_, _, off := tracker.LoadMetrics(projectRoot(proj), wb, nil).Counts()
	if off == 0 {
		return ""
	}
	return fmt.Sprintf("%d off", off)
}

func knobDirs(knobs []tracker.Knob) []string {
	var out []string
	for _, k := range knobs {
		if k.File != "" {
			out = appendUnique(out, path.Dir(k.File)+"/")
		}
	}
	sort.Strings(out)
	return out
}

func addFileSig(h interface{ Write([]byte) (int, error) }, root, glob string) {
	if glob == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(glob)))
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil {
			fmt.Fprint(h, m, st.ModTime().UnixNano(), st.Size())
		}
	}
}

func gitProblemText(err error) string {
	switch {
	case errors.Is(err, tracker.ErrNoGit):
		return "git not available for this project: history, branch values and branches are not shown"
	case errors.Is(err, tracker.ErrGitTimeout):
		return "git timed out: history and branches skipped"
	}
	return err.Error()
}

func shortBranch(name string) string {
	name = strings.TrimPrefix(name, "work/")
	if len(name) > 40 {
		return name[:38] + "…"
	}
	return name
}

// pluralS is "" for one and "s" otherwise.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		found := false
		for _, l := range list {
			if l == it {
				found = true
				break
			}
		}
		if !found {
			list = append(list, it)
		}
	}
	return list
}

func containsFold(list []string, s string) bool {
	for _, l := range list {
		if strings.EqualFold(l, s) {
			return true
		}
	}
	return false
}

func containsQuery(q string, fields ...string) bool {
	q = strings.ToLower(q)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// metricsFuncs are the template helpers for the Metrics view, the metric
// panels and the detail page's dependency rows.
var metricsFuncs = template.FuncMap{
	"list": func(items ...string) []string { return items },
	// kv builds a map from key, value pairs: (kv "group" "Slice").
	"kv": func(pairs ...string) map[string]string {
		m := map[string]string{}
		for i := 0; i+1 < len(pairs); i += 2 {
			m[pairs[i]] = pairs[i+1]
		}
		return m
	},
	"title": func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
	"depsOfKind": func(deps []tracker.IssueDep, kind string) []tracker.IssueDep {
		var out []tracker.IssueDep
		for _, d := range deps {
			if d.Kind == kind {
				out = append(out, d)
			}
		}
		return out
	},
	"depChipClass": func(d tracker.IssueDep) string {
		switch {
		case d.State == "unknown issue", strings.HasPrefix(d.State, "replaced"), d.State == tracker.DecisionStale:
			return "chip-red"
		case d.Closed, d.State == tracker.DecisionActive:
			return "chip-green"
		case d.State == tracker.DecisionDeferred:
			return ""
		}
		return "chip-blue"
	},
	"verdictChip": func(v string) template.HTML {
		switch v {
		case tracker.VerdictOK:
			return `<span class="chip chip-green verdict">✓ ok</span>`
		case tracker.VerdictNear:
			return `<span class="chip chip-amber verdict">≈ near</span>`
		case tracker.VerdictOff:
			return `<span class="chip chip-red verdict">✗ off</span>`
		case tracker.VerdictUnmeasured:
			return `<span class="chip verdict">not measured</span>`
		}
		return `<span class="chip verdict">—</span>`
	},
	"verdictMark": func(v string) string {
		switch v {
		case tracker.VerdictOK:
			return "✓"
		case tracker.VerdictNear:
			return "≈"
		case tracker.VerdictOff:
			return "✗"
		}
		return ""
	},
	"bandText": func(b *tracker.MetricBand) string { return b.String() },
	"metricValue": func(m tracker.GuardMetric, v tracker.MetricValue) string {
		return m.FormatValue(&v)
	},
	"masterValue": func(m tracker.GuardMetric) string { return m.FormatValue(m.Master) },
	"nText": func(n *int) string {
		if n == nil {
			return "–"
		}
		return fmt.Sprint(*n)
	},
	"shortBranch": shortBranch,
	"shortSha": func(s string) string {
		if len(s) > 7 {
			return s[:7]
		}
		return s
	},
	"pluralS": pluralS,
	"withPanel": func(n Nav, p NavPanel) Nav {
		n.Panel = &p
		return n
	},
}

func init() {
	for k, v := range metricsFuncs {
		funcMap[k] = v
	}
}
