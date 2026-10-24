package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/viewerstate"
)

// approvalHint builds the multi-line "click here to approve" message we tack
// onto an ApprovalMissingError so the user does not have to context-switch to
// figure out where the approve button lives.
//
// proj may be nil — in that case the link omits the /p/<projectslug> segment
// (single-project bootstrap mode). slug may also be empty when the validate
// path didn't have one; we still emit the base URL plus a manual instruction.
func approvalHint(proj *tracker.Project, slug, requiredStatus string) string {
	var b strings.Builder
	b.WriteString("A human must approve this in the issue viewer:\n  ")
	b.WriteString(approvalURL(proj, slug, requiredStatus))
	if proj == nil || proj.ViewerURL == "" {
		b.WriteString("\n\n(set viewer_url in projects.yaml if your viewer is on a different host/port)")
	}
	return b.String()
}

// approvalURL is the deep link to the approve button for requiredStatus.
func approvalURL(proj *tracker.Project, slug, requiredStatus string) string {
	base := viewerBaseURL(proj)
	switch {
	case proj != nil && slug != "":
		return fmt.Sprintf("%s/p/%s/issue/%s#approve-%s", base, proj.Slug, slug, fragmentStatus(requiredStatus))
	case slug != "":
		return fmt.Sprintf("%s/issue/%s#approve-%s", base, slug, fragmentStatus(requiredStatus))
	default:
		return base + "/"
	}
}

// viewerBaseURL is the viewer's base URL without a trailing slash, for every
// link issue-cli prints and every call it makes to the viewer: viewer_url
// from projects.yaml, else the running viewer's address when it serves this
// config (viewer.json), else http://localhost:8080.
func viewerBaseURL(proj *tracker.Project) string {
	if proj == nil {
		return viewerstate.BaseURL("", "")
	}
	return viewerstate.BaseURL(proj.ViewerURL, proj.ConfigPath)
}

// fragmentStatus normalises a status name for use in a URL fragment. Spaces
// become hyphens so "in progress" → "in-progress", which matches the id
// emitted on the approve button in templates/detail.html.
func fragmentStatus(status string) string {
	return strings.ReplaceAll(strings.TrimSpace(status), " ", "-")
}

// issueURL is the viewer link to an issue's detail page.
func issueURL(proj *tracker.Project, slug string) string {
	base := viewerBaseURL(proj)
	if proj != nil {
		return fmt.Sprintf("%s/p/%s/issue/%s", base, proj.Slug, slug)
	}
	return fmt.Sprintf("%s/issue/%s", base, slug)
}
