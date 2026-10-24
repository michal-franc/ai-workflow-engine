package testresults

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// goEvent is one line of go test -json (test2json).
type goEvent struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Elapsed float64
}

// parseGoTest reads go test -json output. Top-level tests are Test events
// without a slash; subtests are counted. A package is a part, its elapsed
// time from the package's own pass/fail event. Areas are the package's top
// folder inside the module (".", cmd, internal).
func parseGoTest(data []byte) (*Suite, error) {
	s := &Suite{}
	var first, last time.Time
	var pkgs []string
	pkgSeen := map[string]bool{}
	type key struct{ pkg, test string }
	done := map[key]Test{}
	var order []key
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	lines := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue // a build error printed outside the stream
		}
		var e goEvent
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		lines++
		if !e.Time.IsZero() {
			if first.IsZero() || e.Time.Before(first) {
				first = e.Time
			}
			if e.Time.After(last) {
				last = e.Time
			}
		}
		if e.Package != "" && !pkgSeen[e.Package] {
			pkgSeen[e.Package] = true
			pkgs = append(pkgs, e.Package)
		}
		outcome := goOutcome(e.Action)
		if outcome == "" {
			continue
		}
		if e.Test == "" {
			if e.Elapsed > 0 || outcome != Skip {
				s.Parts = append(s.Parts, Part{Name: e.Package, Seconds: e.Elapsed})
			}
			continue
		}
		if strings.Contains(e.Test, "/") {
			s.Subtests++
			continue
		}
		k := key{e.Package, e.Test}
		if _, ok := done[k]; !ok {
			order = append(order, k)
		}
		done[k] = Test{Name: e.Test, Part: e.Package, Seconds: e.Elapsed, Outcome: outcome}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if lines == 0 {
		return nil, errors.New("no go test -json events (was the command run with -json?)")
	}
	module := modulePath(pkgs)
	rel := func(pkg string) string {
		r := strings.TrimPrefix(strings.TrimPrefix(pkg, module), "/")
		if r == "" {
			return "."
		}
		return r
	}
	for _, k := range order {
		t := done[k]
		t.Part = rel(t.Part)
		t.Area = topFolder(t.Part)
		s.Tests = append(s.Tests, t)
	}
	for i := range s.Parts {
		s.Parts[i].Name = rel(s.Parts[i].Name)
	}
	if !first.IsZero() {
		s.Wall = last.Sub(first)
	}
	return s, nil
}

func goOutcome(action string) string {
	switch action {
	case "pass":
		return Pass
	case "fail":
		return Fail
	case "skip":
		return Skip
	}
	return ""
}

// modulePath is the import path every package shares: the shortest package
// that prefixes all others, else their common folder.
func modulePath(pkgs []string) string {
	if len(pkgs) == 0 {
		return ""
	}
	prefix := pkgs[0]
	for _, p := range pkgs[1:] {
		for !(p == prefix || strings.HasPrefix(p, prefix+"/")) {
			i := strings.LastIndex(prefix, "/")
			if i < 0 {
				return ""
			}
			prefix = prefix[:i]
		}
	}
	return prefix
}

func topFolder(p string) string {
	if i := strings.IndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return p
}

// TRX (dotnet test --logger trx).
type trxRun struct {
	Times struct {
		Start  string `xml:"start,attr"`
		Finish string `xml:"finish,attr"`
	} `xml:"Times"`
	Results struct {
		Items []struct {
			TestID   string `xml:"testId,attr"`
			TestName string `xml:"testName,attr"`
			Duration string `xml:"duration,attr"`
			Outcome  string `xml:"outcome,attr"`
		} `xml:"UnitTestResult"`
	} `xml:"Results"`
	Definitions struct {
		Tests []struct {
			ID     string `xml:"id,attr"`
			Method struct {
				ClassName string `xml:"className,attr"`
			} `xml:"TestMethod"`
		} `xml:"UnitTest"`
	} `xml:"TestDefinitions"`
}

// parseTRX reads a Visual Studio TRX file. A test class is a part; the area
// is the namespace segment after the test assembly's root
// (MaszynkiTests.Combat.HitTests: Combat).
func parseTRX(data []byte) (*Suite, error) {
	var run trxRun
	if err := xml.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("trx: %w", err)
	}
	class := map[string]string{}
	for _, d := range run.Definitions.Tests {
		class[d.ID] = d.Method.ClassName
	}
	s := &Suite{}
	for _, r := range run.Results.Items {
		cls := class[r.TestID]
		s.Tests = append(s.Tests, Test{
			Name:    r.TestName,
			Part:    cls,
			Area:    namespaceArea(cls),
			Seconds: trxDuration(r.Duration),
			Outcome: trxOutcome(r.Outcome),
		})
	}
	if len(s.Tests) == 0 && len(run.Definitions.Tests) == 0 {
		return nil, errors.New("trx: no UnitTestResult entries")
	}
	s.Parts = partsFrom(s.Tests)
	start, err1 := time.Parse(time.RFC3339Nano, run.Times.Start)
	finish, err2 := time.Parse(time.RFC3339Nano, run.Times.Finish)
	if err1 == nil && err2 == nil && finish.After(start) {
		s.Wall = finish.Sub(start)
	}
	return s, nil
}

func trxOutcome(o string) string {
	switch strings.ToLower(o) {
	case "passed", "passedbutrunaborted", "warning":
		return Pass
	case "notexecuted", "inconclusive", "notrunnable", "pending", "disconnected":
		return Skip
	}
	return Fail
}

// trxDuration reads hh:mm:ss.fffffff.
func trxDuration(d string) float64 {
	parts := strings.Split(d, ":")
	if len(parts) != 3 {
		return 0
	}
	h, _ := strconv.ParseFloat(parts[0], 64)
	m, _ := strconv.ParseFloat(parts[1], 64)
	sec, _ := strconv.ParseFloat(parts[2], 64)
	return h*3600 + m*60 + sec
}

// namespaceArea is the segment after the root namespace, "." when the class
// sits directly in it.
func namespaceArea(class string) string {
	seg := strings.Split(class, ".")
	if len(seg) >= 3 {
		return seg[1]
	}
	return "."
}

// JUnit XML (pytest, jest-junit, NUnit's JUnit logger, ...).
type junitSuite struct {
	Name   string       `xml:"name,attr"`
	Time   string       `xml:"time,attr"`
	Suites []junitSuite `xml:"testsuite"`
	Cases  []struct {
		Name      string    `xml:"name,attr"`
		ClassName string    `xml:"classname,attr"`
		Time      string    `xml:"time,attr"`
		Failure   *struct{} `xml:"failure"`
		Error     *struct{} `xml:"error"`
		Skipped   *struct{} `xml:"skipped"`
	} `xml:"testcase"`
}

// parseJUnit reads <testsuites> or a lone <testsuite>. A testsuite is a part
// (its time attribute); the area is the classname's first segment.
func parseJUnit(data []byte) (*Suite, error) {
	var root junitSuite
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("junit: %w", err)
	}
	s := &Suite{}
	var walk func(js junitSuite)
	walk = func(js junitSuite) {
		if len(js.Cases) > 0 {
			s.Parts = append(s.Parts, Part{Name: js.Name, Seconds: atof(js.Time)})
		}
		for _, c := range js.Cases {
			out := Pass
			switch {
			case c.Failure != nil || c.Error != nil:
				out = Fail
			case c.Skipped != nil:
				out = Skip
			}
			s.Tests = append(s.Tests, Test{Name: c.Name, Part: js.Name, Area: topSegment(c.ClassName), Seconds: atof(c.Time), Outcome: out})
		}
		for _, sub := range js.Suites {
			walk(sub)
		}
	}
	walk(root)
	if len(s.Tests) == 0 && len(s.Parts) == 0 {
		return nil, errors.New("junit: no testcase entries")
	}
	return s, nil
}

func topSegment(class string) string {
	if class == "" {
		return "."
	}
	if i := strings.IndexAny(class, "./"); i > 0 {
		return class[:i]
	}
	return class
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 64)
	return f
}

// Playtest is the e2e results file a playtest runner writes into
// $CI_RESULTS_DIR: every session it ran, each step's status and time. The
// shared playtest contract (docs/patterns/playtest/README.md); any project's
// runner can write it.
type Playtest struct {
	Started  time.Time         `json:"started"`
	Finished time.Time         `json:"finished"`
	Sessions []PlaytestSession `json:"sessions"`
}

// PlaytestSession is one session's run.
type PlaytestSession struct {
	Session    string         `json:"session"`
	Issue      string         `json:"issue,omitempty"`
	DurationMS int64          `json:"duration_ms"`
	Steps      []PlaytestStep `json:"steps"`
}

// PlaytestStep is one step: pass, fail, skip or pending (judgement only).
type PlaytestStep struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
}

// parsePlaytest reads the playtest e2e results. A step is a test, its
// session the part, the issue slug's first segment the area.
func parsePlaytest(data []byte) (*Suite, error) {
	var p Playtest
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("playtest: %w", err)
	}
	if len(p.Sessions) == 0 {
		return nil, errors.New("playtest: no sessions")
	}
	s := &Suite{}
	for _, ses := range p.Sessions {
		s.Parts = append(s.Parts, Part{Name: ses.Session, Seconds: float64(ses.DurationMS) / 1000})
		for _, st := range ses.Steps {
			out := st.Status
			switch out {
			case Pass, Fail:
			case "pending":
				out = Pass // judgement only: its setup ran cleanly
			default:
				out = Skip
			}
			s.Tests = append(s.Tests, Test{Name: ses.Session + "/" + st.ID, Part: ses.Session, Area: topFolder(ses.Issue), Seconds: float64(st.DurationMS) / 1000, Outcome: out})
		}
	}
	if !p.Started.IsZero() && p.Finished.After(p.Started) {
		s.Wall = p.Finished.Sub(p.Started)
	}
	return s, nil
}
