package tracker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Branch scan for the workbench views: which branches are open, what each
// changed against its base, and which files more than one of them touches.
// Everything is read from git; nothing is stored. Calls whose arguments are
// commit shas are cached for the life of the process (their output never
// changes), so a page load with no new commits only lists refs.

// GitTimeout bounds every git call the scan makes.
var GitTimeout = 5 * time.Second

var (
	// ErrNoGit means the project root is not a git checkout (or git is missing).
	ErrNoGit = errors.New("git not available for this project")
	// ErrGitTimeout means a git call took longer than GitTimeout.
	ErrGitTimeout = errors.New("git timed out")
)

// OpenBranch is a branch with work not yet merged: a worktree, or an unmerged
// work/<slug> branch whose issue is still open.
type OpenBranch struct {
	Name      string // "work/ui/x"
	Worktree  string // absolute path; "" for a branch without a worktree
	Base      string // the epic's base branch, else the main branch
	MergeBase string // sha
	Head      string // sha
	IssueSlug string // "" when no issue matches work/<slug>
	Ahead     int    // commits on the branch since MergeBase
	// SharesWith counts, per other open branch, the commits both carry on top
	// of their common base (one was forked from the other).
	SharesWith map[string]int
}

// BranchChange is one branch's change to a file, against the branch's base.
type BranchChange struct {
	Branch    string
	Worktree  string
	IssueSlug string
	Added     int
	Removed   int
	// Shared marks a change that is not overlap: one the branch only carries
	// through commits it shares with another open branch, or the change of a
	// branch that another branch touching the file is stacked on.
	Shared bool
}

// HotFile is a file changed on two or more open branches.
type HotFile struct {
	Path    string
	Changes []BranchChange
}

// Branches counts the changes that are not Shared.
func (h HotFile) Branches() int {
	n := 0
	for _, c := range h.Changes {
		if !c.Shared {
			n++
		}
	}
	return n
}

// OpenBranches lists the open branches of the repo at root. Errors (no git, a
// timeout) yield what was found so far; OpenBranchesErr reports them.
func OpenBranches(root, mainBranch string, issues []*Issue) []OpenBranch {
	out, _ := OpenBranchesErr(root, mainBranch, issues)
	return out
}

// OpenBranchesErr is OpenBranches with the error that cut the scan short.
// mainBranch "" means MainBranch(root).
func OpenBranchesErr(root, mainBranch string, issues []*Issue) ([]OpenBranch, error) {
	if mainBranch == "" {
		var err error
		if mainBranch, err = MainBranch(root); err != nil {
			return nil, err
		}
	}
	worktrees, err := listWorktrees(root)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{mainBranch: true}
	var out []OpenBranch
	add := func(name, head, worktree string) {
		if seen[name] {
			return
		}
		seen[name] = true
		b := OpenBranch{Name: name, Head: head, Worktree: worktree}
		if strings.HasPrefix(name, "work/") {
			if issue := FindIssue(issues, strings.TrimPrefix(name, "work/")); issue != nil {
				b.IssueSlug = issue.Slug
			}
		}
		out = append(out, b)
	}
	for i, wt := range worktrees {
		if i == 0 || wt.branch == "" || wt.prunable {
			continue // the main checkout, a detached HEAD, a stale entry
		}
		add(wt.branch, wt.head, wt.path)
	}

	refs, err := runGit(root, "for-each-ref", "--no-merged="+mainBranch, "--format=%(refname:short)%09%(objectname)", "refs/heads/work/")
	if err != nil {
		return out, err
	}
	for _, line := range splitLines(refs) {
		name, head, ok := strings.Cut(line, "\t")
		if !ok || seen[name] {
			continue
		}
		issue := FindIssue(issues, strings.TrimPrefix(name, "work/"))
		if issue == nil || isClosedStatus(issue.Status) {
			continue
		}
		add(name, head, "")
	}

	for i := range out {
		b := &out[i]
		b.Base = mainBranch
		if issue := FindIssue(issues, b.IssueSlug); issue != nil {
			if base := ChildBase(issue, issues); base != "" && base != b.Name && refExists(root, base) {
				b.Base = base
			}
		}
		baseSha, err := runGit(root, "rev-parse", "--verify", "--quiet", b.Base+"^{commit}")
		if err != nil {
			return out, err
		}
		if b.MergeBase, err = gitCached(root, "merge-base", strings.TrimSpace(baseSha), b.Head); err != nil {
			continue // unrelated history: leave MergeBase empty
		}
		b.MergeBase = strings.TrimSpace(b.MergeBase)
		b.Ahead = countCommits(root, b.MergeBase, b.Head)
	}
	restack(root, out)
	if err := fillShared(root, out); err != nil {
		return out, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ahead != out[j].Ahead {
			return out[i].Ahead > out[j].Ahead
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// restack moves a branch stacked on another open branch (its head descends
// from that branch's head, past their base) onto that branch, so it is
// compared with what it was forked from rather than with main. The nearest
// such branch wins.
func restack(root string, branches []OpenBranch) {
	for i := range branches {
		b := &branches[i]
		best, bestDist := -1, b.Ahead
		for j := range branches {
			o := branches[j]
			if i == j || o.Head == b.Head || o.Ahead == 0 || o.MergeBase == "" {
				continue
			}
			mb, err := gitCached(root, "merge-base", o.Head, b.Head)
			if err != nil || strings.TrimSpace(mb) != o.Head {
				continue // o's head is not an ancestor of b's
			}
			if d := countCommits(root, o.Head, b.Head); d < bestDist {
				best, bestDist = j, d
			}
		}
		if best >= 0 {
			b.Base, b.MergeBase, b.Ahead = branches[best].Name, branches[best].Head, bestDist
		}
	}
}

// fillShared sets SharesWith for every pair of branches on the same base
// whose common ancestor is past that base.
func fillShared(root string, branches []OpenBranch) error {
	for i := range branches {
		for j := i + 1; j < len(branches); j++ {
			a, b := &branches[i], &branches[j]
			if a.MergeBase == "" || b.MergeBase == "" || a.Base != b.Base || a.Ahead == 0 || b.Ahead == 0 {
				continue
			}
			mb, err := gitCached(root, "merge-base", a.Head, b.Head)
			if err != nil {
				if errors.Is(err, ErrGitTimeout) {
					return err
				}
				continue
			}
			mb = strings.TrimSpace(mb)
			n := countCommits(root, a.MergeBase, mb)
			if m := countCommits(root, b.MergeBase, mb); m < n {
				n = m
			}
			if n <= 0 {
				continue
			}
			if a.SharesWith == nil {
				a.SharesWith = map[string]int{}
			}
			if b.SharesWith == nil {
				b.SharesWith = map[string]int{}
			}
			a.SharesWith[b.Name], b.SharesWith[a.Name] = n, n
		}
	}
	return nil
}

// ChangedOnBranches lists the branches whose change against their base
// touches path, in branch order.
func ChangedOnBranches(root string, branches []OpenBranch, path string) []BranchChange {
	path = filepath.ToSlash(filepath.Clean(path))
	var out []BranchChange
	for _, b := range branches {
		stats, err := branchNumstat(root, b)
		if err != nil {
			continue
		}
		if st, ok := stats[path]; ok {
			out = append(out, BranchChange{Branch: b.Name, Worktree: b.Worktree, IssueSlug: b.IssueSlug, Added: st[0], Removed: st[1]})
		}
	}
	return out
}

// HotFiles lists files changed on two or more open branches, most branches
// first. A change a branch only has through commits it shares with another
// branch is marked Shared; with hideShared such changes are dropped and a
// file needs two unshared changes to be listed.
func HotFiles(root string, branches []OpenBranch, hideShared bool) []HotFile {
	byPath := map[string][]BranchChange{}
	own := make([]map[string][2]int, len(branches))
	for i, b := range branches {
		stats, err := branchNumstat(root, b)
		if err != nil {
			continue
		}
		own[i] = stats
		for path, st := range stats {
			byPath[path] = append(byPath[path], BranchChange{Branch: b.Name, Worktree: b.Worktree, IssueSlug: b.IssueSlug, Added: st[0], Removed: st[1]})
		}
	}

	// For branches that share commits, a file is theirs only if they changed
	// it after the fork point. sharedOnly[branch][path] = touched only through
	// shared commits, with every branch it shares commits with.
	sharedOnly := map[string]map[string]bool{}
	for i, a := range branches {
		if len(a.SharesWith) == 0 || own[i] == nil {
			continue
		}
		mine := map[string]bool{}
		owner := false
		for other := range a.SharesWith {
			if ownsShared(a, branches, other) {
				owner = true
				break
			}
			mb, err := gitCached(root, "merge-base", a.Head, headOf(branches, other))
			if err != nil {
				continue
			}
			names, err := gitCached(root, "diff", "--no-renames", "--name-only", strings.TrimSpace(mb), a.Head)
			if err != nil {
				continue
			}
			for _, p := range splitLines(names) {
				mine[p] = true
			}
		}
		if owner {
			continue
		}
		so := map[string]bool{}
		for p := range own[i] {
			if !mine[p] {
				so[p] = true
			}
		}
		sharedOnly[a.Name] = so
	}

	// A branch other open branches are stacked on (an epic's base) is not in
	// conflict with them: they build on its change.
	baseOf := map[string]map[string]bool{} // base → branches built on it
	for _, b := range branches {
		if baseOf[b.Base] == nil {
			baseOf[b.Base] = map[string]bool{}
		}
		baseOf[b.Base][b.Name] = true
	}

	var out []HotFile
	for path, changes := range byPath {
		if len(changes) < 2 {
			continue
		}
		var kept []BranchChange
		for _, c := range changes {
			c.Shared = sharedOnly[c.Branch][path]
			for _, o := range changes {
				if baseOf[c.Branch][o.Branch] {
					c.Shared = true
				}
			}
			if hideShared && c.Shared {
				continue
			}
			kept = append(kept, c)
		}
		h := HotFile{Path: path, Changes: kept}
		if len(kept) < 2 || (hideShared && h.Branches() < 2) {
			continue
		}
		sort.Slice(h.Changes, func(i, j int) bool { return h.Changes[i].Branch < h.Changes[j].Branch })
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if bi, bj := len(out[i].Changes), len(out[j].Changes); bi != bj {
			return bi > bj
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// MainBranch is the repo's main branch: origin's HEAD, else main or master,
// else the main checkout's branch.
func MainBranch(root string) (string, error) {
	if ref, err := runGit(root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(ref), "origin/"); name != "" && refExists(root, name) {
			return name, nil
		}
	} else if errors.Is(err, ErrNoGit) || errors.Is(err, ErrGitTimeout) {
		return "", err
	}
	for _, name := range []string{"main", "master"} {
		if refExists(root, name) {
			return name, nil
		}
	}
	head, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(head), nil
}

type worktreeEntry struct {
	path, head, branch string
	prunable           bool
}

func listWorktrees(root string) ([]worktreeEntry, error) {
	text, err := runGit(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var out []worktreeEntry
	var cur *worktreeEntry
	for _, line := range strings.Split(text, "\n") {
		key, val, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch key {
		case "worktree":
			out = append(out, worktreeEntry{path: val})
			cur = &out[len(out)-1]
		case "HEAD":
			if cur != nil {
				cur.head = val
			}
		case "branch":
			if cur != nil {
				cur.branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "prunable":
			if cur != nil {
				cur.prunable = true
			}
		}
	}
	return out, nil
}

// branchNumstat is the branch's change against its merge-base:
// path → {added, removed}. Binary files count 0/0.
func branchNumstat(root string, b OpenBranch) (map[string][2]int, error) {
	if b.MergeBase == "" || b.Ahead == 0 {
		return nil, nil
	}
	text, err := gitCached(root, "diff", "--no-renames", "--numstat", b.MergeBase, b.Head)
	if err != nil {
		return nil, err
	}
	out := map[string][2]int{}
	for _, line := range splitLines(text) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		removed, _ := strconv.Atoi(parts[1])
		out[parts[2]] = [2]int{added, removed}
	}
	return out, nil
}

// ownsShared reports whether a, rather than other, is credited with the
// commits they share: the branch with fewer commits ahead (nearer the fork
// point, usually the one the other was forked from), ties by name.
func ownsShared(a OpenBranch, branches []OpenBranch, other string) bool {
	for _, b := range branches {
		if b.Name == other {
			return a.Ahead < b.Ahead || (a.Ahead == b.Ahead && a.Name < b.Name)
		}
	}
	return true
}

func headOf(branches []OpenBranch, name string) string {
	for _, b := range branches {
		if b.Name == name {
			return b.Head
		}
	}
	return name
}

func countCommits(root, from, to string) int {
	if from == "" || to == "" || from == to {
		return 0
	}
	text, err := gitCached(root, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(text))
	return n
}

func refExists(root, name string) bool {
	_, err := runGit(root, "rev-parse", "--verify", "--quiet", name+"^{commit}")
	return err == nil
}

func isClosedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "parked", "closed":
		return true
	}
	return false
}

func splitLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimRight(line, "\r"); strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// runGit runs git in root with GitTimeout. A missing repo or binary is
// ErrNoGit; a timeout is ErrGitTimeout; any other failure wraps stderr.
func runGit(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", ErrGitTimeout
	}
	if err != nil {
		var exitErr *exec.ExitError
		msg := strings.TrimSpace(stderr.String())
		if !errors.As(err, &exitErr) || strings.Contains(msg, "not a git repository") {
			return "", ErrNoGit
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// gitCache holds output of git calls whose arguments name commits by sha, so
// the output can never change.
var gitCache = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

const gitCacheMax = 20000

// gitCached is runGit for calls keyed only by shas (merge-base, rev-list,
// diff between two commits). Failures are not cached.
func gitCached(root string, args ...string) (string, error) {
	key := root + "\x00" + strings.Join(args, "\x00")
	gitCache.Lock()
	out, ok := gitCache.m[key]
	gitCache.Unlock()
	if ok {
		return out, nil
	}
	out, err := runGit(root, args...)
	if err != nil {
		return "", err
	}
	gitCache.Lock()
	if len(gitCache.m) >= gitCacheMax {
		gitCache.m = map[string]string{}
	}
	gitCache.m[key] = out
	gitCache.Unlock()
	return out, nil
}
