package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// exitCodeError carries a specific process exit code. main prints Msg (when
// non-empty) verbatim — no "Error:" prefix — and skips the repeated-failure
// hint, because these outcomes are expected states, not mistakes to retry.
type exitCodeError struct {
	Code int
	Msg  string
}

func (e *exitCodeError) Error() string { return e.Msg }

// removedWaitVersion is the release that removed --wait (decision
// ui/workbench-board-card-markers-and-agent-footer#D-5).
const removedWaitVersion = "v0.43.0"

// stopAtGate is what an agent does at a human-approval gate instead of
// blocking: the viewer's approve button nudges its tmux session to resume.
const stopAtGate = "stop and end your turn; the approval nudge resumes you"

// removedFlagError is a flag that used to exist; Msg says what to do instead.
// Telemetry still counts it as an unknown flag.
type removedFlagError struct {
	Flag string
	Msg  string
}

func (e *removedFlagError) Error() string { return e.Msg }

// rejectRemovedWaitFlags fails on the --wait / --timeout / --interval flags
// transition and start used to take, before the FlagSet's generic
// "flag provided but not defined" error (and usage dump) can fire.
func rejectRemovedWaitFlags(args []string) error {
	for _, a := range args {
		if a == "--" {
			return nil
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch name {
		case "wait":
			return &removedFlagError{Flag: "--wait", Msg: fmt.Sprintf("--wait was removed in %s: %s", removedWaitVersion, stopAtGate)}
		case "timeout", "interval":
			return &removedFlagError{Flag: "--" + name, Msg: fmt.Sprintf("--%s was removed in %s along with --wait: %s", name, removedWaitVersion, stopAtGate)}
		}
	}
	return nil
}

// approvalNote is the one-liner printed under a next-step command that is
// gated on a human approval.
func approvalNote(required string) string {
	return fmt.Sprintf("(needs human approval for %q — tell the human what to approve, then stop and end your turn; run it once they say it is approved)", required)
}

// nextStepCommand returns the command for the next step from → next and an
// optional note. When the step is gated on a human approval the note tells the
// agent to hand the approval to the human and stop, instead of waiting or polling. `done`
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
	if tracker.IsHandoffStatus(from) {
		return fmt.Sprintf("issue-cli start %s", slug), approvalNote(required)
	}
	return plain, approvalNote(required)
}

// transitionProblem is one unmet requirement for a transition.
type transitionProblem struct {
	// Kind is "order", "field", "validator", or "approval". Only "approval"
	// is resolved by a human click.
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
				"tell the human what to approve, then " + stopAtGate,
			}
		default:
			p.Kind = "validator"
			p.Fix = checkboxFixes(slug, issue.BodyRaw, step.Action.Rule)
		}
		problems = append(problems, p)
	}
	return problems
}

// checkboxFixes is the `issue-cli check` command that ticks every open box a
// checkbox gate is waiting on — by id when ids are available (one call,
// several ids), else one call per box text. Other rules already embed their
// fix in the message.
func checkboxFixes(slug, body, rule string) []string {
	name, arg := rule, ""
	if idx := strings.Index(rule, ": "); idx != -1 {
		name, arg = rule[:idx], strings.TrimSpace(rule[idx+2:])
	}
	if name != "section_checkboxes_checked" && name != "all_checkboxes_checked" {
		return nil
	}
	var ids, byText []string
	for _, item := range tracker.ListCheckboxes(body) {
		if item.Checked || (name == "section_checkboxes_checked" && !strings.EqualFold(item.Section, arg)) {
			continue
		}
		if item.ID == "" {
			byText = append(byText, fmt.Sprintf("issue-cli check %s %q", slug, item.Text))
			continue
		}
		ids = append(ids, item.ID)
	}
	if len(ids) > 0 {
		byText = append([]string{fmt.Sprintf("issue-cli check %s %s", slug, strings.Join(ids, " "))}, byText...)
	}
	return byText
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

// printProblemReport renders problems for --dry-run. Messages from checkRule
// are multi-line (they embed the fix), so continuation lines are indented
// under their requirement.
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
			if strings.Contains(p.Message, fix) {
				continue // already spelled out in the validator's message
			}
			fmt.Fprintf(w, "      → %s\n", fix)
		}
	}
}
