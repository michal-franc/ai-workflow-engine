package main

import (
	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// sessionLive reports whether proj's agent or lead session called name is
// running. A var so tests never consult the real tmux server.
var sessionLive = func(proj *tracker.Project) func(string) bool {
	return func(name string) bool { return agentrun.IsLive(proj, name) }
}
