package main

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// projectImageExts are the file types the /files/ route will serve. It is
// deliberately limited to images so the route can't be used to read source
// or config files out of the project directory.
var projectImageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".avif": true,
}

var imgSrcRe = regexp.MustCompile(`(<img\b[^>]*?\bsrc=")([^"]*)(")`)

// rewriteRelativeImages points relative <img src> paths in rendered markdown
// at the project's /files/ route. Paths are resolved against mdDir — the
// directory of the markdown file on disk — the same way GitHub and editors
// resolve them. Absolute URLs, root-relative paths, data URIs and anything
// that resolves outside root are left untouched.
func rewriteRelativeImages(body, prefix, mdDir, root string) string {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return body
	}
	absDir, err := filepath.Abs(mdDir)
	if err != nil {
		return body
	}
	return imgSrcRe.ReplaceAllStringFunc(body, func(match string) string {
		m := imgSrcRe.FindStringSubmatch(match)
		src := html.UnescapeString(m[2])
		if src == "" || strings.HasPrefix(src, "/") || strings.HasPrefix(src, "#") || strings.Contains(src, ":") {
			return match
		}
		if i := strings.IndexAny(src, "?#"); i >= 0 {
			src = src[:i]
		}
		decoded, err := url.PathUnescape(src)
		if err != nil {
			return match
		}
		rel, ok := relWithinRoot(absRoot, filepath.Join(absDir, filepath.FromSlash(decoded)))
		if !ok {
			return match
		}
		return m[1] + html.EscapeString(prefix+"/files/"+(&url.URL{Path: rel}).EscapedPath()) + m[3]
	})
}

// relWithinRoot returns target relative to root (slash-separated) and whether
// target lies inside root.
func relWithinRoot(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// handleProjectFile serves an image from the project root for
// <prefix>/files/<path>. It rejects non-image extensions and any path that
// escapes the root, including via symlinks.
func (s *Server) handleProjectFile(w http.ResponseWriter, r *http.Request, proj *tracker.Project, prefix string) {
	relPath := strings.TrimPrefix(r.URL.Path, prefix+"/files/")
	if !projectImageExts[strings.ToLower(filepath.Ext(relPath))] {
		http.NotFound(w, r)
		return
	}

	root, err := filepath.Abs(projectRoot(proj))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(filepath.Clean("/"+relPath))))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, ok := relWithinRoot(root, target); !ok {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// SVGs opened directly would otherwise run embedded scripts on our origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, target)
}
