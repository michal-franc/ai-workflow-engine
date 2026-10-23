package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// timelineGitTimeout bounds the one git call the Timeline tab makes, so a
// slow or locked repo never holds up the detail page.
const timelineGitTimeout = 2 * time.Second

// timelineSources is everything the Timeline tab reads for one issue,
// gathered by loadTimelineSources and turned into a page by buildTimeline.
// Missing lists the sources that could not be read; Read describes the ones
// that were.
type timelineSources struct {
	Transitions []statRow
	Dispatches  []time.Time
	MergeSHA    string
	Calls       []cliCall
	Commits     []gitCommit
	Comments    []tracker.Comment
	Decisions   []tracker.Decision
	Runs        []playtestRun
	CI          []ciRun
	Boxes       []tracker.CheckboxItem
	Prompt      string
	Missing     []missingSource
	Read        []string
}

type missingSource struct {
	Name   string // "CLI log", "Work branch", "Stats sidecar"
	Detail string // the paths tried and what that hides
}

// statRow is one stats-sidecar transition. BranchHead and MergeSHA come from
// cli/clilog-and-stats-record-what-the-timeline-needs and are empty in
// sidecars written before it.
type statRow struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	TS         time.Time  `json:"ts"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	BranchHead string     `json:"branch_head,omitempty"`
	MergeSHA   string     `json:"merge_sha,omitempty"`
}

// statFile reads the stats sidecar with the fields the timeline needs,
// including ones newer issue-cli versions add (dispatches).
type statFile struct {
	Transitions []statRow `json:"transitions"`
	Dispatches  []struct {
		At time.Time `json:"at"`
	} `json:"dispatches,omitempty"`
}

// cliCall is one clilog record. Exit and DurMS are nil/0 in records written
// before the clilog recorded results.
type cliCall struct {
	At    time.Time
	Args  []string // global flags stripped
	Raw   []string
	Exit  *int
	DurMS int64
	// Agent: read from a per-agent clilog, so the issue's agent made it.
	// Shared logs also hold the lead's and the owner's calls on the issue.
	Agent bool
}

func (c cliCall) cmd() string {
	if len(c.Args) == 0 {
		return ""
	}
	return c.Args[0]
}

type cliRecordFull struct {
	TS    string   `json:"ts"`
	Args  []string `json:"args"`
	Exit  *int     `json:"exit,omitempty"`
	DurMS int64    `json:"dur_ms,omitempty"`
}

type gitCommit struct {
	SHA     string
	At      time.Time
	Subject string
	Merge   bool
}

type playtestRun struct {
	Session string
	Mode    string // auto (ux-test) or judge (your playtest)
	Started time.Time
	Steps   []playtestStep
}

type playtestStep struct {
	ID     string `yaml:"id"`
	Status string `yaml:"status"`
}

func (r playtestRun) failed() []string {
	var ids []string
	for _, s := range r.Steps {
		if s.Status == "fail" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

func (r playtestRun) passed(id string) bool {
	for _, s := range r.Steps {
		if s.ID == id {
			return s.Status == "pass"
		}
	}
	return false
}

type ciRun struct {
	SHA      string
	Started  time.Time
	State    string
	Commands []string
	Duration time.Duration
}

// loadTimelineSources reads the issue's stats sidecar, clilogs, work branch,
// comments, decisions, playtest runs and local CI runs. A source that can't
// be read is listed in Missing; it never fails the page.
func loadTimelineSources(proj *tracker.Project, issue *tracker.Issue, wf *tracker.WorkflowConfig) timelineSources {
	root := projectRoot(proj)
	workDir := resolveProjectWorkDir(proj)
	worktree := filepath.Join(root, ".worktrees", issue.Slug)
	var src timelineSources

	stats, statsErr := readStatFile(tracker.StatsSidecarPath(issue.FilePath))
	switch {
	case statsErr != nil:
		src.Missing = append(src.Missing, missingSource{"Stats sidecar", statsErr.Error() + ". Status times and approvals are not shown."})
	case len(stats.Transitions) == 0:
		src.Missing = append(src.Missing, missingSource{"Stats sidecar", "no transitions recorded in " + tracker.StatsSidecarPath(issue.FilePath) + ". Status times and approvals are not shown."})
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

	logPaths := timelineLogPaths(workDir, worktree, issue)
	src.Calls = loadCliCalls(logPaths, proj.Slug, issue.Slug)
	if len(src.Calls) == 0 {
		src.Missing = append(src.Missing, missingSource{"CLI log", "no calls for this issue in .agent-logs/<agent>/<agent>.clilog (project and worktree), .agent-logs/clilog.jsonl or $TMPDIR/issue-cli-logs/actions.jsonl. Agent, waiting and idle time can't be split; the CLI calls filter is empty."})
	} else {
		src.Read = append(src.Read, "CLI log ("+plural(len(src.Calls), "call", "calls")+")")
	}
	for _, d := range []string{worktree, workDir} {
		if p := LoadDispatchPrompt(d, tracker.AgentSessionName(issue.Slug)); p != "" {
			src.Prompt = p
			break
		}
		if p := LoadDispatchPrompt(d, issue.Assignee); p != "" {
			src.Prompt = p
			break
		}
	}

	src.Comments, _ = tracker.LoadComments(issue.FilePath)
	src.Read = append(src.Read, "comments ("+strconv.Itoa(len(src.Comments))+")")
	src.Decisions = tracker.ParseDecisions(issue.Slug, issue.BodyRaw)
	src.Boxes = tracker.ListCheckboxes(issue.BodyRaw)

	since := lifeStart(src)
	ref, refName := timelineGitRef(root, issue.Slug, src.Transitions)
	if ref == "" {
		src.Missing = append(src.Missing, missingSource{"Work branch", "work/" + issue.Slug + ": not found, and no branch head or merge sha was recorded for this issue. Commits and CI after merge are not shown."})
	} else if commits, err := loadCommits(root, ref, since); err != nil {
		src.Missing = append(src.Missing, missingSource{"Work branch", refName + ": git log failed (" + err.Error() + "). Commits are not shown."})
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

	src.Runs = loadPlaytestRuns(issue.Slug, root, worktree)
	if len(src.Runs) == 0 {
		src.Read = append(src.Read, "playtest runs (none for this issue)")
	} else {
		src.Read = append(src.Read, "playtest runs ("+strconv.Itoa(len(src.Runs))+")")
	}
	if src.MergeSHA != "" {
		src.CI = loadCIRunsFrom(root, src.MergeSHA)
	}
	return src
}

func readStatFile(issuePath string) (statFile, error) {
	var f statFile
	data, err := os.ReadFile(issuePath)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return statFile{}, err
	}
	return f, nil
}

func countApprovals(rows []statRow) int {
	n := 0
	for _, r := range rows {
		if r.ApprovedAt != nil {
			n++
		}
	}
	return n
}

func lastBranchHead(rows []statRow) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].BranchHead != "" {
			return rows[i].BranchHead
		}
	}
	return ""
}

// timelineLogPaths lists every clilog that may hold the issue's calls. The
// dispatch writes the per-agent log in the worktree under the session name,
// while issue-cli also keeps a copy in the project under the assignee, so
// both places and both names are read. The per-project log and the old
// $TMPDIR log are filtered down to this issue by loadCliCalls.
func timelineLogPaths(workDir, worktree string, issue *tracker.Issue) []string {
	names := []string{tracker.AgentSessionName(issue.Slug), "agent-" + filepath.Base(issue.Slug)}
	if issue.Assignee != "" {
		names = append([]string{issue.Assignee}, names...)
	}
	var paths []string
	seen := map[string]bool{}
	for _, dir := range []string{worktree, workDir} {
		for _, n := range names {
			p := filepath.Join(dir, ".agent-logs", n, n+".clilog")
			if dir != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return append(paths,
		filepath.Join(workDir, ".agent-logs", "clilog.jsonl"),
		filepath.Join(os.TempDir(), "issue-cli-logs", "actions.jsonl"))
}

// loadCliCalls merges the clilogs, keeping per-agent records for this project
// and shared-log records that name this issue, deduped on (ts, args).
func loadCliCalls(paths []string, projSlug, issueSlug string) []cliCall {
	var calls []cliCall
	seen := map[string]bool{}
	for _, p := range paths {
		shared := filepath.Base(p) == "clilog.jsonl" || filepath.Base(p) == "actions.jsonl"
		readFullCliLog(p, func(rec cliRecordFull) {
			if shared && !cliRecordMatchesIssue(cliLogRecord{Args: rec.Args, TS: rec.TS}, projSlug, issueSlug) {
				return
			}
			if p := flagValue(rec.Args, "--project"); !shared && p != "" && projSlug != "" && p != projSlug {
				return
			}
			args := stripGlobalFlags(rec.Args)
			at, err := time.Parse(time.RFC3339, rec.TS)
			if len(args) == 0 || err != nil {
				return
			}
			key := rec.TS + "|" + strings.Join(args, "\x00")
			if seen[key] {
				return
			}
			seen[key] = true
			calls = append(calls, cliCall{At: at, Args: args, Raw: rec.Args, Exit: rec.Exit, DurMS: rec.DurMS, Agent: !shared})
		})
	}
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].At.Before(calls[j].At) })
	return calls
}

func readFullCliLog(path string, visit func(cliRecordFull)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var rec cliRecordFull
		if json.Unmarshal(scanner.Bytes(), &rec) == nil {
			visit(rec)
		}
	}
}

// lifeStart is when the issue's agent life began: the first dispatch, else
// the agent's first call, else the first call, else the first transition.
// Calls on the issue before it (the lead writing it up) are not the agent's.
func lifeStart(src timelineSources) time.Time {
	if len(src.Dispatches) > 0 {
		return src.Dispatches[0]
	}
	for _, c := range src.Calls {
		if c.Agent {
			return c.At
		}
	}
	if len(src.Calls) > 0 {
		return src.Calls[0].At
	}
	if len(src.Transitions) > 0 {
		return src.Transitions[0].TS
	}
	return time.Time{}
}

// timelineGitRef picks where the issue's commits are: the live work branch,
// else the last branch head the stats sidecar recorded, else its merge sha.
func timelineGitRef(root, slug string, rows []statRow) (ref, name string) {
	branch := "work/" + slug
	if out, err := timelineGit(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}"); err == nil && out != "" {
		return strings.TrimSpace(out), branch
	}
	if h := lastBranchHead(rows); h != "" {
		return h, "the recorded branch head " + shortSHA(h)
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].MergeSHA != "" {
			return rows[i].MergeSHA, "the merge " + shortSHA(rows[i].MergeSHA)
		}
	}
	return "", ""
}

// loadCommits walks the first-parent chain from ref back to since, so a
// merge of main into the branch is one commit and other issues' commits that
// came with it never show.
func loadCommits(root, ref string, since time.Time) ([]gitCommit, error) {
	args := []string{"log", "--first-parent", "--format=%H%x1f%P%x1f%ct%x1f%s", ref}
	if !since.IsZero() {
		args = append(args, "--since="+strconv.FormatInt(since.Add(-time.Minute).Unix(), 10))
	}
	out, err := timelineGit(root, args...)
	if err != nil {
		return nil, err
	}
	var commits []gitCommit
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
		commits = append(commits, gitCommit{
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

var timelineGit = func(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timelineGitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	return string(out), err
}

type playtestResultsFile struct {
	Session string         `yaml:"session"`
	Issue   string         `yaml:"issue"`
	Mode    string         `yaml:"mode"`
	Started string         `yaml:"started"`
	Steps   []playtestStep `yaml:"steps"`
}

// loadPlaytestRuns reads the results.yaml of every run for issueSlug, in the
// main checkout's and the issue worktree's .playtest/runs: agents run ux-test
// in their worktree, so most runs live there.
func loadPlaytestRuns(issueSlug string, dirs ...string) []playtestRun {
	var runs []playtestRun
	seen := map[string]bool{}
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, ".playtest", "runs", "*", "*", "results.yaml"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var r playtestResultsFile
			if yaml.Unmarshal(data, &r) != nil || r.Issue != issueSlug {
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
			runs = append(runs, playtestRun{Session: r.Session, Mode: r.Mode, Started: started, Steps: r.Steps})
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Started.Before(runs[j].Started) })
	return runs
}

type ciRunFile struct {
	SHA      string    `json:"sha"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	State    string    `json:"state"`
	Commands []struct {
		Cmd string `json:"cmd"`
	} `json:"commands"`
}

// loadCIRunsFrom returns the local CI run on sha and every run after it.
// Nil when sha was never run.
func loadCIRunsFrom(root, sha string) []ciRun {
	files, _ := filepath.Glob(filepath.Join(root, ".ci", "runs", "*.json"))
	var all []ciRun
	var first time.Time
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r ciRunFile
		if json.Unmarshal(data, &r) != nil || r.Started.IsZero() {
			continue
		}
		run := ciRun{SHA: r.SHA, Started: r.Started, State: r.State, Duration: r.Finished.Sub(r.Started)}
		for _, c := range r.Commands {
			run.Commands = append(run.Commands, c.Cmd)
		}
		if strings.HasPrefix(r.SHA, sha) || strings.HasPrefix(sha, r.SHA) {
			first = r.Started
		}
		all = append(all, run)
	}
	if first.IsZero() {
		return nil
	}
	var out []ciRun
	for _, r := range all {
		if !r.Started.Before(first) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// timelineTotalLabel is the Timeline tab's label on every tab: the life from
// dispatch (or the agent's first call) to done, or "so far". It reads the
// stats sidecar and the first line of each per-agent clilog only.
func timelineTotalLabel(proj *tracker.Project, issue *tracker.Issue) string {
	stats, err := readStatFile(tracker.StatsSidecarPath(issue.FilePath))
	if err != nil {
		return ""
	}
	src := timelineSources{Transitions: stats.Transitions}
	for _, d := range stats.Dispatches {
		src.Dispatches = append(src.Dispatches, d.At)
	}
	if len(src.Dispatches) == 0 {
		worktree := filepath.Join(projectRoot(proj), ".worktrees", issue.Slug)
		paths := timelineLogPaths(resolveProjectWorkDir(proj), worktree, issue)
		for _, p := range paths[:len(paths)-2] { // per-agent logs only
			if c, ok := firstCliCall(p, proj.Slug); ok && (len(src.Calls) == 0 || c.At.Before(src.Calls[0].At)) {
				src.Calls = []cliCall{c}
			}
		}
	}
	start := lifeStart(src)
	end, inFlight := lifeEnd(src, timelineInput{Status: issue.Status, Now: time.Now()})
	if start.IsZero() || !end.After(start) {
		return ""
	}
	label := fmtSpan(end.Sub(start))
	if inFlight {
		label += " so far"
	}
	return label
}

// firstCliCall is the first record in a per-agent clilog for projSlug.
func firstCliCall(path, projSlug string) (cliCall, bool) {
	f, err := os.Open(path)
	if err != nil {
		return cliCall{}, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var rec cliRecordFull
		if json.Unmarshal(scanner.Bytes(), &rec) != nil {
			continue
		}
		if p := flagValue(rec.Args, "--project"); p != "" && projSlug != "" && p != projSlug {
			continue
		}
		at, err := time.Parse(time.RFC3339, rec.TS)
		if err != nil {
			continue
		}
		return cliCall{At: at, Args: stripGlobalFlags(rec.Args), Agent: true}, true
	}
	return cliCall{}, false
}
