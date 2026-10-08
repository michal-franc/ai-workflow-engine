package tracker

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Balance notes are comments on the source issue that start with the
// project's balance prefix (workbench.balance.comment_prefix, default
// "balance:"). A typed note names the metric it moves and its state:
//
//	balance: full-holds · open
//	hypothesis: Defenders revive too freely.
//	change: FinishChance ↑ (proposed)
//	evidence: finish rate 0% across 200 matches
//	verdict: Needs the finish action first.
//	pass: 3
//
// Free-text balance comments are kept as "unsorted" notes. The balance board
// is generated from these comments; nothing else is stored.

const DefaultBalancePrefix = "balance:"

// Balance note states.
const (
	BalanceOpen          = "open"
	BalanceKept          = "kept"
	BalanceReverted      = "reverted"
	BalanceNeedsPieces   = "needs pieces"
	BalanceNotThisBranch = "not this branch"
)

// BalanceStates lists the states in board order.
var BalanceStates = []string{BalanceOpen, BalanceNeedsPieces, BalanceKept, BalanceReverted, BalanceNotThisBranch}

var (
	balanceMetricRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	balanceHeadRe   = regexp.MustCompile(`^(\S+)\s+·\s+(.+?)\s*$`)
	balanceFromRe   = regexp.MustCompile(`^\[#?([^\]\s]+)\]\s*`)
)

// NormalizeBalanceState maps "needs-pieces", "Not_This_Branch" etc. to a
// state, or "" when unknown.
func NormalizeBalanceState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", " ", "_", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	for _, st := range BalanceStates {
		if s == st {
			return st
		}
	}
	return ""
}

// BalanceNote is one typed balance comment.
type BalanceNote struct {
	Issue      *Issue   `json:"-"`
	Slug       string   `json:"slug"`
	CommentID  int      `json:"comment_id"`
	Date       string   `json:"date"`
	Metric     string   `json:"metric"`
	State      string   `json:"state"`
	Hypothesis string   `json:"hypothesis,omitempty"`
	Change     string   `json:"change,omitempty"`
	Evidence   string   `json:"evidence,omitempty"`
	Verdict    string   `json:"verdict,omitempty"`
	Pass       int      `json:"pass,omitempty"`
	Extra      []string `json:"extra,omitempty"`

	// History holds earlier notes with the same key, newest first.
	History []*BalanceNote `json:"history,omitempty"`
}

// Key identifies the experiment a note reports on: issue, metric, change.
func (n *BalanceNote) Key() string {
	return n.Slug + "|" + strings.ToLower(n.Metric) + "|" + strings.ToLower(strings.Join(strings.Fields(n.Change), " "))
}

// IsOpen reports whether the note still waits on a pass.
func (n *BalanceNote) IsOpen() bool {
	return n.State == BalanceOpen || n.State == BalanceNeedsPieces
}

// MatchesBalancePrefix reports whether text starts with prefix
// (case-insensitive) and returns the rest.
func MatchesBalancePrefix(prefix, text string) (string, bool) {
	if prefix == "" {
		prefix = DefaultBalancePrefix
	}
	t := strings.TrimLeft(text, " \t\n")
	if len(t) < len(prefix) || !strings.EqualFold(t[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(t[len(prefix):]), true
}

// ParseBalanceComment reads a comment as a balance note. isBalance is false
// when the comment doesn't start with the prefix; typed is false for a
// free-text balance comment.
func ParseBalanceComment(prefix, text string) (note BalanceNote, typed, isBalance bool) {
	rest, ok := MatchesBalancePrefix(prefix, text)
	if !ok {
		return BalanceNote{}, false, false
	}
	lines := strings.Split(strings.ReplaceAll(rest, "\r\n", "\n"), "\n")
	m := balanceHeadRe.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if m == nil || !balanceMetricRe.MatchString(m[1]) {
		return BalanceNote{}, false, true
	}
	state := NormalizeBalanceState(m[2])
	if state == "" {
		return BalanceNote{}, false, true
	}
	note = BalanceNote{Metric: m[1], State: state}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "hypothesis":
			note.Hypothesis = value
		case "change":
			note.Change = value
		case "evidence":
			note.Evidence = value
		case "verdict":
			note.Verdict = value
		case "pass":
			if n, err := strconv.Atoi(strings.TrimPrefix(value, "#")); err == nil && n > 0 {
				note.Pass = n
				continue
			}
			note.Extra = append(note.Extra, line)
		default:
			note.Extra = append(note.Extra, line)
		}
	}
	return note, true, true
}

// NewBalanceNote is the input for RenderBalanceNote.
type NewBalanceNote struct {
	Metric, State                         string
	Hypothesis, Change, Evidence, Verdict string
	Pass                                  int
}

// RenderBalanceNote validates n and formats it as comment text.
func RenderBalanceNote(prefix string, n NewBalanceNote) (string, error) {
	if prefix == "" {
		prefix = DefaultBalancePrefix
	}
	metric := strings.TrimSpace(n.Metric)
	switch {
	case metric == "":
		return "", fmt.Errorf("--metric is required (the metric id the note is about, e.g. full-holds)")
	case !balanceMetricRe.MatchString(metric):
		return "", fmt.Errorf("--metric %q must be one token of letters, digits, '.', '_' or '-' (e.g. full-holds or lanes.blocks.time_fighting)", n.Metric)
	}
	state := NormalizeBalanceState(n.State)
	if state == "" {
		return "", fmt.Errorf("--state %q: want one of open, kept, reverted, needs-pieces, not-this-branch", n.State)
	}
	if n.Pass < 0 {
		return "", fmt.Errorf("--pass must be a positive pass number")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s", prefix, metric, state)
	for _, f := range []struct{ key, value string }{
		{"hypothesis", n.Hypothesis}, {"change", n.Change}, {"evidence", n.Evidence}, {"verdict", n.Verdict},
	} {
		if v := oneLine(f.value); v != "" {
			fmt.Fprintf(&b, "\n%s: %s", f.key, v)
		}
	}
	if n.Pass > 0 {
		fmt.Fprintf(&b, "\npass: %d", n.Pass)
	}
	return b.String(), nil
}

// LooseBalanceNote is a free-text balance comment.
type LooseBalanceNote struct {
	Issue     *Issue `json:"-"`
	Slug      string `json:"slug"`
	From      string `json:"from,omitempty"` // source issue named by a leading [#slug]
	CommentID int    `json:"comment_id"`
	Date      string `json:"date"`
	Text      string `json:"text"`
}

// BalancePass is an issue marked `balance_pass: <n>`.
type BalancePass struct {
	Issue   *Issue
	Number  int
	Closed  bool
	Date    string
	Notes   int
	Planned bool // the lowest-numbered open pass
}

// BalanceGroup is every current note on one metric.
type BalanceGroup struct {
	Metric string
	Notes  []*BalanceNote
	Open   int
}

// BalanceBoard is the generated balance board.
type BalanceBoard struct {
	Groups   []*BalanceGroup
	Notes    []*BalanceNote // current notes, all groups
	Unsorted []LooseBalanceNote
	Passes   []BalancePass
	Planned  *BalancePass
	Counts   map[string]int // per state
}

// BuildBalanceBoard collects balance comments from every issue. comments
// returns an issue's comments in id order; isClosed tells past passes from
// planned ones.
func BuildBalanceBoard(issues []*Issue, comments func(*Issue) []Comment, prefix string, isClosed func(*Issue) bool) *BalanceBoard {
	b := &BalanceBoard{Counts: map[string]int{}}
	known := map[string]bool{}
	for _, issue := range issues {
		known[issue.Slug] = true
	}

	current := map[string]*BalanceNote{}
	var order []string
	for _, issue := range issues {
		cs := comments(issue)
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
		for _, c := range cs {
			note, typed, isBalance := ParseBalanceComment(prefix, c.Text)
			if !isBalance {
				continue
			}
			if !typed {
				rest, _ := MatchesBalancePrefix(prefix, c.Text)
				loose := LooseBalanceNote{Issue: issue, Slug: issue.Slug, CommentID: c.ID, Date: c.Date, Text: rest}
				if m := balanceFromRe.FindStringSubmatch(rest); m != nil {
					if slug := resolveRefSlug(strings.ToLower(m[1]), known); slug != "" && slug != issue.Slug {
						loose.From = slug
						loose.Text = strings.TrimSpace(rest[len(m[0]):])
					}
				}
				b.Unsorted = append(b.Unsorted, loose)
				continue
			}
			n := note
			n.Issue, n.Slug, n.CommentID, n.Date = issue, issue.Slug, c.ID, c.Date
			key := n.Key()
			if prev, ok := current[key]; ok {
				n.History = append([]*BalanceNote{prev}, prev.History...)
				prev.History = nil
			} else {
				order = append(order, key)
			}
			current[key] = &n
		}
	}

	groups := map[string]*BalanceGroup{}
	for _, key := range order {
		n := current[key]
		b.Notes = append(b.Notes, n)
		b.Counts[n.State]++
		gk := strings.ToLower(n.Metric)
		g := groups[gk]
		if g == nil {
			g = &BalanceGroup{Metric: n.Metric}
			groups[gk] = g
			b.Groups = append(b.Groups, g)
		}
		g.Notes = append(g.Notes, n)
		if n.IsOpen() {
			g.Open++
		}
	}
	for _, g := range b.Groups {
		sort.SliceStable(g.Notes, func(i, j int) bool {
			a, c := g.Notes[i], g.Notes[j]
			if ra, rc := balanceStateRank(a.State), balanceStateRank(c.State); ra != rc {
				return ra < rc
			}
			if a.Date != c.Date {
				return a.Date > c.Date
			}
			return a.Slug < c.Slug
		})
	}
	sort.SliceStable(b.Groups, func(i, j int) bool {
		a, c := b.Groups[i], b.Groups[j]
		if (a.Open > 0) != (c.Open > 0) {
			return a.Open > 0
		}
		if la, lc := latestBalanceDate(a), latestBalanceDate(c); la != lc {
			return la > lc
		}
		return strings.ToLower(a.Metric) < strings.ToLower(c.Metric)
	})
	sort.SliceStable(b.Unsorted, func(i, j int) bool { return b.Unsorted[i].Date > b.Unsorted[j].Date })

	b.Passes = collectBalancePasses(issues, isClosed)
	for i := range b.Passes {
		p := &b.Passes[i]
		if !p.Closed && b.Planned == nil {
			p.Planned = true
			b.Planned = p
		}
	}
	for _, n := range b.Notes {
		for i := range b.Passes {
			p := &b.Passes[i]
			if n.Pass == p.Number || (p.Planned && n.Pass == 0 && n.IsOpen()) {
				p.Notes++
			}
		}
	}
	return b
}

// PlannedPassFor is the pass a note counts towards: its own, or the planned
// pass for an open note without one (0 when none).
func (b *BalanceBoard) PlannedPassFor(n *BalanceNote) int {
	if n.Pass > 0 {
		return n.Pass
	}
	if n.IsOpen() && b.Planned != nil {
		return b.Planned.Number
	}
	return 0
}

func balanceStateRank(state string) int {
	for i, s := range BalanceStates {
		if s == state {
			return i
		}
	}
	return len(BalanceStates)
}

func latestBalanceDate(g *BalanceGroup) string {
	latest := ""
	for _, n := range g.Notes {
		if n.Date > latest {
			latest = n.Date
		}
	}
	return latest
}

func collectBalancePasses(issues []*Issue, isClosed func(*Issue) bool) []BalancePass {
	var passes []BalancePass
	for _, issue := range issues {
		for _, f := range issue.ExtraFields {
			if f.Key != "balance_pass" {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(f.Value, "#")))
			if err != nil || n <= 0 {
				continue
			}
			p := BalancePass{Issue: issue, Number: n, Closed: isClosed != nil && isClosed(issue)}
			p.Date = issue.Created
			if issue.DoneAt != "" {
				p.Date = issue.DoneAt
			}
			passes = append(passes, p)
		}
	}
	sort.SliceStable(passes, func(i, j int) bool { return passes[i].Number < passes[j].Number })
	return passes
}
