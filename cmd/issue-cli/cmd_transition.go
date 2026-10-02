package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var transitionCommand = &Command{
	Name:      "transition",
	ShortHelp: "Move issue to next status (strict ordering)",
	LongHelp: `Transition an issue to a new status. Use --field key=value for declarative
field answers required by the workflow.

--dry-run lists every unmet requirement (checkboxes, comments, fields, human
approval) with the command that fixes each, and changes nothing. Exit 0 when
the transition would succeed, 1 otherwise.

--wait blocks until the required human approval exists, then transitions.
Unmet machine checks fail fast first (a human click will not fix them).
--timeout <dur> gives up after that long with exit code 3: nothing changed,
re-run the same command to keep waiting. Without --timeout it waits forever.
Agents running in a Bash tool should pass --timeout 9m and re-run on exit 3.

Examples:
  issue-cli transition <slug> --to "testing"
  issue-cli transition <slug> --to "backlog" --dry-run
  issue-cli transition <slug> --to "backlog" --wait --timeout 9m
  issue-cli transition <slug> --to "waiting-for-team-input" --field waiting="design review"`,
	Run: runTransition,
}

func init() {
	registerCommand(transitionCommand)
}

type transitionChecklistItem struct {
	Section string `json:"section,omitempty"`
	Index   int    `json:"index"`
	ID      string `json:"id,omitempty"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

type transitionOutput struct {
	From                 string                    `json:"from"`
	To                   string                    `json:"to"`
	Status               string                    `json:"status"`
	StatusOptional       bool                      `json:"status_optional,omitempty"`
	Slug                 string                    `json:"slug"`
	File                 string                    `json:"file"`
	SideEffects          []string                  `json:"side_effects"`
	Checklist            []transitionChecklistItem `json:"checklist"`
	BodyChanged          bool                      `json:"body_changed"`
	CommentsChanged      bool                      `json:"comments_changed"`
	NextStatus           string                    `json:"next_status,omitempty"`
	NextStatusOptional   bool                      `json:"next_status_optional,omitempty"`
	OptionalNextStatuses []string                  `json:"optional_next_statuses,omitempty"`
	NextRequires         []string                  `json:"next_requires,omitempty"`
	NextSideEffects      []string                  `json:"next_side_effects,omitempty"`
	NextCommand          string                    `json:"next_command,omitempty"`
	NextCommandNote      string                    `json:"next_command_note,omitempty"`
	Guidance             []string                  `json:"guidance,omitempty"`
	Wait                 *waitOutput               `json:"wait,omitempty"`
}

func runTransition(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "transition")
	if err != nil {
		return err
	}

	// Pre-extract --field flags before FlagSet parsing — flag.FlagSet does not
	// natively support repeated unknown flags, so we collect them ourselves.
	fields, err := parseFieldFlags(rest)
	if err != nil {
		return err
	}
	rest = stripFieldFlags(rest)

	fs := newFlagSet("transition", ctx)
	toFlag := fs.String("to", "", "destination status")
	dryRun := fs.Bool("dry-run", false, "list every unmet requirement without changing anything")
	waitOpts := registerWaitFlags(fs)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := waitOpts.validate(); err != nil {
		return err
	}
	if *dryRun && *waitOpts.wait {
		return fmt.Errorf("--dry-run and --wait are mutually exclusive")
	}
	to := *toFlag
	if to == "" {
		// Accept positional: transition <slug> <status>
		for _, a := range fs.Args() {
			if !strings.HasPrefix(a, "--") {
				to = a
				break
			}
		}
	}
	if to == "" {
		return fmt.Errorf("--to is required\n\nExamples:\n  issue-cli transition %s --to \"testing\"\n  issue-cli transition %s --to \"waiting-for-team-input\" --field waiting=\"design review\"", slug, slug)
	}
	to = strings.ToLower(to)

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	wf := ctx.Project.LoadWorkflowForIssue(issue)
	if to != "done" {
		ctx.ApprovalWaitCommand = waitCommand(slug, issue.Status, to)
	}

	if *dryRun {
		return runTransitionDryRun(ctx, wf, issue, slug, to, fields)
	}

	var from string
	var result tracker.TransitionResult
	if *waitOpts.wait {
		from, result, err = transitionWithWait(ctx, wf, issue, slug, to, fields, waitOpts)
	} else {
		from, result, err = wf.ApplyTransitionToFileWithFields(issue.FilePath, to, fields)
	}
	if err != nil {
		var codeErr *exitCodeError
		if errors.As(err, &codeErr) {
			return err
		}
		return fmt.Errorf("failed to transition: %w", err)
	}

	issue, _, err = findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	output := buildTransitionOutput(wf, issue, from, to, result)
	output.Wait = lastWait(issue.FilePath, to, ctx.now())
	return printTransitionResult(ctx, output)
}

// runTransitionDryRun reports every unmet requirement for issue → to and
// changes nothing. Exits 1 (silently — the report is the message) when any
// requirement is unmet so scripts can branch on it.
func runTransitionDryRun(ctx *Context, wf *tracker.WorkflowConfig, issue *tracker.Issue, slug, to string, fields map[string]string) error {
	from := issue.Status
	problems := collectTransitionProblems(ctx, wf, issue, slug, from, to, fields)
	out := dryRunOutput{DryRun: true, Ready: len(problems) == 0, From: from, To: to, Slug: slug, Problems: problems}
	if out.Ready {
		_, out.SideEffects = nextTransitionContract(wf, from, to)
	}
	if out.Problems == nil {
		out.Problems = []transitionProblem{}
	}

	if ctx.JSONOutput {
		if err := writeJSON(ctx.Stdout, out); err != nil {
			return err
		}
	} else if out.Ready {
		fmt.Fprintf(ctx.Stdout, "✓ Ready: %s → %s (dry run — nothing was changed)\n", from, to)
		renderNextTransitionContract(ctx.Stdout, nil, out.SideEffects)
	} else {
		fmt.Fprintf(ctx.Stdout, "== Dry run: %s → %s ==\n", from, to)
		printProblemReport(ctx.Stdout, problems)
		fmt.Fprintln(ctx.Stdout, "Nothing was changed.")
	}
	if !out.Ready {
		return &exitCodeError{Code: 1}
	}
	return nil
}

// transitionWithWait fails fast on any unmet machine requirement, then blocks
// until the human approval lands and applies the transition. Each poll calls
// the normal locked ApplyTransitionToFileWithFields, which changes nothing
// while the approval is missing, so a concurrent edit that breaks a machine
// check (or moves the status) ends the wait with that error.
func transitionWithWait(ctx *Context, wf *tracker.WorkflowConfig, issue *tracker.Issue, slug, to string, fields map[string]string, opts waitOptions) (string, tracker.TransitionResult, error) {
	from := issue.Status
	approval, machine := onlyApprovalMissing(collectTransitionProblems(ctx, wf, issue, slug, from, to, fields))
	if len(machine) > 0 {
		printProblemReport(ctx.Stdout, machine)
		return "", tracker.TransitionResult{}, &exitCodeError{
			Code: 1,
			Msg:  fmt.Sprintf("Not waiting: fix the %d requirement(s) above first — a human approval will not resolve them.", len(machine)),
		}
	}

	required := wf.RequiredHumanApproval(from, to)
	if approval {
		if _, err := tracker.BeginWait(issue.FilePath, to, ctx.now()); err != nil {
			return "", tracker.TransitionResult{}, err
		}
		printWaitingLine(ctx, opts, slug, required)
	}

	var gotFrom string
	var result tracker.TransitionResult
	err := pollUntil(ctx, opts, func() (bool, error) {
		f, r, err := wf.ApplyTransitionToFileWithFields(issue.FilePath, to, fields)
		if errors.Is(err, tracker.ErrApprovalMissing) {
			return false, nil
		}
		gotFrom, result = f, r
		return true, err
	})
	if errors.Is(err, errWaitTimedOut) {
		rerun := fmt.Sprintf("issue-cli transition %s --to %q --wait --timeout %s", slug, to, opts.timeout)
		return "", tracker.TransitionResult{}, waitTimeoutError(opts, required, rerun)
	}
	return gotFrom, result, err
}

// stripFieldFlags returns args with every "--field" + value pair removed.
// parseFieldFlags has already validated the structure.
func stripFieldFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--field" {
			if i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func buildTransitionOutput(wf *tracker.WorkflowConfig, issue *tracker.Issue, from, to string, result tracker.TransitionResult) transitionOutput {
	required, optionals := wf.DefaultNextStatus(issue.Status)
	next := required
	nextOptional := false
	if next == "" && len(optionals) > 0 {
		next = optionals[0]
		nextOptional = true
		optionals = optionals[1:]
	}

	guidance := []string{}
	if prompt := strings.TrimSpace(wf.StatusPrompt(issue.Status)); prompt != "" {
		guidance = append(guidance, prompt)
	}
	guidance = append(guidance, result.InjectedPrompts...)
	guidance = append(guidance, wf.EntryPrompts(issue.Status, next)...)

	statusOptional := false
	if s := wf.GetStatus(issue.Status); s != nil {
		statusOptional = s.Optional
	}
	var nextRequires, nextSideEffects []string
	var nextCommand, nextNote string
	if next != "" {
		nextRequires, nextSideEffects = nextTransitionContract(wf, issue.Status, next)
		nextCommand, nextNote = nextStepCommand(wf, issue.Slug, issue.Status, next)
	}
	return transitionOutput{
		From:                 from,
		To:                   to,
		Status:               issue.Status,
		StatusOptional:       statusOptional,
		Slug:                 issue.Slug,
		File:                 issue.FilePath,
		SideEffects:          transitionSideEffects(result),
		Checklist:            collectChecklist(issue.BodyRaw),
		BodyChanged:          result.BodyChanged,
		CommentsChanged:      false,
		NextStatus:           next,
		NextStatusOptional:   nextOptional,
		OptionalNextStatuses: optionals,
		NextRequires:         nextRequires,
		NextSideEffects:      nextSideEffects,
		NextCommand:          nextCommand,
		NextCommandNote:      nextNote,
		Guidance:             guidance,
	}
}

func transitionSideEffects(result tracker.TransitionResult) []string {
	var effects []string
	if result.Update.Assignee != nil {
		if *result.Update.Assignee == "" {
			effects = append(effects, "assignee cleared")
		} else {
			effects = append(effects, fmt.Sprintf("assignee set to %q", *result.Update.Assignee))
		}
	}
	if result.ClearedApproval {
		effects = append(effects, "approval consumed")
	}
	if result.BodyAppended {
		effects = append(effects, "workflow content appended to issue body")
	} else if result.BodyChanged {
		effects = append(effects, "issue body updated")
	}
	if len(result.InjectedPrompts) > 0 {
		effects = append(effects, fmt.Sprintf("%d entry guidance prompt(s) injected", len(result.InjectedPrompts)))
	}
	return effects
}

func collectChecklist(body string) []transitionChecklistItem {
	var items []transitionChecklistItem
	for _, it := range tracker.ListCheckboxes(body) {
		items = append(items, transitionChecklistItem{
			Section: it.Section,
			Index:   it.Index,
			ID:      it.ID,
			Text:    it.Text,
			Checked: it.Checked,
		})
	}
	return items
}

func printTransitionResult(ctx *Context, output transitionOutput) error {
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, output)
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s → %s\n", output.From, output.To)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", output.File)
	statusDisp := output.Status
	if output.StatusOptional {
		statusDisp += " (optional)"
	}
	fmt.Fprintf(ctx.Stdout, "Status: %s\n", statusDisp)
	for _, effect := range output.SideEffects {
		fmt.Fprintf(ctx.Stdout, "✓ %s\n", capitalize(effect))
	}
	printWaited(ctx.Stdout, output.Wait)
	fmt.Fprintln(ctx.Stdout)

	printWorkflowNextStepsFromData(ctx.Stdout, output)
	return nil
}

func printWorkflowNextStepsFromData(w io.Writer, output transitionOutput) {
	checklist, guidance := output.Checklist, output.Guidance
	nextStatus, nextStatusOptional, optionalSidePaths := output.NextStatus, output.NextStatusOptional, output.OptionalNextStatuses
	nextRequires, nextSideEffects, slug := output.NextRequires, output.NextSideEffects, output.Slug
	if len(checklist) > 0 {
		checked := 0
		for _, item := range checklist {
			if item.Checked {
				checked++
			}
		}
		fmt.Fprintf(w, "== Checklist (%d/%d) ==\n", checked, len(checklist))
		const noSection = "\x00"
		lastSection := noSection
		hint := false
		for _, item := range checklist {
			if item.Section != lastSection {
				lastSection = item.Section
				header := item.Section
				if header == "" {
					header = "(no section)"
				}
				fmt.Fprintf(w, "## %s\n", header)
			}
			mark := " "
			if item.Checked {
				mark = "x"
			}
			if item.ID != "" {
				fmt.Fprintf(w, "  %s [%s] %s\n", item.ID, mark, item.Text)
				hint = hint || !item.Checked
			} else {
				fmt.Fprintf(w, "  %d. [%s] %s\n", item.Index, mark, item.Text)
			}
		}
		if hint {
			fmt.Fprintf(w, "Tick done boxes by id, several at once: issue-cli check %s <id> [<id>...]\n", slug)
		}
		fmt.Fprintln(w)
	}
	if len(guidance) > 0 {
		fmt.Fprintln(w, "== Guidance ==")
		for _, prompt := range guidance {
			fmt.Fprintf(w, "- %s\n", prompt)
		}
		fmt.Fprintln(w)
	}
	if nextStatus != "" {
		fmt.Fprintln(w, "== Next ==")
		suffix := ""
		if nextStatusOptional {
			suffix = "   (optional — every remaining status is optional)"
		}
		cmd := output.NextCommand
		if cmd == "" {
			cmd = fmt.Sprintf("issue-cli transition %s --to %q", slug, nextStatus)
		}
		fmt.Fprintf(w, "  %s%s\n", cmd, suffix)
		if output.NextCommandNote != "" {
			fmt.Fprintf(w, "  %s\n", output.NextCommandNote)
		}
		renderNextTransitionContract(w, nextRequires, nextSideEffects)
		if len(optionalSidePaths) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Optional side-paths:")
			for _, opt := range optionalSidePaths {
				fmt.Fprintf(w, "  issue-cli transition %s --to \"%s\"\n", slug, opt)
			}
		}
	}
}
