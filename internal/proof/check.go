package proof

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// checker holds what the link checks share: the branch, its last change,
// the newest tests: report and each playtest step's latest verdict.
type checker struct {
	root, worktree, ref, slug string
	lastChange                time.Time
	reportAt                  time.Time
	hasReport                 bool
	steps                     map[string]stepVerdict
	tests                     map[string]Check
}

// branchCommits lists the work branch's own first-parent commits, newest
// first. A live branch is read as main..ref, so commits merged in from main
// never count. After the merge only the recorded head is left, so the walk
// stops at the issue's first dispatch or transition.
func (c *checker) branchCommits(stats tracker.StatsStore) ([]commit, error) {
	if c.ref == "" {
		return nil, nil
	}
	rng := LogRange(c.root, c.ref, stats)
	if rng == nil {
		return nil, nil
	}
	// Each record starts with \x1e, so the --name-only file list that
	// follows a commit's header stays in its record.
	args := []string{"log", "--first-parent", "--name-only", "--format=%x1e%H%x1f%ct%x1f%P%x1f%(trailers:key=Proves,valueonly,unfold,separator=%x1d)%x1f"}
	out, err := git(c.root, append(args, rng...)...)
	if err != nil {
		return nil, err
	}
	var commits []commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) < 5 {
			continue
		}
		cm := commit{sha: f[0], at: unixTime(f[1]), merge: len(strings.Fields(f[2])) > 1}
		for _, v := range strings.Split(f[3], "\x1d") {
			if v = strings.TrimSpace(v); v != "" {
				cm.proves = append(cm.proves, v)
			}
		}
		cm.files = strings.Fields(f[4])
		commits = append(commits, cm)
	}
	return commits, nil
}

// LogRange is the git log range of the work branch's own commits at ref:
// main..ref while the branch is live, so commits merged in from main never
// count; after the merge only the recorded head is left, so the walk stops
// at the issue's first dispatch or transition. nil: nothing to walk.
func LogRange(root, ref string, stats tracker.StatsStore) []string {
	if ref == "" {
		return nil
	}
	main, _ := tracker.MainBranch(root)
	if _, err := git(root, "merge-base", "--is-ancestor", ref, main); main != "" && err != nil {
		return []string{main + ".." + ref}
	}
	since := issueStart(stats)
	if since.IsZero() {
		return nil
	}
	return []string{"--since=" + strconv.FormatInt(since.Unix(), 10), ref}
}

// IsTestPath reports whether path holds tests in the usual layouts, the
// files a test link's name is looked up in.
func IsTestPath(path string) bool { return testPathRe.MatchString(path) }

// issueStart is when work on the issue began: its first dispatch, else its
// first transition.
func issueStart(stats tracker.StatsStore) time.Time {
	if len(stats.Dispatches) > 0 {
		return stats.Dispatches[0].At
	}
	if len(stats.Transitions) > 0 {
		return stats.Transitions[0].TS
	}
	return time.Time{}
}

func (c *checker) check(l Link) Check {
	switch l.Kind {
	case tracker.ProofTest:
		return c.checkTest(l.Ref)
	case tracker.ProofStep:
		// A declared step already names its session.
		return c.checkStep(l.Ref, l.Source != SourcePlaytest)
	case tracker.ProofCommit:
		return c.checkCommit(l)
	case tracker.ProofValue:
		return Check{OK: true, State: "reported"}
	case tracker.ProofShot:
		return c.checkShot(l.Ref)
	}
	return Check{State: "unknown link kind " + l.Kind}
}

// testPathRe matches files that hold tests in the usual layouts.
var testPathRe = regexp.MustCompile(`(?i)(_test\.|(^|/)test_|\.test\.|\.spec\.|(^|/)(tests?|__tests__)/|tests?\.(cs|java|kt|swift|py)$)`)

// findTests looks every test name up in one git grep at the ref, so a long
// list costs one walk of the tree instead of one per name.
func (c *checker) findTests(names []string) {
	var todo []string
	for _, n := range names {
		if _, ok := c.tests[n]; !ok && n != "" && !containsString(todo, n) {
			todo = append(todo, n)
		}
	}
	if c.ref == "" || len(todo) == 0 {
		return
	}
	args := []string{"grep", "-w", "-F", "-o", "-I"}
	for _, n := range todo {
		args = append(args, "-e", n)
	}
	out, _ := git(c.root, append(args, c.ref, "--")...)
	hit := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		// <ref>:<path>:<match>; the match is one of the names.
		rest := strings.TrimPrefix(line, c.ref+":")
		i := strings.LastIndex(rest, ":")
		if i < 0 {
			continue
		}
		if testPathRe.MatchString(rest[:i]) {
			hit[rest[i+1:]] = true
		}
	}
	for _, n := range todo {
		if hit[n] {
			c.tests[n] = Check{OK: true}
		} else {
			c.tests[n] = Check{State: "not found", Fix: "no test file on the branch has " + n + " as a whole word: check the name"}
		}
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (c *checker) checkTest(name string) Check {
	if c.ref == "" {
		return Check{State: "not checked: no work branch"}
	}
	c.findTests([]string{name})
	found := c.tests[name]
	if !found.OK {
		return found
	}
	if !c.hasReport {
		return Check{State: "no tests: report", Fix: fmt.Sprintf("run the tests, then: issue-cli comment %s --text \"tests: ...\"", c.slug)}
	}
	if !c.lastChange.IsZero() && c.reportAt.Before(c.lastChange) {
		return Check{State: "stale (code changed since the last tests: comment)", Fix: fmt.Sprintf("re-run the tests, then: issue-cli comment %s --text \"tests: ...\"", c.slug)}
	}
	return Check{OK: true, State: "reported after the last change"}
}

func (c *checker) checkStep(id string, nameSession bool) Check {
	v, ok := c.steps[strings.ToUpper(id)]
	in := ""
	if ok && nameSession {
		in = " in " + v.session
	}
	switch {
	case !ok:
		return Check{State: "not run", Fix: "run the playtest session that has step " + id + " (ux-test, or the owner's playtest for a judgement step)"}
	case v.status == "fail":
		return Check{State: "failed" + in, Fix: "fix it and run the session again"}
	}
	return Check{OK: true, State: "passed" + in}
}

func (c *checker) checkCommit(l Link) Check {
	if l.Source == SourceTrailer {
		return Check{OK: true, State: "found"}
	}
	if c.ref == "" {
		return Check{State: "not checked: no work branch"}
	}
	full, err := ResolveCommit(c.root, l.Ref)
	if err != nil {
		return Check{State: "not found", Fix: "no commit " + l.Ref + " in the repository"}
	}
	if _, err := git(c.root, "merge-base", "--is-ancestor", full, c.ref); err != nil {
		return Check{State: "not found", Fix: l.Ref + " is not on the work branch or its merge"}
	}
	return Check{OK: true, State: "found"}
}

func (c *checker) checkShot(path string) Check {
	for _, p := range shotCandidates(c.root, c.worktree, path) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return Check{OK: true, State: "shown"}
		}
	}
	return Check{State: "missing", Fix: path + " is not in the worktree or the project checkout"}
}

// shotCandidates are the files a --shot path can mean: itself when
// absolute, else under the worktree, then the project checkout.
func shotCandidates(root, worktree, path string) []string {
	if filepath.IsAbs(path) {
		return []string{path}
	}
	return []string{filepath.Join(worktree, path), filepath.Join(root, path)}
}

// InsideCheckout reports whether a --shot path resolves under the worktree
// or the project checkout.
func InsideCheckout(root, slug, path string) bool {
	worktree := filepath.Join(root, ".worktrees", slug)
	for _, p := range shotCandidates(root, worktree, path) {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		for _, base := range []string{worktree, root} {
			if b, err := filepath.Abs(base); err == nil && (abs == b || strings.HasPrefix(abs, b+string(filepath.Separator))) {
				return true
			}
		}
	}
	return false
}

// Ways prints the ways to prove criterion id, filled in.
func Ways(slug, id string) []string {
	return []string{
		fmt.Sprintf("a playtest step that shows it: add `proves: [%s]` to the step", id),
		fmt.Sprintf("a test: a `Proves: %s <TestName>` trailer on the commit that adds or changes it", id),
		fmt.Sprintf("the change itself: a `Proves: %s` trailer on its commit", id),
		fmt.Sprintf("or: issue-cli prove %s %s --test <Name> | --step <id> | --commit <sha> | --value \"<measured>\"", slug, id),
	}
}

// GateError is nil when every criterion is proven; otherwise it names each
// unproven one, why its links fail, and how to prove it.
func GateError(r Report) error {
	unproven := r.Unproven()
	if len(unproven) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d acceptance criteria are not proven:", len(unproven), len(r.Criteria))
	for _, c := range unproven {
		fmt.Fprintf(&b, "\n  %s %s", c.ID, c.Text)
		for _, l := range c.Links {
			fmt.Fprintf(&b, "\n    ✗ %s — %s", describe(l.Link), l.Check.State)
			if l.Check.Fix != "" {
				fmt.Fprintf(&b, "\n      → %s", l.Check.Fix)
			}
		}
	}
	fmt.Fprintf(&b, "\n\nProve each one with any of (shown for %s):", unproven[0].ID)
	for _, w := range Ways(r.Slug, unproven[0].ID) {
		fmt.Fprintf(&b, "\n  - %s", w)
	}
	fmt.Fprintf(&b, "\n\nEvery link and its check: issue-cli prove %s --list", r.Slug)
	return fmt.Errorf("%s", b.String())
}

// Describe renders a link as "test TestFoo (trailer 3f2a1c9)".
func Describe(l Link) string { return describe(l) }

func describe(l Link) string {
	ref := l.Ref
	if l.Kind == tracker.ProofCommit {
		ref = timeline.ShortSHA(ref)
	}
	if l.Kind == tracker.ProofValue {
		ref = strconv.Quote(ref)
	}
	s := l.Kind + " " + ref
	switch l.Source {
	case SourceTrailer:
		if l.Kind != tracker.ProofCommit {
			s += " (trailer on " + l.From + ")"
		} else {
			s += " (trailer)"
		}
	case SourcePlaytest:
		s += " (proves: in session " + l.From + ")"
	default:
		s += " (prove)"
	}
	if l.Changed {
		s += " [criterion text changed since linked]"
	}
	return s
}

// TickProven ticks the unticked boxes of the proven criteria and returns
// their ids. A ticked box whose proof stopped passing is left alone.
func TickProven(issuePath string, r Report) ([]string, error) {
	proven := map[string]bool{}
	for _, c := range r.Criteria {
		if c.Proven() && !c.Checked {
			proven[strings.ToUpper(c.ID)] = true
		}
	}
	if len(proven) == 0 {
		return nil, nil
	}
	var ticked []string
	_, _, err := tracker.UpdateIssueBody(issuePath, func(body string) (string, bool, error) {
		var lines []int
		for _, it := range tracker.Criteria(body) {
			if !it.Checked && proven[strings.ToUpper(it.ID)] {
				lines = append(lines, it.Line)
				ticked = append(ticked, it.ID)
			}
		}
		if len(lines) == 0 {
			return body, false, nil
		}
		return tracker.CheckLines(body, lines), true, nil
	})
	if err != nil {
		return nil, err
	}
	return ticked, nil
}

// ResolveCommit returns the full sha of a commit in the repository at root.
func ResolveCommit(root, sha string) (string, error) {
	out, err := git(root, "rev-parse", "--verify", "--quiet", sha+"^{commit}")
	full := strings.TrimSpace(out)
	if err != nil || full == "" {
		return "", fmt.Errorf("no commit %s in %s", sha, root)
	}
	return full, nil
}
