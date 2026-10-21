package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The Balance tab groups typed `balance:` comments by the metric they move,
// lists free-text balance comments as unsorted notes, and shows the joint
// passes (issues marked `balance_pass: <n>`). It is read-only; notes are
// written with `issue-cli balance`.

const balanceLooseCut = 280

type BalanceGroupView struct {
	Metric string
	Notes  []BalanceNoteView
	Total  int
	Open   int
}

type BalanceNoteView struct {
	*tracker.BalanceNote
	PassLabel string
	DateLabel string
}

type LooseNoteView struct {
	tracker.LooseBalanceNote
	DateLabel string
	Short     string // cut text, "" when the whole text fits
}

type BalancePassView struct {
	tracker.BalancePass
	DateLabel string
}

type BalanceStateChip struct {
	State string
	Label string
	Count int
}

type BalanceData struct {
	Prefix      string
	ProjectName string
	Hash        string // /hash at render time, the auto-reload baseline

	Enabled       bool
	CommentPrefix string
	State         string // filter
	Chips         []BalanceStateChip
	Groups        []BalanceGroupView
	Unsorted      []LooseNoteView
	Passes        []BalancePassView
	Planned       *BalancePassView
	PlannedAgent  string // tmux session already working the planned pass

	Total, Open, Metrics int
}

// StateURL links the Balance tab with a state filter.
func (d *BalanceData) StateURL(state string) string {
	if state == "" {
		return d.Prefix + "/balance"
	}
	return d.Prefix + "/balance?state=" + strings.ReplaceAll(state, " ", "+")
}

// StateClass is the CSS modifier for a state chip.
func (d *BalanceData) StateClass(state string) string {
	return "s-" + strings.ReplaceAll(state, " ", "-")
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessionMap, activeBots := sessionsByIssueSlug(proj, issues)
	wb := proj.LoadWorkflow().Workbench
	data := BalanceData{
		Prefix:        prefix,
		ProjectName:   proj.Name,
		Hash:          issuesHash(issues, sessionMap, activeBots),
		Enabled:       wb.HasBalance(),
		CommentPrefix: wb.Balance.CommentPrefix,
		State:         tracker.NormalizeBalanceState(r.URL.Query().Get("state")),
	}
	if data.CommentPrefix == "" {
		data.CommentPrefix = tracker.DefaultBalancePrefix
	}
	if !data.Enabled {
		s.renderBalance(w, &data)
		return
	}

	board := tracker.BuildBalanceBoard(issues, loadIssueCommentsQuiet, data.CommentPrefix, tracker.ClosedIssueFunc(proj))
	now := time.Now()
	data.Total = len(board.Notes)
	data.Open = board.Counts[tracker.BalanceOpen] + board.Counts[tracker.BalanceNeedsPieces]
	data.Metrics = len(board.Groups)
	for _, st := range tracker.BalanceStates {
		data.Chips = append(data.Chips, BalanceStateChip{State: st, Label: strings.ToUpper(st[:1]) + st[1:], Count: board.Counts[st]})
	}

	for _, g := range board.Groups {
		view := BalanceGroupView{Metric: g.Metric, Total: len(g.Notes), Open: g.Open}
		for _, n := range g.Notes {
			if data.State != "" && n.State != data.State {
				continue
			}
			nv := BalanceNoteView{BalanceNote: n, DateLabel: shortDateLabel(n.Date, now)}
			if p := board.PlannedPassFor(n); p > 0 {
				if n.Pass == 0 {
					nv.PassLabel = "planned pass " + strconv.Itoa(p)
				} else {
					nv.PassLabel = "pass " + strconv.Itoa(p)
				}
			}
			view.Notes = append(view.Notes, nv)
		}
		if len(view.Notes) > 0 {
			data.Groups = append(data.Groups, view)
		}
	}
	for _, l := range board.Unsorted {
		lv := LooseNoteView{LooseBalanceNote: l, DateLabel: shortDateLabel(l.Date, now)}
		if r := []rune(l.Text); len(r) > balanceLooseCut {
			lv.Short = strings.TrimSpace(string(r[:balanceLooseCut])) + "…"
		}
		data.Unsorted = append(data.Unsorted, lv)
	}
	for _, p := range board.Passes {
		pv := BalancePassView{BalancePass: p, DateLabel: shortDateLabel(p.Date, now)}
		data.Passes = append(data.Passes, pv)
		if p.Planned {
			planned := pv
			data.Planned = &planned
			if sessions := sessionMap[p.Issue.Slug]; len(sessions) > 0 {
				data.PlannedAgent = sessions[0].Name
			}
		}
	}
	s.renderBalance(w, &data)
}

func (s *Server) renderBalance(w http.ResponseWriter, data *BalanceData) {
	if err := s.tmpl.ExecuteTemplate(w, "balance.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func loadIssueCommentsQuiet(issue *tracker.Issue) []tracker.Comment {
	comments, _ := tracker.LoadComments(issue.FilePath)
	return comments
}

// shortDateLabel renders "2026-10-06" as "6 Oct" ("6 Oct 2025" in another
// year); anything unparseable is returned as is.
func shortDateLabel(date string, now time.Time) string {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), time.Local)
	if err != nil {
		return date
	}
	return decisionDateLabel(t, now)
}
