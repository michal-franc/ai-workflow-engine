package timeline

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The `workflow` health family: Aggregate over the issues done in the last
// 30 days, as series. The names are documented in docs/workbench-files.md,
// "Health snapshots"; keep them stable, the project stats dashboard reads
// them.
func init() {
	health.Register("workflow", workflowFamily)
	health.Describe("workflow", "tables: /stats")
}

func workflowFamily(ctx context.Context, env health.Env) ([]health.Metric, error) {
	if env.Project == nil || env.Project.IssueDir == "" {
		return nil, errors.New("no project issues folder")
	}
	issues, err := tracker.LoadIssues(env.Project.IssueDir)
	if err != nil {
		return nil, err
	}
	now := env.Now
	if now.IsZero() {
		now = time.Now()
	}
	return Metrics(Aggregate(ctx, env.Project, env.Workflow, issues, now, Window)), ctx.Err()
}

// Metrics turns the report into the family's rows.
func Metrics(rep Report) []health.Metric {
	var out []health.Metric
	add := func(name string, labels map[string]string, v float64, unit, help string) {
		out = append(out, health.Metric{Name: "workflow." + name, Labels: labels, Value: v, Unit: unit, Help: help})
	}
	secs := func(d time.Duration) float64 { return d.Seconds() }
	add("unread", nil, float64(len(rep.Unread)), health.UnitCount, "Done issues with no recorded transition to done, left out")
	for _, t := range rep.Types {
		l := map[string]string{"type": t.Type}
		add("issues", l, float64(t.N), health.UnitCount, "Issues done in the last 30 days")
		add("issues_unsplit", l, float64(t.Unsplit), health.UnitCount, "Of those, issues without CLI calls: cycle time only")
		add("cycle.p50", l, secs(t.Cycle.P50), health.UnitSeconds, "Cycle time, dispatch to done, p50")
		add("cycle.p90", l, secs(t.Cycle.P90), health.UnitSeconds, "Cycle time, dispatch to done, p90")
		add("dispatches.p50", l, t.Dispatches[0], health.UnitCount, "Agent dispatches per issue, p50")
		add("dispatches.p90", l, t.Dispatches[1], health.UnitCount, "Agent dispatches per issue, p90")
		add("rework.loops", l, float64(t.Loops), health.UnitCount, "Rework loops: backward transitions and failed playtests from testing on")
		add("rework.backward", l, float64(t.Backward), health.UnitCount, "Backward transitions")
		add("rework.share", l, t.ReworkShare, health.UnitShare, "Agent time inside rework loops, share of agent working time")
		add("working.p50", l, secs(t.WorkingPer.P50), health.UnitSeconds, "Agent working time per issue, p50")
		add("working.p90", l, secs(t.WorkingPer.P90), health.UnitSeconds, "Agent working time per issue, p90")
		add("time.working", l, secs(t.Working), health.UnitSeconds, "Agent working time, summed over the issues")
		add("time.waiting", l, secs(t.Waiting), health.UnitSeconds, "Time waiting on the owner, summed over the issues")
		add("time.idle", l, secs(t.Idle), health.UnitSeconds, "Idle time, summed over the issues")
		if t.Type == All && t.Working+t.Waiting+t.Idle > 0 {
			add("time.share", map[string]string{"kind": "waiting"}, t.WaitingShare(), health.UnitShare, "Share of issue time waiting on the owner")
		}
	}
	for _, s := range rep.Statuses {
		l := map[string]string{"status": s.Status, "type": s.Type}
		for _, k := range []struct {
			kind string
			p    Pct
			help string
		}{
			{"total", s.Total, "Time in the status"},
			{"working", s.Working, "Agent working in the status"},
			{"waiting", s.Waiting, "Waiting on the owner in the status"},
			{"idle", s.Idle, "Idle in the status"},
		} {
			add("status."+k.kind+".p50", l, secs(k.p.P50), health.UnitSeconds, k.help+", p50")
			add("status."+k.kind+".p90", l, secs(k.p.P90), health.UnitSeconds, k.help+", p90")
		}
	}
	for _, a := range rep.Approvals {
		l := map[string]string{"transition": a.Transition}
		add("approvals", l, float64(a.N), health.UnitCount, "Approvals at the transition")
		add("approval_wait.p50", l, secs(a.Wait.P50), health.UnitSeconds, "Owner wait from the agent's last call to the approval, p50")
		add("approval_wait.p90", l, secs(a.Wait.P90), health.UnitSeconds, "Owner wait from the agent's last call to the approval, p90")
	}
	for _, f := range rep.Failures {
		l := map[string]string{"transition": f.Transition, "err_class": f.ErrClass, "dry_run": strconv.FormatBool(f.DryRun)}
		add("gate_failures", l, float64(f.N), health.UnitCount, "Refused transition calls")
	}
	return out
}
