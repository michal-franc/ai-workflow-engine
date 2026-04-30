package main

import (
	"net/http"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type StaticTransitionRow struct {
	From   string
	To     string
	Tokens int
}

type RecordedTransitionRow struct {
	From         string
	To           string
	Count        int
	AvgStatic    int
	AvgDynamic   int
	TotalDynamic int
}

type IssueTotalRow struct {
	Slug              string
	Title             string
	Status            string
	TransitionCount   int
	TotalStaticTokens int
	TotalDynamicTokens int
}

type StatsData struct {
	Prefix              string
	ProjectName         string
	SupportsGitHub      bool
	DispatchPromptCost  int
	StaticReference     []StaticTransitionRow
	RecordedTransitions []RecordedTransitionRow
	IssueTotals         []IssueTotalRow
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	wf := proj.LoadWorkflow()

	staticRows := buildStaticReferenceRows(wf)

	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	recorded, totals := aggregateRecordedStats(issues)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "stats.html", StatsData{
		Prefix:              prefix,
		ProjectName:         proj.Name,
		SupportsGitHub:      proj.SupportsGitHub,
		DispatchPromptCost:  tracker.AgentDispatchPromptStaticCost(),
		StaticReference:     staticRows,
		RecordedTransitions: recorded,
		IssueTotals:         totals,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// buildStaticReferenceRows enumerates every (from→to) pair declared in the
// project's workflow.yaml — both explicit transitions and "*" wildcard edges,
// expanded so each potential source status gets a row — and computes the
// pure static token cost for each.
func buildStaticReferenceRows(wf *tracker.WorkflowConfig) []StaticTransitionRow {
	if wf == nil {
		return nil
	}

	type pair struct{ from, to string }
	seen := map[pair]bool{}
	var rows []StaticTransitionRow

	statusOrder := wf.GetStatusOrder()

	for _, t := range wf.Transitions {
		if t.From == "*" {
			for _, src := range statusOrder {
				if src == t.To {
					continue
				}
				p := pair{src, t.To}
				if seen[p] {
					continue
				}
				seen[p] = true
				rows = append(rows, StaticTransitionRow{
					From:   src,
					To:     t.To,
					Tokens: tracker.StaticTransitionCost(wf, src, t.To),
				})
			}
			continue
		}
		p := pair{t.From, t.To}
		if seen[p] {
			continue
		}
		seen[p] = true
		rows = append(rows, StaticTransitionRow{
			From:   t.From,
			To:     t.To,
			Tokens: tracker.StaticTransitionCost(wf, t.From, t.To),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tokens != rows[j].Tokens {
			return rows[i].Tokens > rows[j].Tokens
		}
		if rows[i].From != rows[j].From {
			return rows[i].From < rows[j].From
		}
		return rows[i].To < rows[j].To
	})
	return rows
}

func aggregateRecordedStats(issues []*tracker.Issue) ([]RecordedTransitionRow, []IssueTotalRow) {
	type bucket struct {
		count        int
		sumStatic    int
		sumDynamic   int
		totalDynamic int
	}
	perTransition := map[string]*bucket{}
	keys := []string{}
	var totals []IssueTotalRow

	for _, issue := range issues {
		if issue == nil || issue.FilePath == "" {
			continue
		}
		store, err := tracker.LoadStats(issue.FilePath)
		if err != nil || len(store.Transitions) == 0 {
			continue
		}

		issueTotal := IssueTotalRow{
			Slug:            issue.Slug,
			Title:           issue.Title,
			Status:          issue.Status,
			TransitionCount: len(store.Transitions),
		}
		for _, st := range store.Transitions {
			key := st.From + "→" + st.To
			b, ok := perTransition[key]
			if !ok {
				b = &bucket{}
				perTransition[key] = b
				keys = append(keys, key)
			}
			b.count++
			b.sumStatic += st.StaticTokens
			b.sumDynamic += st.DynamicTokens
			b.totalDynamic += st.DynamicTokens

			issueTotal.TotalStaticTokens += st.StaticTokens
			issueTotal.TotalDynamicTokens += st.DynamicTokens
		}
		totals = append(totals, issueTotal)
	}

	rows := make([]RecordedTransitionRow, 0, len(keys))
	for _, key := range keys {
		b := perTransition[key]
		parts := strings.SplitN(key, "→", 2)
		from, to := parts[0], ""
		if len(parts) == 2 {
			to = parts[1]
		}
		rows = append(rows, RecordedTransitionRow{
			From:         from,
			To:           to,
			Count:        b.count,
			AvgStatic:    b.sumStatic / b.count,
			AvgDynamic:   b.sumDynamic / b.count,
			TotalDynamic: b.totalDynamic,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].AvgDynamic > rows[j].AvgDynamic
	})

	sort.Slice(totals, func(i, j int) bool {
		return totals[i].TotalDynamicTokens > totals[j].TotalDynamicTokens
	})

	return rows, totals
}

