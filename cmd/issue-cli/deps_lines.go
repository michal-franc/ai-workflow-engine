package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// sharedFilesShown caps the hot files show lists for an issue's branch.
const sharedFilesShown = 10

// printDepLines prints the issue's depends-on, depended-on-by and decided-by
// links under show's header, plus a one-line guard-metrics summary for its
// system when workbench.metrics lists any.
func printDepLines(ctx *Context, issue *tracker.Issue, issues []*tracker.Issue) {
	closed := tracker.ClosedIssueFunc(ctx.Project)
	deps := tracker.IssueDeps(issue, issues, tracker.BuildDecisionIndex(issues, closed), closed)
	for _, kind := range []string{"depends on", "depended on by", "decided by"} {
		var parts []string
		for _, d := range deps {
			if d.Kind == kind {
				parts = append(parts, fmt.Sprintf("%s (%s)", d.Target, d.State))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(ctx.Stdout, "%s: %s\n", strings.ToUpper(kind[:1])+kind[1:], strings.Join(parts, ", "))
		}
	}
	if line := guardMetricsLine(ctx.Project, issue); line != "" {
		fmt.Fprintln(ctx.Stdout, line)
	}
}

// guardMetricsLine summarises the metrics the issue's system lists:
// "Guard metrics for Sim: 2 off (stuck, spawn-kills), 1 near. See …".
func guardMetricsLine(proj *tracker.Project, issue *tracker.Issue) string {
	wb := proj.LoadWorkflow().Workbench
	if wb.Metrics == (tracker.WorkbenchMetrics{}) || issue.System == "" {
		return ""
	}
	rows := tracker.MetricsForSystem(tracker.LoadMetrics(projectRoot(proj), wb, nil), wb, issue.System, "")
	if len(rows) == 0 {
		return ""
	}
	var off, near []string
	for _, m := range rows {
		switch m.Verdict() {
		case tracker.VerdictOff:
			off = append(off, m.ID)
		case tracker.VerdictNear:
			near = append(near, m.ID)
		}
	}
	var parts []string
	if len(off) > 0 {
		parts = append(parts, fmt.Sprintf("%d off (%s)", len(off), strings.Join(off, ", ")))
	}
	if len(near) > 0 {
		parts = append(parts, fmt.Sprintf("%d near", len(near)))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("all %d ok", len(rows)))
	}
	return fmt.Sprintf("Guard metrics for %s: %s. See %s", issue.System, strings.Join(parts, ", "), metricsURL(proj, issue.System))
}

func metricsURL(proj *tracker.Project, system string) string {
	base := viewerBaseURL(proj)
	return fmt.Sprintf("%s/p/%s/metrics?system=%s", base, proj.Slug, system)
}

// printSharedFiles lists the hot files the issue's open branch shares with
// other open branches, so an agent knows where a merge may conflict.
func printSharedFiles(w io.Writer, proj *tracker.Project, issue *tracker.Issue, issues []*tracker.Issue) {
	shared := tracker.IssueSharedFiles(projectRoot(proj), issue, issues)
	if shared == nil {
		return
	}
	fmt.Fprintf(w, "== Shared files (%d, with %d other branch%s) ==\n", len(shared.Files), shared.Others, map[bool]string{true: "", false: "es"}[shared.Others == 1])
	for i, h := range shared.Files {
		if i == sharedFilesShown {
			fmt.Fprintf(w, "  … %d more: see the Branches tab of the Metrics view\n", len(shared.Files)-i)
			break
		}
		var others []string
		for _, c := range h.Changes {
			if c.Branch != shared.Branch && !c.Shared {
				others = append(others, strings.TrimPrefix(c.Branch, "work/"))
			}
		}
		fmt.Fprintf(w, "  %s  also on %s\n", h.Path, strings.Join(others, ", "))
	}
	fmt.Fprintln(w, "Merge main often and coordinate on these files with the branches' agents.")
	fmt.Fprintln(w)
}
