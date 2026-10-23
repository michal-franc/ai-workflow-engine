package chart

import (
	"html/template"
	"math"
	"strings"
)

// Segment is one part of a stacked bar.
type Segment struct {
	Name  string // legend name: "agent working", "feature"
	Class string
	Value float64
}

// Bar is one stacked bar: a row of HBar or a column of Columns. Href makes
// it a link (its issues); Selected outlines it.
type Bar struct {
	Label    string
	Segments []Segment
	Href     string
	Selected bool
	// SelectedSegment outlines one segment (by Name) instead of the bar.
	SelectedSegment string
	// SegmentHref links each segment by Name; it wins over Href.
	SegmentHref map[string]string
}

func (b Bar) total() float64 {
	t := 0.0
	for _, s := range b.Segments {
		t += s.Value
	}
	return t
}

// AsShare is the bar with each segment as its share of the bar's total.
func (b Bar) AsShare() Bar {
	t := b.total()
	out := b
	out.Segments = make([]Segment, len(b.Segments))
	for i, s := range b.Segments {
		out.Segments[i] = s
		if t > 0 {
			out.Segments[i].Value = s.Value / t
		}
	}
	return out
}

// HBar is horizontal stacked bars, one row per Bar, the total at its end.
type HBar struct {
	Label string
	Unit  Unit
	Bars  []Bar
}

const (
	hbarLabelX, hbarX, hbarMaxW = 104.0, 112.0, 380.0
	hbarRow, hbarH              = 30.0, 18.0
	segGap, minSeg              = 2.0, 2.0
)

// SVG draws the rows.
func (h HBar) SVG() template.HTML {
	max := 0.0
	for _, b := range h.Bars {
		if t := b.total(); t > max {
			max = t
		}
	}
	if max <= 0 {
		max = 1
	}
	height := 10 + hbarRow*float64(len(h.Bars))
	var sb strings.Builder
	sb.WriteString(`<svg class="chart chart-hbar" viewBox="0 0 560 ` + f(height) + `" role="img" aria-label="` + esc(h.Label) + `">`)
	for i, b := range h.Bars {
		y := 10 + hbarRow*float64(i)
		sb.WriteString(`<text class="chart-label" x="` + f(hbarLabelX) + `" y="` + f(y+13) + `" text-anchor="end">` + esc(b.Label) + `</text>`)
		x := hbarX
		for _, s := range b.Segments {
			if s.Value <= 0 {
				continue
			}
			w := math.Max(minSeg, s.Value/max*hbarMaxW)
			sb.WriteString(segment(b, s, f(x), f(y), f(w), f(hbarH), h.Unit))
			x += w + segGap
		}
		sb.WriteString(`<text class="chart-value" x="` + f(x+4) + `" y="` + f(y+13) + `">` + esc(Format(h.Unit, b.total())) + `</text>`)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// segment draws one rect with its tooltip, linked when the bar or segment
// has a link.
func segment(b Bar, s Segment, x, y, w, h string, u Unit) string {
	cls := "chart-seg " + s.Class
	if (b.Selected && b.SelectedSegment == "") || (b.SelectedSegment != "" && b.SelectedSegment == s.Name) {
		cls += " selected"
	}
	r := `<rect class="` + esc(cls) + `" x="` + x + `" y="` + y + `" width="` + w + `" height="` + h + `" rx="3"><title>` + esc(b.Label+" · "+s.Name+" · "+Format(u, s.Value)) + `</title></rect>`
	href := b.Href
	if sh, ok := b.SegmentHref[s.Name]; ok {
		href = sh
	}
	if href != "" {
		return `<a href="` + esc(href) + `">` + r + `</a>`
	}
	return r
}

// Table is one row per bar, one column per segment name, then the total.
func (h HBar) Table() Table { return barTable(h.Bars, h.Unit, "") }

func barTable(bars []Bar, u Unit, first string) Table {
	var names []string
	seen := map[string]bool{}
	for _, b := range bars {
		for _, s := range b.Segments {
			if !seen[s.Name] {
				seen[s.Name] = true
				names = append(names, s.Name)
			}
		}
	}
	t := Table{Head: append(append([]string{first}, names...), "Total")}
	for _, b := range bars {
		row := []string{b.Label}
		for _, n := range names {
			v := 0.0
			for _, s := range b.Segments {
				if s.Name == n {
					v = s.Value
				}
			}
			row = append(row, Format(u, v))
		}
		t.Rows = append(t.Rows, append(row, Format(u, b.total())))
	}
	return t
}

// Columns is vertical stacked bars, one column per Bar (a week), bottom up.
type Columns struct {
	Label string
	Unit  Unit
	Bars  []Bar
}

// SVG draws the columns.
func (c Columns) SVG() template.HTML {
	max := 0.0
	for _, b := range c.Bars {
		if t := b.total(); t > max {
			max = t
		}
	}
	top, step := niceMax(c.Unit, max, lineTicks)
	y := func(v float64) float64 { return plotB - v/top*(plotB-plotT) }
	var sb strings.Builder
	sb.WriteString(`<svg class="chart chart-columns" viewBox="0 0 560 196" role="img" aria-label="` + esc(c.Label) + `">`)
	sb.WriteString(`<g class="chart-grid">`)
	for i := 0; i <= lineTicks; i++ {
		v := step * float64(i)
		sb.WriteString(`<line x1="` + f(plotL) + `" y1="` + f(y(v)) + `" x2="` + f(plotR) + `" y2="` + f(y(v)) + `"></line>`)
	}
	sb.WriteString(`</g>`)
	for i := 0; i <= lineTicks; i++ {
		v := step * float64(i)
		sb.WriteString(`<text class="chart-axis" x="` + f(plotL-6) + `" y="` + f(y(v)+3) + `" text-anchor="end">` + esc(Format(c.Unit, v)) + `</text>`)
	}
	n := len(c.Bars)
	if n == 0 {
		sb.WriteString(`</svg>`)
		return template.HTML(sb.String())
	}
	slot := (plotR - plotL) / float64(n)
	w := math.Min(48, slot*0.6)
	for i, b := range c.Bars {
		cx := plotL + slot*(float64(i)+0.5)
		sb.WriteString(`<text class="chart-axis" x="` + f(cx) + `" y="188" text-anchor="middle">` + esc(b.Label) + `</text>`)
		base := plotB
		for _, s := range b.Segments {
			if s.Value <= 0 {
				continue
			}
			h := math.Max(minSeg, s.Value/top*(plotB-plotT))
			sb.WriteString(segment(b, s, f(cx-w/2), f(base-h), f(w), f(h), c.Unit))
			base -= h + segGap
		}
		if t := b.total(); t > 0 {
			sb.WriteString(`<text class="chart-value" x="` + f(cx) + `" y="` + f(base-3) + `" text-anchor="middle">` + esc(Format(c.Unit, t)) + `</text>`)
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// Table is one row per column.
func (c Columns) Table() Table { return barTable(c.Bars, c.Unit, "") }

// Gauge is a half ring: Value against Max, with a tick at Limit (0: none).
// Class colours the value arc.
type Gauge struct {
	Label string
	Value float64
	Max   float64
	Limit float64
	Class string
}

// gauge geometry: centre (80, 86), radius 70.
const gcx, gcy, gr = 80.0, 86.0, 70.0

func gaugeAt(frac, r float64) (float64, float64) {
	a := math.Pi * (1 - frac)
	return gcx + r*math.Cos(a), gcy - r*math.Sin(a)
}

// Frac is how full the ring is, 0..1.
func (g Gauge) Frac() float64 {
	if g.Max <= 0 {
		return 0
	}
	return math.Max(0, math.Min(1, g.Value/g.Max))
}

// SVG draws the gauge.
func (g Gauge) SVG() template.HTML {
	var sb strings.Builder
	sb.WriteString(`<svg class="chart chart-gauge" viewBox="0 0 160 96" role="img" aria-label="` + esc(g.Label) + `">`)
	sb.WriteString(`<path class="chart-track" d="M10 86 A70 70 0 0 1 150 86"></path>`)
	if fr := g.Frac(); fr > 0 {
		x, y := gaugeAt(fr, gr)
		sb.WriteString(`<path class="chart-arc ` + esc(g.Class) + `" d="M10 86 A70 70 0 0 1 ` + f(x) + ` ` + f(y) + `"></path>`)
	}
	if g.Limit > 0 && g.Max > 0 {
		lf := math.Min(1, g.Limit/g.Max)
		x1, y1 := gaugeAt(lf, gr-10)
		x2, y2 := gaugeAt(lf, gr+10)
		sb.WriteString(`<line class="chart-tick" x1="` + f(x1) + `" y1="` + f(y1) + `" x2="` + f(x2) + `" y2="` + f(y2) + `"></line>`)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// Spark is a small line without axes; NaN values are gaps.
type Spark struct {
	Label  string
	Values []float64
	Class  string
}

// SVG draws the sparkline, 64 × 18.
func (s Spark) SVG() template.HTML {
	min, max := math.Inf(1), math.Inf(-1)
	for _, v := range s.Values {
		if math.IsNaN(v) {
			continue
		}
		min, max = math.Min(min, v), math.Max(max, v)
	}
	var sb strings.Builder
	sb.WriteString(`<svg class="chart chart-spark" width="64" height="18" viewBox="0 0 64 18" role="img" aria-label="` + esc(s.Label) + `">`)
	if !math.IsInf(min, 1) && len(s.Values) > 0 {
		x := func(i int) float64 {
			if len(s.Values) == 1 {
				return 63
			}
			return 1 + float64(i)/float64(len(s.Values)-1)*62
		}
		y := func(v float64) float64 {
			if max == min {
				return 9
			}
			return 16 - (v-min)/(max-min)*14
		}
		sb.WriteString(`<g class="chart-series ` + esc(s.Class) + `">`)
		var run []string
		flush := func() {
			if len(run) > 1 {
				sb.WriteString(`<polyline points="` + strings.Join(run, " ") + `"></polyline>`)
			} else if len(run) == 1 {
				xy := strings.Split(run[0], ",")
				sb.WriteString(`<circle class="chart-point" cx="` + xy[0] + `" cy="` + xy[1] + `" r="1.5"></circle>`)
			}
			run = run[:0]
		}
		for i, v := range s.Values {
			if math.IsNaN(v) {
				flush()
				continue
			}
			run = append(run, f(x(i))+","+f(y(v)))
		}
		flush()
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
