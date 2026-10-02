package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
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
	Slug               string
	Title              string
	Status             string
	TransitionCount    int
	TotalStaticTokens  int
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
	// CLIUsage is the issue-cli telemetry report for this project; nil when
	// it could not be produced, in which case CLIUsageNotice says why.
	CLIUsage       *telemetry.Report
	CLIUsageNotice string
}

// runTelemetryReport produces `issue-cli telemetry report --json` for a
// project. It shells out because only the CLI binary knows its own command
// registry (needed for the never-used lists). Package-level so tests can
// stub it.
var runTelemetryReport = func(proj *tracker.Project) ([]byte, error) {
	args := []string{}
	if cfg := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_CONFIG")); cfg != "" {
		args = append(args, "--config", cfg, "--project", proj.Slug)
	}
	args = append(args, "--json", "telemetry", "report", "--since", "30d")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "issue-cli", args...)
	// The viewer's own report call must not count as bot usage.
	cmd.Env = append(os.Environ(), telemetry.SkipEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, firstLineOf(msg))
		}
		return nil, err
	}
	return out, nil
}

// loadCLIUsage runs the telemetry report and turns every failure mode into
// an inline notice — the stats page must never fail because of it.
func loadCLIUsage(proj *tracker.Project) (*telemetry.Report, string) {
	out, err := runTelemetryReport(proj)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, "issue-cli is not on the server's PATH — run make install to see CLI usage here."
		}
		return nil, "Could not load CLI usage: " + err.Error()
	}
	var r telemetry.Report
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, "Could not parse issue-cli telemetry output: " + err.Error()
	}
	if r.V > telemetry.ReportVersion {
		return nil, fmt.Sprintf("issue-cli telemetry report v%d is newer than this viewer understands (v%d) — rebuild the viewer.", r.V, telemetry.ReportVersion)
	}
	if !r.Enabled {
		notice := "CLI usage telemetry is disabled for this project"
		if r.Disabled != "" {
			notice += " (" + r.Disabled + ")"
		}
		if r.Summary.Events == 0 {
			return nil, notice + "."
		}
		return &r, notice + "; showing previously recorded events."
	}
	return &r, ""
}

func firstLineOf(s string) string {
	return strings.SplitN(s, "\n", 2)[0]
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
	usage, usageNotice := loadCLIUsage(proj)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "stats.html", StatsData{
		Prefix:              prefix,
		ProjectName:         proj.Name,
		SupportsGitHub:      proj.SupportsGitHub,
		DispatchPromptCost:  tracker.AgentDispatchPromptStaticCost(),
		StaticReference:     staticRows,
		RecordedTransitions: recorded,
		IssueTotals:         totals,
		CLIUsage:            usage,
		CLIUsageNotice:      usageNotice,
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
