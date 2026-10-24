package tracker

import "strings"

// NextGate returns the issue's next status and, when that transition needs
// approval not yet given, the status the approval is for.
func NextGate(wf *WorkflowConfig, issue *Issue) (next, gate string) {
	order := wf.GetStatusOrder()
	idx := wf.GetStatusIndex(issue.Status)
	if idx < 0 || idx+1 >= len(order) {
		return "", ""
	}
	next = order[idx+1]
	required := wf.RequiredHumanApproval(issue.Status, next)
	if required == "" || strings.EqualFold(issue.HumanApproval, required) {
		return next, ""
	}
	return next, required
}

// WaitingOnGate is the status the issue waits on the owner to approve when
// that approval is all its next transition still needs; "" otherwise. It is
// the Status page's "waiting on you" gate rule, and what dispatch refuses
// on: a worker started there would only stop at the same gate.
func WaitingOnGate(wf *WorkflowConfig, issue *Issue, comments []Comment) string {
	next, gate := NextGate(wf, issue)
	if gate == "" || !wf.ReadyForApproval(issue, issue.Status, next, comments) {
		return ""
	}
	return gate
}
