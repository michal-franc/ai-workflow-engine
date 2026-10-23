package timeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// GitTimeout bounds each git call, so a slow or locked repo never holds up
// a page.
const GitTimeout = 2 * time.Second

// ProjectRoot is the project's checkout: its workdir, else the folder that
// holds its issues folder, else the current directory.
func ProjectRoot(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	if proj != nil && proj.IssueDir != "" {
		if abs, err := filepath.Abs(proj.IssueDir); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(proj.IssueDir)
	}
	cwd, _ := os.Getwd()
	return cwd
}

// WorkDir is where the project's .agent-logs are: its workdir, else the
// current directory.
func WorkDir(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	wd, _ := os.Getwd()
	return wd
}

// Cache keeps what Load reads from files many issues share: the clilogs and
// the playtest runs' results, read once per Cache rather than once per
// issue. The aggregate uses one; a nil *Cache reads from disk on every call.
// Not safe for concurrent use.
type Cache struct {
	recs map[string][]clilog.Record
	runs map[string][]playtestResultsFile
}

func NewCache() *Cache {
	return &Cache{recs: map[string][]clilog.Record{}, runs: map[string][]playtestResultsFile{}}
}

func (c *Cache) read(path string, visit func(clilog.Record)) {
	if c == nil {
		clilog.Read(path, visit)
		return
	}
	recs, ok := c.recs[path]
	if !ok {
		clilog.Read(path, func(r clilog.Record) { recs = append(recs, r) })
		c.recs[path] = recs
	}
	for _, r := range recs {
		visit(r)
	}
}

// results are the playtest results.yaml files in dir's .playtest/runs.
func (c *Cache) results(dir string) []playtestResultsFile {
	if c != nil {
		if r, ok := c.runs[dir]; ok {
			return r
		}
	}
	var out []playtestResultsFile
	files, _ := filepath.Glob(filepath.Join(dir, ".playtest", "runs", "*", "*", "results.yaml"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r playtestResultsFile
		if yaml.Unmarshal(data, &r) == nil {
			out = append(out, r)
		}
	}
	if c != nil {
		c.runs[dir] = out
	}
	return out
}

// Load reads the issue's stats sidecar, clilogs, work branch and playtest
// runs. A source that can't be read is listed in Missing; it never fails.
// cache may be nil.
func Load(proj *tracker.Project, issue *tracker.Issue, cache *Cache) Sources {
	root := ProjectRoot(proj)
	workDir := WorkDir(proj)
	worktree := filepath.Join(root, ".worktrees", issue.Slug)
	projSlug := ""
	if proj != nil {
		projSlug = proj.Slug
	}
	var src Sources

	stats, statsErr := tracker.LoadStats(issue.FilePath)
	switch {
	case statsErr != nil:
		src.Missing = append(src.Missing, Missing{"Stats sidecar", statsErr.Error() + ". Status times and approvals are not shown."})
	case len(stats.Transitions) == 0:
		src.Missing = append(src.Missing, Missing{"Stats sidecar", "no transitions recorded in " + tracker.StatsSidecarPath(issue.FilePath) + ". Status times and approvals are not shown."})
	default:
		src.Read = append(src.Read, "stats sidecar ("+plural(len(stats.Transitions), "transition", "transitions")+", "+plural(countApprovals(stats.Transitions), "approval", "approvals")+")")
	}
	src.Transitions = stats.Transitions
	for _, d := range stats.Dispatches {
		src.Dispatches = append(src.Dispatches, d.At)
	}
	for i := len(src.Transitions) - 1; i >= 0; i-- {
		if src.Transitions[i].MergeSHA != "" {
			src.MergeSHA = src.Transitions[i].MergeSHA
			break
		}
	}

	src.Calls = LoadCalls(cache, LogPaths(workDir, worktree, issue), projSlug, issue.Slug)
	if len(src.Calls) == 0 {
		src.Missing = append(src.Missing, Missing{"CLI log", "no calls for this issue in .agent-logs/<agent>/<agent>.clilog (project and worktree), .agent-logs/clilog.jsonl or $TMPDIR/issue-cli-logs/actions.jsonl. Agent, waiting and idle time can't be split; the CLI calls filter is empty."})
	} else {
		src.Read = append(src.Read, "CLI log ("+plural(len(src.Calls), "call", "calls")+")")
	}

	since := LifeStart(src)
	ref, refName := GitRef(root, issue.Slug, src.Transitions)
	if ref == "" {
		src.Missing = append(src.Missing, Missing{"Work branch", "work/" + issue.Slug + ": not found, and no branch head or merge sha was recorded for this issue. Commits and CI after merge are not shown."})
	} else if commits, err := LoadCommits(root, ref, since); err != nil {
		src.Missing = append(src.Missing, Missing{"Work branch", refName + ": git log failed (" + err.Error() + "). Commits are not shown."})
	} else {
		src.Commits = commits
		src.Read = append(src.Read, "git ("+plural(len(commits), "commit", "commits")+" on "+refName+")")
	}
	if src.MergeSHA == "" && issue.Status == "done" {
		// A fast-forward merge makes main's commit the branch head itself.
		src.MergeSHA = lastBranchHead(src.Transitions)
		if src.MergeSHA == "" && ref != "" && strings.HasPrefix(refName, "work/") {
			src.MergeSHA = ref
		}
	}

	src.Runs = LoadPlaytestRuns(cache, issue.Slug, root, worktree)
	if len(src.Runs) == 0 {
		src.Read = append(src.Read, "playtest runs (none for this issue)")
	} else {
		src.Read = append(src.Read, "playtest runs ("+strconv.Itoa(len(src.Runs))+")")
	}
	return src
}

func countApprovals(rows []tracker.TransitionStat) int {
	n := 0
	for _, r := range rows {
		if r.ApprovedAt != nil {
			n++
		}
	}
	return n
}

func lastBranchHead(rows []tracker.TransitionStat) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].BranchHead != "" {
			return rows[i].BranchHead
		}
	}
	return ""
}

// LogPaths lists every clilog that may hold the issue's calls. The dispatch
// writes the per-agent log in the worktree under the session name, while
// issue-cli also keeps a copy in the project under the assignee, so both
// places and both names are read. The last two are the per-project log and
// the old $TMPDIR log, which LoadCalls filters down to the issue.
func LogPaths(workDir, worktree string, issue *tracker.Issue) []string {
	names := []string{tracker.AgentSessionName(issue.Slug), "agent-" + filepath.Base(issue.Slug)}
	if issue.Assignee != "" {
		names = append([]string{issue.Assignee}, names...)
	}
	var paths []string
	seen := map[string]bool{}
	for _, dir := range []string{worktree, workDir} {
		for _, n := range names {
			p := clilog.AgentPath(dir, n)
			if dir != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return append(paths, clilog.ProjectPath(workDir), clilog.LegacyPath())
}

// LoadCalls merges the clilogs, keeping per-agent records for this project
// and shared-log records that name this issue, deduped on (ts, args): a call
// can be in both the per-agent and the project log. cache may be nil.
func LoadCalls(cache *Cache, paths []string, projSlug, issueSlug string) []Call {
	var calls []Call
	seen := map[string]bool{}
	for _, p := range paths {
		legacy := p == clilog.LegacyPath()
		shared := legacy || filepath.Base(p) == clilog.FileName
		// The project log holds only this project's calls, many without a
		// --project flag; the legacy $TMPDIR log mixes every project's.
		wantProj := ""
		if legacy {
			wantProj = projSlug
		}
		cache.read(p, func(rec clilog.Record) {
			if shared && !MatchesIssue(rec, wantProj, issueSlug) {
				return
			}
			if p := clilog.FlagValue(rec.Args, "--project"); !shared && p != "" && projSlug != "" && p != projSlug {
				return
			}
			args := clilog.StripGlobal(rec.Args)
			at := rec.Start()
			if len(args) == 0 || at.IsZero() {
				return
			}
			key := rec.TS + "|" + strings.Join(args, "\x00")
			if seen[key] {
				return
			}
			seen[key] = true
			calls = append(calls, Call{Record: rec, At: at, Args: args, Agent: !shared})
		})
	}
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].At.Before(calls[j].At) })
	return calls
}

// MatchesIssue reports whether a shared-log record targets the given project
// (--project matches projSlug, or projSlug is empty) and names issueSlug in
// its positional args. The slug is a positional arg of every issue-targeting
// command (start, show, check, transition, append, comment, retrospective,
// checklist, replace, set-meta).
func MatchesIssue(rec clilog.Record, projSlug, issueSlug string) bool {
	if issueSlug == "" {
		return false
	}
	if projSlug != "" && clilog.FlagValue(rec.Args, "--project") != projSlug {
		return false
	}
	for _, a := range clilog.StripGlobal(rec.Args) {
		if a == issueSlug {
			return true
		}
	}
	return false
}

// GitRef picks where the issue's commits are: the live work branch, else
// the last branch head the stats sidecar recorded, else its merge sha.
func GitRef(root, slug string, rows []tracker.TransitionStat) (ref, name string) {
	branch := "work/" + slug
	if out, err := git(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}"); err == nil && out != "" {
		return strings.TrimSpace(out), branch
	}
	if h := lastBranchHead(rows); h != "" {
		return h, "the recorded branch head " + ShortSHA(h)
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].MergeSHA != "" {
			return rows[i].MergeSHA, "the merge " + ShortSHA(rows[i].MergeSHA)
		}
	}
	return "", ""
}

// LoadCommits walks the first-parent chain from ref back to since, so a
// merge of main into the branch is one commit and other issues' commits that
// came with it never show.
func LoadCommits(root, ref string, since time.Time) ([]Commit, error) {
	args := []string{"log", "--first-parent", "--format=%H%x1f%P%x1f%ct%x1f%s", ref}
	if !since.IsZero() {
		args = append(args, "--since="+strconv.FormatInt(since.Add(-time.Minute).Unix(), 10))
	}
	out, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 4 {
			continue
		}
		sec, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		at := time.Unix(sec, 0)
		if !since.IsZero() && at.Before(since.Add(-time.Minute)) {
			continue
		}
		commits = append(commits, Commit{
			SHA:     parts[0],
			At:      at,
			Subject: parts[3],
			Merge:   len(strings.Fields(parts[1])) > 1,
		})
	}
	// git lists newest first; reverse rather than sort, so commits made in
	// the same second keep their order.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

func git(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	return string(out), err
}

func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

type playtestResultsFile struct {
	Session string         `yaml:"session"`
	Issue   string         `yaml:"issue"`
	Mode    string         `yaml:"mode"`
	Started string         `yaml:"started"`
	Steps   []PlaytestStep `yaml:"steps"`
}

// LoadPlaytestRuns reads the results.yaml of every run for issueSlug, in
// each dir's .playtest/runs: agents run ux-test in their worktree, so most
// runs live there. cache may be nil.
func LoadPlaytestRuns(cache *Cache, issueSlug string, dirs ...string) []PlaytestRun {
	var runs []PlaytestRun
	seen := map[string]bool{}
	for _, d := range dirs {
		for _, r := range cache.results(d) {
			if r.Issue != issueSlug {
				continue
			}
			started, err := time.Parse(time.RFC3339, r.Started)
			if err != nil {
				continue
			}
			key := r.Session + "|" + r.Started + "|" + r.Mode
			if seen[key] {
				continue
			}
			seen[key] = true
			runs = append(runs, PlaytestRun{Session: r.Session, Mode: r.Mode, Started: started, Steps: r.Steps})
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Started.Before(runs[j].Started) })
	return runs
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
