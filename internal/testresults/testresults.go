// Package testresults reads machine-readable test results (go test -json,
// TRX, JUnit XML and the playtest e2e results) into one shape: every test
// with its area, part, time and outcome, plus the suite's wall time. Local CI
// parses the files a ci.run command writes into $CI_RESULTS_DIR, and the
// health tests family reads the summaries. See docs/workbench-files.md,
// "Test results".
package testresults

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Formats Parse reads.
const (
	FormatGoTestJSON = "go-test-json"
	FormatTRX        = "trx"
	FormatJUnit      = "junit"
	FormatPlaytest   = "playtest"
)

// Outcomes.
const (
	Pass = "pass"
	Fail = "fail"
	Skip = "skip"
)

// Test is one top-level test (a Go Test function, a TRX result, a JUnit
// testcase, a playtest step).
type Test struct {
	Name    string  `json:"name"`
	Area    string  `json:"area,omitempty"`
	Part    string  `json:"part,omitempty"`
	Seconds float64 `json:"s"`
	Outcome string  `json:"outcome"`
}

// Part is a unit that runs as a whole: a Go package, a test class or suite,
// a playtest session. Seconds is its own wall time.
type Part struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"s"`
}

// Suite is one results file, parsed.
type Suite struct {
	Format   string
	Tests    []Test
	Subtests int
	Parts    []Part
	// Wall is the suite's own wall time: first to last event, or the
	// file's start/finish times. Zero when the file has neither.
	Wall time.Duration
}

// Parse reads data in format.
func Parse(format string, data []byte) (*Suite, error) {
	var s *Suite
	var err error
	switch format {
	case FormatGoTestJSON:
		s, err = parseGoTest(data)
	case FormatTRX:
		s, err = parseTRX(data)
	case FormatJUnit:
		s, err = parseJUnit(data)
	case FormatPlaytest:
		s, err = parsePlaytest(data)
	default:
		return nil, fmt.Errorf("unknown results format %q", format)
	}
	if err != nil {
		return nil, err
	}
	s.Format = format
	return s, nil
}

// Summary is what a run record keeps per kind (.ci/runs/<sha>.json,
// "results"), and what the health tests family reads.
type Summary struct {
	Format   string  `json:"format"`
	File     string  `json:"file,omitempty"`
	Tests    int     `json:"tests"`
	Subtests int     `json:"subtests,omitempty"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	Skipped  int     `json:"skipped"`
	WallS    float64 `json:"wall_s"`
	SumS     float64 `json:"sum_s"`
	MeanMS   float64 `json:"mean_ms"`
	P50MS    float64 `json:"p50_ms"`
	P90MS    float64 `json:"p90_ms"`
	MaxS     float64 `json:"max_s"`
	// Parts are the slowest parts, slowest first (at most TopParts).
	Parts []Part `json:"parts,omitempty"`
	// List is the per-test list file, relative to the project root.
	List  string `json:"list,omitempty"`
	Error string `json:"error,omitempty"`
}

// TopParts caps Summary.Parts.
const TopParts = 10

// Summarize counts s.
func Summarize(s *Suite) Summary {
	sum := Summary{Format: s.Format, Tests: len(s.Tests), Subtests: s.Subtests}
	secs := make([]float64, 0, len(s.Tests))
	for _, t := range s.Tests {
		switch t.Outcome {
		case Pass:
			sum.Passed++
		case Fail:
			sum.Failed++
		case Skip:
			sum.Skipped++
		}
		sum.SumS += t.Seconds
		secs = append(secs, t.Seconds)
	}
	sort.Float64s(secs)
	if n := len(secs); n > 0 {
		sum.MeanMS = round(sum.SumS / float64(n) * 1000)
		sum.P50MS = round(Percentile(secs, 50) * 1000)
		sum.P90MS = round(Percentile(secs, 90) * 1000)
		sum.MaxS = round(secs[n-1])
	}
	sum.SumS = round(sum.SumS)
	sum.WallS = round(s.Wall.Seconds())
	if sum.WallS == 0 {
		// No timestamps: the slowest part is the best guess of the wall.
		for _, p := range s.Parts {
			sum.WallS = max(sum.WallS, round(p.Seconds))
		}
	}
	parts := append([]Part(nil), s.Parts...)
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].Seconds > parts[j].Seconds })
	if len(parts) > TopParts {
		parts = parts[:TopParts]
	}
	for i := range parts {
		parts[i].Seconds = round(parts[i].Seconds)
	}
	sum.Parts = parts
	return sum
}

// Percentile is the nearest-rank percentile of sorted values (0 when empty).
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	rank = min(max(rank, 1), len(sorted))
	return sorted[rank-1]
}

// round keeps three decimals, so summaries stay readable.
func round(f float64) float64 { return math.Round(f*1000) / 1000 }

// partsFrom sums test time per part, for formats without part timings.
func partsFrom(tests []Test) []Part {
	by := map[string]float64{}
	var order []string
	for _, t := range tests {
		if _, ok := by[t.Part]; !ok {
			order = append(order, t.Part)
		}
		by[t.Part] += t.Seconds
	}
	out := make([]Part, 0, len(order))
	for _, name := range order {
		out = append(out, Part{Name: name, Seconds: by[name]})
	}
	return out
}
