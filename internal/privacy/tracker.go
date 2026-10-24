package privacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tracker is a project's private tracker repo: a git folder outside the
// checkout whose work tree is the checkout itself, so issues/, sessions/
// and the other work paths stay where everything expects them while the
// code repo ignores them. status.showUntrackedFiles = no keeps the code
// files out of its view, and `git clean -fdx` in the checkout can't reach
// its history.
type Tracker struct {
	Root   string   // the checkout (code repo root)
	GitDir string   // the tracker's git folder
	Paths  []string // work paths relative to Root
}

// DefaultGitDir is <Root's parent>/.trackers/<slug>.git.
func DefaultGitDir(root, slug string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(root)), ".trackers", slug+".git")
}

// SyncFile is the sync record inside the tracker's git folder.
const SyncFile = "issue-cli-sync.json"

// SyncState is the last `tracker sync` result, read by the pages.
type SyncState struct {
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	Committed int       `json:"committed,omitempty"` // files in this run's commit
	LastOK    time.Time `json:"last_ok,omitempty"`
}

// Exists reports whether the tracker's git folder is there.
func (t Tracker) Exists() bool {
	st, err := os.Stat(filepath.Join(t.GitDir, "HEAD"))
	return err == nil && !st.IsDir()
}

func (t Tracker) git(args ...string) (string, error) {
	return git(t.Root, append([]string{"--git-dir=" + t.GitDir, "--work-tree=" + t.Root}, args...)...)
}

// Origin is the tracker's remote URL, "" without a git folder or a remote.
func (t Tracker) Origin() string {
	if !t.Exists() {
		return ""
	}
	url, _ := t.git("remote", "get-url", "origin")
	return url
}

// InitResult says what Init did, for the CLI to print.
type InitResult struct {
	Adopted   bool     // the git folder already existed
	Created   bool     // a new git folder was made
	RemoteSet bool     // origin was added
	Ignored   []string // paths added to the code repo's .gitignore
	Tracked   []string // work paths the code repo still tracks
}

// Init creates the tracker's git folder, or adopts an existing one whose
// work tree is Root, adds remote as origin when given and origin is unset,
// and ignores the work paths in the code repo. It never untracks files in
// the code repo: Tracked lists what the owner must `git rm --cached`.
func (t Tracker) Init(remote string) (InitResult, error) {
	var res InitResult
	if t.Exists() {
		wt, _ := git(t.Root, "--git-dir="+t.GitDir, "config", "core.worktree")
		if wt != "" && !samePath(wt, t.Root) {
			return res, fmt.Errorf("tracker git folder %s belongs to %s, not %s (pass another folder with tracker_git_dir: in projects.yaml)", t.GitDir, wt, t.Root)
		}
		res.Adopted = true
	} else {
		if err := os.MkdirAll(filepath.Dir(t.GitDir), 0o755); err != nil {
			return res, err
		}
		if _, err := git(t.Root, "init", "-q", "--bare", "-b", "main", t.GitDir); err != nil {
			return res, err
		}
		res.Created = true
	}
	for _, kv := range [][2]string{{"core.bare", "false"}, {"core.worktree", t.Root}, {"status.showUntrackedFiles", "no"}} {
		if _, err := git(t.Root, "--git-dir="+t.GitDir, "config", kv[0], kv[1]); err != nil {
			return res, err
		}
	}
	if remote != "" {
		if cur, _ := t.git("remote", "get-url", "origin"); cur == "" {
			if _, err := t.git("remote", "add", "origin", remote); err != nil {
				return res, err
			}
			res.RemoteSet = true
		} else if cur != remote {
			return res, fmt.Errorf("the tracker's origin is %s, not %s (change it with git --git-dir=%s remote set-url origin <url>)", cur, remote, t.GitDir)
		}
	}
	added, err := ensureIgnored(t.Root, t.Paths)
	if err != nil {
		return res, err
	}
	res.Ignored = added
	res.Tracked = t.CodeTracked()
	return res, nil
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// ignoreHeader introduces the lines Init adds to .gitignore.
const ignoreHeader = "# Work files live in the private tracker repo (issue-cli tracker init)"

// ensureIgnored adds /<path>/ to root/.gitignore for each work path not
// already ignored there, and returns the paths it added.
func ensureIgnored(root string, paths []string) ([]string, error) {
	file := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "!") {
			continue
		}
		have[strings.Trim(l, "/")] = true
	}
	var added []string
	var b strings.Builder
	for _, p := range paths {
		if have[strings.Trim(p, "/")] {
			continue
		}
		if len(added) == 0 {
			if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
				b.WriteString("\n")
			}
			if len(data) > 0 {
				b.WriteString("\n")
			}
			b.WriteString(ignoreHeader + "\n")
		}
		b.WriteString("/" + p + "/\n")
		added = append(added, p)
	}
	if len(added) == 0 {
		return nil, nil
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return nil, err
	}
	return added, nil
}

// CodeTracked lists the work paths the code repo tracks (local, no network).
func (t Tracker) CodeTracked() []string {
	var out []string
	for p, n := range CodeTrackedCounts(t.Root, t.Paths) {
		if n > 0 {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// CodeTrackedCounts is, per work path, how many files the code repo at
// root tracks under it.
func CodeTrackedCounts(root string, paths []string) map[string]int {
	counts := map[string]int{}
	if len(paths) == 0 {
		return counts
	}
	out, err := git(root, append([]string{"ls-files", "-z", "--"}, paths...)...)
	if err != nil {
		return counts
	}
	for _, f := range strings.Split(out, "\x00") {
		if f == "" {
			continue
		}
		for _, p := range paths {
			if f == p || strings.HasPrefix(f, p+"/") {
				counts[p]++
				break
			}
		}
	}
	return counts
}

// SyncResult says what Sync did.
type SyncResult struct {
	Committed int    // files in the new commit (0: nothing changed)
	Pulled    bool   // the remote had commits that were rebased under ours
	Pushed    bool   // the branch was pushed
	NoRemote  bool   // no origin: committed locally only
	Branch    string // the tracker's branch
}

// Sync commits every change under the work paths ("tracker: N files"), then
// pulls with rebase and pushes when origin is set. A conflict aborts the
// rebase, so the local commit stays and nothing in the checkout is left
// half-merged. The result, good or bad, goes to the sync file.
func (t Tracker) Sync(now time.Time) (SyncResult, error) {
	res, err := t.sync()
	t.record(now, res, err)
	return res, err
}

func (t Tracker) sync() (SyncResult, error) {
	var res SyncResult
	if !t.Exists() {
		return res, fmt.Errorf("no tracker git folder at %s: issue-cli tracker init", t.GitDir)
	}
	branch, err := t.git("symbolic-ref", "--short", "HEAD")
	if err != nil || branch == "" {
		branch = "main"
	}
	res.Branch = branch
	if paths := t.present(); len(paths) > 0 {
		if _, err := t.git(append([]string{"add", "-A", "-f", "--"}, paths...)...); err != nil {
			return res, err
		}
	}
	staged, err := t.git("diff", "--cached", "--name-only")
	if err != nil {
		return res, err
	}
	if n := len(lines(staged)); n > 0 {
		msg := fmt.Sprintf("tracker: %d %s", n, plural(n, "file", "files"))
		if _, err := t.git("-c", "commit.gpgsign=false", "commit", "-q", "--no-verify", "-m", msg); err != nil {
			return res, err
		}
		res.Committed = n
	}
	if url, _ := t.git("remote", "get-url", "origin"); url == "" {
		res.NoRemote = true
		return res, nil
	}
	heads, err := t.git("ls-remote", "--heads", "origin", branch)
	if err != nil {
		return res, err
	}
	ahead := true
	if heads != "" {
		if _, err := t.git("fetch", "-q", "origin", branch); err != nil {
			return res, err
		}
		upstream := "refs/remotes/origin/" + branch
		if _, err := t.git("update-ref", upstream, "FETCH_HEAD"); err != nil {
			return res, err
		}
		if _, err := t.git("rev-parse", "-q", "--verify", "HEAD"); err != nil {
			// A new machine with nothing of its own yet: take the remote as is.
			if _, err := t.git("update-ref", "refs/heads/"+branch, upstream); err != nil {
				return res, err
			}
			if _, err := t.git("reset", "-q", "--hard"); err != nil {
				return res, err
			}
			res.Pulled = true
		} else if n, _ := t.git("rev-list", "--count", "HEAD.."+upstream); n != "0" {
			if _, err := t.git("-c", "commit.gpgsign=false", "rebase", "-q", "--autostash", upstream); err != nil {
				t.git("rebase", "--abort")
				return res, fmt.Errorf("the tracker's remote has changes that conflict with ours (local commits kept, nothing pushed; resolve with git --git-dir=%s rebase origin/%s): %w", t.GitDir, branch, err)
			}
			res.Pulled = true
		}
		n, _ := t.git("rev-list", "--count", upstream+"..HEAD")
		ahead = n != "0"
	}
	if ahead {
		if _, err := t.git("push", "-q", "origin", "HEAD:refs/heads/"+branch); err != nil {
			return res, fmt.Errorf("push to the tracker failed (local commits kept): %w", err)
		}
		res.Pushed = true
	}
	return res, nil
}

// present is the work paths that exist on disk or that the tracker tracks
// (so a deleted folder's files are removed from the tracker too).
func (t Tracker) present() []string {
	var out []string
	for _, p := range t.Paths {
		if _, err := os.Stat(filepath.Join(t.Root, filepath.FromSlash(p))); err == nil {
			out = append(out, p)
			continue
		}
		if ls, _ := t.git("ls-files", "--", p); ls != "" {
			out = append(out, p)
		}
	}
	return out
}

func (t Tracker) record(now time.Time, res SyncResult, err error) {
	prev := t.ReadSync()
	st := SyncState{At: now, OK: err == nil, Committed: res.Committed}
	if prev != nil {
		st.LastOK = prev.LastOK
	}
	if err != nil {
		st.Error = err.Error()
	} else {
		st.LastOK = now
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	_ = os.WriteFile(filepath.Join(t.GitDir, SyncFile), append(data, '\n'), 0o644)
}

// ReadSync is the last sync record, nil when there is none.
func (t Tracker) ReadSync() *SyncState {
	data, err := os.ReadFile(filepath.Join(t.GitDir, SyncFile))
	if err != nil {
		return nil
	}
	var st SyncState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return &st
}

// LastBackup is the time of the tracker's newest commit (zero without one).
func (t Tracker) LastBackup() time.Time {
	out, err := t.git("log", "-1", "--format=%ct")
	if err != nil || out == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// RepoName is the tracker remote's repo name (ai-workflow-engine-tracker),
// or the git folder's name without a remote.
func (t Tracker) RepoName() string {
	url, _ := t.git("remote", "get-url", "origin")
	if url == "" {
		return strings.TrimSuffix(filepath.Base(t.GitDir), ".git")
	}
	return RepoNameFromURL(url)
}

// RepoNameFromURL is the last path element of a git URL without .git.
func RepoNameFromURL(url string) string {
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	return path.Base(url)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
