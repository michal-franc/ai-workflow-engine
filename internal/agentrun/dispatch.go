package agentrun

import (
	"fmt"
	"path/filepath"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// StatusWaitingOnGate is the Response status of a dispatch refused because
// the issue only waits on the owner's approval.
const StatusWaitingOnGate = "waiting-on-gate"

// DispatchSpec builds the launch spec for issue's worker: what ▶ on the board
// and `issue-cli dispatch` both start, from the same prompt
// (tracker.BuildDispatchPrompt), session and worktree. workDir is the project
// checkout the worktree is resolved against. The caller starts the spec with
// Start.
//
// refused is non-nil when no worker session is live, the issue waits on the
// owner's approval and nothing else (tracker.WaitingOnGate), and force is off:
// a worker started there would only stop at the same gate. Start nothing and
// return refused as the response. A live session is never refused: Start
// re-attaches to it.
func DispatchSpec(proj *tracker.Project, issue *tracker.Issue, agentType, workDir string, force bool) (spec Spec, refused *Response) {
	if workDir == "" {
		workDir = ProjectWorkDir(proj)
	}
	wf := proj.LoadWorkflowForIssue(issue)
	session := tracker.AgentSessionName(issue.Slug)
	if !force && !IsLive(proj, session) {
		comments, _ := tracker.LoadComments(issue.FilePath)
		if gate := tracker.WaitingOnGate(wf, issue, comments); gate != "" {
			return Spec{}, &Response{
				Status:  StatusWaitingOnGate,
				Session: tracker.AgentDisplayName(proj, session),
				Gate:    gate,
				Steps: []Step{{
					Name:   "Waiting on the owner's approval for " + gate,
					Status: "error",
					Detail: fmt.Sprintf("everything else %s → %s needs is done, so a new worker would only stop at the gate; nothing was started. Approve it, or dispatch anyway (%s)", issue.Status, gate, tracker.InjectProject(proj, "issue-cli dispatch "+issue.Slug+" --force"+QuoteHint)),
				}},
			}
		}
	}
	dp := tracker.BuildDispatchPrompt(proj, issue, wf, workDir, func(name string) bool { return IsLive(proj, name) })
	return Spec{
		Session:      session,
		Prompt:       dp.Prompt,
		AgentType:    agentType,
		IssueSlug:    issue.Slug,
		WorkDir:      workDir,
		Worktree:     tracker.IssueWorktree(workDir, issue, wf),
		Workflow:     wf,
		RepromptHint: RepromptHint(proj, issue.Slug),
	}, nil
}

// RepromptHint is the command that re-sends issue slug's current briefing
// into its live worker session.
func RepromptHint(proj *tracker.Project, slug string) string {
	return tracker.InjectProject(proj, "issue-cli dispatch "+slug+" --reprompt"+QuoteHint)
}

// QuoteHint ends every issue-cli dispatch command the hints print: dispatch
// starts nothing without the human's words.
const QuoteHint = ` --quote "<the human's words>"`

// Reprompt pastes issue's current briefing into its live worker session (see
// Paste): Status "reprompted" when it arrived, "no-session" when no worker is
// running. workDir is the project checkout, as for DispatchSpec.
func Reprompt(proj *tracker.Project, issue *tracker.Issue, workDir string) Response {
	if workDir == "" {
		workDir = ProjectWorkDir(proj)
	}
	session := tracker.AgentSessionName(issue.Slug)
	dp := tracker.BuildDispatchPrompt(proj, issue, proj.LoadWorkflowForIssue(issue), workDir, func(name string) bool { return IsLive(proj, name) })
	textFile := filepath.Join(workDir, ".agent-logs", session, "dispatch-prompt.txt")
	response := Paste(proj, session, dp.Prompt, textFile, RepromptHint(proj, issue.Slug))
	switch response.Status {
	case "sent":
		response.Status = "reprompted"
	case "no-session":
		response.Steps = []Step{{
			Name:   "No live agent session",
			Status: "error",
			Detail: fmt.Sprintf("%s is not running; dispatch the issue instead: %s", response.Session, tracker.InjectProject(proj, "issue-cli dispatch "+issue.Slug+QuoteHint)),
		}}
	}
	return response
}
