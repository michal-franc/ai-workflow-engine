package chart

import (
	"html/template"
	"sort"
	"strings"
	"time"
)

// Point is one value at a time. Href makes it a link (the issues behind
// it); Note is added to its tooltip.
type Point struct {
	At    time.Time
	Value float64
	Href  string
	Note  string
}

// Series is one line. Class colours it (see the package doc); Faint draws
// it dashed and lighter (a p90 beside its p50).
type Series struct {
	Name   string
	Class  string
	Faint  bool
	Points []Point
}

// Line is a line chart over [From, To]. Gap breaks a line where two points
// are further apart than it (a day without data); 0 never breaks. Limit
// draws a dashed threshold line labelled LimitLabel. Marker draws a dashed
// vertical line at MarkerAt with MarkerText (where the data starts).
type Line struct {
	Label      string
	From, To   time.Time
	Unit       Unit
	Series     []Series
	Gap        time.Duration
	Limit      float64
	LimitLabel string
	MarkerAt   time.Time
	MarkerText string
	// Selected rings the point at this time in the series with this name.
	SelectedAt     time.Time
	SelectedSeries string
}

const (
	lineW, lineH            = 560, 200
	plotL, plotR            = 48.0, 548.0
	plotT, plotB            = 16.0, 170.0
	lineTicks               = 3
	labelGap                = 13.0
	pointR, pointHit, ringR = 4, 10, 6
)

// Empty reports whether no series has a point.
func (l Line) Empty() bool {
	for _, s := range l.Series {
		if len(s.Points) > 0 {
			return false
		}
	}
	return true
}

// Points is how many points the chart has across its series.
func (l Line) Points() int {
	n := 0
	for _, s := range l.Series {
		n += len(s.Points)
	}
	return n
}

func (l Line) x(t time.Time) float64 {
	span := l.To.Sub(l.From)
	if span <= 0 {
		return plotR
	}
	return plotL + float64(t.Sub(l.From))/float64(span)*(plotR-plotL)
}

// SVG draws the chart.
func (l Line) SVG() template.HTML {
	max := l.Limit
	for _, s := range l.Series {
		for _, p := range s.Points {
			if p.Value > max {
				max = p.Value
			}
		}
	}
	top, step := niceMax(l.Unit, max, lineTicks)
	y := func(v float64) float64 { return plotB - v/top*(plotB-plotT) }

	var b strings.Builder
	b.WriteString(`<svg class="chart chart-line" viewBox="0 0 560 200" role="img" aria-label="` + esc(l.Label) + `">`)
	b.WriteString(`<g class="chart-grid">`)
	for i := 0; i <= lineTicks; i++ {
		v := step * float64(i)
		b.WriteString(`<line x1="` + f(plotL) + `" y1="` + f(y(v)) + `" x2="` + f(plotR) + `" y2="` + f(y(v)) + `"></line>`)
	}
	b.WriteString(`</g>`)
	for i := 0; i <= lineTicks; i++ {
		v := step * float64(i)
		label := Format(l.Unit, v)
		if v == 0 {
			label = "0"
		}
		b.WriteString(`<text class="chart-axis" x="` + f(plotL-6) + `" y="` + f(y(v)+3) + `" text-anchor="end">` + esc(label) + `</text>`)
	}
	for i, t := range dateTicks(l.From, l.To) {
		anchor := "middle"
		if i == 0 {
			anchor = "start"
		}
		b.WriteString(`<text class="chart-axis" x="` + f(l.x(t)) + `" y="188" text-anchor="` + anchor + `">` + esc(t.Format("2 Jan")) + `</text>`)
	}
	if !l.MarkerAt.IsZero() && l.MarkerAt.After(l.From) {
		x := l.x(l.MarkerAt)
		b.WriteString(`<line class="chart-marker" x1="` + f(x) + `" y1="` + f(plotT) + `" x2="` + f(x) + `" y2="` + f(plotB) + `"></line>`)
		b.WriteString(`<text class="chart-axis" x="` + f(x-6) + `" y="` + f(plotT+14) + `" text-anchor="end">` + esc(l.MarkerText) + `</text>`)
	}
	if l.Limit > 0 {
		ly := y(l.Limit)
		b.WriteString(`<line class="chart-limit" x1="` + f(plotL) + `" y1="` + f(ly) + `" x2="` + f(plotR) + `" y2="` + f(ly) + `"></line>`)
		b.WriteString(`<text class="chart-axis" x="` + f(plotL+6) + `" y="` + f(ly-5) + `">` + esc(l.LimitLabel) + `</text>`)
	}

	type endLabel struct {
		y    float64
		text string
	}
	var ends []endLabel
	for _, s := range l.Series {
		if len(s.Points) == 0 {
			continue
		}
		pts := append([]Point(nil), s.Points...)
		sort.Slice(pts, func(i, j int) bool { return pts[i].At.Before(pts[j].At) })
		cls := "chart-series " + s.Class
		if s.Faint {
			cls += " faint"
		}
		b.WriteString(`<g class="` + esc(cls) + `">`)
		var run []string
		flush := func() {
			if len(run) > 1 {
				b.WriteString(`<polyline points="` + strings.Join(run, " ") + `"></polyline>`)
			}
			run = run[:0]
		}
		for i, p := range pts {
			if i > 0 && l.Gap > 0 && p.At.Sub(pts[i-1].At) > l.Gap {
				flush()
			}
			run = append(run, f(l.x(p.At))+","+f(y(p.Value)))
		}
		flush()
		// Many points: small dots, the last one full size.
		dense := len(pts) > 16
		for i, p := range pts {
			title := s.Name + " · " + p.At.Format("2 Jan") + " · " + Format(l.Unit, p.Value)
			if p.Note != "" {
				title += " · " + p.Note
			}
			x, py := f(l.x(p.At)), f(y(p.Value))
			r := float64(pointR)
			cls := "chart-point"
			if dense && i < len(pts)-1 {
				r, cls = 2, "chart-point dense"
			}
			if !l.SelectedAt.IsZero() && p.At.Equal(l.SelectedAt) && s.Name == l.SelectedSeries {
				r, cls = ringR, "chart-point selected"
			}
			mark := `<circle class="chart-hit" cx="` + x + `" cy="` + py + `" r="10"></circle><circle class="` + cls + `" cx="` + x + `" cy="` + py + `" r="` + f(r) + `"></circle><title>` + esc(title) + `</title>`
			if p.Href != "" {
				b.WriteString(`<a href="` + esc(p.Href) + `">` + mark + `</a>`)
			} else {
				b.WriteString(`<g>` + mark + `</g>`)
			}
		}
		b.WriteString(`</g>`)
		last := pts[len(pts)-1]
		ends = append(ends, endLabel{y(last.Value) - 8, s.Name + " " + Format(l.Unit, last.Value)})
	}
	// Direct labels at the right edge, nudged apart so they don't overlap.
	sort.Slice(ends, func(i, j int) bool { return ends[i].y < ends[j].y })
	for i := range ends {
		if ends[i].y < plotT+4 {
			ends[i].y = plotT + 4
		}
		if i > 0 && ends[i].y-ends[i-1].y < labelGap {
			ends[i].y = ends[i-1].y + labelGap
		}
		b.WriteString(`<text class="chart-value" x="` + f(plotR-10) + `" y="` + f(ends[i].y) + `" text-anchor="end">` + esc(ends[i].text) + `</text>`)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// dateTicks are up to five evenly spaced day labels across [from, to].
func dateTicks(from, to time.Time) []time.Time {
	span := to.Sub(from)
	if span <= 0 {
		return nil
	}
	days := int(span.Hours() / 24)
	step := 1
	for days/step > 4 {
		step++
	}
	if days > 20 {
		step = (step + 6) / 7 * 7 // whole weeks
	}
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	if start.Before(from) {
		start = start.AddDate(0, 0, 1)
	}
	var out []time.Time
	for t := start; !t.After(to); t = t.AddDate(0, 0, step) {
		out = append(out, t)
	}
	return out
}

// Table is one row per time, one column per series, newest first.
func (l Line) Table() Table {
	t := Table{Head: []string{"Date"}}
	vals := map[time.Time]map[int]float64{}
	for i, s := range l.Series {
		t.Head = append(t.Head, s.Name)
		for _, p := range s.Points {
			if vals[p.At] == nil {
				vals[p.At] = map[int]float64{}
			}
			vals[p.At][i] = p.Value
		}
	}
	var times []time.Time
	for at := range vals {
		times = append(times, at)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].After(times[j]) })
	for _, at := range times {
		row := []string{at.Format("2 Jan 2006")}
		for i := range l.Series {
			if v, ok := vals[at][i]; ok {
				row = append(row, Format(l.Unit, v))
			} else {
				row = append(row, "–")
			}
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}
