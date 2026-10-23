package chart

import (
	"encoding/xml"
	"math"
	"strings"
	"testing"
	"time"
)

// wellFormed parses the SVG as XML, so a stray quote or unclosed tag fails.
func wellFormed(t *testing.T, svg string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(svg))
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return
			}
			t.Fatalf("not well formed: %v\n%s", err, svg)
		}
	}
}

func TestFormat(t *testing.T) {
	for _, c := range []struct {
		u    Unit
		v    float64
		want string
	}{
		{Seconds, 88 * 60, "1h 28m"},
		{Seconds, 34 * 60, "34m"},
		{Seconds, 2*86400 + 4*3600, "2d 4h"},
		{Seconds, 3600, "1h"},
		{Share, 0.523, "52%"},
		{Count, 96705, "96.7k"},
		{Count, 237086, "237k"},
		{Count, 950, "950"},
		{Count, 0.33, "0.33"},
		{Hours, 1458 * 3600, "1 458h"},
		{Hours, 1.8 * 3600, "1.8h"},
	} {
		if got := Format(c.u, c.v); got != c.want {
			t.Errorf("Format(%d, %v) = %q, want %q", c.u, c.v, got, c.want)
		}
	}
}

func TestNiceMax(t *testing.T) {
	if top, step := niceMax(Share, 0.52, 3); top != 0.6 || step != 0.2 {
		t.Errorf("share 0.52: %v/%v", top, step)
	}
	if top, step := niceMax(Seconds, 88*60, 3); top != 5400 || step != 1800 {
		t.Errorf("seconds 88m: %v/%v", top, step)
	}
	if top, _ := niceMax(Count, 96705, 4); top != 100000 {
		t.Errorf("count 96705: %v", top)
	}
}

func TestLine(t *testing.T) {
	to := time.Date(2026, 10, 23, 23, 59, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)
	l := Line{
		Label: "Cycle time p50 by type", From: from, To: to, Unit: Seconds, Gap: 36 * time.Hour,
		Limit: 7200, LimitLabel: "limit 2h", MarkerAt: to.AddDate(0, 0, -1), MarkerText: "first issues with CLI calls",
		Series: []Series{
			{Name: "feature", Class: "s-feature", Points: []Point{
				{At: to.AddDate(0, 0, -5), Value: 2700},
				{At: to.AddDate(0, 0, -1), Value: 2700, Href: "/p/x/stats?drill=a"},
				{At: to, Value: 5280, Note: "13 issues"},
			}},
			{Name: "bugfix", Class: "s-bugfix", Points: []Point{{At: to, Value: 660}}},
		},
		SelectedAt: to, SelectedSeries: "feature",
	}
	svg := string(l.SVG())
	wellFormed(t, svg)
	for _, want := range []string{
		`role="img" aria-label="Cycle time p50 by type"`,
		`<title>feature · 23 Oct · 1h 28m · 13 issues</title>`,
		`<a href="/p/x/stats?drill=a">`,
		`class="chart-series s-feature"`,
		`class="chart-point selected"`,
		`>feature 1h 28m</text>`,
		`>bugfix 11m</text>`,
		`class="chart-limit"`, `>limit 2h</text>`,
		`>first issues with CLI calls</text>`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG lacks %q", want)
		}
	}
	// The 4-day gap breaks the feature line: one polyline (the last two
	// points), not one through all three.
	if n := strings.Count(svg, "<polyline"); n != 1 {
		t.Errorf("polylines = %d, want 1 (gap broken, lone bugfix point drawn as a dot)", n)
	}
	if strings.Contains(svg, "#") {
		t.Error("SVG carries a hex colour; colours must come from classes")
	}
	tab := l.Table()
	if strings.Join(tab.Head, "|") != "Date|feature|bugfix" || tab.Rows[0][0] != "23 Oct 2026" || tab.Rows[0][1] != "1h 28m" || tab.Rows[1][2] != "–" {
		t.Errorf("table = %+v", tab)
	}
	if l.Points() != 4 || l.Empty() || !(Line{}).Empty() {
		t.Error("Points/Empty wrong")
	}
}

func TestBars(t *testing.T) {
	h := HBar{Label: "Time by status", Unit: Hours, Bars: []Bar{
		{Label: "human-testing", Href: "/x?drill=status:human-testing", Segments: []Segment{
			{Name: "agent working", Class: ClassAgent, Value: 20 * 3600},
			{Name: "waiting on you", Class: ClassWaiting, Value: 1438 * 3600},
		}},
		{Label: "testing", Segments: []Segment{{Name: "agent working", Class: ClassAgent, Value: 0.5 * 3600}, {Name: "idle", Class: ClassIdle, Value: 0}}},
	}}
	svg := string(h.SVG())
	wellFormed(t, svg)
	for _, want := range []string{`>1 458h</text>`, `<title>human-testing · waiting on you · 1 438h</title>`, `<a href="/x?drill=status:human-testing">`, `class="chart-seg s-agent"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("hbar lacks %q", want)
		}
	}
	if strings.Count(svg, "<rect") != 3 {
		t.Errorf("rects = %d, want 3 (a zero segment is not drawn)", strings.Count(svg, "<rect"))
	}
	tab := h.Table()
	if strings.Join(tab.Head, "|") != "|agent working|waiting on you|idle|Total" || tab.Rows[1][3] != "0h" {
		t.Errorf("table = %+v", tab)
	}
	share := h.Bars[0].AsShare()
	if math.Abs(share.Segments[1].Value-1438.0/1458) > 1e-9 {
		t.Errorf("share = %v", share.Segments[1].Value)
	}

	c := Columns{Label: "Issues done per week", Unit: Count, Bars: []Bar{
		{Label: "W40", Segments: []Segment{{Name: "none", Class: "s-none", Value: 3}}},
		{Label: "W43", SelectedSegment: "feature", SegmentHref: map[string]string{"feature": "/x?drill=week:2026-W43:feature"}, Segments: []Segment{{Name: "feature", Class: "s-feature", Value: 13}, {Name: "bugfix", Class: "s-bugfix", Value: 10}}},
	}}
	svg = string(c.SVG())
	wellFormed(t, svg)
	for _, want := range []string{`>W43</text>`, `>23</text>`, `class="chart-seg s-feature selected"`, `<a href="/x?drill=week:2026-W43:feature">`} {
		if !strings.Contains(svg, want) {
			t.Errorf("columns lack %q", want)
		}
	}
}

func TestGaugeAndSpark(t *testing.T) {
	g := Gauge{Label: "Waiting on you: 52%, limit 25%", Value: 0.52, Max: 1, Limit: 0.25, Class: ClassWaiting}
	svg := string(g.SVG())
	wellFormed(t, svg)
	if !strings.Contains(svg, `class="chart-arc s-waiting"`) || !strings.Contains(svg, `class="chart-tick"`) {
		t.Errorf("gauge = %s", svg)
	}
	if (Gauge{Value: 5, Max: 0}).Frac() != 0 || (Gauge{Value: 30, Max: 20}).Frac() != 1 {
		t.Error("Frac not clamped")
	}
	s := Spark{Label: "Lines of code", Values: []float64{1, 2, math.NaN(), 4}, Class: "s-all"}
	svg = string(s.SVG())
	wellFormed(t, svg)
	if strings.Count(svg, "<polyline") != 1 || strings.Count(svg, "<circle") != 1 {
		t.Errorf("spark = %s", svg)
	}
	wellFormed(t, string(Spark{}.SVG()))
}

func TestTypeClass(t *testing.T) {
	others := []string{"feature", "research", "spike", "chore"}
	if TypeClass("feature", others) != "s-feature" || TypeClass("research", others) != "s-cat5" || TypeClass("spike", others) != "s-cat6" || TypeClass("chore", others) != "s-other" {
		t.Errorf("classes: %s %s %s %s", TypeClass("feature", others), TypeClass("research", others), TypeClass("spike", others), TypeClass("chore", others))
	}
}

func TestDateTicks(t *testing.T) {
	to := time.Date(2026, 10, 23, 23, 59, 0, 0, time.UTC)
	ticks := dateTicks(to.AddDate(0, 0, -30), to)
	if len(ticks) < 3 || len(ticks) > 6 {
		t.Errorf("30-day ticks = %v", ticks)
	}
	if got := dateTicks(to.AddDate(0, 0, -7), to); len(got) < 3 {
		t.Errorf("7-day ticks = %v", got)
	}
}
