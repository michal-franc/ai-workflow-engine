package main

import (
	"fmt"
	"strconv"

	"github.com/michal-franc/issue-viewer/internal/chart"
	"github.com/michal-franc/issue-viewer/internal/health"
)

// AreaBar is one row of a card's area list: a label, its size and a bar
// scaled to the largest row.
type AreaBar struct {
	Label, Value string
	Pct          int
}

// storageCard is the Stats page's Disk use card: the newest storage
// snapshot's total and a bar per area, and the total's line over the range.
// ok is false until a snapshot has the storage family (the card is pending).
func storageCard(d *Dashboard, daily []*health.Snapshot, latest *health.Snapshot) (ChartCard, bool) {
	newest := latest
	if newest == nil || !hasFamily(newest, "storage") {
		newest = nil
		for i := len(daily) - 1; i >= 0; i-- {
			if hasFamily(daily[i], "storage") {
				newest = daily[i]
				break
			}
		}
	}
	if newest == nil {
		return ChartCard{}, false
	}
	c := ChartCard{ID: "storage", Title: "Disk use"}
	if r := newest.Families["storage"]; r.Error != "" {
		c.Error = "The storage family failed in the latest snapshot: " + r.Error
	}
	total, _ := newest.Value("storage.total_mb", nil)
	c.Big = fmtSize(total)
	c.Sub = "The project checkout on disk on " + newest.Time().Format("2 Jan")
	if n, ok := newest.Value("storage.worktrees", nil); ok {
		c.Sub += " · " + plural(int(n), "worktree", "worktrees")
	}
	c.Sub += ". Tidy playtest shots: issue-cli playtest prune --all"
	largest := 0.0
	vals := make([]float64, len(health.StorageAreas))
	for i, a := range health.StorageAreas {
		vals[i], _ = newest.Value(a.Metric, nil)
		largest = max(largest, vals[i])
	}
	for i, a := range health.StorageAreas {
		pct := 0
		if largest > 0 {
			pct = max(1, int(vals[i]/largest*100+0.5))
		}
		c.Bars = append(c.Bars, AreaBar{Label: a.Label, Value: fmtMB(vals[i]), Pct: pct})
	}

	l := chart.Line{Label: "Disk use", From: d.From, To: d.To, Gap: pointGap, Unit: chart.Count}
	ser := chart.Series{Name: "total MB", Class: "s-all"}
	for _, s := range daily {
		if v, ok := s.Value("storage.total_mb", nil); ok {
			ser.Points = append(ser.Points, chart.Point{At: s.Time(), Value: v})
		}
	}
	l.Series = []chart.Series{ser}
	c.Points = l.Points()
	if len(ser.Points) < 2 {
		c.Empty = "The line starts with the second daily snapshot: a past size can't be backfilled."
		return c, true
	}
	c.SVG, c.Table = l.SVG(), l.Table()
	return c, true
}

func hasFamily(s *health.Snapshot, family string) bool {
	_, ok := s.Families[family]
	return ok
}

// fmtSize is a size given in MB as MB, or as GB from 1024 MB ("3.35 GB").
func fmtSize(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmtMB(mb)
}

// fmtMB is a size given in MB, always in MB so rows compare: "1,336 MB".
func fmtMB(mb float64) string {
	if mb < 10 {
		return strconv.FormatFloat(mb, 'f', 1, 64) + " MB"
	}
	n := strconv.Itoa(int(mb + 0.5))
	for i := len(n) - 3; i > 0; i -= 3 {
		n = n[:i] + "," + n[i:]
	}
	return n + " MB"
}
