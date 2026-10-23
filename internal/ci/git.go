package ci

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const defaultTier = tracker.CIDefaultTier

// gitTimeout bounds every git call except worktree creation.
var gitTimeout = 30 * time.Second

// worktreeTimeout bounds creating the CI worktree (a full checkout).
var worktreeTimeout = 10 * time.Minute

func git(dir string, args ...string) (string, error) {
	return gitWithin(gitTimeout, dir, args...)
}

func gitWithin(limit time.Duration, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RepoRoot is the main checkout of the repository dir belongs to (the first
// entry of `git worktree list`), so CI state lives in one place even when a
// project's workdir is a worktree or a subdirectory. Falls back to dir.
func RepoRoot(dir string) string {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return dir
	}
	for _, line := range strings.Split(out, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			return path
		}
	}
	return dir
}

// ResolveBranch is cfg.Branch, else the branch checked out in the main
// checkout.
func ResolveBranch(root string, cfg *tracker.CIConfig) (string, error) {
	if cfg != nil && strings.TrimSpace(cfg.Branch) != "" {
		return strings.TrimSpace(cfg.Branch), nil
	}
	out, err := git(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || out == "" {
		return "", fmt.Errorf("the main checkout %s has no branch checked out; set ci.branch in workflow.yaml", root)
	}
	return out, nil
}

// Head is the branch's current commit.
func Head(root, branch string) (string, error) {
	return git(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
}

func subject(root, sha string) string {
	out, _ := git(root, "log", "-1", "--format=%s", sha)
	return out
}

func isAncestor(root, a, b string) bool {
	_, err := git(root, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

var (
	mergeBranchRe = regexp.MustCompile(`^Merge (?:remote-tracking )?branch '(?:origin/)?([^']+)'(?: of \S+)?(?: into (\S+))?`)
	mergePRRe     = regexp.MustCompile(`^Merge pull request #\d+ from [^/\s]+/(\S+)`)
)

// maxDirect caps the commits listed as made straight on the branch.
const maxDirect = 20

// blame lists what landed on branch between from (the last green commit) and
// to: local branches whose tip is in the range, branches named by merge
// commits, then commits that belong to no branch. A rewound branch counts
// from the merge-base. from == "" (no green run yet) blames nothing.
func blame(root, branch, from, to string, issues []*tracker.Issue) []Merged {
	if from == "" || from == to {
		return nil
	}
	if !isAncestor(root, from, to) {
		base, err := git(root, "merge-base", from, to)
		if err != nil || base == "" {
			return nil
		}
		from = base
	}
	var out []Merged
	seen := map[string]bool{branch: true}
	add := func(name, sha, subj string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		m := Merged{Branch: name, Sha: sha, Subject: subj}
		if issue := tracker.FindIssue(issues, strings.TrimPrefix(name, "work/")); issue != nil {
			m.Slug = issue.Slug
		}
		out = append(out, m)
	}

	var tips []string
	refs, _ := git(root, "for-each-ref", "--merged="+to, "--no-merged="+from, "--format=%(refname:short)%09%(objectname)", "refs/heads/")
	for _, line := range splitLines(refs) {
		name, sha, ok := strings.Cut(line, "\t")
		if !ok || name == branch {
			continue
		}
		tips = append(tips, sha)
		add(name, sha, "")
	}

	merges, _ := git(root, "log", "--merges", "--format=%H%x09%s", from+".."+to)
	for _, line := range splitLines(merges) {
		sha, subj, _ := strings.Cut(line, "\t")
		if m := mergePRRe.FindStringSubmatch(subj); m != nil {
			add(m[1], sha, subj)
			continue
		}
		m := mergeBranchRe.FindStringSubmatch(subj)
		if m == nil {
			continue
		}
		// "Merge branch 'main' into work/x": work/x absorbed main, then
		// landed. Plain "Merge branch 'work/x'": work/x merged into branch.
		if into := m[2]; into != "" && into != branch {
			add(into, sha, subj)
		} else if m[1] != branch {
			add(m[1], sha, subj)
		}
	}

	// --first-parent leaves out what merge commits brought in; ^tip leaves
	// out fast-forwarded branches.
	args := []string{"log", "--first-parent", "--no-merges", fmt.Sprintf("--max-count=%d", maxDirect), "--format=%H%x09%s", to, "^" + from}
	for _, t := range tips {
		args = append(args, "^"+t)
	}
	direct, _ := git(root, args...)
	for _, line := range splitLines(direct) {
		sha, subj, _ := strings.Cut(line, "\t")
		out = append(out, Merged{Sha: sha, Subject: subj})
	}
	return out
}

// Slugs is the unique issue slugs among merged, in order.
func Slugs(merged []Merged) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range merged {
		if m.Slug != "" && !seen[m.Slug] {
			seen[m.Slug] = true
			out = append(out, m.Slug)
		}
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
