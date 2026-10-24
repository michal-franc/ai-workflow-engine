package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Whether the code repo (origin) is private on GitHub. Visibility isn't known
// locally, so the viewer's health watcher asks GitHub at most once per
// VisibilityMaxAge and caches the answer in the repo's git folder; badges
// only read the cache, never the network.

// VisibilityMaxAge is how old a cached answer may be before it is asked again.
const VisibilityMaxAge = time.Hour

// Visibility values. Unknown: not asked yet, not a GitHub remote, or GitHub
// didn't answer.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// githubAPI is GitHub's API base; tests point it at a local server.
var githubAPI = "https://api.github.com"

type visibilityCache struct {
	URL        string    `json:"url"`
	Visibility string    `json:"visibility"`
	Checked    time.Time `json:"checked"`
}

func visibilityPath(root string) string {
	dir, err := git(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Join(dir, "issue-cli-visibility.json")
}

func readVisibility(root string) (visibilityCache, bool) {
	var c visibilityCache
	p := visibilityPath(root)
	if p == "" {
		return c, false
	}
	data, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(data, &c) != nil {
		return c, false
	}
	return c, true
}

// Visibility is the cached visibility of root's origin: "public", "private"
// or "" (unknown). It reads only the cache, and an answer for another URL
// (the remote changed) counts as unknown.
func Visibility(root string) string {
	c, ok := readVisibility(root)
	if !ok {
		return ""
	}
	if url, _ := git(root, "remote", "get-url", "origin"); url != c.URL {
		return ""
	}
	return c.Visibility
}

var githubRepoRe = regexp.MustCompile(`github\.com[:/]+([^/]+)/([^/]+?)(\.git)?/?$`)

// GitHubRepo is owner/repo of a github.com remote URL (ssh or https), or "".
func GitHubRepo(url string) string {
	m := githubRepoRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// RefreshVisibility asks GitHub whether root's origin is private, unless the
// cached answer for the same URL is younger than maxAge, and caches it. A
// repo GitHub doesn't show without signing in (404) counts as private: not
// publicly visible is what matters here. It returns the visibility and
// whether it asked; a remote that isn't on GitHub returns "" and no error.
func RefreshVisibility(ctx context.Context, root string, now time.Time, maxAge time.Duration) (string, bool, error) {
	url, _ := git(root, "remote", "get-url", "origin")
	repo := GitHubRepo(url)
	if repo == "" {
		return "", false, nil
	}
	if c, ok := readVisibility(root); ok && c.URL == url && now.Sub(c.Checked) < maxAge {
		return c.Visibility, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI+"/repos/"+repo, nil)
	if err != nil {
		return "", true, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("asking GitHub about %s: %w", repo, err)
	}
	defer resp.Body.Close()
	vis := ""
	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Private bool `json:"private"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return "", true, fmt.Errorf("reading GitHub's answer about %s: %w", repo, err)
		}
		vis = VisibilityPublic
		if body.Private {
			vis = VisibilityPrivate
		}
	case http.StatusNotFound:
		vis = VisibilityPrivate
	default:
		return "", true, fmt.Errorf("GitHub answered %s about %s", resp.Status, repo)
	}
	data, _ := json.Marshal(visibilityCache{URL: url, Visibility: vis, Checked: now})
	if p := visibilityPath(root); p != "" {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return vis, true, err
		}
	}
	return vis, true, nil
}
