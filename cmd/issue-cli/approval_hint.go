package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// defaultViewerURL is the fallback base URL used when ISSUE_VIEWER_URL is unset.
// Matches the default port baked into main.go's -port flag.
const defaultViewerURL = "http://localhost:8080"

// approvalHint builds the multi-line "click here to approve" message we tack
// onto an ApprovalMissingError so the user does not have to context-switch to
// figure out where the approve button lives.
//
// proj may be nil — in that case the link omits the /p/<projectslug> segment
// (single-project bootstrap mode). slug may also be empty when the validate
// path didn't have one; we still emit the base URL plus a manual instruction.
func approvalHint(proj *tracker.Project, slug, requiredStatus string) string {
	base := strings.TrimRight(viewerBaseURL(), "/")

	var url string
	switch {
	case proj != nil && slug != "":
		url = fmt.Sprintf("%s/p/%s/issue/%s#approve-%s", base, proj.Slug, slug, fragmentStatus(requiredStatus))
	case slug != "":
		url = fmt.Sprintf("%s/issue/%s#approve-%s", base, slug, fragmentStatus(requiredStatus))
	default:
		url = base + "/"
	}

	var b strings.Builder
	b.WriteString("A human must approve this in the issue viewer:\n  ")
	b.WriteString(url)
	if os.Getenv("ISSUE_VIEWER_URL") == "" {
		b.WriteString("\n\n(set ISSUE_VIEWER_URL if your viewer is on a different host/port)")
	}
	return b.String()
}

// viewerBaseURL returns the configured viewer base URL or the default. The env
// var is the contract — dispatched bot sessions inherit it from the server
// that spawned them (see handlers_dispatch.go).
func viewerBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("ISSUE_VIEWER_URL")); u != "" {
		return u
	}
	return defaultViewerURL
}

// fragmentStatus normalises a status name for use in a URL fragment. Spaces
// become hyphens so "in progress" → "in-progress", which matches the id
// emitted on the approve button in templates/detail.html.
func fragmentStatus(status string) string {
	return strings.ReplaceAll(strings.TrimSpace(status), " ", "-")
}
