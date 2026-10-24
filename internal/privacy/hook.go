package privacy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// hookMarker is the line that tells our pre-push hook from someone else's.
const hookMarker = "# issue-cli privacy guard"

// hookScript is the pre-push hook. It keeps an existing hook as
// pre-push.local and runs it first, then asks issue-cli whether this push
// may go out.
func hookScript(cliArgs string) string {
	return `#!/bin/sh
` + hookMarker + ` (issue-cli privacy hook). Reinstall with: issue-cli privacy hook
input=$(cat)
here=$(dirname "$0")
if [ -x "$here/pre-push.local" ]; then
	printf '%s\n' "$input" | "$here/pre-push.local" "$@" || exit $?
fi
if ! command -v issue-cli >/dev/null 2>&1; then
	echo "privacy guard: issue-cli is not on PATH, so this push can't be checked. Fix PATH, or check it yourself and push with --no-verify." >&2
	exit 1
fi
printf '%s\n' "$input" | issue-cli ` + cliArgs + ` privacy pre-push "$@"
`
}

// HooksDir is where the repository at root keeps its hooks: core.hooksPath
// when set, else the common git folder's hooks/ (shared by every worktree).
func HooksDir(root string) (string, error) {
	if p, _ := git(root, "config", "core.hooksPath"); p != "" {
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		return p, nil
	}
	common, err := git(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	return filepath.Join(common, "hooks"), nil
}

// HookInstalled reports whether root's pre-push hook is the privacy guard.
func HookInstalled(root string) bool {
	dir, err := HooksDir(root)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "pre-push"))
	return err == nil && strings.Contains(string(data), hookMarker)
}

// InstallHook writes the guard as root's pre-push hook. cliArgs are the
// global flags the hook passes to issue-cli (--project <slug>, --config).
// A pre-push hook that isn't ours is kept as pre-push.local and still runs.
// It returns the hook's path and whether an existing hook was kept.
func InstallHook(root, cliArgs string) (path string, kept bool, err error) {
	dir, err := HooksDir(root)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	path = filepath.Join(dir, "pre-push")
	if data, rerr := os.ReadFile(path); rerr == nil && !strings.Contains(string(data), hookMarker) {
		local := filepath.Join(dir, "pre-push.local")
		if _, serr := os.Stat(local); serr == nil {
			return "", false, fmt.Errorf("%s exists and is not the privacy guard, and %s is taken too: merge them by hand", path, local)
		}
		if err := os.Rename(path, local); err != nil {
			return "", false, err
		}
		kept = true
	} else if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return "", false, rerr
	}
	if err := os.WriteFile(path, []byte(hookScript(cliArgs)), 0o755); err != nil {
		return "", false, err
	}
	return path, kept, nil
}

// PushRef is one line of a pre-push hook's stdin.
type PushRef struct {
	LocalRef, LocalSHA, RemoteRef, RemoteSHA string
}

// ReadPushRefs parses the pre-push stdin.
func ReadPushRefs(r io.Reader) []PushRef {
	var refs []PushRef
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 4 {
			refs = append(refs, PushRef{f[0], f[1], f[2], f[3]})
		}
	}
	return refs
}

func isZero(sha string) bool { return strings.Trim(sha, "0") == "" }

// pushedCommits are the commits a ref update sends that the remote lacks.
func pushedCommits(root, remote string, ref PushRef) ([]string, error) {
	if isZero(ref.LocalSHA) {
		return nil, nil // a delete sends nothing
	}
	args := []string{"rev-list", ref.LocalSHA}
	if !isZero(ref.RemoteSHA) {
		if _, err := git(root, "cat-file", "-e", ref.RemoteSHA+"^{commit}"); err == nil {
			args = append(args, "^"+ref.RemoteSHA)
		}
	}
	args = append(args, "--not", "--remotes="+remote)
	out, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// PrePush checks a push of refs to remote (name and url, as git passes
// them) against the project's privacy choice, and returns the refusal, nil
// when the push may go out:
//   - code: snapshots, a push to the public remote: every new commit, and
//     every tag, must be a release snapshot (issue-cli release publish);
//   - code: public: no pushed commit message may contain a recorded
//     decision's quote (quotes, 16+ characters);
//   - work_files: tracker: no pushed commit may add a work path.
func PrePush(root string, cfg *tracker.PrivacyConfig, remote, url string, refs []PushRef, quotes []string) error {
	if cfg == nil {
		return nil
	}
	toPublic := false
	if cfg.Snapshots() {
		pub, _ := git(root, "remote", "get-url", cfg.Remote())
		toPublic = remote == cfg.Remote() || (pub != "" && url == pub)
	}
	paths := cfg.Paths()
	for _, ref := range refs {
		if isZero(ref.LocalSHA) {
			continue
		}
		if toPublic && strings.HasPrefix(ref.RemoteRef, "refs/tags/") {
			target, _ := git(root, "rev-parse", ref.LocalSHA+"^{commit}")
			if !IsSnapshot(root, target) {
				return fmt.Errorf("privacy guard: %s gets release snapshots only, and tag %s is on a dev commit. Publish with issue-cli release publish <version>", remote, strings.TrimPrefix(ref.RemoteRef, "refs/tags/"))
			}
			continue
		}
		commits, err := pushedCommits(root, remote, ref)
		if err != nil {
			return fmt.Errorf("privacy guard: can't list the commits in this push: %w", err)
		}
		for _, c := range commits {
			if toPublic && !IsSnapshot(root, c) {
				subj, _ := git(root, "log", "-1", "--format=%s", c)
				return fmt.Errorf("privacy guard: %s gets release snapshots only, and %s (%q) is a dev commit. Development goes to origin; publish a release with issue-cli release publish <version>", remote, short(c), subj)
			}
			if cfg.Code == tracker.CodePublic {
				if q := quotedIn(root, c, quotes); q != "" {
					return fmt.Errorf("privacy guard: %s's message quotes a decision (%q), and this repo is public (privacy.code: public). Reword it without the quote (git commit --amend, or rebase -i for older ones) and push again", short(c), clip(q, 60))
				}
			}
			if cfg.Tracker() {
				out, err := git(root, append([]string{"diff-tree", "--no-commit-id", "--name-only", "-r", "--root", "--diff-filter=A", c, "--"}, paths...)...)
				if err != nil {
					return fmt.Errorf("privacy guard: %w", err)
				}
				if added := lines(out); len(added) > 0 {
					return fmt.Errorf("privacy guard: %s adds work files to the code repo (%s); they belong in the private tracker (privacy.work_files: tracker). Remove them from that commit (git rm --cached) and push again", short(c), firstFew(added, 3))
				}
			}
		}
	}
	return nil
}

// minQuote is the shortest decision quote the guard looks for: shorter
// ones ("yes", "go ahead") are too common to mean a quote.
const minQuote = 16

// quotedIn is the first of quotes that commit c's message contains.
func quotedIn(root, c string, quotes []string) string {
	msg, err := git(root, "log", "-1", "--format=%B", c)
	if err != nil {
		return ""
	}
	low := strings.ToLower(msg)
	for _, q := range quotes {
		if len(q) >= minQuote && strings.Contains(low, strings.ToLower(q)) {
			return q
		}
	}
	return ""
}

// DecisionQuotes are the owner's quoted words in issues' ## Decisions.
func DecisionQuotes(issues []*tracker.Issue) []string {
	var out []string
	for _, is := range issues {
		for _, d := range tracker.ParseDecisions(is.Slug, is.BodyRaw) {
			if q := strings.TrimSpace(unquote(d.Quote)); len(q) >= minQuote {
				out = append(out, q)
			}
		}
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstFew(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" and %d more", len(items)-n)
}
