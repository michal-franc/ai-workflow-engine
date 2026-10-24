package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/michal-franc/issue-viewer/internal/testresults"
)

// resultsDefault is where ux-test writes the e2e results by default: into
// local CI's $CI_RESULTS_DIR, and nowhere outside CI.
func resultsDefault() string {
	if dir := os.Getenv("CI_RESULTS_DIR"); dir != "" {
		return filepath.Join(dir, "e2e.json")
	}
	return ""
}

// e2eSession is one session's run in the shared e2e results shape
// (internal/testresults, format playtest).
func e2eSession(res *Results, took time.Duration) testresults.PlaytestSession {
	ses := testresults.PlaytestSession{Session: res.Session, Issue: res.Issue, DurationMS: took.Milliseconds()}
	for _, st := range res.Steps {
		ses.Steps = append(ses.Steps, testresults.PlaytestStep{ID: st.ID, Status: st.Status, DurationMS: st.DurationMS})
	}
	return ses
}

func writeE2E(path string, p testresults.Playtest) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
