package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/ci"
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

At a human-approval gate, tell the human what to approve, then stop and end
your turn. The viewer's approve button nudges your session to resume; re-run
the transition then.

Examples:
  issue-cli transition <slug> --to "testing"
  issue-cli transition <slug> --to "backlog" --dry-run
  issue-cli transition <slug> --to "waiting-for-team-input" --field waiting="design review"`,
	Run:        runTransition,
	ExtraFlags: []string{"field"},
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
	Done                 bool                      `json:"done,omitempty"`
	Slug                 string                    `json:"slug"`
	File                 string                    `json:"file"`
	SideEffects          []string                  `json:"side_effects"`
	Checklist            []transitionChecklistItem `json:"checklist"`
	BodyChanged          bool                      `json:"body_changed"`
	CommentsChanged      bool                      `json:"comments_changed"`
	Type                 string                    `json:"type,omitempty"`
	TypePath             string                    `json:"type_path,omitempty"`
	NextStatus           string                    `json:"next_status,omitempty"`
	NextStatusOptional   bool                      `json:"next_status_optional,omitempty"`
	OptionalNextStatuses []string                  `json:"optional_next_statuses,omitempty"`
	NextRequires         []string                  `json:"next_requires,omitempty"`
	NextSideEffects      []string                  `json:"next_side_effects,omitempty"`
	NextCommand          string                    `json:"next_command,omitempty"`
	NextCommandNote      string                    `json:"next_command_note,omitempty"`
	Guidance             []string                  `json:"guidance,omitempty"`
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
	if err := rejectRemovedWaitFlags(rest); err != nil {
		return err
	}
	if err := parseFlagsArgs(ctx, fs, rest); err != nil {
		return err
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
	printTypeWarnings(ctx.Stderr, wf, issue)

	if *dryRun {
		return runTransitionDryRun(ctx, wf, issue, slug, to, fields)
	}

	from, result, err := wf.ApplyTransitionToFileWithFields(issue.FilePath, to, fields)
	if err != nil {
		return fmt.Errorf("failed to transition: %w", err)
	}

	issue, _, err = findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if err := printTransitionResult(ctx, buildTransitionOutput(wf, issue, from, to, result)); err != nil {
		return err
	}
	if to == "done" && !ctx.JSONOutput {
		printCIAfterDone(ctx, wf)
	}
	return nil
}

// printCIAfterDone tells the agent that just shipped that local CI tests
// main without it, so it stops instead of waiting.
func printCIAfterDone(ctx *Context, wf *tracker.WorkflowConfig) {
	if !wf.CI.Enabled() {
		return
	}
	root := ci.RepoRoot(projectRoot(ctx.Project))
	branch, err := ci.ResolveBranch(root, wf.CI)
	if err != nil {
		return
	}
	head, _ := ci.Head(root, branch)
	fmt.Fprintf(ctx.Stdout, "Local CI tests %s at %s by itself: don't wait for it, you are done. If it goes red on your merge, the lead may re-open this issue (%s).\n",
		branch, ci.Short(head), tracker.InjectProject(ctx.Project, "issue-cli ci status"))
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
		out.SideEffects = sectionDryRunEffects(wf, issue, from, to, fields, out.SideEffects)
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
	next, nextOptional, optionals := defaultNextStep(wf, issue.Status)

	guidance := []string{}
	if prompt := strings.TrimSpace(wf.StatusPrompt(issue.Status)); prompt != "" {
		guidance = append(guidance, prompt)
	}
	guidance = append(guidance, result.InjectedPrompts...)

	statusOptional := false
	if s := wf.GetStatus(issue.Status); s != nil {
		statusOptional = s.Optional
	}
	var nextRequires, nextSideEffects []string
	var nextCommand, nextNote string
	if next != "" {
		guidance = append(guidance, wf.EntryPrompts(issue.Status, next)...)
		nextRequires, nextSideEffects = nextTransitionContract(wf, issue.Status, next)
		nextCommand, nextNote = nextStepCommand(wf, issue.Slug, issue.Status, next)
	}
	return transitionOutput{
		From:                 from,
		To:                   to,
		Status:               issue.Status,
		StatusOptional:       statusOptional,
		Done:                 next == "" && !statusOptional,
		Slug:                 issue.Slug,
		File:                 issue.FilePath,
		SideEffects:          transitionSideEffects(result),
		Checklist:            collectChecklist(issue.BodyRaw),
		BodyChanged:          result.BodyChanged,
		CommentsChanged:      false,
		Type:                 wf.ActiveType,
		TypePath:             typePath(wf),
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

// defaultNextStep picks the status to suggest after status: the next required
// one, else the first remaining optional one (allOptional), plus the optional
// side-paths. Side statuses (parked) are dropped when nothing required is
// left, so a finished issue gets no next step.
func defaultNextStep(wf *tracker.WorkflowConfig, status string) (next string, allOptional bool, optionals []string) {
	required, optionals := wf.DefaultNextStatus(status)
	if required != "" {
		return required, false, optionals
	}
	var onPath []string
	for _, o := range optionals {
		if !wf.IsSideStatus(o) {
			onPath = append(onPath, o)
		}
	}
	if len(onPath) == 0 {
		return "", false, nil
	}
	return onPath[0], true, onPath[1:]
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
	if result.PromotedApproval != "" {
		effects = append(effects, fmt.Sprintf("queued approval for %q is now active — run issue-cli start to continue", result.PromotedApproval))
	}
	sections := sectionEffects(result)
	effects = append(effects, sections...)
	if result.BodyAppended && len(sections) == 0 {
		effects = append(effects, "workflow content appended to issue body")
	} else if result.BodyChanged && !result.BodyAppended {
		effects = append(effects, "issue body updated")
	}
	if len(result.InjectedPrompts) > 0 {
		effects = append(effects, fmt.Sprintf("%d entry guidance prompt(s) injected", len(result.InjectedPrompts)))
	}
	return effects
}

// sectionEffects names each gate section a transition created or topped up
// with missing boxes, so an agent sees which checklist it now has to work.
func sectionEffects(result tracker.TransitionResult) []string {
	var out []string
	for _, sec := range result.Sections {
		switch {
		case sec.Created:
			out = append(out, fmt.Sprintf("## %s appended", sec.Title))
		case sec.BoxesAdded > 0:
			out = append(out, fmt.Sprintf("## %s existed — added %s", sec.Title, missingBoxes(sec.BoxesAdded)))
		}
	}
	return out
}

// sectionDryRunEffects rewrites the static "appends ## X section" side
// effect for each section the issue already has: the transition adds only
// the boxes it lacks, or leaves it as is.
func sectionDryRunEffects(wf *tracker.WorkflowConfig, issue *tracker.Issue, from, to string, fields map[string]string, effects []string) []string {
	probe := *issue
	result := wf.ApplyTransitionWithFields(&probe, from, to, fields)
	for _, sec := range result.Sections {
		if sec.Created {
			continue
		}
		line := fmt.Sprintf("Side-effect: ## %s exists with all its boxes — left as is", sec.Title)
		if sec.BoxesAdded > 0 {
			line = fmt.Sprintf("Side-effect: ## %s exists — adds %s", sec.Title, missingBoxes(sec.BoxesAdded))
		}
		static := fmt.Sprintf("Side-effect: appends ## %s section", sec.Title)
		for i, e := range effects {
			if e == static {
				effects[i] = line
			}
		}
	}
	return effects
}

func missingBoxes(n int) string {
	if n == 1 {
		return "1 missing box"
	}
	return fmt.Sprintf("%d missing boxes", n)
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
	if output.Type != "" {
		statusDisp += " | Type: " + output.Type
	}
	fmt.Fprintf(ctx.Stdout, "Status: %s\n", statusDisp)
	for _, effect := range output.SideEffects {
		fmt.Fprintf(ctx.Stdout, "✓ %s\n", capitalize(effect))
	}
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
				fmt.Fprintf(w, "  %s [%s] %s\n", item.ID, mark, tracker.StripCheckboxID(item.ID, item.Text))
				hint = hint || !item.Checked
			} else {
				fmt.Fprintf(w, "  %d. [%s] %s\n", item.Index, mark, item.Text)
			}
		}
		if hint {
			fmt.Fprintf(w, "Tick done boxes by id, several at once: issue-cli check %s <id> [<id>...]\n", slug)
			fmt.Fprintf(w, "Box text wrong? issue-cli check-edit %s <id> --text \"...\"\n", slug)
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
	if output.Done {
		fmt.Fprintln(w, "== Done ==")
		fmt.Fprintf(w, "  %s is the end of the workflow: nothing is left to do on this issue.\n", output.Status)
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
