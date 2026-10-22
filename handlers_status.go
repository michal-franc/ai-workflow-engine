package main

import (
	"bytes"
	"html/template"
	"net/http"
	"regexp"
	"time"

	"github.com/yuin/goldmark"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// loadStatus builds the Status model for a request. A tmux failure only
// drops the agent columns.
func (s *Server) loadStatus(proj *tracker.Project, prefix string) (*StatusModel, error) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil, err
	}
	agents, _ := s.buildAgentsModel(proj, prefix)
	m := buildStatus(proj, issues, agents, time.Now())
	m.Prefix = prefix
	return m, nil
}

// handleStatusMD serves the Status page as markdown: the stand-up the lead
// pastes, and what "Copy as markdown" copies.
func (s *Server) handleStatusMD(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	m, err := s.loadStatus(proj, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(statusMarkdown(m)))
}

// StatusPageData is the Status page: the model plus what only the HTML needs.
type StatusPageData struct {
	*StatusModel
	SupportsGitHub bool
	// ReleasesURL is the GitHub releases page, when the project has one.
	ReleasesURL string
	NextHTML    template.HTML
	// HasProjectPage: workbench.status_page is set, so the page shows the
	// Project status / Built-in tabs.
	HasProjectPage bool
}

// IssueURL links an issue's detail page.
func (d StatusPageData) IssueURL(slug string) string { return d.Prefix + "/issue/" + slug }

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	hasPage := proj.LoadWorkflow().Workbench.StatusPage != ""
	if hasPage && r.URL.Query().Get("view") != "builtin" {
		s.handleStatusProjectPage(w, r, proj, prefix)
		return
	}
	m, err := s.loadStatus(proj, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := StatusPageData{StatusModel: m, SupportsGitHub: proj.SupportsGitHub, HasProjectPage: hasPage}
	if proj.SupportsGitHub && proj.Repo != "" {
		data.ReleasesURL = "https://github.com/" + proj.Repo + "/releases"
	}
	if m.Next.Found {
		issues, _ := tracker.LoadIssues(proj.IssueDir)
		data.NextHTML = template.HTML(linkCodeSlugs(renderMarkdown(m.Next.Text), prefix, issues))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "status.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderMarkdown renders lead-written markdown; raw HTML in it is dropped.
func renderMarkdown(text string) string {
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(text), &buf); err != nil {
		return template.HTMLEscapeString(text)
	}
	return buf.String()
}

var codeSpanRe = regexp.MustCompile(`<code>([^<]+)</code>`)

// linkCodeSlugs links `slug` code spans that name an issue to its page.
func linkCodeSlugs(html, prefix string, issues []*tracker.Issue) string {
	known := map[string]bool{}
	for _, issue := range issues {
		known[issue.Slug] = true
	}
	return codeSpanRe.ReplaceAllStringFunc(html, func(m string) string {
		slug := codeSpanRe.FindStringSubmatch(m)[1]
		if !known[slug] {
			return m
		}
		return `<a href="` + prefix + `/issue/` + slug + `">` + m + `</a>`
	})
}
