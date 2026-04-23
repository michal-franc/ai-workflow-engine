package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type GitHubIssue struct {
	Number    int              `json:"number"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	State     string           `json:"state"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Labels    []GitHubLabel    `json:"labels"`
	Imported  bool             `json:"imported"`
}

type GitHubLabel struct {
	Name string `json:"name"`
}

// FetchGitHubIssues shells out to `gh issue list` and returns open issues for the repo.
// Auth is inherited from the machine's `gh` CLI — no token override (option A).
func FetchGitHubIssues(repo string) ([]GitHubIssue, error) {
	if repo == "" {
		return nil, fmt.Errorf("repo is empty")
	}
	cmd := exec.Command("gh", "issue", "list",
		"--repo", repo,
		"--state", "open",
		"--limit", "100",
		"--json", "number,title,body,labels,updatedAt,state",
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh issue list: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	var issues []GitHubIssue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}
	return issues, nil
}

// LocalGitHubURLs returns the set of GitHub issue URLs already present in the
// project. An issue contributes its computed URL (from `repo` + `number`) and,
// if set, any explicit `github_url` frontmatter value. The URL is the canonical
// reference — matching on it avoids the ambiguity of bare issue numbers.
func LocalGitHubURLs(proj *tracker.Project) (map[string]bool, error) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return nil, err
	}
	urls := make(map[string]bool, len(issues))
	for _, iss := range issues {
		if iss.GithubURL != "" {
			urls[iss.GithubURL] = true
		}
		for _, ef := range iss.ExtraFields {
			if ef.Key == "github_url" && ef.Value != "" {
				urls[ef.Value] = true
			}
		}
	}
	return urls, nil
}

// RemoteIssueURL returns the canonical GitHub URL for a remote issue.
func RemoteIssueURL(repo string, number int) string {
	return fmt.Sprintf("https://github.com/%s/issues/%d", repo, number)
}

// ImportGitHubIssue writes a single GitHub issue to <issueDir>/<number>.md with status=backlog.
// Refuses to overwrite an existing file.
func ImportGitHubIssue(proj *tracker.Project, gh GitHubIssue) (string, error) {
	if gh.Number <= 0 {
		return "", fmt.Errorf("issue has no number")
	}
	filename := filepath.Join(proj.IssueDir, fmt.Sprintf("%d.md", gh.Number))
	if _, err := os.Stat(filename); err == nil {
		return "", fmt.Errorf("already exists: %s", filename)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", gh.Title))
	b.WriteString("status: \"backlog\"\n")
	b.WriteString(fmt.Sprintf("number: %d\n", gh.Number))
	if proj.Repo != "" {
		b.WriteString(fmt.Sprintf("repo: %q\n", proj.Repo))
		b.WriteString(fmt.Sprintf("github_url: %q\n", RemoteIssueURL(proj.Repo, gh.Number)))
	}
	b.WriteString(fmt.Sprintf("created: %q\n", gh.UpdatedAt.Format("2006-01-02")))
	if len(gh.Labels) > 0 {
		b.WriteString("labels:\n")
		for _, l := range gh.Labels {
			b.WriteString(fmt.Sprintf("  - %s\n", l.Name))
		}
	}
	b.WriteString("---\n\n")
	if gh.Body != "" {
		b.WriteString(gh.Body)
		if !strings.HasSuffix(gh.Body, "\n") {
			b.WriteString("\n")
		}
	}

	if err := os.WriteFile(filename, []byte(b.String()), 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", filename, err)
	}
	return filename, nil
}
