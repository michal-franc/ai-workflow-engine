package tracker

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ScoreComponent is a named contribution to a ticket's score.
type ScoreComponent struct {
	Name   string
	Points float64
	Detail string
}

// ScoreBreakdown is the computed score plus per-component contributions for
// transparency in the detail view.
type ScoreBreakdown struct {
	Total      float64
	Components []ScoreComponent
}

const dueUrgencyHorizonDays = 30

// ComputeScore applies cfg to issue and returns the breakdown. When cfg is nil
// or cfg.Enabled is false the breakdown is nil — callers should treat nil as
// "scoring disabled, render nothing".
//
// Clock is injected for deterministic tests; ComputeScore uses time.Now when
// called via the zero-value wrapper at the bottom of this file.
func ComputeScore(issue *Issue, cfg *ScoringConfig, now time.Time) *ScoreBreakdown {
	if cfg == nil || !cfg.Enabled || issue == nil {
		return nil
	}

	var comps []ScoreComponent

	if pts, ok := lookupPriorityWeight(cfg.Formula.Priority, issue.Priority); ok {
		comps = append(comps, ScoreComponent{
			Name:   "priority",
			Points: pts,
			Detail: issue.Priority,
		})
	}

	if duePts, dueDetail, ok := computeDueUrgency(issue, cfg.Formula.DueDate, now); ok {
		comps = append(comps, ScoreComponent{
			Name:   "due",
			Points: duePts,
			Detail: dueDetail,
		})
	}

	if agePts, ageDetail, ok := computeAge(issue, cfg.Formula.Age, now); ok {
		comps = append(comps, ScoreComponent{
			Name:   "age",
			Points: agePts,
			Detail: ageDetail,
		})
	}

	if labelPts, labelDetail, ok := computeLabels(issue, cfg.Formula.Labels); ok {
		comps = append(comps, ScoreComponent{
			Name:   "labels",
			Points: labelPts,
			Detail: labelDetail,
		})
	}

	if boostPts, ok := extractScoreBoost(issue); ok {
		comps = append(comps, ScoreComponent{
			Name:   "boost",
			Points: boostPts,
			Detail: "score_boost",
		})
	}

	if len(comps) == 0 {
		return nil
	}

	total := 0.0
	for _, c := range comps {
		total += c.Points
	}

	return &ScoreBreakdown{
		Total:      total,
		Components: comps,
	}
}

func lookupPriorityWeight(weights ScoringPriority, priority string) (float64, bool) {
	if len(weights) == 0 || priority == "" {
		return 0, false
	}
	if w, ok := weights[priority]; ok {
		return w, true
	}
	// Case-insensitive fallback: YAML keys might be declared with any casing.
	lower := strings.ToLower(priority)
	for k, v := range weights {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return 0, false
}

func computeDueUrgency(issue *Issue, cfg ScoringDueDate, now time.Time) (float64, string, bool) {
	if cfg.UrgencyWeight == 0 {
		return 0, "", false
	}
	dueStr := extraFieldValue(issue, "due")
	if dueStr == "" {
		return 0, "", false
	}
	due, ok := parseDate(dueStr)
	if !ok {
		return 0, "", false
	}

	daysUntil := math.Floor(due.Sub(now).Hours() / 24)
	daysInHorizon := dueUrgencyHorizonDays - daysUntil
	if daysInHorizon < 0 {
		daysInHorizon = 0
	}
	pts := daysInHorizon * cfg.UrgencyWeight
	if cfg.OverdueCap > 0 && pts > cfg.OverdueCap {
		pts = cfg.OverdueCap
	}
	if pts == 0 {
		return 0, "", false
	}

	var detail string
	switch {
	case daysUntil < 0:
		detail = "overdue " + strconv.FormatFloat(-daysUntil, 'f', 0, 64) + "d"
	case daysUntil == 0:
		detail = "due today"
	default:
		detail = "due in " + strconv.FormatFloat(daysUntil, 'f', 0, 64) + "d"
	}
	return pts, detail, true
}

func computeAge(issue *Issue, cfg ScoringAge, now time.Time) (float64, string, bool) {
	if cfg.StalenessWeight == 0 || issue.Created == "" {
		return 0, "", false
	}
	created, ok := parseDate(issue.Created)
	if !ok {
		return 0, "", false
	}
	days := math.Floor(now.Sub(created).Hours() / 24)
	if days <= 0 {
		return 0, "", false
	}
	pts := days * cfg.StalenessWeight
	return pts, strconv.FormatFloat(days, 'f', 0, 64) + "d old", true
}

func computeLabels(issue *Issue, weights ScoringLabels) (float64, string, bool) {
	if len(weights) == 0 || len(issue.Labels) == 0 {
		return 0, "", false
	}
	var total float64
	var matched []string
	for _, label := range issue.Labels {
		if w, ok := weights[label]; ok && w != 0 {
			total += w
			matched = append(matched, label)
		}
	}
	if len(matched) == 0 {
		return 0, "", false
	}
	sort.Strings(matched)
	return total, strings.Join(matched, ", "), true
}

func extractScoreBoost(issue *Issue) (float64, bool) {
	raw := extraFieldValue(issue, "score_boost")
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return v, true
}

func extraFieldValue(issue *Issue, key string) string {
	for _, ef := range issue.ExtraFields {
		if ef.Key == key {
			return strings.TrimSpace(ef.Value)
		}
	}
	return ""
}

// parseDate accepts either `YYYY-MM-DD` or RFC3339 and returns the date at
// UTC midnight for day-granularity math.
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		y, m, d := t.UTC().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}
