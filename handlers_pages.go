package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Project pages: HTML the project keeps in its repo (workbench.status_page
// and workbench.pages), shown in the viewer frame inside a sandboxed iframe.
// See docs/workbench-files.md, "Project pages".

// pageSandbox is the iframe's sandbox and the raw route's CSP sandbox. There
// is no allow-same-origin: the page gets an opaque origin, so it can't read
// the viewer's cookies or storage, or read viewer API responses.
const pageSandbox = "allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation"

// pageRawCSP keeps a raw page sandboxed when it is opened directly, and lets
// only the viewer frame it.
const pageRawCSP = "sandbox " + pageSandbox + "; frame-ancestors 'self'"

// pageHashFileCap bounds the files /hash stats under the page folders.
const pageHashFileCap = 500

// pageContentTypes is the raw route's allowlist: a page and its assets.
var pageContentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".woff2": "font/woff2",
}

// PageViewData is the frame around one project page (page.html).
type PageViewData struct {
	Project string
	Prefix  string
	Title   string
	// Active is the sidebar key: "status" or "page/<n>".
	Active string
	// Key names the config entry in messages: status_page or pages[<n>].
	Key string
	// Path is the configured path, as written in workflow.yaml.
	Path string
	// IsStatus: the Status page's project tab, with the Built-in tab beside it.
	IsStatus bool
	RawURL   string
	// Problem, when set, replaces the iframe: the file is missing or the
	// path is not allowed. ProblemTitle heads the card.
	ProblemTitle string
	Problem      string
	// ChangedMs is the file's mtime in Unix milliseconds.
	ChangedMs int64
	Sandbox   string
	// PagesHash is /hash's "pages" value at render time.
	PagesHash string
}

// pageRoot is the project root with symlinks resolved.
func pageRoot(proj *tracker.Project) (string, error) {
	root, err := filepath.Abs(projectRoot(proj))
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

// resolvePageFile maps rel (slash-separated, relative to the project root)
// to a file the raw route may serve: inside the root after symlinks, under
// one of the configured page folders, with an allowlisted extension, and a
// regular file.
func resolvePageFile(proj *tracker.Project, wb tracker.WorkbenchConfig, rel string) (string, bool) {
	if _, ok := pageContentTypes[strings.ToLower(filepath.Ext(rel))]; !ok {
		return "", false
	}
	root, err := pageRoot(proj)
	if err != nil {
		return "", false
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(filepath.Clean("/"+rel))))
	if err != nil {
		return "", false
	}
	if _, ok := relWithinRoot(root, target); !ok {
		return "", false
	}
	if _, ok := pageContentTypes[strings.ToLower(filepath.Ext(target))]; !ok {
		return "", false
	}
	if !underPageDir(root, wb, target) {
		return "", false
	}
	if info, err := os.Stat(target); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return target, true
}

// underPageDir reports whether target lies under a configured page folder.
func underPageDir(root string, wb tracker.WorkbenchConfig, target string) bool {
	for _, dir := range wb.PageDirs() {
		abs, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		if _, ok := relWithinRoot(root, abs); !ok {
			continue
		}
		if _, ok := relWithinRoot(abs, target); ok {
			return true
		}
	}
	return false
}

// handlePageRaw serves <prefix>/page-raw/<path>: a project page or one of its
// assets, sandboxed by CSP even when opened directly.
func (s *Server) handlePageRaw(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	rel := strings.TrimPrefix(r.URL.Path, prefix+"/page-raw/")
	target, ok := resolvePageFile(proj, proj.LoadWorkflow().Workbench, rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", pageContentTypes[strings.ToLower(filepath.Ext(target))])
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pageRawCSP)
	h.Set("Cache-Control", "no-cache")
	// The sandboxed page's own fetch("data.json") comes from origin null.
	if r.Header.Get("Origin") == "null" {
		h.Set("Access-Control-Allow-Origin", "null")
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// pageView builds the frame for one configured page. A missing file or a
// path that is not allowed becomes Problem instead of the iframe.
func pageView(proj *tracker.Project, prefix, key, title, path string) PageViewData {
	wb := proj.LoadWorkflow().Workbench
	d := PageViewData{
		Project: proj.Name,
		Prefix:  prefix,
		Title:   title,
		Key:     key,
		Path:    path,
		Sandbox: pageSandbox,
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if err := checkPagePath(path); err != nil {
		d.ProblemTitle = "This project page can't be shown"
		d.Problem = fmt.Sprintf("workbench.%s in workflow.yaml: %v.", key, err)
		return d
	}
	target, ok := resolvePageFile(proj, wb, clean)
	if !ok {
		d.ProblemTitle = "The project page is missing"
		if key == "status_page" {
			d.ProblemTitle = "The project status page is missing"
		}
		d.Problem = fmt.Sprintf("workbench.%s in workflow.yaml points at %s, but there is no such file in the project root. Create it, or remove the key.", key, clean)
		return d
	}
	if info, err := os.Stat(target); err == nil {
		d.ChangedMs = info.ModTime().UnixMilli()
	}
	d.RawURL = prefix + "/page-raw/" + clean
	d.PagesHash = pagesHash(proj)
	return d
}

// checkPagePath is the lint's rule for a page path (Workbench.Validate).
func checkPagePath(path string) error {
	probe := tracker.WorkbenchConfig{StatusPage: path}
	if problems := probe.Validate(); len(problems) > 0 {
		msg := strings.TrimPrefix(problems[0], "workbench.status_page: ")
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (s *Server) renderPageView(w http.ResponseWriter, d PageViewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "page.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleStatusProjectPage renders /status with the project's status_page.
func (s *Server) handleStatusProjectPage(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	d := pageView(proj, prefix, "status_page", "Status", proj.LoadWorkflow().Workbench.StatusPage)
	d.Active = "status"
	d.IsStatus = true
	s.renderPageView(w, d)
}

// handleProjectPage renders <prefix>/pages/<n>.
func (s *Server) handleProjectPage(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix, rest string) {
	n, err := strconv.Atoi(strings.TrimPrefix(rest, "pages/"))
	pages := proj.LoadWorkflow().Workbench.Pages
	if err != nil || n < 0 || n >= len(pages) {
		http.NotFound(w, r)
		return
	}
	d := pageView(proj, prefix, fmt.Sprintf("pages[%d]", n), pages[n].Title, pages[n].Path)
	d.Active = fmt.Sprintf("page/%d", n)
	s.renderPageView(w, d)
}

// pagesHash is /hash's "pages" value: it changes when any page or asset
// under the configured page folders is added, removed or rewritten. Empty
// when the project has no pages.
func pagesHash(proj *tracker.Project) string {
	wb := proj.LoadWorkflow().Workbench
	dirs := wb.PageDirs()
	if len(dirs) == 0 {
		return ""
	}
	root, err := pageRoot(proj)
	if err != nil {
		return ""
	}
	h := sha256.New()
	files := 0
	for _, dir := range dirs {
		abs, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			fmt.Fprintf(h, "missing:%s\n", dir)
			continue
		}
		if _, ok := relWithinRoot(root, abs); !ok {
			continue
		}
		filepath.WalkDir(abs, func(path string, e fs.DirEntry, err error) error {
			if err != nil || files >= pageHashFileCap {
				return filepath.SkipDir
			}
			if e.IsDir() {
				if path != abs && strings.HasPrefix(e.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if _, ok := pageContentTypes[strings.ToLower(filepath.Ext(path))]; !ok {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			files++
			fmt.Fprintf(h, "%s:%d:%d\n", path, info.Size(), info.ModTime().UnixNano())
			return nil
		})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// statusJSON is the model the frame posts to a project page
// ({type: "issue-viewer.status", project, status}) and /status.json serves.
// It is a small fixed shape, documented in docs/workbench-files.md.
type statusJSON struct {
	Project  string               `json:"project"`
	AsOf     time.Time            `json:"as_of"`
	Open     int                  `json:"open"`
	All      statusJSONProgress   `json:"all"`
	Versions []statusJSONProgress `json:"versions"`
	Epics    []statusJSONEpic     `json:"epics"`
	Active   []statusJSONActive   `json:"active"`
	Waiting  []statusJSONWaiting  `json:"waiting"`
	Shipped  []statusJSONRelease  `json:"shipped"`
}

type statusJSONProgress struct {
	Label string `json:"label,omitempty"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

type statusJSONEpic struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
}

type statusJSONActive struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Agent  string `json:"agent,omitempty"`
}

type statusJSONWaiting struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// Kind is gate, playtest or question; To is a gate's target status.
	Kind string `json:"kind"`
	To   string `json:"to,omitempty"`
}

type statusJSONRelease struct {
	Version string `json:"version"`
	Date    string `json:"date,omitempty"`
	Text    string `json:"text,omitempty"`
}

func buildStatusJSON(m *StatusModel) statusJSON {
	out := statusJSON{
		Project:  m.Project,
		AsOf:     m.AsOf,
		Open:     m.All.Total - m.All.Done,
		All:      statusJSONProgress{Done: m.All.Done, Total: m.All.Total},
		Versions: []statusJSONProgress{},
		Epics:    []statusJSONEpic{},
		Active:   []statusJSONActive{},
		Waiting:  []statusJSONWaiting{},
		Shipped:  []statusJSONRelease{},
	}
	for _, v := range m.Versions {
		out.Versions = append(out.Versions, statusJSONProgress{Label: v.Label, Done: v.Done, Total: v.Total})
	}
	for _, e := range m.Epics {
		out.Epics = append(out.Epics, statusJSONEpic{Slug: e.Issue.Slug, Title: e.Issue.Title, Status: e.Issue.Status, Done: e.Progress.Done, Total: e.Progress.Total})
	}
	for _, a := range m.Active {
		out.Active = append(out.Active, statusJSONActive{Slug: a.Issue.Slug, Title: a.Issue.Title, Status: a.Issue.Status, Agent: a.Agent})
	}
	for _, w := range m.Waiting {
		out.Waiting = append(out.Waiting, statusJSONWaiting{Slug: w.Issue.Slug, Title: w.Issue.Title, Kind: w.Kind, To: w.Target})
	}
	for _, r := range m.Releases {
		out.Shipped = append(out.Shipped, statusJSONRelease{Version: r.Version, Date: r.Date, Text: r.SummaryText()})
	}
	return out
}

// handleStatusJSON serves the bridge model the frame posts to project pages.
func (s *Server) handleStatusJSON(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	m, err := s.loadStatus(proj, prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildStatusJSON(m))
}
