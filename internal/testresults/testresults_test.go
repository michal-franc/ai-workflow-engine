package testresults

import (
	"strings"
	"testing"
)

const goJSON = `{"Time":"2026-10-23T18:05:48Z","Action":"start","Package":"example.com/m"}
{"Time":"2026-10-23T18:05:48Z","Action":"run","Package":"example.com/m","Test":"TestA"}
{"Time":"2026-10-23T18:05:49Z","Action":"output","Package":"example.com/m","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Time":"2026-10-23T18:05:49Z","Action":"pass","Package":"example.com/m","Test":"TestA/sub","Elapsed":0.2}
{"Time":"2026-10-23T18:05:49Z","Action":"pass","Package":"example.com/m","Test":"TestA","Elapsed":0.5}
{"Time":"2026-10-23T18:05:50Z","Action":"fail","Package":"example.com/m","Test":"TestB","Elapsed":1.5}
{"Time":"2026-10-23T18:05:50Z","Action":"fail","Package":"example.com/m","Elapsed":2}
{"Time":"2026-10-23T18:05:48Z","Action":"skip","Package":"example.com/m/internal/x","Test":"TestC","Elapsed":0}
{"Time":"2026-10-23T18:06:18Z","Action":"pass","Package":"example.com/m/cmd/tool","Test":"TestD","Elapsed":9}
{"Time":"2026-10-23T18:06:18Z","Action":"pass","Package":"example.com/m/cmd/tool","Elapsed":30}
{"Time":"2026-10-23T18:05:48Z","Action":"skip","Package":"example.com/m/internal/empty","Elapsed":0}
`

const trx = `<?xml version="1.0" encoding="utf-8"?>
<TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010">
  <Times creation="2026-10-23T18:00:00.000+02:00" start="2026-10-23T18:00:01.000+02:00" finish="2026-10-23T18:00:16.000+02:00" />
  <Results>
    <UnitTestResult testId="1" testName="Hits" duration="00:00:00.0100000" outcome="Passed" />
    <UnitTestResult testId="2" testName="Misses(3)" duration="00:00:01.5000000" outcome="Failed" />
    <UnitTestResult testId="3" testName="Later" duration="00:00:00" outcome="NotExecuted" />
  </Results>
  <TestDefinitions>
    <UnitTest id="1" name="Hits"><TestMethod className="MaszynkiTests.Combat.HitTests" name="Hits" /></UnitTest>
    <UnitTest id="2" name="Misses"><TestMethod className="MaszynkiTests.Combat.HitTests" name="Misses" /></UnitTest>
    <UnitTest id="3" name="Later"><TestMethod className="MaszynkiTests.SaveTests" name="Later" /></UnitTest>
  </TestDefinitions>
</TestRun>`

const junit = `<testsuites>
  <testsuite name="tests.test_api" time="1.25">
    <testcase classname="tests.test_api" name="test_ok" time="0.25"/>
    <testcase classname="tests.test_api" name="test_bad" time="1"><failure message="x"/></testcase>
    <testcase classname="tests.test_api" name="test_later" time="0"><skipped/></testcase>
  </testsuite>
  <testsuite name="pkg" time="3"><testsuite name="pkg.inner" time="3">
    <testcase classname="pkg.Inner" name="deep" time="3"/>
  </testsuite></testsuite>
</testsuites>`

const playtest = `{"started":"2026-10-23T18:00:00Z","finished":"2026-10-23T18:03:10Z","sessions":[
 {"session":"approve-words","issue":"ui/approve-button","duration_ms":60000,"steps":[
  {"id":"TP1","status":"pass","duration_ms":20000},{"id":"TP2","status":"pending","duration_ms":10000},{"id":"TP3","status":"fail","duration_ms":30000}]},
 {"session":"stats","issue":"workflow/stats","duration_ms":130000,"steps":[{"id":"TP1","status":"pass","duration_ms":130000}]}]}`

func TestParse(t *testing.T) {
	cases := []struct {
		format, data                    string
		tests, subtests, passed, failed int
		skipped                         int
		wallS, sumS, maxS               float64
		firstPart                       string
		firstPartS                      float64
		areas                           map[string]int
	}{
		{FormatGoTestJSON, goJSON, 4, 1, 2, 1, 1, 30, 11, 9, "cmd/tool", 30, map[string]int{".": 2, "internal": 1, "cmd": 1}},
		{FormatTRX, trx, 3, 0, 1, 1, 1, 15, 1.51, 1.5, "MaszynkiTests.Combat.HitTests", 1.51, map[string]int{"Combat": 2, ".": 1}},
		{FormatJUnit, junit, 4, 0, 2, 1, 1, 3, 4.25, 3, "pkg.inner", 3, map[string]int{"tests": 3, "pkg": 1}},
		{FormatPlaytest, playtest, 4, 0, 3, 1, 0, 190, 190, 130, "stats", 130, map[string]int{"ui": 3, "workflow": 1}},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			s, err := Parse(c.format, []byte(c.data))
			if err != nil {
				t.Fatal(err)
			}
			sum := Summarize(s)
			if sum.Tests != c.tests || sum.Subtests != c.subtests || sum.Passed != c.passed || sum.Failed != c.failed || sum.Skipped != c.skipped {
				t.Errorf("counts = %d tests, %d sub, %d/%d/%d; want %d, %d, %d/%d/%d", sum.Tests, sum.Subtests, sum.Passed, sum.Failed, sum.Skipped, c.tests, c.subtests, c.passed, c.failed, c.skipped)
			}
			if sum.WallS != c.wallS || sum.SumS != c.sumS || sum.MaxS != c.maxS {
				t.Errorf("wall %v sum %v max %v; want %v %v %v", sum.WallS, sum.SumS, sum.MaxS, c.wallS, c.sumS, c.maxS)
			}
			if len(sum.Parts) == 0 || sum.Parts[0].Name != c.firstPart || sum.Parts[0].Seconds != c.firstPartS {
				t.Errorf("slowest part = %+v, want %s %v", sum.Parts, c.firstPart, c.firstPartS)
			}
			areas := map[string]int{}
			for _, tt := range s.Tests {
				areas[tt.Area]++
			}
			for k, v := range c.areas {
				if areas[k] != v {
					t.Errorf("areas = %v, want %v", areas, c.areas)
					break
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, c := range []struct{ format, data, want string }{
		{FormatGoTestJSON, "ok  \texample.com/m\t0.1s\n", "-json"},
		{FormatTRX, "<TestRun/>", "no UnitTestResult"},
		{FormatJUnit, "not xml", "junit"},
		{FormatPlaytest, `{"sessions":[]}`, "no sessions"},
		{"tap", "", "unknown results format"},
	} {
		_, err := Parse(c.format, []byte(c.data))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", c.format, err, c.want)
		}
	}
}

func TestPercentileNearestRank(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := Percentile(v, 50); got != 5 {
		t.Errorf("p50 = %v", got)
	}
	if got := Percentile(v, 90); got != 9 {
		t.Errorf("p90 = %v", got)
	}
	if got := Percentile(nil, 90); got != 0 {
		t.Errorf("empty = %v", got)
	}
}

func TestSummaryStats(t *testing.T) {
	s := &Suite{Tests: []Test{{Seconds: 0.01, Outcome: Pass}, {Seconds: 0.03, Outcome: Pass}, {Seconds: 0.2, Outcome: Pass}}}
	sum := Summarize(s)
	if sum.MeanMS != 80 || sum.P50MS != 30 || sum.P90MS != 200 {
		t.Errorf("mean %v p50 %v p90 %v", sum.MeanMS, sum.P50MS, sum.P90MS)
	}
}
