package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The design doc panel marks each "## " section of a system's design doc
// (D-3): stale when it cites a replaced decision, changed / new / removed when
// an open branch edited it since its merge-base with main, current otherwise.
// Comparing a branch's copy with main's would flag every old branch that is
// behind main, so only the branch's own edits count.

// Section states, in the order the panel counts them.
const (
	sectionCurrent = "current"
	sectionStale   = "stale"
	sectionChanged = "changed on branch"
	sectionNew     = "new on branch"
	sectionRemoved = "removed on branch"
)

// docBranch is an open branch the doc panel scans: a worktree or an
// unmerged work/* branch with commits ahead of main.
type docBranch struct {
	Name      string
	IssueSlug string
	MergeBase string
	Head      string
	Ahead     int
}

// openDocBranches lists the open branches that edit doc; ok is false when
// the scan ran out of time. It is a var so tests can stub git.
var openDocBranches = func(root, doc string, issues []*tracker.Issue) ([]docBranch, bool) {
	return listDocBranches(root, doc, issues)
}

// DocPanel is the design doc panel of a system page.
type DocPanel struct {
	Path        string
	Href        string
	Missing     bool     // not on main
	OnBranches  []string // branches that have it while main doesn't
	Sections    []DocSectionView
	Current     int
	Changed     int // changed, new or removed on a branch
	Stale       int
	Branches    int // branches that edit the doc
	ScanSkipped bool
}

// DocSectionView is one row of the panel.
type DocSectionView struct {
	Title    string
	Href     string
	State    string
	Tone     string
	Note     string
	NoteHref string
}

func (s *Server) docPanel(proj *tracker.Project, issues []*tracker.Issue, prefix, doc, href string, index *tracker.DecisionIndex) *DocPanel {
	root := projectRoot(proj)
	p := &DocPanel{Path: doc, Href: href}
	branches, ok := openDocBranches(root, doc, issues)
	p.ScanSkipped = !ok

	raw, err := os.ReadFile(filepath.Join(root, doc))
	if err != nil {
		p.Missing = true
		for _, b := range branches {
			if _, ok := gitShow(root, b.Head, doc); ok {
				p.OnBranches = append(p.OnBranches, b.Name)
			}
		}
		return p
	}
	body := string(raw)
	sections := tracker.SplitSections(body)
	anchors := tracker.SectionAnchors(body)
	byTitle := map[string]int{}
	for i, sec := range sections {
		v := DocSectionView{Title: sec.Title, State: sectionCurrent, Tone: "green"}
		if href != "" && len(anchors) == len(sections) && anchors[i] != "" {
			v.Href = href + "#" + anchors[i]
		}
		for _, e := range index.CitedIn(sec.Text) {
			if e.ReplacedBy != nil {
				v.State, v.Tone = sectionStale, "red"
				v.Note = fmt.Sprintf("cites %s, replaced by %s", e.ID, e.ReplacedBy.ID)
				v.NoteHref = prefix + "/issue/" + e.Issue.Slug
				break
			}
		}
		byTitle[sectionKey(sec.Title)] = len(p.Sections)
		p.Sections = append(p.Sections, v)
	}

	for _, b := range branches {
		changes := branchSectionChanges(root, doc, b)
		if len(changes) == 0 {
			continue
		}
		p.Branches++
		for _, c := range changes {
			note := shortBranch(b.Name)
			if b.Ahead > 0 {
				note += " · " + plural(b.Ahead, "commit", "commits")
			}
			noteHref := ""
			if b.IssueSlug != "" {
				noteHref = prefix + "/issue/" + b.IssueSlug
			}
			i, known := byTitle[sectionKey(c.title)]
			switch {
			case c.state == sectionNew && !known:
				byTitle[sectionKey(c.title)] = len(p.Sections)
				p.Sections = append(p.Sections, DocSectionView{Title: c.title, State: sectionNew, Tone: "blue", Note: note, NoteHref: noteHref})
			case known && p.Sections[i].State == sectionCurrent:
				tone := "amber"
				if c.state == sectionRemoved {
					tone = "red"
				}
				p.Sections[i].State, p.Sections[i].Tone, p.Sections[i].Note, p.Sections[i].NoteHref = c.state, tone, note, noteHref
			}
		}
	}
	for _, v := range p.Sections {
		switch v.State {
		case sectionCurrent:
			p.Current++
		case sectionStale:
			p.Stale++
		default:
			p.Changed++
		}
	}
	return p
}

// shortBranch drops the work/<system>/ prefix and cuts long slugs.
func shortBranch(name string) string {
	short := name
	if rest, ok := strings.CutPrefix(name, "work/"); ok {
		if _, slug, ok := strings.Cut(rest, "/"); ok {
			short = slug
		} else {
			short = rest
		}
	}
	if r := []rune(short); len(r) > 28 {
		short = string(r[:27]) + "…"
	}
	return short
}

func sectionKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

type sectionChange struct {
	title string
	state string
}

// branchSectionChanges compares the doc's sections at the branch's
// merge-base with its sections at the branch head. Results are cached by
// (merge-base, head, doc).
func branchSectionChanges(root, doc string, b docBranch) []sectionChange {
	if b.Ahead <= 0 || b.MergeBase == "" || b.Head == "" {
		return nil
	}
	key := root + "\x00" + b.MergeBase + "\x00" + b.Head + "\x00" + doc
	sectionCache.Lock()
	cached, hit := sectionCache.m[key]
	sectionCache.Unlock()
	if hit {
		return cached
	}
	baseText, _ := gitShow(root, b.MergeBase, doc)
	headText, headOK := gitShow(root, b.Head, doc)
	var out []sectionChange
	if headOK || baseText != "" {
		out = diffSections(baseText, headText)
	}
	sectionCache.Lock()
	sectionCache.m[key] = out
	sectionCache.Unlock()
	return out
}

var sectionCache = struct {
	sync.Mutex
	m map[string][]sectionChange
}{m: map[string][]sectionChange{}}

func diffSections(base, head string) []sectionChange {
	before := map[string]string{}
	var order []string
	for _, s := range tracker.SplitSections(base) {
		k := sectionKey(s.Title)
		before[k] = s.Text
		order = append(order, s.Title)
	}
	after := map[string]bool{}
	var out []sectionChange
	for _, s := range tracker.SplitSections(head) {
		k := sectionKey(s.Title)
		after[k] = true
		old, existed := before[k]
		switch {
		case !existed:
			out = append(out, sectionChange{title: s.Title, state: sectionNew})
		case old != s.Text:
			out = append(out, sectionChange{title: s.Title, state: sectionChanged})
		}
	}
	for _, t := range order {
		if !after[sectionKey(t)] {
			out = append(out, sectionChange{title: t, state: sectionRemoved})
		}
	}
	return out
}

// gitTimeout bounds every git call the doc panel makes.
const gitTimeout = 5 * time.Second

func gitOutput(root string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// gitShow returns the doc's text at rev, ok false when it isn't there.
func gitShow(root, rev, doc string) (string, bool) {
	return gitOutput(root, "show", rev+":"+filepath.ToSlash(doc))
}

// listDocBranches adapts tracker.OpenBranches (worktrees plus unmerged
// work/* branches whose issue is open; MergeBase is taken against the branch's
// own base) to those ahead of their base that change doc.
func listDocBranches(root, doc string, issues []*tracker.Issue) ([]docBranch, bool) {
	branches, err := tracker.OpenBranchesErr(root, "", issues)
	if errors.Is(err, tracker.ErrNoGit) {
		return nil, true
	}
	touches := map[string]bool{}
	for _, c := range tracker.ChangedOnBranches(root, branches, filepath.ToSlash(doc)) {
		touches[c.Branch] = true
	}
	var out []docBranch
	for _, b := range branches {
		if b.Ahead <= 0 || !touches[b.Name] {
			continue
		}
		out = append(out, docBranch{Name: b.Name, IssueSlug: b.IssueSlug, MergeBase: b.MergeBase, Head: b.Head, Ahead: b.Ahead})
	}
	return out, err == nil
}
