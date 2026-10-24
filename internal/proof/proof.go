// Package proof links an issue's acceptance criteria to their proof and
// checks every link. Links come from three places: `Proves:` trailers on the
// work branch's own commits, `proves:` on playtest steps (copied into each
// run's results.yaml), and `issue-cli prove` (the data sidecar's "proofs").
// Nothing is copied: each link points at something that exists, and Collect
// checks it every time it reads it.
package proof

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func init() {
	tracker.SetProofChecker(func(w *tracker.WorkflowConfig, issue *tracker.Issue, comments []tracker.Comment) error {
		return GateError(Collect(w.RepoRoot, issue, comments))
	})
}

// Where a link came from.
const (
	SourceProve    = "prove"
	SourceTrailer  = "trailer"
	SourcePlaytest = "playtest"
)

// Link is one criterion → proof link.
type Link struct {
	Kind string `json:"kind"` // tracker.ProofTest, ProofStep, …
	Ref  string `json:"ref"`
	// Source is where the link was declared; From names it (the commit
	// sha or the playtest session).
	Source string `json:"source"`
	From   string `json:"from,omitempty"`
	// Changed: a prove link whose criterion text differs from the text it
	// was linked to (the box was rewritten or renumbered since).
	Changed bool `json:"changed,omitempty"`
}

// Check is a link's check result.
type Check struct {
	OK    bool   `json:"ok"`
	State string `json:"state"`
	// Fix says what makes a failing check pass.
	Fix string `json:"fix,omitempty"`
}

// CheckedLink is a link with its check result.
type CheckedLink struct {
	Link
	Check Check `json:"check"`
}

// Criterion is one acceptance criterion with its links.
type Criterion struct {
	ID      string        `json:"id"`
	Text    string        `json:"text"`
	Checked bool          `json:"checked"`
	Visual  bool          `json:"visual"`
	Links   []CheckedLink `json:"links"`
}

// Proven reports whether any link passes its check.
func (c Criterion) Proven() bool {
	for _, l := range c.Links {
		if l.Check.OK {
			return true
		}
	}
	return false
}

// Stray is a declared link naming a criterion id the issue doesn't have.
type Stray struct {
	ID string `json:"id"`
	Link
}

// Report is everything Collect found for one issue.
type Report struct {
	Slug     string      `json:"slug"`
	Ref      string      `json:"ref,omitempty"`
	RefName  string      `json:"ref_name,omitempty"`
	Criteria []Criterion `json:"criteria"`
	Strays   []Stray     `json:"strays,omitempty"`
	// Missing names the sources that could not be read.
	Missing []string `json:"missing,omitempty"`
	// TestsAt is the newest tests: comment (zero: none). LastChange and
	// LastChangeSHA are the newest commit that changed code, the one a test
	// link's report must come after.
	TestsAt       time.Time `json:"tests_at,omitzero"`
	LastChange    time.Time `json:"last_change,omitzero"`
	LastChangeSHA string    `json:"last_change_sha,omitempty"`
}

// Unproven returns the criteria with no passing link.
func (r Report) Unproven() []Criterion {
	var out []Criterion
	for _, c := range r.Criteria {
		if !c.Proven() {
			out = append(out, c)
		}
	}
	return out
}

// Collect reads every link for issue's criteria and checks each one. root is
// the project checkout; the issue's worktree is root/.worktrees/<slug>.
// It never fails: a source it can't read is listed in Missing.
func Collect(root string, issue *tracker.Issue, comments []tracker.Comment) Report {
	if root == "" {
		root, _ = os.Getwd()
	}
	worktree := filepath.Join(root, ".worktrees", issue.Slug)
	r := Report{Slug: issue.Slug}
	criteria := tracker.Criteria(issue.BodyRaw)
	byID := map[string]int{}
	for i, it := range criteria {
		byID[strings.ToUpper(it.ID)] = i
		r.Criteria = append(r.Criteria, Criterion{ID: it.ID, Text: it.Label(), Checked: it.Checked, Visual: tracker.IsVisualCriterion(it.Text), Links: []CheckedLink{}})
	}
	prefix := criteriaPrefix(criteria)

	stats, _ := tracker.LoadStats(issue.FilePath)
	r.Ref, r.RefName = timeline.GitRef(root, issue.Slug, stats.Transitions)
	c := &checker{root: root, worktree: worktree, ref: r.Ref, slug: issue.Slug, tests: map[string]Check{}}
	if r.Ref == "" {
		r.Missing = append(r.Missing, "work/"+issue.Slug+": no branch, recorded branch head or merge sha; test and commit links can't be checked")
	}
	c.reportAt, c.hasReport = lastTestsComment(comments)

	add := func(id string, l Link) {
		i, ok := byID[strings.ToUpper(id)]
		if !ok {
			r.Strays = append(r.Strays, Stray{ID: id, Link: l})
			return
		}
		r.Criteria[i].Links = append(r.Criteria[i].Links, CheckedLink{Link: l})
	}

	// Trailers on the branch's own commits.
	commits, err := c.branchCommits(stats)
	if err != nil {
		r.Missing = append(r.Missing, "git log on "+r.RefName+": "+err.Error())
	}
	if lc, ok := lastChange(commits); ok {
		c.lastChange, r.LastChange, r.LastChangeSHA = lc.at, lc.at, lc.sha
	}
	if c.hasReport {
		r.TestsAt = c.reportAt
	}
	for _, cm := range commits {
		for _, value := range cm.proves {
			ids, tests := splitTrailer(value, prefix)
			for _, id := range ids {
				if len(tests) == 0 {
					add(id, Link{Kind: tracker.ProofCommit, Ref: cm.sha, Source: SourceTrailer, From: timeline.ShortSHA(cm.sha)})
				}
				for _, t := range tests {
					add(id, Link{Kind: tracker.ProofTest, Ref: t, Source: SourceTrailer, From: timeline.ShortSHA(cm.sha)})
				}
			}
		}
	}

	// proves: on playtest steps.
	runs, problems := timeline.LoadPlaytestRuns(nil, issue.Slug, root, worktree)
	r.Missing = append(r.Missing, problems...)
	c.steps = latestSteps(runs)
	for _, d := range declaredSteps(runs) {
		add(d.criterion, Link{Kind: tracker.ProofStep, Ref: d.step, Source: SourcePlaytest, From: d.session})
	}

	// issue-cli prove links.
	store, err := tracker.LoadData(issue.FilePath)
	if err != nil {
		r.Missing = append(r.Missing, "data sidecar: "+err.Error())
	}
	ids := make([]string, 0, len(store.Proofs))
	for id := range store.Proofs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, pl := range store.Proofs[id] {
			l := Link{Kind: pl.Kind, Ref: pl.Ref, Source: SourceProve}
			if i, ok := byID[strings.ToUpper(id)]; ok && pl.Criterion != "" && strings.TrimSpace(pl.Criterion) != strings.TrimSpace(criteria[i].Text) {
				l.Changed = true
			}
			add(id, l)
		}
	}

	var tests []string
	for _, cr := range r.Criteria {
		for _, l := range cr.Links {
			if l.Kind == tracker.ProofTest {
				tests = append(tests, l.Ref)
			}
		}
	}
	c.findTests(tests)
	for i := range r.Criteria {
		for j := range r.Criteria[i].Links {
			r.Criteria[i].Links[j].Check = c.check(r.Criteria[i].Links[j].Link)
		}
	}
	return r
}

// criteriaPrefix is the id prefix of the criteria boxes ("AC").
func criteriaPrefix(items []tracker.CheckboxItem) string {
	for _, it := range items {
		if it.ID != "" {
			return strings.TrimRightFunc(it.ID, func(r rune) bool { return r >= '0' && r <= '9' })
		}
	}
	return "AC"
}

var idShapeRe = regexp.MustCompile(`^([A-Za-z]+)\d+$`)

// splitTrailer reads one Proves: value ("AC1, AC3 TestA TestB"): words
// shaped like a criterion id (the criteria prefix plus a number) are ids,
// the rest test names.
func splitTrailer(value, prefix string) (ids, tests []string) {
	for _, w := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == ';' }) {
		if m := idShapeRe.FindStringSubmatch(w); m != nil && strings.EqualFold(m[1], prefix) {
			ids = append(ids, strings.ToUpper(w))
			continue
		}
		tests = append(tests, w)
	}
	return ids, tests
}

type commit struct {
	sha    string
	at     time.Time
	merge  bool
	proves []string
	files  []string
}

// lastChange is the time of the newest commit that changed code: merging
// main into the branch, or a commit that only touches docs, doesn't make a
// tests: report stale.
func lastChange(commits []commit) (commit, bool) {
	for _, c := range commits {
		if !c.merge && !docsOnly(c.files) {
			return c, true
		}
	}
	return commit{}, false
}

// docsOnly reports whether every file is documentation: under docs/, a
// Markdown file, or a CHANGELOG, in any folder. No files is not docs only.
func docsOnly(files []string) bool {
	for _, f := range files {
		base := path.Base(f)
		if !strings.HasPrefix(f, "docs/") && !strings.EqualFold(path.Ext(f), ".md") && !strings.HasPrefix(strings.ToUpper(base), "CHANGELOG") {
			return false
		}
	}
	return len(files) > 0
}

// lastTestsComment is the time of the newest tests: comment.
func lastTestsComment(comments []tracker.Comment) (time.Time, bool) {
	var last time.Time
	found := false
	for _, c := range comments {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Text)), "tests:") {
			continue
		}
		at, ok := tracker.CommentTime(c.Date)
		if !ok {
			continue
		}
		if !found || at.After(last) {
			last, found = at, true
		}
	}
	return last, found
}

type stepVerdict struct {
	status  string
	session string
}

// latestSteps is each step's latest verdict (pass or fail) across runs; a
// pending or skipped result is not a verdict.
func latestSteps(runs []timeline.PlaytestRun) map[string]stepVerdict {
	out := map[string]stepVerdict{}
	for _, run := range runs { // oldest first
		for _, st := range run.Steps {
			if st.Status == "pass" || st.Status == "fail" {
				out[strings.ToUpper(st.ID)] = stepVerdict{status: st.Status, session: run.Session}
			}
		}
	}
	return out
}

type declared struct{ criterion, step, session string }

// declaredSteps lists each step → criterion pair the runs declare, once.
func declaredSteps(runs []timeline.PlaytestRun) []declared {
	var out []declared
	seen := map[string]bool{}
	for i := len(runs) - 1; i >= 0; i-- { // newest first: the current session file wins
		for _, st := range runs[i].Steps {
			for _, id := range st.Proves {
				key := strings.ToUpper(st.ID) + "|" + strings.ToUpper(id)
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, declared{criterion: strings.ToUpper(strings.TrimSpace(id)), step: st.ID, session: runs[i].Session})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].step < out[j].step })
	return out
}

func git(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tracker.GitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	return string(out), err
}

func unixTime(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}
