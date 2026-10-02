package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// suggestedWaitTimeout is the --timeout every hint suggests. Agents run
// issue-cli from a Bash tool capped at ~10 minutes in the foreground, so the
// suggested wait must expire just inside that cap and be re-run.
const suggestedWaitTimeout = "9m"

// exitWaitTimeout is the exit code for "--wait timed out, nothing changed,
// re-run to keep waiting". 2 is avoided: flag parsing and shells use it for
// usage errors.
const exitWaitTimeout = 3

// exitCodeError carries a specific process exit code. main prints Msg (when
// non-empty) verbatim — no "Error:" prefix — and skips the repeated-failure
// hint, because these outcomes are expected states, not mistakes to retry.
type exitCodeError struct {
	Code int
	Msg  string
}

func (e *exitCodeError) Error() string { return e.Msg }

// waitOptions are the --wait / --timeout / --interval flags shared by
// transition and start.
type waitOptions struct {
	wait     *bool
	timeout  *time.Duration
	interval *time.Duration
}

func registerWaitFlags(fs *flag.FlagSet) waitOptions {
	return waitOptions{
		wait:     fs.Bool("wait", false, "block until the required human approval exists, then proceed"),
		timeout:  fs.Duration("timeout", 0, "with --wait: give up after this long and exit 3 (0 = wait forever)"),
		interval: fs.Duration("interval", 2*time.Second, "with --wait: how often to re-check the issue"),
	}
}

func (o waitOptions) validate() error {
	if *o.timeout < 0 {
		return fmt.Errorf("--timeout must not be negative")
	}
	if *o.interval <= 0 {
		return fmt.Errorf("--interval must be positive")
	}
	if !*o.wait && *o.timeout != 0 {
		return fmt.Errorf("--timeout only applies with --wait")
	}
	return nil
}

// timeoutLabel renders the timeout for the waiting line and re-run hint.
func (o waitOptions) timeoutLabel() string {
	if *o.timeout == 0 {
		return "none"
	}
	return o.timeout.String()
}

// waitCommand is the blocking command an agent should run to cross the gate
// from → to. Handoff statuses (backlog, human-testing) are left with `start`,
// which claims and advances; everything else uses `transition`.
func waitCommand(slug, from, to string) string {
	if tracker.IsHandoffStatus(from) {
		return fmt.Sprintf("issue-cli start %s --wait --timeout %s", slug, suggestedWaitTimeout)
	}
	return fmt.Sprintf("issue-cli transition %s --to %q --wait --timeout %s", slug, to, suggestedWaitTimeout)
}

// waitNote is the one-liner printed under a suggested --wait command.
func waitNote(required string) string {
	return fmt.Sprintf("(needs human approval for %q — blocks until approved; exit 3 = still waiting, re-run it)", required)
}

// nextStepCommand returns the command for the next step from → next and an
// optional note. When the step is gated on a human approval it is the --wait
// form, so agents discover blocking from the output they already read. `done`
// is excluded: only humans close issues.
func nextStepCommand(wf *tracker.WorkflowConfig, slug, from, next string) (cmd, note string) {
	plain := fmt.Sprintf("issue-cli transition %s --to %q", slug, next)
	if next == "done" {
		return plain, ""
	}
	required := wf.RequiredHumanApproval(from, next)
	if required == "" {
		return plain, ""
	}
	return waitCommand(slug, from, next), waitNote(required)
}

// transitionProblem is one unmet requirement for a transition.
type transitionProblem struct {
	// Kind is "order", "field", "validator", or "approval". Only "approval"
	// can be resolved by a human click; --wait blocks on nothing else.
	Kind        string   `json:"kind"`
	Requirement string   `json:"requirement"`
	Message     string   `json:"message,omitempty"`
	Fix         []string `json:"fix,omitempty"`
}

// collectTransitionProblems evaluates every requirement of from → to against
// issue without mutating anything. It reuses PreviewTransitionAll — the same
// engine as the viewer's transition preview — so the CLI and the viewer agree
// on what is missing.
func collectTransitionProblems(ctx *Context, wf *tracker.WorkflowConfig, issue *tracker.Issue, slug, from, to string, fields map[string]string) []transitionProblem {
	if err := wf.CheckTransitionOrder(from, to); err != nil {
		return []transitionProblem{{Kind: "order", Requirement: fmt.Sprintf("%s → %s is a valid next step", from, to), Message: err.Error()}}
	}

	var problems []transitionProblem
	merged := wf.MergeFieldValuesFromFrontmatter(issue, from, to, fields)
	for _, field := range wf.TransitionFields(from, to) {
		if !field.Required || strings.TrimSpace(merged[field.Name]) != "" {
			continue
		}
		label := field.Prompt
		if label == "" {
			label = field.Name
		}
		problems = append(problems, transitionProblem{
			Kind:        "field",
			Requirement: fmt.Sprintf("Field %q answered: %s", field.Name, label),
			Fix:         []string{fmt.Sprintf("issue-cli transition %s --to %q --field %s=\"...\"", slug, to, field.Name)},
		})
	}

	comments, _ := tracker.LoadComments(issue.FilePath)
	preview := wf.PreviewTransitionAll(issue, from, to, issue.System, comments)
	for _, step := range preview.Steps {
		if step.Outcome != "failed" {
			continue
		}
		p := transitionProblem{Requirement: step.Summary, Message: step.Message}
		switch step.ActionType {
		case "require_human_approval":
			required := wf.RequiredHumanApproval(from, to)
			p.Kind = "approval"
			p.Requirement = tracker.DescribeAction(step.Action, to)
			p.Message = ""
			p.Fix = []string{
				"a human approves at " + approvalURL(ctx.Project, slug, required),
				"meanwhile block on it: " + waitCommand(slug, from, to),
			}
		default:
			p.Kind = "validator"
			p.Fix = checkboxFixes(slug, issue.BodyRaw, step.Action.Rule)
		}
		problems = append(problems, p)
	}
	return problems
}

// checkboxFixes lists one `issue-cli check` per unchecked box a checkbox gate
// is waiting on. Other rules already embed their fix in the message.
func checkboxFixes(slug, body, rule string) []string {
	name, arg := rule, ""
	if idx := strings.Index(rule, ": "); idx != -1 {
		name, arg = rule[:idx], strings.TrimSpace(rule[idx+2:])
	}
	if name != "section_checkboxes_checked" && name != "all_checkboxes_checked" {
		return nil
	}
	var fixes []string
	for _, item := range tracker.ListCheckboxes(body) {
		if item.Checked || (name == "section_checkboxes_checked" && !strings.EqualFold(item.Section, arg)) {
			continue
		}
		fixes = append(fixes, fmt.Sprintf("issue-cli check %s %q", slug, item.Text))
	}
	return fixes
}

func onlyApprovalMissing(problems []transitionProblem) (approval bool, machine []transitionProblem) {
	for _, p := range problems {
		if p.Kind == "approval" {
			approval = true
			continue
		}
		machine = append(machine, p)
	}
	return approval, machine
}

type dryRunOutput struct {
	DryRun      bool                `json:"dry_run"`
	Ready       bool                `json:"ready"`
	From        string              `json:"from"`
	To          string              `json:"to"`
	Slug        string              `json:"slug"`
	Problems    []transitionProblem `json:"problems"`
	SideEffects []string            `json:"side_effects,omitempty"`
}

// printProblemReport renders problems for --dry-run and for --wait's
// fail-fast path. Messages from checkRule are multi-line (they embed the fix),
// so continuation lines are indented under their requirement.
func printProblemReport(w io.Writer, problems []transitionProblem) {
	fmt.Fprintf(w, "✗ %d unmet requirement(s):\n", len(problems))
	for _, p := range problems {
		fmt.Fprintf(w, "  - [%s] %s\n", p.Kind, p.Requirement)
		if msg := strings.TrimSpace(p.Message); msg != "" {
			for _, line := range strings.Split(msg, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				fmt.Fprintf(w, "      %s\n", strings.TrimSpace(line))
			}
		}
		for _, fix := range p.Fix {
			fmt.Fprintf(w, "      → %s\n", fix)
		}
	}
}

// pollUntil calls attempt immediately and then every interval until it
// reports done or the timeout (0 = none) elapses. On timeout it returns
// errWaitTimedOut so the caller can render its own re-run hint.
var errWaitTimedOut = errors.New("wait timed out")

func pollUntil(ctx *Context, opts waitOptions, attempt func() (done bool, err error)) error {
	var deadline time.Time
	if *opts.timeout > 0 {
		deadline = ctx.now().Add(*opts.timeout)
	}
	for {
		done, err := attempt()
		if done || err != nil {
			return err
		}
		if !deadline.IsZero() && !ctx.now().Before(deadline) {
			return errWaitTimedOut
		}
		ctx.sleep(*opts.interval)
	}
}

// waitTimeoutError is the exit-3 outcome: nothing changed, re-run to resume.
func waitTimeoutError(opts waitOptions, required, rerun string) error {
	return &exitCodeError{
		Code: exitWaitTimeout,
		Msg: fmt.Sprintf("Still waiting for human approval for %q after %s — nothing changed.\nRe-run to keep waiting (the wait's start time is kept):\n  %s",
			required, opts.timeout, rerun),
	}
}

// printWaitingLine is the single stderr line emitted when a wait begins. No
// heartbeat follows: agents pay for every output line.
func printWaitingLine(ctx *Context, opts waitOptions, slug, required string) {
	fmt.Fprintf(ctx.Stderr, "Waiting for human approval for %q — approve: %s (timeout: %s)\n",
		required, approvalURL(ctx.Project, slug, required), opts.timeoutLabel())
}

// waitOutput reports a completed wait, read back from the stats sidecar row
// the transition just wrote (the single source of truth for gate timing).
type waitOutput struct {
	StartedAt     time.Time  `json:"started_at"`
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`
	WaitedSeconds int64      `json:"waited_seconds"`
}

func lastWait(issuePath, to string, now time.Time) *waitOutput {
	store, err := tracker.LoadStats(issuePath)
	if err != nil || len(store.Transitions) == 0 {
		return nil
	}
	row := store.Transitions[len(store.Transitions)-1]
	if !strings.EqualFold(row.To, to) || row.WaitStartedAt == nil {
		return nil
	}
	return &waitOutput{
		StartedAt:     *row.WaitStartedAt,
		ApprovedAt:    row.ApprovedAt,
		WaitedSeconds: int64(now.Sub(*row.WaitStartedAt).Round(time.Second) / time.Second),
	}
}

func printWaited(w io.Writer, wo *waitOutput) {
	if wo == nil {
		return
	}
	line := fmt.Sprintf("✓ Waited: %s", (time.Duration(wo.WaitedSeconds) * time.Second).String())
	if wo.ApprovedAt != nil {
		line += fmt.Sprintf(" (approved %s)", wo.ApprovedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintln(w, line)
}
