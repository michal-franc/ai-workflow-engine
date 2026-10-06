package main

import (
	"fmt"
	"io"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// typeLabel renders " | Type: <t>" for status headers, or "" when the
// project has no types.
func typeLabel(wf *tracker.WorkflowConfig) string {
	if wf == nil || wf.ActiveType == "" {
		return ""
	}
	return " | Type: " + wf.ActiveType
}

// typePath is the scoped lifecycle line for typed issues, "" otherwise.
func typePath(wf *tracker.WorkflowConfig) string {
	if wf == nil || wf.ActiveType == "" {
		return ""
	}
	return wf.PathLine()
}

// printTypeWarnings flags an undefined type: value and a status that is off
// the type's path, each with the command that fixes it.
func printTypeWarnings(w io.Writer, wf *tracker.WorkflowConfig, issue *tracker.Issue) {
	if wf == nil || issue == nil {
		return
	}
	if msg := wf.UnknownTypeWarning(issue.Slug, issue.Type); msg != "" {
		fmt.Fprintf(w, "⚠ %s\n", msg)
	}
	if msg := wf.OffPathWarning(); msg != "" {
		fmt.Fprintf(w, "⚠ %s\n", msg)
	}
}
