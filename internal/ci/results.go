package ci

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/michal-franc/issue-viewer/internal/testresults"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// ResultsEnv names the folder every ci.run command may write its test
// results into; the command's results: entries say which files to read.
const ResultsEnv = "CI_RESULTS_DIR"

// ResultsDir is one run's results folder, .ci/results/<sha>[.<tier>].
func ResultsDir(root, sha, tier string) string {
	return filepath.Join(Dir(root), "results", runName(sha, tier))
}

// prepareResultsDir empties and creates the run's results folder, so a file
// left by an earlier run of the same commit never counts.
func prepareResultsDir(root, sha, tier string) (string, error) {
	dir := ResultsDir(root, sha, tier)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := ensureDir(root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// collectResults parses the files c's results: entries name, after c ran
// (red or green), into run.Results, and writes each kind's per-test list next
// to them. A missing or broken file is recorded as the summary's error and
// never changes the verdict.
func collectResults(root string, run *Run, c tracker.CICommand, dir string, out io.Writer) {
	for _, r := range c.Results {
		sum := parseResult(root, dir, r)
		if run.Results == nil {
			run.Results = map[string]testresults.Summary{}
		}
		run.Results[r.Kind] = sum
		if sum.Error != "" {
			fmt.Fprintf(out, "results: %s %s: %s\n", r.Kind, r.File, sum.Error)
			continue
		}
		fmt.Fprintf(out, "results: %s %d tests, %d failed, %.1f s wall (%s)\n", r.Kind, sum.Tests, sum.Failed, sum.WallS, r.File)
	}
}

func parseResult(root, dir string, r tracker.CIResult) testresults.Summary {
	path := filepath.Join(dir, r.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return testresults.Summary{Format: r.Format, File: r.File, Error: fmt.Sprintf("not written (the command must write $%s/%s)", ResultsEnv, r.File)}
	}
	suite, err := testresults.Parse(r.Format, data)
	if err != nil {
		return testresults.Summary{Format: r.Format, File: r.File, Error: err.Error()}
	}
	sum := testresults.Summarize(suite)
	sum.File = r.File
	list := filepath.Join(dir, r.Kind+".tests.json")
	if err := writeJSONFile(list, suite.Tests); err == nil {
		if rel, err := filepath.Rel(root, list); err == nil {
			sum.List = rel
		}
	}
	return sum
}

// LatestResults is the newest run started at or before t (zero: any time)
// with a readable summary for kind.
func LatestResults(root, kind string, t time.Time) (*Run, testresults.Summary, bool) {
	for _, r := range RecentRuns(root, 0) {
		if !t.IsZero() && r.Started.After(t) {
			continue
		}
		if s, ok := r.Results[kind]; ok && s.Error == "" {
			return &r, s, true
		}
	}
	return nil, testresults.Summary{}, false
}

// ReadTests reads a summary's per-test list; nil when it is gone.
func ReadTests(root string, s testresults.Summary) []testresults.Test {
	if s.List == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, s.List))
	if err != nil {
		return nil
	}
	var tests []testresults.Test
	if json.Unmarshal(data, &tests) != nil {
		return nil
	}
	return tests
}

// HasResultsConfig reports whether any ci.run command declares results.
func HasResultsConfig(cfg *tracker.CIConfig) bool {
	if cfg == nil {
		return false
	}
	for _, c := range cfg.Run {
		if len(c.Results) > 0 {
			return true
		}
	}
	return false
}
