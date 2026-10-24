package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clibug"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// reportBugFixture is a machine with an engine project and a reporter
// project, and a private XDG_DATA_HOME for the queue.
type reportBugFixture struct {
	dir       string
	config    string
	engineDir string
	queueDir  string
	reporter  *tracker.Project
}

// newReportBugFixture writes projects.yaml. engineYAML is the engine
// project's entry (or "" for none); top is prepended (e.g. engine_project:).
func newReportBugFixture(t *testing.T, top, engineYAML string) *reportBugFixture {
	t.Helper()
	dir := t.TempDir()
	withCwd(t, dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "combat/heat-overflow")
	t.Setenv("ISSUE_CLI_LOG", "")
	t.Setenv("ISSUE_VIEWER_LEAD_SCOPE", "")
	t.Setenv("TMUX", "")

	wf := repoWorkflow(t)
	f := &reportBugFixture{dir: dir, engineDir: filepath.Join(dir, "engine-issues"), queueDir: filepath.Join(dir, "xdg", "issue-cli", "bug-reports")}
	reporterIssues := filepath.Join(dir, "roguetech-issues", "Combat")
	if err := os.MkdirAll(reporterIssues, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(reporterIssues, "heat-overflow.md"), []byte("---\ntitle: \"Heat overflow\"\nstatus: \"in progress\"\nsystem: \"Combat\"\n---\nbody\n"), 0644)
	os.MkdirAll(f.engineDir, 0755)

	var b strings.Builder
	b.WriteString(top)
	b.WriteString("projects:\n")
	b.WriteString("  - name: RogueTech\n    slug: roguetech\n    issues: " + filepath.Join(dir, "roguetech-issues") + "\n")
	if engineYAML != "" {
		b.WriteString(strings.NewReplacer("{{issues}}", f.engineDir, "{{workflow}}", wf).Replace(engineYAML))
	}
	f.config = filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(f.config, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	f.reporter = &tracker.Project{Name: "RogueTech", Slug: "roguetech", IssueDir: filepath.Join(dir, "roguetech-issues")}
	return f
}

// repoWorkflowPath is the repo's workflow.yaml (it defines the bugfix
// type), resolved before any test changes directory.
var repoWorkflowPath, _ = filepath.Abs(filepath.Join("..", "..", "workflow.yaml"))

func repoWorkflow(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(repoWorkflowPath); err != nil {
		t.Fatalf("repo workflow.yaml: %v", err)
	}
	return repoWorkflowPath
}

const engineEntry = "  - name: Issue Viewer\n    slug: issue-viewer\n    repo: michal-franc/ai-workflow-engine\n    issues: {{issues}}\n    workflow: {{workflow}}\n"

func (f *reportBugFixture) run(t *testing.T, jsonOut bool, args ...string) string {
	t.Helper()
	ctx, stdout, stderr := newTestContext(f.reporter, jsonOut)
	ctx.ConfigPath = f.config
	if err := runReportBug(ctx, args); err != nil {
		t.Fatalf("report-bug %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func (f *reportBugFixture) cliBugs(t *testing.T) []*tracker.Issue {
	t.Helper()
	issues, err := tracker.LoadIssues(f.engineDir)
	if err != nil {
		t.Fatal(err)
	}
	return issues
}

func TestReportBugCreatesCliBugIssue(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	out := f.run(t, false, "--cmd", "issue-cli check foo D3", "Check ticks the wrong box when ids repeat. Seen twice today.")

	issues := f.cliBugs(t)
	if len(issues) != 1 {
		t.Fatalf("engine issues = %d, want 1", len(issues))
	}
	issue := issues[0]
	if issue.Title != "Check ticks the wrong box when ids repeat" {
		t.Errorf("title = %q", issue.Title)
	}
	if issue.Status != "idea" || issue.Type != "bugfix" || issue.System != "CLI" {
		t.Errorf("status/type/system = %q/%q/%q, want idea/bugfix/CLI", issue.Status, issue.Type, issue.System)
	}
	if !hasLabel(issue, "cli-bug") {
		t.Errorf("labels = %v, want cli-bug", issue.Labels)
	}
	if issue.Created != time.Now().Format("2006-01-02") {
		t.Errorf("created = %q, want today", issue.Created)
	}
	fields := map[string]string{}
	for _, ef := range issue.ExtraFields {
		fields[ef.Key] = ef.Value
	}
	if fields["reported_from"] != "roguetech:combat/heat-overflow" {
		t.Errorf("reported_from = %q", fields["reported_from"])
	}
	if fields["reported_by"] != tracker.AgentSessionName("combat/heat-overflow") {
		t.Errorf("reported_by = %q", fields["reported_by"])
	}
	for _, want := range []string{"## Report", "Seen twice today.", "- reported from: roguetech:combat/heat-overflow", "- command: `issue-cli check foo D3`", "- issue-cli: dev"} {
		if !strings.Contains(issue.BodyRaw, want) {
			t.Errorf("body missing %q:\n%s", want, issue.BodyRaw)
		}
	}
	if !strings.Contains(out, "✓ Reported as new cli-bug issue "+issue.Slug+" on the issue-viewer board") {
		t.Errorf("output does not name the issue:\n%s", out)
	}
}

func TestReportBugRepeatCommentsOnOpenIssue(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	f.run(t, false, "Check ticks the wrong box when ids repeat")
	out := f.run(t, false, "check: ticks the WRONG box, when ids repeat! other words")

	issues := f.cliBugs(t)
	if len(issues) != 1 {
		t.Fatalf("engine issues = %d, want 1 (deduped)", len(issues))
	}
	comments, _ := tracker.LoadComments(issues[0].FilePath)
	if len(comments) != 1 || !strings.HasPrefix(comments[0].Text, "report: check: ticks the WRONG box") || comments[0].Source != "issue-cli" {
		t.Fatalf("comments = %+v", comments)
	}
	if !strings.Contains(comments[0].Text, "roguetech:combat/heat-overflow") {
		t.Errorf("report comment has no reporter: %q", comments[0].Text)
	}
	if !strings.Contains(out, "Already reported") || !strings.Contains(out, issues[0].Slug) || !strings.Contains(out, "(2 reports)") {
		t.Errorf("output:\n%s", out)
	}
}

func TestReportBugClosedSameTitleFilesAgain(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	f.run(t, false, "Retrospective lands on the wrong issue")
	old := f.cliBugs(t)[0]
	status := "done"
	if err := tracker.UpdateIssueFrontmatter(old.FilePath, tracker.IssueUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}

	f.run(t, false, "Retrospective lands on the wrong issue")
	var again *tracker.Issue
	for _, issue := range f.cliBugs(t) {
		if issue.Slug != old.Slug {
			again = issue
		}
	}
	if again == nil || again.Title != "Retrospective lands on the wrong issue (again)" {
		t.Fatalf("regression issue = %+v", again)
	}
	if !strings.Contains(again.BodyRaw, "Reported again after `"+old.Slug+"` was closed.") {
		t.Errorf("body does not link the closed issue:\n%s", again.BodyRaw)
	}

	// A third report goes onto the open (again) issue.
	f.run(t, false, "Retrospective lands on the wrong issue")
	if n := len(f.cliBugs(t)); n != 2 {
		t.Fatalf("engine issues = %d, want 2", n)
	}
	if comments, _ := tracker.LoadComments(again.FilePath); len(comments) != 1 {
		t.Fatalf("again issue comments = %d, want 1", len(comments))
	}
}

func TestReportBugQueuesWithoutEngineThenDrains(t *testing.T) {
	f := newReportBugFixture(t, "", "")
	out := f.run(t, false, "Transition hangs on a locked file")
	if !strings.Contains(out, "saved locally, not sent: "+f.queueDir) || !strings.Contains(out, "no engine project in "+f.config) || !strings.Contains(out, "issue-cli report-bug --send") {
		t.Fatalf("queued output:\n%s", out)
	}
	if n := clibug.PendingCount(); n != 1 {
		t.Fatalf("queue = %d, want 1", n)
	}

	// --send still has nowhere to go.
	out = f.run(t, false, "--send")
	if !strings.Contains(out, "1 report(s) still queued") {
		t.Fatalf("--send without engine:\n%s", out)
	}

	// The engine appears; the next report sends itself and the queue.
	cfg, _ := os.ReadFile(f.config)
	os.WriteFile(f.config, append(cfg, []byte(strings.NewReplacer("{{issues}}", f.engineDir, "{{workflow}}", repoWorkflow(t)).Replace(engineEntry))...), 0644)
	out = f.run(t, false, "Another bug entirely")
	if !strings.Contains(out, "✓ Sent 1 queued report(s): cli/transition-hangs-on-a-locked-file") {
		t.Fatalf("drain output:\n%s", out)
	}
	if n := len(f.cliBugs(t)); n != 2 {
		t.Fatalf("engine issues = %d, want 2", n)
	}
	if n := clibug.PendingCount(); n != 0 {
		t.Fatalf("queue = %d after drain, want 0", n)
	}
}

func TestReportBugSendDrainsQueue(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	clibug.Enqueue(clibug.Report{At: time.Now(), Text: "Queued on a laptop", Project: "roguetech"})
	out := f.run(t, false, "--send")
	if !strings.Contains(out, "✓ Sent 1 queued report(s): cli/queued-on-a-laptop") {
		t.Fatalf("--send output:\n%s", out)
	}
	out = f.run(t, false, "--send")
	if !strings.Contains(out, "No queued reports in "+f.queueDir) {
		t.Fatalf("empty --send output:\n%s", out)
	}
}

func TestReportBugUnreachableBoardQueues(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	os.RemoveAll(f.engineDir)
	out := f.run(t, false, "Board is gone")
	if !strings.Contains(out, "saved locally, not sent") || !strings.Contains(out, "has no issues directory") {
		t.Fatalf("output:\n%s", out)
	}
	if n := clibug.PendingCount(); n != 1 {
		t.Fatalf("queue = %d, want 1", n)
	}
}

func TestReportBugEngineProjectKeyWins(t *testing.T) {
	// roguetech is named engine even though issue-viewer has the engine repo.
	f := newReportBugFixture(t, "engine_project: roguetech\n", engineEntry)
	f.run(t, false, "Goes to roguetech")
	if n := len(f.cliBugs(t)); n != 0 {
		t.Fatalf("engine-repo project got %d issues, want 0", n)
	}
	issues, _ := tracker.LoadIssues(f.reporter.IssueDir)
	found := false
	for _, issue := range issues {
		found = found || hasLabel(issue, clibug.Label)
	}
	if !found {
		t.Fatal("no cli-bug issue on the engine_project board")
	}
}

func TestReportBugImport(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	bugs := filepath.Join(f.dir, "bugs")
	os.MkdirAll(bugs, 0755)
	old := filepath.Join(bugs, "20260417-101010-issue-cli-bug.json")
	os.WriteFile(old, []byte(`{"ts":"2026-04-17T10:10:10Z","description":"Approve drops the queued approval","tool":"issue-cli","issue_slug":"combat/heat-overflow"}`+"\n"), 0644)
	broken := filepath.Join(bugs, "broken.json")
	os.WriteFile(broken, []byte("not json"), 0644)

	out := f.run(t, false, "--import", old, broken)
	issues := f.cliBugs(t)
	if len(issues) != 1 {
		t.Fatalf("engine issues = %d, want 1\n%s", len(issues), out)
	}
	fields := map[string]string{}
	for _, ef := range issues[0].ExtraFields {
		fields[ef.Key] = ef.Value
	}
	if fields["reported_from"] != "roguetech:combat/heat-overflow" {
		t.Errorf("reported_from = %q (project looked up from the slug)", fields["reported_from"])
	}
	if _, err := os.Stat(filepath.Join(bugs, "imported", filepath.Base(old))); err != nil {
		t.Errorf("imported file not moved: %v", err)
	}
	if _, err := os.Stat(broken); err != nil {
		t.Errorf("unparseable file should stay put: %v", err)
	}
	if !strings.Contains(out, "skipped:") {
		t.Errorf("output does not report the skipped file:\n%s", out)
	}
}

func TestReportBugJSON(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	out := f.run(t, true, "Json shaped report")
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got["result"] != "created" || got["project"] != "issue-viewer" || got["slug"] != "cli/json-shaped-report" || got["still_queued"] != float64(0) {
		t.Fatalf("json = %v", got)
	}
}

func TestReportBugLastCommandFromClilog(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	logPath := filepath.Join(f.dir, "agent.clilog")
	now := time.Now().UTC()
	lines := []string{
		`{"ts":"` + now.Add(-2*time.Minute).Format(time.RFC3339) + `","args":["--project","roguetech","transition","combat/heat-overflow","--to","in review"]}`,
		`{"ts":"` + now.Format(time.RFC3339) + `","args":["--project","roguetech","report-bug","x"]}`,
	}
	os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	t.Setenv("ISSUE_CLI_LOG", logPath)

	f.run(t, false, "Transition rejects in review")
	body := f.cliBugs(t)[0].BodyRaw
	if !strings.Contains(body, "- command: `issue-cli --project roguetech transition combat/heat-overflow --to \"in review\"`") {
		t.Fatalf("body has no last command:\n%s", body)
	}
}

func TestReportBugRequiresDescription(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	ctx, _, _ := newTestContext(f.reporter, false)
	ctx.ConfigPath = f.config
	for args, want := range map[string]string{
		"":                "requires a description",
		"--import":        "--import needs the report files",
		"--send|extra":    "--send takes no description",
		"--send|--import": "--send takes no description",
	} {
		var a []string
		if args != "" {
			a = strings.Split(args, "|")
		}
		if err := runReportBug(ctx, a); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("report-bug %v: err = %v, want %q", a, err, want)
		}
	}
}

// An agent in a multi-project config runs report-bug without --project.
func TestReportBugRunsWithoutProject(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	var out, errw bytes.Buffer
	if err := run([]string{"--config", f.config, "report-bug", "No project given"}, strings.NewReader(""), &out, &errw); err != nil {
		t.Fatalf("run: %v\n%s", err, errw.String())
	}
	if !strings.Contains(out.String(), "cli/no-project-given") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// withVersion builds the binary as v for one test.
func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := Version
	Version = v
	t.Cleanup(func() { Version = orig })
}

func TestReportBugFromReleaseCandidateJoinsTheMilestone(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	// The engine board already labels the milestone "0.54".
	os.WriteFile(filepath.Join(f.engineDir, "planned.md"), []byte("---\ntitle: \"Planned\"\nstatus: \"backlog\"\nversion: \"0.54\"\n---\nbody\n"), 0644)
	os.WriteFile(filepath.Join(f.engineDir, "later.md"), []byte("---\ntitle: \"Later bug\"\nstatus: \"idea\"\nversion: \"0.55\"\nlabels:\n  - cli-bug\n---\nbody\n"), 0644)
	withVersion(t, "v0.54.0-rc.2")

	out := f.run(t, false, "Status card loses the candidate row after a restart")
	issue := tracker.FindIssue(f.cliBugs(t), "cli/status-card-loses-the-candidate-row-after-a-restart")
	if issue == nil {
		t.Fatalf("no new cli-bug issue:\n%s", out)
	}
	if issue.Version != "0.54" {
		t.Errorf("version = %q, want 0.54 (the label the board uses for v0.54.0)", issue.Version)
	}
	if !strings.Contains(issue.BodyRaw, "- issue-cli: v0.54.0-rc.2 ") {
		t.Errorf("body does not record the candidate:\n%s", issue.BodyRaw)
	}
	if !strings.Contains(out, "Added to milestone 0.54 (found in release candidate v0.54.0-rc.2): the release waits for its fix") {
		t.Errorf("output does not say where it went:\n%s", out)
	}

	// A report on an open bug that already has a version leaves it there.
	out = f.run(t, false, "Later bug")
	if got := tracker.FindIssue(f.cliBugs(t), "later-bug"); got == nil || got.Version != "0.55" {
		t.Fatalf("later bug moved: %+v", got)
	}
	if !strings.Contains(out, "Filed against release candidate v0.54.0-rc.2; the issue already has a version, so it stays there") {
		t.Errorf("output:\n%s", out)
	}
}

func TestReportBugCommentFromCandidateSetsAMissingVersion(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	f.run(t, false, "Check ticks the wrong box when ids repeat")
	withVersion(t, "v0.54.0-rc.1-3-gabc1234")
	out := f.run(t, true, "Check ticks the wrong box when ids repeat")
	issues := f.cliBugs(t)
	if len(issues) != 1 || issues[0].Version != "0.54" {
		t.Fatalf("issues = %+v, want one with version 0.54", issues)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["result"] != "commented" || got["milestone"] != "0.54" || got["found_in"] != "v0.54.0-rc.1-3-gabc1234" {
		t.Fatalf("json = %s", out)
	}
}

func TestReportBugFromReleaseLeavesVersionAlone(t *testing.T) {
	f := newReportBugFixture(t, "", engineEntry)
	withVersion(t, "v0.54.0")
	out := f.run(t, false, "Something broke")
	issues := f.cliBugs(t)
	if len(issues) != 1 || issues[0].Version != "" || strings.Contains(out, "milestone") || strings.Contains(out, "candidate") {
		t.Fatalf("a release build's report should not pick a milestone: %+v\n%s", issues, out)
	}
}

func TestVersionNamesReleaseCandidate(t *testing.T) {
	for v, want := range map[string]string{
		"dev":                     "issue-cli dev\n",
		"v0.54.0":                 "issue-cli v0.54.0\n",
		"v0.54.0-rc.1":            "issue-cli v0.54.0-rc.1 (release candidate for v0.54.0: report problems with issue-cli report-bug \"…\")\n",
		"v0.54.0-rc.1-2-gabc1234": "issue-cli v0.54.0-rc.1-2-gabc1234 (a build past release candidate v0.54.0-rc.1: report problems with issue-cli report-bug \"…\")\n",
	} {
		withVersion(t, v)
		ctx, stdout, _ := newTestContext(nil, false)
		if err := runVersion(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != want {
			t.Errorf("version %s: got %q, want %q", v, stdout.String(), want)
		}
	}
}
