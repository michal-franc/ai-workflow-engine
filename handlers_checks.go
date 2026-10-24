package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/checks"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// checkRoots are the checkouts that may hold the issue's guided check
// scripts: its worktree first (scripts stay on the branch), then the project.
func checkRoots(proj *tracker.Project, issue *tracker.Issue, wf *tracker.WorkflowConfig) []string {
	roots := playtestRoots(proj, issue, wf)
	if len(roots) == 2 {
		roots[0], roots[1] = roots[1], roots[0]
	}
	return roots
}

// checkScriptCommand is what the check-script-<slug> window runs: the
// owner's run, then a pause so the summary stays readable.
func checkScriptCommand(proj *tracker.Project, slug string) []string {
	run := "issue-cli --project " + shellQuote(proj.Slug) + " check-script " + shellQuote(slug)
	return []string{"sh", "-c", run + `; printf '\nPress Enter to close this window. '; read _`}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// createCheckWindow and checkWindowLive are seams over tmux for tests.
var createCheckWindow = func(proj *tracker.Project, name, dir string, env, command []string, steps *[]DispatchStep) bool {
	desc, cmd := agentrun.CreateTmux(proj, name, dir, env, command...)
	return agentrun.RunStep(steps, desc, cmd)
}
var checkWindowLive = agentrun.IsLive

// handleRunChecks is ▶ Run checks: it starts the owner's run of the issue's
// guided check scripts in a tmux window named check-script-<slug>, in the
// issue's worktree, and opens it like an agent (a terminal, or the window
// selected in the shared session). A second click while it runs only brings
// the window back.
func (s *Server) handleRunChecks(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	slug := strings.TrimPrefix(r.URL.Path, prefix+"/issue/")
	slug = strings.TrimSuffix(slug, "/check-script")
	issue := s.findIssueBySlug(proj, slug)
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	name := checks.WindowName(issue.Slug)
	resp := DispatchResponse{Session: name, AttachCmd: agentrun.AttachCmd(proj, name), SwitchCmd: agentrun.SwitchCmd(proj, name)}
	w.Header().Set("Content-Type", "application/json")

	roots := checkRoots(proj, issue, proj.LoadWorkflowForIssue(issue))
	found := checks.Find(roots, issue.Slug)
	if len(found) == 0 {
		resp.Status = "no-scripts"
		resp.Steps = []DispatchStep{{Name: "No scripts/checks/*.sh names " + issue.Slug, Status: "error"}}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(resp)
		return
	}

	if checkWindowLive(proj, name) {
		resp.Status = "already-running"
		resp.Steps = append(resp.Steps, DispatchStep{Name: name + " is already running", Status: "ok"})
	} else {
		var env []string
		if cfg := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_CONFIG")); cfg != "" {
			env = append(env, "ISSUE_VIEWER_CONFIG="+cfg)
		}
		if !createCheckWindow(proj, name, found[0].Root, env, checkScriptCommand(proj, issue.Slug), &resp.Steps) {
			resp.Status = "error"
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp.Status = "started"
	}
	if !attachAgent(proj, name, &resp.Steps) {
		resp.Status = "error"
	}
	json.NewEncoder(w).Encode(resp)
}
