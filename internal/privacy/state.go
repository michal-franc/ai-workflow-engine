package privacy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Level is a badge's colour: green private and working, amber something
// to fix, red the work or the history went where it shouldn't, grey public
// by the owner's choice or not set up.
type Level string

const (
	Green Level = "green"
	Amber Level = "amber"
	Red   Level = "red"
	Grey  Level = "grey"
)

// Badge is one of the two privacy badges (work files, code) as the Status
// title, its Health card line, the projects list and `privacy check` show
// it. Everything comes from local files and git; nothing touches the
// network.
type Badge struct {
	Kind     string // "work" or "code"
	Level    Level
	Label    string // the badge text: Work private, No backup, …
	Marker   string // the projects list's word: Private, No backup, In code repo, Public, Not set up
	Sentence string // title / aria-label / Health line
	Fix      string // what to do, empty when nothing
	Note     string // extra advice on a quiet line (going public from a private repo)
	Where    string // the projects list's Where column
	NotSetUp bool   // the owner has not chosen yet
}

// setupFix points at the feature's card, where the owner is asked.
const setupFix = "Set up a private tracker on the Updates tab"

// goPublicNote is what a private repo needs before it goes public.
const goPublicNote = "To make it public: issue-cli tracker init --remote <private tracker repo>, issue-cli release setup --dev <private dev repo>, then clean the public history once from a mirror (docs/workflow.md, Private work and code)"

// Input is what a badge is computed from.
type Input struct {
	Name   string // project name, for the sentences
	Root   string // the checkout (code repo root)
	GitDir string // the tracker's git folder (DefaultGitDir when empty)
	Slug   string
	Cfg    *tracker.PrivacyConfig
	Now    time.Time
	// Visibility is the code repo's (origin's) cached visibility on GitHub:
	// "public", "private" or "" (unknown). See visibility.go.
	Visibility string
}

// TrackerFor is the project's tracker as in describes it.
func (in Input) TrackerFor() Tracker {
	dir := in.GitDir
	if dir == "" {
		dir = DefaultGitDir(in.Root, in.Slug)
	}
	return Tracker{Root: in.Root, GitDir: dir, Paths: in.Cfg.Paths()}
}

// WorkBadge is the work files badge.
func WorkBadge(in Input) Badge {
	b := Badge{Kind: "work"}
	paths := in.Cfg.Paths()
	counts := CodeTrackedCounts(in.Root, paths)
	var tracked []string
	var where []string
	for _, p := range paths {
		if n := counts[p]; n > 0 {
			tracked = append(tracked, fmt.Sprintf("%s/ (%d)", p, n))
			where = append(where, fmt.Sprintf("%s %d", p, n))
		}
	}
	name := in.Name
	if name == "" {
		name = "the code repo"
	}
	switch {
	case in.Cfg != nil && in.Cfg.WorkFiles == tracker.WorkFilesInRepo:
		b.Level, b.Label, b.Marker = Grey, "Work public", "Public"
		b.Sentence = "Work files public by choice"
		if d := strings.TrimSpace(in.Cfg.Decided); d != "" {
			b.Sentence += " (" + d + ")"
		}
		b.Sentence += ": they live in the code repo"
		b.Where = "public by choice"
		return b
	case len(tracked) > 0 && in.Visibility == VisibilityPrivate && !in.Cfg.Tracker():
		// Not leaked: the code repo itself is private. Info, not a warning.
		b.Level, b.Label, b.Marker = Green, "Private repo", "Private repo"
		b.Sentence = fmt.Sprintf("Work files are in the code repo (%s), which is private on GitHub", joinAnd(tracked))
		b.Note = goPublicNote
		b.Where = "private repo · " + strings.Join(where, " · ")
		b.NotSetUp = in.Cfg == nil || in.Cfg.WorkFiles == ""
		return b
	case len(tracked) > 0:
		b.Level, b.Label, b.Marker = Red, "Work in code repo", "In code repo"
		b.Sentence = fmt.Sprintf("Work files are in the code repo: %s %s tracked by %s", joinAnd(tracked), plural(len(tracked), "is", "are"), name)
		b.Where = strings.Join(where, " · ")
		if in.Cfg.Tracker() {
			b.Fix = "git rm -r --cached -- " + strings.Join(trackedPaths(paths, counts), " ") + " and commit: the tracker keeps them"
		} else {
			b.Fix = setupFix
		}
		b.NotSetUp = in.Cfg == nil || in.Cfg.WorkFiles == ""
		return b
	case in.Cfg.Tracker():
		return trackerBadge(in, b)
	}
	b.NotSetUp = true
	if anyExists(in.Root, paths) {
		b.Level, b.Label, b.Marker = Amber, "No backup", "No backup"
		b.Sentence = "No private tracker: " + joinAnd(slashed(existing(in.Root, paths))) + " are kept out of git, but nothing backs them up"
		b.Fix = setupFix
		b.Where = "ignored, on this disk only"
		return b
	}
	b.Level, b.Label, b.Marker = Grey, "Not set up", "Not set up"
	b.Sentence = "Work files privacy is not set up: issues, sessions and retros could live in a private tracker"
	return b
}

func trackerBadge(in Input, b Badge) Badge {
	t := in.TrackerFor()
	if !t.Exists() {
		b.Level, b.Label, b.Marker = Amber, "No backup", "No backup"
		b.Sentence = "privacy.work_files is tracker, but there is no tracker git folder at " + t.GitDir
		b.Fix = "issue-cli tracker init --remote <private repo url>"
		b.Where = "tracker missing"
		return b
	}
	repo := t.RepoName()
	if url, _ := t.git("remote", "get-url", "origin"); url == "" {
		b.Level, b.Label, b.Marker = Amber, "No backup", "No backup"
		b.Sentence = "The tracker " + repo + " has no remote, so its history is on this disk only"
		b.Fix = "issue-cli tracker init --remote <private repo url>"
		b.Where = repo + " · no remote"
		return b
	}
	last := t.LastBackup()
	if st := t.ReadSync(); st != nil && !st.OK {
		b.Level, b.Label, b.Marker = Amber, "Backup failing", "Backup failing"
		since := "never"
		if !st.LastOK.IsZero() {
			since = Ago(in.Now, st.LastOK)
		}
		b.Sentence = fmt.Sprintf("The tracker could not sync to %s (last ok %s): %s", repo, since, st.Error)
		b.Fix = "issue-cli tracker sync"
		b.Where = repo + " · failing"
		return b
	}
	b.Level, b.Label, b.Marker = Green, "Work private", "Private"
	ago := "no backup yet"
	if !last.IsZero() {
		ago = Ago(in.Now, last)
	}
	b.Sentence = fmt.Sprintf("Work files private: %s, last backup %s", repo, ago)
	b.Where = repo + " · " + ago
	return b
}

func trackedPaths(paths []string, counts map[string]int) []string {
	var out []string
	for _, p := range paths {
		if counts[p] > 0 {
			out = append(out, p)
		}
	}
	return out
}

func existing(root string, paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func anyExists(root string, paths []string) bool { return len(existing(root, paths)) > 0 }

func slashed(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p + "/"
	}
	return out
}

// joinAnd is "a", "a and b", "a, b and c".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// Ago is "just now", "12m ago", "3h ago", "2d ago".
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// ForProject is the Input for proj with privacy config cfg: its main
// checkout (not a worktree) and its tracker git folder (tracker_git_dir in
// projects.yaml, ~ expanded, or the default).
func ForProject(proj *tracker.Project, cfg *tracker.PrivacyConfig, now time.Time) Input {
	root := MainCheckout(proj.TelemetryRoot())
	in := Input{Name: proj.Name, Root: root, Slug: proj.Slug, Cfg: cfg, Now: now, Visibility: Visibility(root)}
	if dir := strings.TrimSpace(proj.TrackerGitDir); dir != "" {
		if rest, ok := strings.CutPrefix(dir, "~/"); ok {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, rest)
			}
		}
		in.GitDir = dir
	}
	return in
}

// MainCheckout is the main checkout of the repository dir is in (the first
// `git worktree list` entry), so a worktree's commands act on the real one.
// Falls back to dir.
func MainCheckout(dir string) string {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return dir
	}
	for _, l := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok {
			return p
		}
	}
	return dir
}

// CodeBadge is the code badge: whether development is private and the
// public repo gets release snapshots only. It reads the public branch's
// remote-tracking ref (as of the last fetch); no network.
func CodeBadge(in Input) Badge {
	b := Badge{Kind: "code"}
	cfg := in.Cfg
	switch {
	case (cfg == nil || cfg.Code == "") && in.Visibility == VisibilityPrivate:
		b.Level, b.Label, b.Marker, b.NotSetUp = Green, "Private repo", "Private repo", true
		b.Sentence = "Code in a private repo on GitHub: its history isn't public"
		b.Note = goPublicNote
		b.Where = "private repo"
		return b
	case cfg == nil || cfg.Code == "":
		b.Level, b.Label, b.Marker, b.NotSetUp = Grey, "Not set up", "Not set up", true
		b.Sentence = "Code privacy is not set up: develop in a private repo and publish releases only"
		return b
	case cfg.Code == tracker.CodePublic:
		if !HookInstalled(in.Root) {
			b.Level, b.Label, b.Marker = Amber, "Guard off", "Guard off"
			b.Sentence = "Code public by choice, but this clone has no pre-push guard, so commit messages are not checked for quotes"
			b.Fix = "issue-cli privacy hook"
			return b
		}
		b.Level, b.Label, b.Marker = Grey, "Code public", "Public"
		b.Sentence = "Code public by choice"
		if d := strings.TrimSpace(cfg.Decided); d != "" {
			b.Sentence += " (" + d + ")"
		}
		b.Sentence += ": developed in the open; the guard checks commit messages for quotes before a push"
		b.Where = "public by choice"
		return b
	}
	remote, branch := cfg.Remote(), cfg.Branch()
	dev, _ := git(in.Root, "remote", "get-url", "origin")
	if pub, _ := git(in.Root, "remote", "get-url", remote); pub == "" {
		b.Level, b.Label, b.Marker = Amber, "Not switched", "Not switched"
		b.Sentence = "privacy.code is snapshots, but there is no " + remote + " remote yet: origin still gets the dev history"
		b.Fix = "issue-cli release setup --dev <private dev repo url>"
		b.Where = "setup pending"
		return b
	}
	if !HookInstalled(in.Root) {
		b.Level, b.Label, b.Marker = Amber, "Guard off", "Guard off"
		b.Sentence = "This clone has no pre-push guard, so a push to " + remote + " is not checked"
		b.Fix = "issue-cli privacy hook"
		b.Where = "guard off"
		return b
	}
	tip, _ := git(in.Root, "rev-parse", "-q", "--verify", "refs/remotes/"+remote+"/"+branch)
	base, _ := git(in.Root, "config", publicBaseKey)
	last := "nothing published yet"
	if tip != "" && tip != base {
		if !IsSnapshot(in.Root, tip) {
			n := 0
			if base != "" {
				c, _ := git(in.Root, "rev-list", "--count", base+".."+tip)
				n, _ = strconv.Atoi(c)
			}
			b.Level, b.Label, b.Marker = Red, "Dev history public", "Dev history public"
			what := "a commit that is not a release snapshot"
			if n > 1 {
				what = fmt.Sprintf("%d commits that are not release snapshots", n)
			}
			b.Sentence = fmt.Sprintf("Dev history is public: %s/%s has %s (newest %s)", remote, branch, what, short(tip))
			b.Fix = "Clean it from a mirror (docs/workflow.md, Private work and code)"
			b.Where = remote + "/" + branch + " not a snapshot"
			return b
		}
		last = "last published "
		if subj, _ := git(in.Root, "log", "-1", "--format=%s", tip); subj != "" {
			last += subj
		}
	}
	// The public tip is what GitHub shows as the latest release: unsigned, it
	// reads Unverified there.
	if tip != "" && !commitSigned(in.Root, tip) {
		b.Level, b.Label, b.Marker = Amber, "Release unsigned", "Unsigned"
		b.Sentence = fmt.Sprintf("%s/%s's tip %s is not signed, so GitHub shows it Unverified", remote, branch, short(tip))
		if key, _ := git(in.Root, "config", "user.signingkey"); key == "" {
			b.Fix = "git config user.signingkey <a key registered on GitHub>; release publish then signs each snapshot"
		} else {
			b.Fix = "the next issue-cli release publish signs its snapshot with user.signingkey"
		}
		b.Where = remote + "/" + branch + " unsigned"
		return b
	}
	b.Level, b.Label, b.Marker = Green, "Code private", "Private"
	b.Sentence = fmt.Sprintf("Code private: develops in %s; %s gets one snapshot per release, %s", RepoNameFromURL(dev), remote, last)
	b.Where = strings.TrimPrefix(last, "last published ")
	if b.Where == "nothing published yet" {
		b.Where = "no release yet"
	} else {
		b.Where = "public " + b.Where
	}
	return b
}

// commitSigned reports whether sha carries a signature (gpgsig header, GPG
// or SSH). It does not verify it: that needs the signer's keys, and GitHub
// is the one that shows Verified.
func commitSigned(root, sha string) bool {
	out, err := git(root, "cat-file", "commit", sha)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			return false // end of the header
		}
		if strings.HasPrefix(line, "gpgsig ") || strings.HasPrefix(line, "gpgsig-sha256 ") {
			return true
		}
	}
	return false
}
