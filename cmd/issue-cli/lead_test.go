package main

import "github.com/michal-franc/issue-viewer/internal/tracker"

func init() {
	// No test consults the real tmux server: nothing is live unless a test
	// says so.
	sessionLive = func(*tracker.Project) func(string) bool {
		return func(string) bool { return false }
	}
}
