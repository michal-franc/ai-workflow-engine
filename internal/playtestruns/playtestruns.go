// Package playtestruns keeps .playtest/runs from piling up. A run folder is
// <runs>/<session>/<time>-<mode>/ with a results.yaml and a shots/ folder.
// A game's runner writes modes ux-test / playtest (Mode reads them as auto /
// judge), saves shots beside results.yaml and names a run that reused an
// older one's shots <time>-ux-test-cached. The Evidence tab needs only the
// latest auto and judge run of each of an issue's own sessions, so:
//
//   - Archive (at → done) keeps those, moves them from the issue's worktree
//     to the project root's .playtest/runs and deletes the runs a newer kept
//     run replaces, so removing the worktree loses no evidence;
//   - Prune does the same for existing worktrees, or for an open issue only
//     deletes the runs of other issues' sessions;
//   - Shrink turns the shots of issues done long ago into small JPEGs.
//
// See ui/evidence-tab-proof-shots-and-criteria D-5, D-7, D-8.
package playtestruns

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Run is one run folder.
type Run struct {
	Dir     string // absolute folder
	Name    string // <time>-<mode>; sorts by time
	Session string
	Issue   string
	Mode    string // auto or judge (Mode); another mode is kept as it is
	Reduced string
	Bytes   int64
	// Steps is how many steps results.yaml has: 0 is a header-only run (the
	// runner stopped, or the owner left a playtest).
	Steps int
	// Cached names the runs (<session>/<name>) whose shots a -cached run's
	// steps reuse.
	Cached []string
	// Unreadable: results.yaml is there but isn't YAML; Issue comes from its
	// issue: line, if any.
	Unreadable bool
}

// header is the part of results.yaml a run is sorted by.
type header struct {
	Session string `yaml:"session"`
	Issue   string `yaml:"issue"`
	Mode    string `yaml:"mode"`
	Reduced string `yaml:"reduced"`
	Steps   []struct {
		Cached string `yaml:"cached"`
	} `yaml:"steps"`
}

// Mode is a run's mode in ours: a game's runner names its modes after its
// commands, ux-test (auto) and playtest (judge). The Timeline and Evidence
// tabs read modes through it too.
func Mode(m string) string {
	switch m {
	case "ux-test":
		return "auto"
	case "playtest":
		return "judge"
	}
	return m
}

var issueLineRe = regexp.MustCompile(`(?m)^issue:\s*["']?([^"'\s]+)`)

// RunsDir is dir's .playtest/runs.
func RunsDir(dir string) string { return filepath.Join(dir, ".playtest", "runs") }

// WorktreeDir is the issue's worktree under root.
func WorktreeDir(root, slug string) string {
	return filepath.Join(root, ".worktrees", filepath.FromSlash(slug))
}

// List reads every run folder in runsDir. A folder without a readable
// results.yaml is listed with an empty Issue (an aborted run: nobody's
// evidence). A missing runsDir is no runs.
func List(runsDir string) ([]Run, error) {
	sessions, err := os.ReadDir(runsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []Run
	for _, s := range sessions {
		if !s.IsDir() || strings.HasPrefix(s.Name(), ".") {
			continue // .bin: the runner's binaries, not a session
		}
		entries, err := os.ReadDir(filepath.Join(runsDir, s.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(runsDir, s.Name(), e.Name())
			r := Run{Dir: dir, Name: e.Name(), Session: s.Name(), Bytes: dirBytes(dir)}
			if b, err := os.ReadFile(filepath.Join(dir, "results.yaml")); err == nil {
				var h header
				if yaml.Unmarshal(b, &h) == nil {
					r.Issue, r.Mode, r.Reduced, r.Steps = h.Issue, Mode(h.Mode), h.Reduced, len(h.Steps)
					if h.Session != "" {
						r.Session = h.Session
					}
					for _, st := range h.Steps {
						if c := cachedRun(st.Cached); c != "" {
							r.Cached = append(r.Cached, c)
						}
					}
				} else {
					r.Unreadable = true
					if m := issueLineRe.FindSubmatch(b); m != nil {
						r.Issue = string(m[1])
					}
				}
			}
			if r.Mode == "" {
				r.Mode = modeFromName(e.Name())
			}
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Session != runs[j].Session {
			return runs[i].Session < runs[j].Session
		}
		return runs[i].Name < runs[j].Name
	})
	return runs, nil
}

// modeFromName reads the mode from <date>-<time>-<mode>[-cached].
func modeFromName(name string) string {
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 3 {
		return ""
	}
	return Mode(strings.TrimSuffix(parts[2], "-cached"))
}

// cachedRun is <session>/<name> of a step's cached: path
// (.playtest/runs/<session>/<name>).
func cachedRun(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	parts := strings.Split(p, "/")
	if p == "." || len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// folder is the run's <session folder>/<name>, what a cached: path names.
func (r Run) folder() string {
	return filepath.Base(filepath.Dir(r.Dir)) + "/" + r.Name
}

// empty: no results.yaml and no file at all, an aborted run.
func (r Run) empty() bool {
	return r.Bytes == 0 && !r.Unreadable && r.Issue == ""
}

// Report says what an operation did, or would do in a dry run.
type Report struct {
	// Own: the runs whose results.yaml names the issue. Archived: those
	// kept, moved to the root or already there (done only).
	Own, Archived                       int
	Kept, Moved, Deleted                int
	KeptBytes, MovedBytes, DeletedBytes int64
	// Sessions are the own sessions whose runs were kept, sorted.
	Sessions []string
	// CacheBytes: the runner's cached binaries deleted with a done issue's
	// runs (counted in DeletedBytes too).
	CacheBytes int64
	// Shrink only.
	Issues      int
	BytesBefore int64
	BytesAfter  int64
}

// Add folds o into r.
func (r *Report) Add(o Report) {
	r.Own += o.Own
	r.Archived += o.Archived
	r.Kept += o.Kept
	r.Moved += o.Moved
	r.Deleted += o.Deleted
	r.KeptBytes += o.KeptBytes
	r.MovedBytes += o.MovedBytes
	r.DeletedBytes += o.DeletedBytes
	r.CacheBytes += o.CacheBytes
	r.Issues += o.Issues
	r.BytesBefore += o.BytesBefore
	r.BytesAfter += o.BytesAfter
	r.Sessions = mergeSorted(r.Sessions, o.Sessions)
}

func mergeSorted(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// keepAtDone picks the runs a done issue keeps. Of each own session and
// mode (auto, judge) it keeps the newest run with steps (the newest run
// when none has any) and every run newer than it: a run that names the
// issue goes only when a newer run of its session and mode is kept. It
// also keeps the runs a kept -cached run takes its shots from, an own run
// of another mode, and a run it can't classify (results.yaml that isn't
// YAML and names no other issue, or no results.yaml but files).
func keepAtDone(runs []Run, slug string) map[string]bool {
	keep := map[string]bool{}
	groups := map[string][]Run{}
	for _, r := range runs {
		switch {
		case r.Issue == slug && (r.Mode == "auto" || r.Mode == "judge"):
			k := r.Session + "|" + r.Mode
			groups[k] = append(groups[k], r)
		case r.Issue == slug, unclassified(r):
			keep[r.Dir] = true
		}
	}
	for _, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i].Name < g[j].Name })
		from := len(g) - 1
		for i := len(g) - 1; i >= 0; i-- {
			if g[i].Steps > 0 {
				from = i
				break
			}
		}
		for _, r := range g[from:] {
			keep[r.Dir] = true
		}
	}
	byFolder := map[string]string{}
	for _, r := range runs {
		byFolder[r.folder()] = r.Dir
	}
	for _, r := range runs {
		if !keep[r.Dir] {
			continue
		}
		for _, c := range r.Cached {
			if d, ok := byFolder[c]; ok {
				keep[d] = true
			}
		}
	}
	return keep
}

// unclassified: a run Prune can't tell the issue of, so it is kept.
func unclassified(r Run) bool {
	return r.Issue == "" && !r.empty()
}

// Archive is Prune for a done issue, applied.
func Archive(root, slug string) (Report, error) {
	return Prune(root, slug, true, false)
}

// Prune tidies the issue's worktree runs. done: keep what keepAtDone
// picks, move it to the root's .playtest/runs (a run already there is left
// in place and counted as kept), delete the rest. Not done: delete only the
// runs of other issues' sessions and empty aborted runs; the own runs and
// any run it can't classify stay for the work in progress. dryRun reports
// without touching a file.
func Prune(root, slug string, done, dryRun bool) (Report, error) {
	var rep Report
	runs, err := List(RunsDir(WorktreeDir(root, slug)))
	if err != nil {
		return rep, err
	}
	keep := keepAtDone(runs, slug)
	sessions := map[string]bool{}
	for _, r := range runs {
		own := r.Issue == slug
		if own {
			rep.Own++
		}
		switch {
		case done && keep[r.Dir]:
			if own {
				sessions[r.Session] = true
				rep.Archived++
			}
			target := filepath.Join(RunsDir(root), filepath.Base(filepath.Dir(r.Dir)), r.Name)
			if _, err := os.Stat(target); err == nil {
				rep.Kept++
				rep.KeptBytes += r.Bytes
				continue
			}
			rep.Moved++
			rep.MovedBytes += r.Bytes
			if !dryRun {
				if err := move(r.Dir, target); err != nil {
					return rep, err
				}
			}
		case !done && (own || unclassified(r)):
			if own {
				sessions[r.Session] = true
			}
			rep.Kept++
			rep.KeptBytes += r.Bytes
		default:
			rep.Deleted++
			rep.DeletedBytes += r.Bytes
			if !dryRun {
				if err := os.RemoveAll(r.Dir); err != nil {
					return rep, err
				}
			}
		}
	}
	for s := range sessions {
		rep.Sessions = append(rep.Sessions, s)
	}
	sort.Strings(rep.Sessions)
	// A done issue's worktree runs no more sessions: the runner's cached
	// binaries (runs/.bin, ~35 MB) go too. They are rebuilt when needed.
	if bin := filepath.Join(RunsDir(WorktreeDir(root, slug)), ".bin"); done {
		if n := dirBytes(bin); n > 0 {
			rep.CacheBytes = n
			rep.DeletedBytes += n
			if !dryRun {
				if err := os.RemoveAll(bin); err != nil {
					return rep, err
				}
			}
		}
	}
	if !dryRun {
		removeEmptySessionDirs(RunsDir(WorktreeDir(root, slug)))
	}
	return rep, nil
}

// move renames src to dst, copying when they are on different devices.
func move(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}

func removeEmptySessionDirs(runsDir string) {
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			os.Remove(filepath.Join(runsDir, e.Name())) // only succeeds when empty
		}
	}
}

func dirBytes(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// MB formats bytes as megabytes with one decimal below 10 MB.
func MB(b int64) string {
	mb := float64(b) / (1 << 20)
	if mb < 10 {
		return fmt.Sprintf("%.1f MB", mb)
	}
	return fmt.Sprintf("%.0f MB", mb)
}
