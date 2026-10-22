package main

import (
	"net/url"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// panelRows is how many rows a system-page panel shows before "All …".
const panelRows = 5

// MetricsPanel feeds the "system-guard-metrics" partial.
type MetricsPanel struct {
	Scope string // "Sim", or "Sim › Plays" for a family
	Run   *tracker.MetricRun
	Rows  []tracker.GuardMetric
	Total int
	Off   int
	Link  string
}

// KnobsPanel feeds the "system-knobs" partial.
type KnobsPanel struct {
	Scope string
	Rows  []tracker.Knob
	Total int
	Link  string
}

// systemMetricsPanels builds the two panels a system (or family) page embeds:
// the metrics and knobs that workbench.systems.<system>(.families.<family>)
// lists, worst verdict and most recent change first. Either is nil when the
// project has no such source or the system lists nothing.
func systemMetricsPanels(proj *tracker.Project, issues []*tracker.Issue, prefix, system, family string) (*MetricsPanel, *KnobsPanel) {
	wb := proj.LoadWorkflow().Workbench
	if wb.Metrics == (tracker.WorkbenchMetrics{}) && len(wb.Tuning) == 0 {
		return nil, nil
	}
	root := projectRoot(proj)
	branches := tracker.OpenBranches(root, "", issues)
	scope := system
	if family != "" {
		scope += " › " + family
	}
	link := func(tab string) string {
		q := url.Values{"tab": {tab}, "system": {system}}
		return prefix + "/metrics?" + q.Encode()
	}

	var mp *MetricsPanel
	if wb.Metrics != (tracker.WorkbenchMetrics{}) {
		rep := tracker.LoadMetrics(root, wb, branches)
		if rows := tracker.MetricsForSystem(rep, wb, system, family); len(rows) > 0 {
			mp = &MetricsPanel{Scope: scope, Run: rep.Run, Total: len(rows), Link: link("metrics")}
			for _, m := range rows {
				if m.Verdict() == tracker.VerdictOff {
					mp.Off++
				}
			}
			mp.Rows = rows[:min(panelRows, len(rows))]
		}
	}
	var kp *KnobsPanel
	if len(wb.Tuning) > 0 {
		knobs, _ := tracker.LoadKnobs(root, wb, branches)
		if rows := tracker.KnobsForSystem(knobs, wb, system, family); len(rows) > 0 {
			kp = &KnobsPanel{Scope: scope, Total: len(rows), Rows: rows[:min(panelRows, len(rows))], Link: link("knobs")}
		}
	}
	return mp, kp
}
