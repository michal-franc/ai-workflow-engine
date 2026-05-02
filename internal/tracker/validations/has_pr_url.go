package validations

import (
	"fmt"
	"regexp"
	"strings"
)

func init() { register("has_pr_url", HasPRURL) }

var prURLRe = regexp.MustCompile(`^https?://github\.com/[^/]+/[^/]+/pull/\d+/?$`)

// HasPRURL passes when the issue's frontmatter "pr" field contains a github
// pull request URL.
func HasPRURL(action Action, issue *IssueView, _ Config) error {
	v := strings.TrimSpace(issue.Frontmatter["pr"])
	if prURLRe.MatchString(v) {
		return nil
	}
	return fail(action, issue,
		"frontmatter \"pr\" is not a github PR url",
		fmt.Sprintf("set: issue-cli set-meta %s --key pr --value \"https://github.com/<org>/<repo>/pull/<N>\"", issue.Slug))
}
