package agentrun

import (
	"os/exec"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// LeadSpec builds the launch spec for the project lead, rooted at the
// project checkout root. The caller fills in AgentType and Headless, then
// starts it.
func LeadSpec(proj *tracker.Project, wf *tracker.WorkflowConfig, root string, now time.Time) Spec {
	return Spec{
		WorkDir:   root,
		Workflow:  wf,
		Session:   tracker.LeadSessionName(proj),
		LeadScope: tracker.LeadScopeProject,
		Prompt:    tracker.LeadBrief(wf, tracker.LeadBriefInput{Project: proj, Root: root, Now: now}),
	}
}

// MainBranch names the repository's main branch: main, else master, else "".
func MainBranch(workdir string) string {
	for _, candidate := range []string{"main", "master"} {
		if exec.Command("git", "-C", workdir, "rev-parse", "--verify", "--quiet", "refs/heads/"+candidate).Run() == nil {
			return candidate
		}
	}
	return ""
}
