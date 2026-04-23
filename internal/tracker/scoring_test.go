package tracker

import (
	"testing"
	"time"
)

func issueFixture(priority, created, due, boost string, labels []string) *Issue {
	iss := &Issue{
		Priority: priority,
		Created:  created,
		Labels:   labels,
	}
	if due != "" {
		iss.ExtraFields = append(iss.ExtraFields, ExtraField{Key: "due", Value: due})
	}
	if boost != "" {
		iss.ExtraFields = append(iss.ExtraFields, ExtraField{Key: "score_boost", Value: boost})
	}
	return iss
}

func newFormula() *ScoringConfig {
	return &ScoringConfig{
		Enabled: true,
		Formula: ScoringFormula{
			Priority: ScoringPriority{
				"critical": 40,
				"high":     20,
				"medium":   10,
				"low":      0,
			},
			DueDate: ScoringDueDate{
				UrgencyWeight: 2,
				OverdueCap:    60,
			},
			Age: ScoringAge{
				StalenessWeight: 0.1,
			},
			Labels: ScoringLabels{
				"bug":         5,
				"blocker":     25,
				"enhancement": 0,
			},
		},
	}
}

func TestComputeScore_Disabled(t *testing.T) {
	cfg := newFormula()
	cfg.Enabled = false
	got := ComputeScore(issueFixture("critical", "", "", "", nil), cfg, time.Now())
	if got != nil {
		t.Fatalf("expected nil when disabled, got %+v", got)
	}
}

func TestComputeScore_NilConfig(t *testing.T) {
	if got := ComputeScore(&Issue{}, nil, time.Now()); got != nil {
		t.Fatalf("expected nil for nil config, got %+v", got)
	}
}

func TestComputeScore_Priority(t *testing.T) {
	cfg := newFormula()
	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		priority string
		want     float64
	}{
		{"critical", 40},
		{"high", 20},
		{"medium", 10},
		{"low", 0}, // zero weight → not included, so total 0
		{"", 0},
		{"CRITICAL", 40}, // case-insensitive fallback
	}

	for _, tc := range cases {
		got := ComputeScore(issueFixture(tc.priority, "", "", "", nil), cfg, now)
		if got == nil {
			if tc.want == 0 {
				continue
			}
			t.Fatalf("priority %q: expected %v, got nil", tc.priority, tc.want)
		}
		if got.Total != tc.want {
			t.Errorf("priority %q: got total %v, want %v", tc.priority, got.Total, tc.want)
		}
	}
}

func TestComputeScore_DueDate(t *testing.T) {
	cfg := newFormula()
	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		due  string
		want float64
	}{
		{"beyond horizon", "2026-06-30", 0},         // >30 days out → 0 urgency
		{"at horizon", "2026-05-23", 0},             // exactly 30 days → 0
		{"inside horizon", "2026-05-03", 40},        // 10 days until due → (30-10)*2 = 40
		{"due today", "2026-04-23", 60},             // 30*2 = 60, under cap
		{"overdue 10d", "2026-04-13", 60},           // (30-(-10))*2 = 80, capped to 60
		{"overdue far", "2025-01-01", 60},           // capped
	}

	for _, tc := range cases {
		got := ComputeScore(issueFixture("", "", tc.due, "", nil), cfg, now)
		gotTotal := 0.0
		if got != nil {
			gotTotal = got.Total
		}
		if gotTotal != tc.want {
			t.Errorf("%s (due=%s): got %v, want %v", tc.name, tc.due, gotTotal, tc.want)
		}
	}
}

func TestComputeScore_Age(t *testing.T) {
	cfg := newFormula()
	cfg.Formula.Priority = nil // isolate age contribution
	cfg.Formula.DueDate = ScoringDueDate{}
	cfg.Formula.Labels = nil

	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	got := ComputeScore(issueFixture("", "2026-04-13", "", "", nil), cfg, now)
	if got == nil || got.Total != 1.0 {
		t.Fatalf("10 days old @ 0.1/day: expected 1.0, got %+v", got)
	}

	// No created date → no age contribution
	got = ComputeScore(issueFixture("", "", "", "", nil), cfg, now)
	if got != nil && got.Total != 0 {
		t.Errorf("missing created: expected 0, got %+v", got)
	}
}

func TestComputeScore_Labels(t *testing.T) {
	cfg := newFormula()
	cfg.Formula.Priority = nil
	cfg.Formula.DueDate = ScoringDueDate{}
	cfg.Formula.Age = ScoringAge{}

	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	got := ComputeScore(issueFixture("", "", "", "", []string{"bug", "blocker", "enhancement", "unknown"}), cfg, now)
	if got == nil || got.Total != 30 {
		t.Fatalf("bug(5) + blocker(25) + enhancement(0 ignored) + unknown(absent): expected 30, got %+v", got)
	}
}

func TestComputeScore_Boost(t *testing.T) {
	cfg := newFormula()
	cfg.Formula.Priority = nil
	cfg.Formula.DueDate = ScoringDueDate{}
	cfg.Formula.Age = ScoringAge{}
	cfg.Formula.Labels = nil

	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	got := ComputeScore(issueFixture("", "", "", "17", nil), cfg, now)
	if got == nil || got.Total != 17 {
		t.Fatalf("boost only: expected 17, got %+v", got)
	}

	// Negative boost works (manual deprioritization)
	got = ComputeScore(issueFixture("", "", "", "-5", nil), cfg, now)
	if got == nil || got.Total != -5 {
		t.Fatalf("negative boost: expected -5, got %+v", got)
	}

	// Invalid boost is silently ignored
	got = ComputeScore(issueFixture("", "", "", "not-a-number", nil), cfg, now)
	if got != nil && got.Total != 0 {
		t.Errorf("invalid boost: expected 0, got %+v", got)
	}
}

func TestComputeScore_CombinedBreakdown(t *testing.T) {
	cfg := newFormula()
	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	iss := issueFixture("high", "2026-04-13", "2026-04-23", "5", []string{"bug"})
	got := ComputeScore(iss, cfg, now)
	if got == nil {
		t.Fatal("expected non-nil breakdown")
	}
	// priority=20 + due=60(cap since today→30*2=60) + age=1 (10d*0.1) + labels=5 (bug) + boost=5 = 91
	want := 20.0 + 60.0 + 1.0 + 5.0 + 5.0
	if got.Total != want {
		t.Errorf("combined: got %v, want %v; breakdown=%+v", got.Total, want, got.Components)
	}
	if len(got.Components) != 5 {
		t.Errorf("expected 5 components, got %d: %+v", len(got.Components), got.Components)
	}
}
