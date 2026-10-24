package health

import (
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// snapOf builds a snapshot from "metric{k=v}" ids, the way take writes them.
func snapOf(rows map[string]float64) *Snapshot {
	s := &Snapshot{}
	for id, v := range rows {
		name, rest, hasLabels := strings.Cut(id, "{")
		var labels map[string]string
		if hasLabels {
			labels = map[string]string{}
			for _, kv := range strings.Split(strings.TrimSuffix(rest, "}"), ",") {
				k, val, _ := strings.Cut(kv, "=")
				labels[k] = val
			}
		}
		s.Metrics = append(s.Metrics, Row{ID: RowID(name, labels), Metric: name, Labels: labels, Value: v})
	}
	return s
}

func scoreRows(ms []Metric) map[string]float64 {
	out := map[string]float64{}
	for _, m := range ms {
		out[RowID(m.Name, m.Labels)] = m.Value
	}
	return out
}

func TestComponentSub(t *testing.T) {
	low := Component{Good: 60, Bad: 240}   // lower is better
	high := Component{Good: 0.9, Bad: 0.5} // higher is better
	for _, c := range []struct {
		comp Component
		v    float64
		want float64
	}{
		{low, 30, 100}, {low, 60, 100}, {low, 150, 50}, {low, 240, 0}, {low, 999, 0},
		{high, 1, 100}, {high, 0.79, 73}, {high, 0.5, 0}, {high, 0.1, 0},
		{Component{Good: 1, Bad: 1}, 1, 0},
	} {
		if got := c.comp.Sub(c.v); got != c.want {
			t.Errorf("Sub(%v) with good %v bad %v = %v, want %v", c.v, c.comp.Good, c.comp.Bad, got, c.want)
		}
	}
}

// issue-viewer on 23 Oct 2026, the numbers in the design: 86 from 6 of 9
// components (70% of the weight).
func TestDeriveIssueViewer(t *testing.T) {
	cur := snapOf(map[string]float64{
		"tests.wall_seconds{kind=unit}":               30.3,
		"tests.e2e.coverage":                          1,
		"shape.big_lines.share":                       0.182,
		"shape.long_funcs.share":                      0.032,
		"shape.churn.big_share{window=30d}":           0.121,
		"codebase.test_ratio":                         0.52,
		"codebase.lines{kind=code}":                   65091,
		"codebase.lines{kind=test}":                   33829,
		"codebase.growth.lines{kind=code,window=30d}": 38021,
		"codebase.growth.lines{kind=test,window=30d}": 20471,
		"tests.per_test_ms{kind=unit,stat=mean}":      55,
	})
	got := scoreRows(Derive(cur, nil, DefaultComponents()))
	want := map[string]float64{
		"score.component{component=unit_wait}":    100,
		"score.component{component=e2e_coverage}": 100,
		"score.component{component=big_files}":    47,
		"score.component{component=long_funcs}":   85,
		"score.component{component=test_balance}": 100,
		"score.component{component=hotspots}":     95,
		"score.coverage":                          0.636, // 70 of 110: the lint component has no row here
		"score.derived":                           86,
		"score.component.weight{component=flaky}": 10,
	}
	for id, v := range want {
		if got[id] != v {
			t.Errorf("%s = %v, want %v", id, got[id], v)
		}
	}
	for _, missing := range []string{"unit_cost_trend", "e2e_wait", "flaky"} {
		if _, ok := got["score.component{component="+missing+"}"]; ok {
			t.Errorf("%s has no data and must have no sub-score", missing)
		}
	}
}

func TestDeriveTrendsAndConfig(t *testing.T) {
	past := snapOf(map[string]float64{"tests.per_test_ms{kind=unit,stat=mean}": 10, "codebase.test_ratio": 0.5})
	cur := snapOf(map[string]float64{
		"tests.per_test_ms{kind=unit,stat=mean}": 15,   // +50%: (1.0-0.5)/(0.9) → 56
		"codebase.test_ratio":                    0.4,  // -0.10: (−0.15+0.10)/(−0.13) → 38
		"tests.wall_seconds{kind=e2e}":           1500, // (2400-1500)/1800 → 50
		"tests.run_age_days{kind=e2e}":           3,
	})
	got := scoreRows(Derive(cur, past, DefaultComponents()))
	for id, v := range map[string]float64{
		"score.component{component=unit_cost_trend}":       56,
		"score.component.value{component=unit_cost_trend}": 0.5,
		"score.component{component=test_balance}":          38,
		"score.component{component=e2e_wait}":              50,
	} {
		if got[id] != v {
			t.Errorf("%s = %v, want %v", id, got[id], v)
		}
	}
	if _, ok := got["score.derived"]; ok {
		t.Error("30% of the weight is under the 50% floor: no total")
	}

	stale := snapOf(map[string]float64{"tests.wall_seconds{kind=e2e}": 100, "tests.run_age_days{kind=e2e}": 20, "tests.e2e.estimated_seconds": 2400})
	if got := scoreRows(Derive(stale, nil, DefaultComponents())); got["score.component{component=e2e_wait}"] != 0 {
		t.Errorf("a CI run over 14 days old must give way to the estimate: %v", got)
	}

	w0, w50 := 0.0, 50.0
	good := tracker.ScoreBound(0.5)
	cfg := &tracker.HealthConfig{Score: map[string]tracker.ScoreKnob{
		"unit_cost_trend": {Weight: &w0},
		"test_balance":    {Weight: &w50, Good: &good},
	}}
	got = scoreRows(Derive(cur, past, Components(cfg)))
	if _, ok := got["score.component.weight{component=unit_cost_trend}"]; ok {
		t.Error("weight 0 must turn the component off")
	}
	if got["score.component.weight{component=test_balance}"] != 50 {
		t.Errorf("weight override: %v", got)
	}
}

func TestDeriveNothingMeasured(t *testing.T) {
	if got := Derive(&Snapshot{}, nil, DefaultComponents()); got != nil {
		t.Errorf("no data must write no score rows: %v", got)
	}
}
