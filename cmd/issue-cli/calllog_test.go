package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
	"github.com/michal-franc/issue-viewer/internal/telemetry"
)

// callLogFixture is a project on disk (config, workflow, one issue) that
// issue-cli finds through ISSUE_VIEWER_CONFIG, as an agent's shell would.
type callLogFixture struct {
	root, issuePath string
}

func newCallLogFixture(t *testing.T, status, assignee, approval string) callLogFixture {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "workflow.yaml"), strings.TrimSpace(`
statuses:
  - name: "in design"
  - name: "backlog"
  - name: "in progress"
transitions:
  - from: "in design"
    to: "backlog"
    actions:
      - type: require_human_approval
        status: "backlog"
      - type: set_fields
        field: "assignee"
        value: ""
  - from: "backlog"
    to: "in progress"
    actions:
      - type: require_human_approval
        status: "in progress"
`)+"\n")
	issuePath := filepath.Join(root, "issues", "CLI", "sample.md")
	if err := os.MkdirAll(filepath.Dir(issuePath), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, issuePath, fmt.Sprintf("---\ntitle: \"sample\"\nstatus: %q\nsystem: \"CLI\"\nassignee: %q\nhuman_approval: %q\n---\n\nbody\n", status, assignee, approval))
	cfg := filepath.Join(root, "projects.yaml")
	mustWrite(t, cfg, fmt.Sprintf("projects:\n  - name: Demo\n    slug: demo\n    workdir: %q\n    issues: %q\n    workflow: %q\n",
		root, filepath.Join(root, "issues"), filepath.Join(root, "workflow.yaml")))
	t.Setenv("ISSUE_VIEWER_CONFIG", cfg)
	t.Setenv("ISSUE_CLI_LOG", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv(telemetry.DisableEnv, "off")
	t.Setenv("TMPDIR", filepath.Join(root, "tmp"))
	return callLogFixture{root: root, issuePath: issuePath}
}

// runLogged runs args the way main does: the call, then its clilog record.
func runLogged(t *testing.T, args ...string) (*callLog, error) {
	t.Helper()
	l := newCallLog(args, time.Now())
	inv := &invocation{}
	var out, errw bytes.Buffer
	err := runTraced(args, strings.NewReader(""), &out, &errw, inv)
	l.write(inv, err, false)
	return l, err
}

func readRecords(t *testing.T, path string) []clilog.Record {
	t.Helper()
	var recs []clilog.Record
	clilog.Read(path, func(r clilog.Record) { recs = append(recs, r) })
	return recs
}

func TestCallLogWritesOneRedactedRecordAfterTheCall(t *testing.T) {
	f := newCallLogFixture(t, "in design", "agent-sample", "")
	agentLog := clilog.AgentPath(f.root, "agent-sample")

	l, err := runLogged(t, "--project", "demo", "comment", "cli/sample", "--text", "progress: secret words")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if want := clilog.ProjectPath(f.root); l.shared != want {
		t.Fatalf("shared log %q, want %q", l.shared, want)
	}
	for _, p := range []string{clilog.ProjectPath(f.root), agentLog} {
		recs := readRecords(t, p)
		if len(recs) != 1 {
			t.Fatalf("%s: %d records, want 1", p, len(recs))
		}
		r := recs[0]
		if r.Exit == nil || *r.Exit != 0 || r.DurMS == nil || r.ErrClass != "" {
			t.Fatalf("%s: record %+v", p, r)
		}
		joined := strings.Join(r.Args, " ")
		if strings.Contains(joined, "secret") || !strings.Contains(joined, "comment cli/sample --text ‹text n=22 h=") {
			t.Fatalf("%s: args not redacted as expected: %q", p, r.Args)
		}
	}
	if _, err := os.Stat(clilog.LegacyPath()); err == nil {
		t.Fatal("record written to the legacy $TMPDIR log")
	}
}

func TestCallLogRecordsRefusedGateWithExitAndClass(t *testing.T) {
	f := newCallLogFixture(t, "backlog", "agent-sample", "")
	_, err := runLogged(t, "--project", "demo", "transition", "cli/sample", "--to", "in progress")
	if err == nil {
		t.Fatal("transition without approval succeeded")
	}
	recs := readRecords(t, clilog.ProjectPath(f.root))
	if len(recs) != 1 || *recs[0].Exit != 1 || recs[0].ErrClass != telemetry.ErrApprovalMissing {
		t.Fatalf("records %+v", recs)
	}
	if got := clilog.FlagValue(recs[0].Args, "--to"); got != "in progress" {
		t.Fatalf("--to = %q, want it kept", got)
	}
}

// The transition to backlog clears the assignee; its record still lands in
// the agent's clilog because the path is resolved before the call.
func TestCallLogKeepsAgentPathWhenTransitionClearsAssignee(t *testing.T) {
	f := newCallLogFixture(t, "in design", "agent-sample", "backlog")
	if _, err := runLogged(t, "--project", "demo", "transition", "cli/sample", "--to", "backlog"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if strings.Contains(readFile(t, f.issuePath), "agent-sample") {
		t.Fatalf("assignee not cleared:\n%s", readFile(t, f.issuePath))
	}
	if recs := readRecords(t, clilog.AgentPath(f.root, "agent-sample")); len(recs) != 1 {
		t.Fatalf("agent clilog has %d records, want 1", len(recs))
	}
}

// start claims the issue during the call; the record follows the new
// assignee into its clilog.
func TestCallLogFollowsAssigneeSetByStart(t *testing.T) {
	f := newCallLogFixture(t, "backlog", "", "in progress")
	t.Setenv("AGENT_NAME", "agent-sample")
	if _, err := runLogged(t, "--project", "demo", "start", "cli/sample"); err != nil {
		t.Fatalf("start: %v", err)
	}
	assignee := ""
	for _, line := range strings.Split(readFile(t, f.issuePath), "\n") {
		if v, ok := strings.CutPrefix(line, "assignee: "); ok {
			assignee = strings.Trim(v, `"`)
		}
	}
	if assignee == "" {
		t.Fatal("start set no assignee")
	}
	if recs := readRecords(t, clilog.AgentPath(f.root, assignee)); len(recs) != 1 {
		t.Fatalf("agent clilog of %s has %d records, want 1", assignee, len(recs))
	}
}

func TestCallLogWritesEnvLogOnceWhenItIsTheAgentLog(t *testing.T) {
	f := newCallLogFixture(t, "in design", "agent-sample", "")
	agentLog := clilog.AgentPath(f.root, "agent-sample")
	t.Setenv("ISSUE_CLI_LOG", agentLog)
	if _, err := runLogged(t, "--project", "demo", "show", "cli/sample"); err != nil {
		t.Fatalf("show: %v", err)
	}
	if recs := readRecords(t, agentLog); len(recs) != 1 {
		t.Fatalf("agent clilog has %d records, want 1", len(recs))
	}
}

func TestCallLogWithoutProjectUsesGlobalLog(t *testing.T) {
	newCallLogFixture(t, "in design", "", "")
	t.Setenv("ISSUE_VIEWER_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	l, _ := runLogged(t, "--project", "nope", "next")
	if l.shared != clilog.GlobalPath() || len(readRecords(t, l.shared)) != 1 {
		t.Fatalf("shared %q, want global %q with one record", l.shared, clilog.GlobalPath())
	}
}

func TestCountRecentRetriesCountsFailedRepeatsFromThisSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clilog.jsonl")
	add := func(ppid, exit int, args ...string) {
		dur := int64(1)
		if err := clilog.Append(path, clilog.Record{TS: "2026-10-23T10:00:00Z", PPID: ppid, Args: args, Exit: &exit, DurMS: &dur}); err != nil {
			t.Fatal(err)
		}
	}
	add(7, 0, "transition", "x", "--to", "done") // succeeded: not a retry
	add(7, 1, "transition", "x", "--to", "done")
	add(9, 1, "show", "y") // another session, skipped
	add(7, 1, "transition", "x", "--to", "done")
	add(7, 1, "transition", "x", "--to", "done") // the current call
	if got := countRecentRetries(path); got != 2 {
		t.Fatalf("countRecentRetries = %d, want 2", got)
	}
	if got := countRecentRetries(filepath.Join(t.TempDir(), "none")); got != 0 {
		t.Fatalf("missing log: %d", got)
	}
}
