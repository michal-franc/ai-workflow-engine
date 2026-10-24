package privacy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Private development, public releases (privacy.code: snapshots): origin is
// a private dev repo with every branch and tag; the public repo is the
// remote `public` and gets ONE commit per release, the release branch tip's
// tree with the message vX.Y.Z, built with git commit-tree so no dev
// commit, message or branch name travels with it. A release is cut first as
// release/vX.Y with rc tags on it, soaks there (fixes are cherry-picked from
// main), and is published from that branch.

// publicBaseKey is the git config key holding the public branch's tip when
// release setup ran: the old history the first snapshot goes on top of.
const publicBaseKey = "issue-cli.publicbase"

var snapshotSubjectRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// IsSnapshot reports whether sha is a release snapshot commit: the message
// is exactly vX.Y.Z and it has at most one parent.
func IsSnapshot(root, sha string) bool {
	out, err := git(root, "log", "-1", "--format=%P%x00%B", sha)
	if err != nil {
		return false
	}
	parents, msg, _ := strings.Cut(out, "\x00")
	if len(strings.Fields(parents)) > 1 {
		return false
	}
	return snapshotSubjectRe.MatchString(strings.TrimSpace(msg))
}

// SetupResult says what Setup did.
type SetupResult struct {
	RenamedOrigin bool     // origin became the public remote
	Base          string   // the public branch's tip, recorded as the base
	Pushed        []string // what went to the dev remote: "branches", "tags"
	SkippedRC     int      // candidate tags kept local
}

// Setup makes devURL the origin and the old origin the public remote (tags
// never fetched from it), pushes every branch and every non-candidate tag to
// the dev repo, and records the public branch's tip as the base the first
// snapshot goes on. It never pushes anything to the public remote.
func Setup(root, devURL string, cfg *tracker.PrivacyConfig) (SetupResult, error) {
	var res SetupResult
	remote := cfg.Remote()
	origin, _ := git(root, "remote", "get-url", "origin")
	public, _ := git(root, "remote", "get-url", remote)
	switch {
	case public == "" && origin == "":
		return res, fmt.Errorf("no origin and no %s remote: add the public repo first (git remote add %s <url>)", remote, remote)
	case public == "" && origin == devURL:
		return res, fmt.Errorf("origin is already the dev repo %s and there is no %s remote: add the public repo (git remote add %s <url>)", devURL, remote, remote)
	case public == "":
		if _, err := git(root, "remote", "rename", "origin", remote); err != nil {
			return res, err
		}
		res.RenamedOrigin = true
		origin = ""
	}
	if origin == "" {
		if _, err := git(root, "remote", "add", "origin", devURL); err != nil {
			return res, err
		}
	} else if origin != devURL {
		return res, fmt.Errorf("origin is %s, not the dev repo %s: fix it with git remote set-url origin <url>", origin, devURL)
	}
	if _, err := git(root, "config", "remote."+remote+".tagOpt", "--no-tags"); err != nil {
		return res, err
	}
	branch := cfg.Branch()
	if _, err := git(root, "fetch", "-q", "--no-tags", remote, "+refs/heads/"+branch+":refs/remotes/"+remote+"/"+branch); err == nil {
		res.Base, _ = git(root, "rev-parse", "refs/remotes/"+remote+"/"+branch)
		if res.Base != "" {
			if _, err := git(root, "config", publicBaseKey, res.Base); err != nil {
				return res, err
			}
		}
	}
	if _, err := git(root, "push", "-q", "origin", "--all"); err != nil {
		return res, fmt.Errorf("pushing the branches to the dev repo: %w", err)
	}
	res.Pushed = append(res.Pushed, "branches")
	tags, _ := git(root, "tag", "-l")
	var refs []string
	for _, t := range lines(tags) {
		if milestone.IsPreRelease(t) {
			res.SkippedRC++
			continue
		}
		refs = append(refs, "refs/tags/"+t)
	}
	if len(refs) > 0 {
		if _, err := git(root, append([]string{"push", "-q", "origin"}, refs...)...); err != nil {
			return res, fmt.Errorf("pushing the tags to the dev repo: %w", err)
		}
		res.Pushed = append(res.Pushed, "tags")
	}
	return res, nil
}

// CutResult says what Cut did.
type CutResult struct {
	Branch   string
	Worktree string
	Created  bool   // the branch is new
	Tag      string // the candidate tag
	Pushed   bool   // the branch went to origin
}

// Cut tags the next release candidate of cut on its release branch,
// creating the branch from from (dev main) and its worktree when they are
// missing. push sends the branch to origin (the dev repo).
func Cut(root, cut, from string, push bool) (CutResult, error) {
	res := CutResult{Branch: milestone.ReleaseBranch(cut), Worktree: filepath.Join(root, ".worktrees", milestone.ReleaseWorktreeName(cut))}
	if _, err := git(root, "rev-parse", "-q", "--verify", "refs/heads/"+res.Branch); err != nil {
		if _, err := git(root, "branch", res.Branch, from); err != nil {
			return res, err
		}
		res.Created = true
	}
	if _, err := os.Stat(res.Worktree); err != nil {
		if _, err := git(root, "worktree", "add", "-q", res.Worktree, res.Branch); err != nil {
			return res, err
		}
	} else if b, _ := git(res.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); b != res.Branch {
		return res, fmt.Errorf("%s is on %s, not %s: switch it back first", res.Worktree, b, res.Branch)
	}
	tags, _ := git(root, "tag", "-l", cut+"-rc.*")
	rc, have := milestone.NewestRC(lines(tags), cut)
	n := 1
	if have {
		tagSha, _ := git(root, "rev-parse", rc.Tag+"^{commit}")
		tip, _ := git(root, "rev-parse", res.Branch)
		if tagSha == tip {
			return res, fmt.Errorf("%s already tags %s's tip: pick a fix first (issue-cli release pick <slug>), or soak %s", rc.Tag, res.Branch, rc.Tag)
		}
		n = rc.N + 1
	}
	res.Tag = milestone.RCTag(cut, n)
	if _, err := git(root, "tag", "-a", res.Tag, "-m", res.Tag, res.Branch); err != nil {
		return res, err
	}
	if push {
		if _, err := git(root, "push", "-q", "origin", "refs/heads/"+res.Branch); err != nil {
			return res, fmt.Errorf("%s is tagged; pushing %s to origin failed: %w", res.Tag, res.Branch, err)
		}
		res.Pushed = true
	}
	return res, nil
}

// PickResult is the commits Pick applied.
type PickResult struct {
	Picked  []string // short shas, in order
	Already []string // items whose commits the branch already has
}

// Pick cherry-picks onto the release branch, in its worktree, each item's
// commits the branch lacks: for an issue slug the commits of work/<slug>
// (git cherry), else the item as a commit. A conflict aborts the whole pick.
func Pick(root, cut string, items []string) (PickResult, error) {
	var res PickResult
	branch := milestone.ReleaseBranch(cut)
	wt := filepath.Join(root, ".worktrees", milestone.ReleaseWorktreeName(cut))
	if b, err := git(wt, "rev-parse", "--abbrev-ref", "HEAD"); err != nil || b != branch {
		return res, fmt.Errorf("no release worktree on %s at %s: cut the release first (issue-cli release cut %s)", branch, wt, cut)
	}
	if st, _ := git(wt, "status", "--porcelain", "--untracked-files=no"); st != "" {
		return res, fmt.Errorf("%s has uncommitted changes: commit or discard them first", wt)
	}
	var shas []string
	for _, item := range items {
		ref := "work/" + item
		if _, err := git(root, "rev-parse", "-q", "--verify", "refs/heads/"+ref); err == nil {
			out, err := git(root, "cherry", branch, ref)
			if err != nil {
				return res, err
			}
			var mine []string
			for _, l := range lines(out) {
				if sha, ok := strings.CutPrefix(l, "+ "); ok {
					mine = append(mine, sha)
				}
			}
			if len(mine) == 0 {
				res.Already = append(res.Already, item)
			}
			shas = append(shas, mine...)
			continue
		}
		sha, err := git(root, "rev-parse", "-q", "--verify", item+"^{commit}")
		if err != nil {
			return res, fmt.Errorf("%s is neither an issue with a work/%s branch nor a commit", item, item)
		}
		if _, err := git(root, "merge-base", "--is-ancestor", sha, branch); err == nil {
			res.Already = append(res.Already, item)
			continue
		}
		shas = append(shas, sha)
	}
	if len(shas) == 0 {
		return res, nil
	}
	if _, err := git(wt, append([]string{"-c", "commit.gpgsign=false", "cherry-pick", "-x"}, shas...)...); err != nil {
		git(wt, "cherry-pick", "--abort")
		return res, fmt.Errorf("cherry-pick onto %s stopped (nothing applied): %w. Resolve it by hand in %s, or fix main first", branch, err, wt)
	}
	for _, s := range shas {
		res.Picked = append(res.Picked, short(s))
	}
	return res, nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// PublishInput is what Publish needs.
type PublishInput struct {
	Root      string // the main checkout
	Worktree  string // where publish runs: must be on the release branch
	Cut       string // "v0.54.0"
	Cfg       *tracker.PrivacyConfig
	Changelog string // the changelog's path relative to the root, "" for none
	First     bool   // accept a public tip that isn't a snapshot (the old history)
	Now       time.Time
}

// PublishResult says what Publish did.
type PublishResult struct {
	Snapshot   string // the public commit
	Parent     string // the public tip it went on ("" for the first commit of an empty repo)
	MainMoved  bool   // main's changelog got the version section
	MainBranch string
	Signed     bool // the snapshot was signed with user.signingkey
}

// Publish pushes the release branch tip as one snapshot commit to the
// public branch, then tags the release on the dev branch and pushes both to
// origin. See the package comment on release.go.
func Publish(in PublishInput) (PublishResult, error) {
	var res PublishResult
	if !in.Cfg.Snapshots() {
		return res, fmt.Errorf("privacy.code is not snapshots: this project publishes by pushing main (docs/roadmap.md, Cutting a release)")
	}
	branch := milestone.ReleaseBranch(in.Cut)
	want := filepath.Join(in.Root, ".worktrees", milestone.ReleaseWorktreeName(in.Cut))
	if b, _ := git(in.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); b != branch {
		return res, fmt.Errorf("publish runs on %s, in its worktree: cd %s (HEAD here is %s)", branch, want, valueOr(b, "no branch"))
	}
	if st, _ := git(in.Worktree, "status", "--porcelain", "--untracked-files=no"); st != "" {
		return res, fmt.Errorf("%s has uncommitted changes: commit or discard them first", in.Worktree)
	}
	if _, err := git(in.Root, "rev-parse", "-q", "--verify", "refs/tags/"+in.Cut); err == nil {
		return res, fmt.Errorf("%s is already tagged in the dev repo: it was published (or tag it by hand only if you know why)", in.Cut)
	}
	remote, pubBranch := in.Cfg.Remote(), in.Cfg.Branch()
	if url, _ := git(in.Root, "remote", "get-url", remote); url == "" {
		return res, fmt.Errorf("no %s remote: issue-cli release setup --dev <url> first", remote)
	}
	paths := in.Cfg.Paths()
	if out, _ := git(in.Worktree, append([]string{"ls-tree", "-r", "--name-only", "HEAD", "--"}, paths...)...); out != "" {
		return res, fmt.Errorf("work files are in %s's tree (%s): they would go public. Untrack them on main (git rm -r --cached), pick that commit, cut a new rc", branch, firstFew(lines(out), 3))
	}
	tracking := "refs/remotes/" + remote + "/" + pubBranch
	if _, err := git(in.Root, "fetch", "-q", "--no-tags", remote, "+refs/heads/"+pubBranch+":"+tracking); err != nil && !strings.Contains(err.Error(), "couldn't find remote ref") {
		return res, fmt.Errorf("fetching %s/%s: %w", remote, pubBranch, err)
	}
	res.Parent, _ = git(in.Root, "rev-parse", "-q", "--verify", tracking)
	if res.Parent != "" && !IsSnapshot(in.Root, res.Parent) {
		base, _ := git(in.Root, "config", publicBaseKey)
		if !in.First && res.Parent != base {
			subj, _ := git(in.Root, "log", "-1", "--format=%s", res.Parent)
			return res, fmt.Errorf("%s/%s's tip %s (%q) is not a release snapshot: dev history reached the public repo. Clean it (docs/workflow.md, Private work and code), or pass --first if this is the old history the first snapshot goes on", remote, pubBranch, short(res.Parent), subj)
		}
	}

	// The changelog heading, on the branch: the only direct commit there.
	if in.Changelog != "" {
		path := filepath.Join(in.Worktree, in.Changelog)
		if data, err := os.ReadFile(path); err == nil {
			heading := "## " + in.Cut + " — " + in.Now.Format("2006-01-02")
			if text, ok := renameUnreleased(string(data), heading); ok {
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					return res, err
				}
				if _, err := git(in.Worktree, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "docs(changelog): "+in.Cut, "--", in.Changelog); err != nil {
					return res, err
				}
			}
		}
	}

	tree, err := git(in.Worktree, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return res, err
	}
	args := []string{"commit-tree", tree, "-m", in.Cut}
	if res.Parent != "" {
		args = []string{"commit-tree", tree, "-p", res.Parent, "-m", in.Cut}
	}
	// The public commit is signed with the owner's user.signingkey, so the
	// public repo shows it Verified. commit-tree signs only with -S (it
	// ignores commit.gpgsign). A key that fails to sign stops the publish:
	// nothing unsigned goes public by accident.
	if key, _ := git(in.Root, "config", "user.signingkey"); key != "" {
		args = append([]string{"commit-tree", "-S" + key}, args[1:]...)
		res.Signed = true
	}
	res.Snapshot, err = git(in.Root, args...)
	if err != nil {
		if res.Signed {
			return res, fmt.Errorf("signing the %s snapshot with user.signingkey failed, so nothing was published (unlock the key, or check gpg / gpg.format): %w", in.Cut, err)
		}
		return res, err
	}
	if _, err := git(in.Root, "push", "-q", remote, res.Snapshot+":refs/heads/"+pubBranch); err != nil {
		return res, fmt.Errorf("pushing the snapshot %s to %s: %w", short(res.Snapshot), remote, err)
	}
	git(in.Root, "update-ref", tracking, res.Snapshot)

	if _, err := git(in.Root, "tag", "-a", in.Cut, "-m", in.Cut, branch); err != nil {
		return res, fmt.Errorf("%s is public; tagging the dev branch failed: %w", in.Cut, err)
	}
	if _, err := git(in.Root, "push", "-q", "origin", "refs/heads/"+branch, "refs/tags/"+in.Cut); err != nil {
		return res, fmt.Errorf("%s is public and tagged; pushing %s and the tag to origin failed: %w", in.Cut, branch, err)
	}

	res.MainBranch, _ = git(in.Root, "rev-parse", "--abbrev-ref", "HEAD")
	if in.Changelog != "" {
		moved, err := moveReleasedLines(in.Root, in.Worktree, in.Changelog, in.Cut)
		if err != nil {
			return res, fmt.Errorf("%s is published; moving its changelog section on %s: %w", in.Cut, res.MainBranch, err)
		}
		res.MainMoved = moved
	}
	return res, nil
}

func valueOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

var unreleasedRe = regexp.MustCompile(`(?mi)^##[ \t]+\[?unreleased\]?[ \t]*$`)

// renameUnreleased replaces the ## Unreleased heading with heading; ok is
// false without one.
func renameUnreleased(text, heading string) (string, bool) {
	loc := unreleasedRe.FindStringIndex(text)
	if loc == nil {
		return text, false
	}
	return text[:loc[0]] + heading + text[loc[1]:], true
}

// section is the lines of the "## <prefix>…" section of text (the heading
// included), and where it starts and ends.
func section(text string, match func(heading string) bool) (start, end int, ok bool) {
	ls := strings.SplitAfter(text, "\n")
	pos := 0
	start = -1
	for _, l := range ls {
		if strings.HasPrefix(l, "## ") {
			if start >= 0 {
				return start, pos, true
			}
			if match(strings.TrimSpace(l)) {
				start = pos
			}
		}
		pos += len(l)
	}
	if start >= 0 {
		return start, len(text), true
	}
	return 0, 0, false
}

// moveReleasedLines puts the branch's "## <cut> — date" section into main's
// changelog below ## Unreleased, and drops its lines from main's Unreleased
// (entries landed after the cut stay). It commits only the changelog.
func moveReleasedLines(root, worktree, changelog, cut string) (bool, error) {
	branchText, err := os.ReadFile(filepath.Join(worktree, changelog))
	if err != nil {
		return false, err
	}
	bs, be, ok := section(string(branchText), func(h string) bool { return strings.HasPrefix(h, "## "+cut+" ") || h == "## "+cut })
	if !ok {
		return false, nil
	}
	released := string(branchText[bs:be])
	mainPath := filepath.Join(root, changelog)
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		return false, err
	}
	mainText := string(mainData)
	if _, _, has := section(mainText, func(h string) bool { return strings.HasPrefix(h, "## "+cut+" ") }); has {
		return false, nil
	}
	us, ue, ok := section(mainText, func(h string) bool { return unreleasedRe.MatchString(h) })
	if !ok {
		return false, nil
	}
	drop := map[string]bool{}
	for _, l := range strings.Split(released, "\n")[1:] {
		if strings.TrimSpace(l) != "" {
			drop[l] = true
		}
	}
	var kept []string
	for _, l := range strings.Split(strings.TrimRight(mainText[us:ue], "\n"), "\n") {
		if !drop[l] {
			kept = append(kept, l)
		}
	}
	unreleased := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n\n"
	newText := mainText[:us] + unreleased + strings.TrimRight(released, "\n") + "\n\n" + strings.TrimLeft(mainText[ue:], "\n")
	if err := os.WriteFile(mainPath, []byte(newText), 0o644); err != nil {
		return false, err
	}
	if _, err := git(root, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "docs(changelog): "+cut+" is published", "--", changelog); err != nil {
		return false, err
	}
	return true, nil
}

// ParseVersion checks a release argument: "v0.54.0" (the v is added when
// missing).
func ParseVersion(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !snapshotSubjectRe.MatchString(v) {
		return "", fmt.Errorf("%q is not a release version like v0.54.0", v)
	}
	return v, nil
}

// unquote reads a decision's quote ("…" with escapes).
func unquote(q string) string {
	if s, err := strconv.Unquote(q); err == nil {
		return s
	}
	return strings.Trim(q, `"`)
}
