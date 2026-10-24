package health

import (
	"math"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The derived health score: after the families ran, Derive turns the
// snapshot's rows (and the snapshot from 30 days before) into one 0–100
// score. Each component maps one measured value linearly from Good (100) to
// Bad (0), clamped; components without data are left out and the weights
// renormalised. It rewards fast feedback and lean code: more tests is never
// a reward in itself, only their cost counts. See docs/workbench-files.md,
// "Scores".

// ScoreFamily is the group the score rows carry.
const ScoreFamily = "score"

// minCoverage is the share of the weight that must have data before a total
// is written.
const minCoverage = 0.5

// trendDays is how far back the trend components compare.
const trendDays = 30

// Component is one part of the derived score.
type Component struct {
	Name   string
	Help   string // what is measured, one line
	Good   float64
	Bad    float64
	Weight float64
	// Unit says how to show Good, Bad and the value: seconds, share,
	// change (a relative change, 0.1 = +10%) or delta (an absolute change).
	Unit string
	// Missing is why there is often no data, for the Health view.
	Missing string
	measure func(cur, past *Snapshot) (float64, bool)
}

// DefaultComponents is the score with its defaults, in display order.
func DefaultComponents() []Component {
	return []Component{
		{Name: "unit_wait", Help: "Unit suite wall time", Good: 60, Bad: 240, Weight: 20, Unit: UnitSeconds,
			Missing: "no unit results from local CI (ci.run results:)", measure: row("tests.wall_seconds", "kind", "unit")},
		{Name: "unit_cost_trend", Help: "Mean time per unit test vs 30 days ago", Good: 0.10, Bad: 1.00, Weight: 10, Unit: "change",
			Missing: "needs unit results from 30 days ago", measure: change("tests.per_test_ms", "kind", "unit", "stat", "mean")},
		{Name: "e2e_wait", Help: "E2E suite wall time (CI run under 14 days old, else the estimate)", Good: 600, Bad: 2400, Weight: 10, Unit: UnitSeconds,
			Missing: "no e2e run in local CI (issue-cli ci run --tier slow) and no estimate", measure: e2eWait},
		{Name: "e2e_coverage", Help: "Done work (90 days) in systems with a playtest session", Good: 0.9, Bad: 0.5, Weight: 10, Unit: UnitShare,
			Missing: "no playtest sessions or no done issues with a system", measure: row("tests.e2e.coverage")},
		{Name: "flaky", Help: "Flaky e2e steps / steps run, 14 days", Good: 0, Bad: 0.05, Weight: 10, Unit: UnitShare,
			Missing: "needs the playtest telemetry (health-check-speed)", measure: row("tests.e2e.flaky_share", "window", "14d")},
		{Name: "big_files", Help: "Source lines in big files", Good: 0.05, Bad: 0.30, Weight: 15, Unit: UnitShare,
			Missing: "no shape family rows", measure: row("shape.big_lines.share")},
		{Name: "long_funcs", Help: "Functions over the long-function limit", Good: 0.02, Bad: 0.10, Weight: 10, Unit: UnitShare,
			Missing: "no functions parsed", measure: row("shape.long_funcs.share")},
		{Name: "test_balance", Help: "Change in test lines per code line over 30 days (only a fall counts)", Good: -0.02, Bad: -0.15, Weight: 10, Unit: "delta",
			Missing: "no codebase growth for 30 days", measure: testBalance},
		{Name: "hotspots", Help: "Share of 30-day churn in big files", Good: 0.10, Bad: 0.50, Weight: 5, Unit: UnitShare,
			Missing: "no churn in 30 days", measure: row("shape.churn.big_share", "window", "30d")},
		{Name: "lint", Help: "issue-cli health lint warnings: workflow.yaml problems and undated issues", Good: 0, Bad: 20, Weight: 10, Unit: UnitCount,
			Missing: "no lint family in the snapshot (taken before it existed)", measure: row("lint.warnings")},
	}
}

// Components is the score as configured: the defaults with health.score
// overrides applied.
func Components(cfg *tracker.HealthConfig) []Component {
	comps := DefaultComponents()
	if cfg == nil {
		return comps
	}
	for i, c := range comps {
		k, ok := cfg.Score[c.Name]
		if !ok {
			continue
		}
		if k.Good != nil {
			comps[i].Good = float64(*k.Good)
		}
		if k.Bad != nil {
			comps[i].Bad = float64(*k.Bad)
		}
		if k.Weight != nil {
			comps[i].Weight = *k.Weight
		}
	}
	return comps
}

// Sub maps v to 0–100 between Good and Bad.
func (c Component) Sub(v float64) float64 {
	if c.Bad == c.Good {
		return 0
	}
	f := (c.Bad - v) / (c.Bad - c.Good)
	return math.Round(math.Max(0, math.Min(1, f)) * 100)
}

// Derive computes the score rows for cur. past is the newest snapshot at or
// before 30 days earlier (nil when there is none).
func Derive(cur, past *Snapshot, comps []Component) []Metric {
	var out []Metric
	total, withData, sum := 0.0, 0.0, 0.0
	for _, c := range comps {
		if c.Weight <= 0 {
			continue
		}
		total += c.Weight
		l := map[string]string{"component": c.Name}
		out = append(out, Metric{Name: "score.component.weight", Labels: l, Value: c.Weight, Help: "Weight of each score component (configured)"})
		v, ok := c.measure(cur, past)
		if !ok {
			continue
		}
		sub := c.Sub(v)
		withData += c.Weight
		sum += sub * c.Weight
		out = append(out,
			Metric{Name: "score.component", Labels: l, Value: sub, Help: "Sub-score of each component, 0–100"},
			Metric{Name: "score.component.value", Labels: l, Value: math.Round(v*10000) / 10000, Help: "The measured value behind each sub-score"})
	}
	if withData == 0 {
		return nil // nothing measured: no score, not a zero
	}
	cov := math.Round(withData/total*1000) / 1000
	out = append(out, Metric{Name: "score.coverage", Value: cov, Unit: UnitShare, Help: "Share of the score's weight that had data"})
	if cov >= minCoverage {
		out = append(out, Metric{Name: "score.derived", Value: math.Round(sum / withData), Help: "Derived health, 0–100"})
	}
	return out
}

// deriveInto adds the score rows to snap, from the snapshot 30 days before.
func deriveInto(env Env, snap *Snapshot) {
	past := Before(env.Root, env.Now.AddDate(0, 0, -trendDays))
	rows := Derive(snap, past, Components(env.Config))
	if len(rows) == 0 {
		return
	}
	for _, m := range rows {
		snap.Metrics = append(snap.Metrics, Row{ID: RowID(m.Name, m.Labels), Metric: m.Name, Labels: m.Labels, Value: m.Value, Name: m.Help, Group: ScoreFamily, Band: band(m.Unit)})
	}
	snap.Families[ScoreFamily] = FamilyResult{Metrics: len(rows), Hint: "components and opinion: /health"}
}

func band(unit string) *Band {
	if unit == "" {
		return nil
	}
	return &Band{Unit: unit}
}

// row reads one metric with the given label pairs.
func row(name string, kv ...string) func(cur, past *Snapshot) (float64, bool) {
	labels := pairs(kv)
	return func(cur, _ *Snapshot) (float64, bool) {
		return cur.Value(name, labels)
	}
}

// change is the relative change of a metric against the past snapshot.
func change(name string, kv ...string) func(cur, past *Snapshot) (float64, bool) {
	labels := pairs(kv)
	return func(cur, past *Snapshot) (float64, bool) {
		if past == nil {
			return 0, false
		}
		now, ok1 := cur.Value(name, labels)
		then, ok2 := past.Value(name, labels)
		if !ok1 || !ok2 || then <= 0 {
			return 0, false
		}
		return now/then - 1, true
	}
}

func pairs(kv []string) map[string]string {
	if len(kv) == 0 {
		return nil
	}
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// e2eWait is the e2e wall from a CI run at most 14 days old, else the
// telemetry estimate.
func e2eWait(cur, _ *Snapshot) (float64, bool) {
	k := map[string]string{"kind": "e2e"}
	if age, ok := cur.Value("tests.run_age_days", k); ok && age <= 14 {
		if v, ok := cur.Value("tests.wall_seconds", k); ok {
			return v, true
		}
	}
	return cur.Value("tests.e2e.estimated_seconds", nil)
}

// testBalance is the change in codebase.test_ratio over 30 days, from the
// past snapshot or, without one, from the 30-day growth rows. A rise counts
// as zero: only code outgrowing its tests is penalised.
func testBalance(cur, past *Snapshot) (float64, bool) {
	now, ok := cur.Value("codebase.test_ratio", nil)
	if !ok {
		return 0, false
	}
	then, ok := 0.0, false
	if past != nil {
		then, ok = past.Value("codebase.test_ratio", nil)
	}
	if !ok {
		w := func(kind string) map[string]string { return map[string]string{"kind": kind, "window": "30d"} }
		code, ok1 := cur.Value("codebase.lines", map[string]string{"kind": KindCode})
		test, ok2 := cur.Value("codebase.lines", map[string]string{"kind": KindTest})
		gc, ok3 := cur.Value("codebase.growth.lines", w(KindCode))
		gt, ok4 := cur.Value("codebase.growth.lines", w(KindTest))
		if !ok1 || !ok2 || !ok3 || !ok4 || code-gc <= 0 {
			return 0, false
		}
		then = (test - gt) / (code - gc)
	}
	return math.Min(0, now-then), true
}

// ScoreAt is the derived score of the newest snapshot at or before t, for the
// 7- and 30-day changes; ok is false without one.
func ScoreAt(root string, t time.Time) (float64, bool) {
	s := Before(root, t)
	if s == nil {
		return 0, false
	}
	return s.Value("score.derived", nil)
}
