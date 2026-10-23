// Package chart draws the stats page's charts as inline SVG on the server:
// lines over time, stacked bars, gauges and sparklines. No script draws
// them, so they work under a strict CSP and without JavaScript.
//
// Colours are CSS classes, never hex: a series or segment carries a class
// (s-agent, s-feature, ...) and style.css maps it to the theme's token, so
// every theme recolours the charts. Marks use currentColor.
//
// Every chart is accessible on its own: role="img" with an aria-label on the
// svg, a <title> on each mark (also the hover tooltip), and Table() gives
// the same numbers as rows for the page's table view.
package chart

import (
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"
)

// Unit says how values are formatted.
type Unit int

const (
	Count   Unit = iota // 96.7k
	Seconds             // 1h 28m
	Share               // 52%
	Hours               // 1 458h
)

// Format renders v in unit u.
func Format(u Unit, v float64) string {
	switch u {
	case Seconds:
		return Duration(time.Duration(v * float64(time.Second)))
	case Share:
		return strconv.Itoa(int(math.Round(v*100))) + "%"
	case Hours:
		h := v / 3600
		if h < 10 {
			return strconv.FormatFloat(math.Round(h*10)/10, 'f', -1, 64) + "h"
		}
		return thousands(int(math.Round(h))) + "h"
	default:
		return compact(v)
	}
}

// Duration is d as the stats page writes it: 34m, 1h 28m, 2d 4h.
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return strconv.Itoa(h) + "h"
		}
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		days, h := int(d.Hours())/24, int(d.Hours())%24
		if h == 0 {
			return strconv.Itoa(days) + "d"
		}
		return fmt.Sprintf("%dd %dh", days, h)
	}
}

// compact is 950, 9.5k, 96.7k, 237k, 1.2M.
func compact(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e6:
		return trimZero(v/1e6) + "M"
	case a >= 1e5:
		return strconv.Itoa(int(math.Round(v/1e3))) + "k"
	case a >= 1e3:
		return trimZero(v/1e3) + "k"
	case a == math.Trunc(a):
		return strconv.Itoa(int(v))
	default:
		return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
	}
}

func trimZero(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }

// thousands groups digits with a thin space: 1 458.
func thousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Table is a chart's numbers as rows, for the table view.
type Table struct {
	Head []string
	Rows [][]string
}

// esc escapes text for SVG content and attributes.
func esc(s string) string { return template.HTMLEscapeString(s) }

// f writes a coordinate with at most one decimal.
func f(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }

// niceMax is the axis top for values up to max: a 1, 2, 2.5 or 5 step times
// a power of ten, with ticks steps apart. Durations snap to clock steps.
func niceMax(u Unit, max float64, ticks int) (top, step float64) {
	if max <= 0 {
		max = 1
	}
	if u == Share {
		for _, s := range []float64{0.05, 0.1, 0.2, 0.25} {
			if s*float64(ticks) >= max {
				return math.Round(s*float64(ticks)*1000) / 1000, s
			}
		}
		return 1, 1 / float64(ticks)
	}
	if u == Seconds {
		for _, s := range []float64{60, 300, 600, 900, 1800, 3600, 7200, 3 * 3600, 6 * 3600, 12 * 3600, 86400, 2 * 86400, 7 * 86400, 14 * 86400, 30 * 86400} {
			if s*float64(ticks) >= max {
				return s * float64(ticks), s
			}
		}
	}
	raw := max / float64(ticks)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*mag >= raw {
			step = m * mag
			break
		}
	}
	return step * float64(ticks), step
}

// The time split's classes: agent working, waiting on the owner, idle.
const (
	ClassAgent   = "s-agent"
	ClassWaiting = "s-waiting"
	ClassIdle    = "s-idle"
)

// typeClasses are the fixed hues of the types of work; any other type takes
// the next free slot in order, and past the slots it is "s-other". A type
// keeps its colour whatever else is on the chart.
var typeClasses = map[string]string{
	"feature": "s-feature",
	"bugfix":  "s-bugfix",
	"tweak":   "s-tweak",
	"epic":    "s-epic",
	"none":    "s-none",
	"all":     "s-all",
}

var extraClasses = []string{"s-cat5", "s-cat6"}

// TypeClass is the class for a type of work. others are the chart's other
// unknown types, in display order, so an unknown type's slot is stable for
// one chart.
func TypeClass(typ string, others []string) string {
	if c, ok := typeClasses[typ]; ok {
		return c
	}
	n := 0
	for _, o := range others {
		if _, known := typeClasses[o]; known {
			continue
		}
		if o == typ {
			if n < len(extraClasses) {
				return extraClasses[n]
			}
			break
		}
		n++
	}
	return "s-other"
}
