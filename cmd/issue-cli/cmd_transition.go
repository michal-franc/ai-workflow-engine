package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var transitionCommand = &Command{
	Name:      "transition",
	ShortHelp: "Move issue to next status (strict ordering)",
	LongHelp: `Transition an issue to a new status. Use --field key=value for declarative
field answers required by the workflow.

Examples:
  issue-cli transition <slug> --to "testing"
  issue-cli transition <slug> --to "waiting-for-team-input" --field waiting="design review"`,
	Run: runTransition,
}

func init() {
	registerCommand(transitionCommand)
}

type transitionChecklistItem struct {
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
	if err := fs.Parse(rest); err != nil {
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

	from, result, err := wf.ApplyTransitionToFileWithFields(issue.FilePath, to, fields)
	if err != nil {
		return fmt.Errorf("failed to transition: %w", err)
	}

	issue, _, err = findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	output := buildTransitionOutput(wf, issue, from, to, result)
	return printTransitionResult(ctx, output)
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
	if next != "" {
		nextRequires, nextSideEffects = nextTransitionContract(wf, issue.Status, next)
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
	re := regexp.MustCompile(`^\s*-\s*\[([ xX])\]\s*(.*)$`)
	var items []transitionChecklistItem
	for _, line := range strings.Split(body, "\n") {
		m := re.FindStringSubmatch(line)
		if len(m) == 3 {
			items = append(items, transitionChecklistItem{
				Text:    strings.TrimSpace(m[2]),
				Checked: strings.EqualFold(m[1], "x"),
			})
		}
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
	fmt.Fprintln(ctx.Stdout)

	printWorkflowNextStepsFromData(ctx.Stdout, output.Checklist, output.Guidance, output.NextStatus, output.NextStatusOptional, output.OptionalNextStatuses, output.NextRequires, output.NextSideEffects, output.Slug)
	return nil
}

func printWorkflowNextStepsFromData(w io.Writer, checklist []transitionChecklistItem, guidance []string, nextStatus string, nextStatusOptional bool, optionalSidePaths []string, nextRequires, nextSideEffects []string, slug string) {
	if len(checklist) > 0 {
		checked := 0
		for _, item := range checklist {
			if item.Checked {
				checked++
			}
		}
		fmt.Fprintf(w, "== Checklist (%d/%d) ==\n", checked, len(checklist))
		for _, item := range checklist {
			mark := " "
			if item.Checked {
				mark = "x"
			}
			fmt.Fprintf(w, "- [%s] %s\n", mark, item.Text)
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
		fmt.Fprintf(w, "  issue-cli transition %s --to \"%s\"%s\n", slug, nextStatus, suffix)
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
