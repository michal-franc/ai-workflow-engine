package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var decideCommand = &Command{
	Name:      "decide",
	ShortHelp: "Record a decision as a D-n line under ## Decisions",
	LongHelp: `Record a decision as a dated D-n line under the issue's ## Decisions
section: one made in chat, by a project rule or after a playtest. Questions
themselves stay in chat (Claude Code / Codex); only the outcome is recorded
here. The viewer's
Decisions tab (/p/<project>/decisions) is generated from these lines.

Other issues cite a decision as <slug>#D-n anywhere in their body.
--replaces marks older decisions as replaced by this one; the output lists the
open issues that still cite them, so they can be revised.

  --text       the decision, one line (required)
  --how        how it was made, e.g. "chat", "project rule", "playtest"
               (default "recorded by agent"; must not contain ':')
  --quote      the human's own words, quoted after the text
  --replaces   repeatable: <slug>#D-n (or D-n in this issue) it replaces
  --deferred   the decision is to decide later

Examples:
  issue-cli decide sim/et-style-objective-format-6v6 \
    --text "The continuous raid becomes an ET-style objective format, 6v6." \
    --how chat --quote "The old continuous raid had no readable tension." \
    --replaces sim/gdd-match-phases#D-1
  issue-cli decide ai/fantasy-classes --deferred --how chat \
    --text "Balance classes once more elements are in place."`,
	Run: runDecide,
}

var decisionsCommand = &Command{
	Name:      "decisions",
	ShortHelp: "List the project's decisions index (D-n lines, citations, replaced/stale)",
	LongHelp: `List every D-n decision in the project, newest first, with its state:
active, deferred, replaced, or stale (replaced but still cited by open issues).

  --system S       only decisions recorded in issues of system S
  --state X        active | deferred | replaced | stale
  --issue <slug>   only decisions recorded in this issue

The same index is the viewer's Decisions tab (/p/<project>/decisions).

Examples:
  issue-cli decisions --state stale
  issue-cli decisions --issue sim/gdd-match-phases --json`,
	Run: runDecisions,
}

func init() {
	registerCommand(decideCommand)
	registerCommand(decisionsCommand)
}

type staleCiter struct {
	Slug   string `json:"slug"`
	Status string `json:"status"`
	Cites  string `json:"cites"`
}

type decideOutput struct {
	Decision    tracker.Decision `json:"decision"`
	StaleCiters []staleCiter     `json:"stale_citers"`
	Decisions   string           `json:"decisions_url"`
}

func runDecide(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "decide")
	if err != nil {
		return err
	}
	fs := newFlagSet("decide", ctx)
	textFlag := fs.String("text", "", "the decision, one line")
	howFlag := fs.String("how", "", `how it was made: "chat", "project rule", "playtest" (default "recorded by agent")`)
	quoteFlag := fs.String("quote", "", "the human's own words")
	deferredFlag := fs.Bool("deferred", false, "the decision is deferred (decide later)")
	var replaces []string
	fs.Func("replaces", "repeatable: <slug>#D-n (or D-n in this issue) this decision replaces", func(v string) error {
		replaces = append(replaces, v)
		return nil
	})
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}

	issue, issues, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	nd := tracker.NewDecision{Text: *textFlag, How: *howFlag, Quote: *quoteFlag, Deferred: *deferredFlag}
	if err := tracker.ValidateNewDecision(nd); err != nil {
		return fmt.Errorf("%w\n\nExample:\n  issue-cli decide %s --text \"Gold resets each stage.\" --how chat", err, issue.Slug)
	}

	index := tracker.BuildDecisionIndex(issues, tracker.ClosedIssueFunc(ctx.Project))
	for _, raw := range replaces {
		ref := tracker.NormalizeDecisionRef(issue.Slug, raw)
		if ref == "" {
			return fmt.Errorf("--replaces %q is not a decision ref; use <slug>#D-n (or D-n in this issue)", raw)
		}
		if index.Lookup(ref) == nil {
			refSlug, _, _ := strings.Cut(ref, "#")
			return fmt.Errorf("--replaces %s: no such decision\n\nList the decisions in that issue:\n  issue-cli decisions --issue %s", ref, refSlug)
		}
		nd.Replaces = append(nd.Replaces, ref)
	}

	d, err := tracker.AddDecision(issue.FilePath, issue.Slug, nd, ctx.now())
	if err != nil {
		return fmt.Errorf("failed to record decision: %w", err)
	}

	var stale []staleCiter
	for _, ref := range nd.Replaces {
		for _, citer := range index.Lookup(ref).CitedOpen {
			if citer.Slug != issue.Slug {
				stale = append(stale, staleCiter{Slug: citer.Slug, Status: citer.Status, Cites: ref})
			}
		}
	}
	out := decideOutput{Decision: d, StaleCiters: stale, Decisions: decisionsURL(ctx.Project)}
	if out.StaleCiters == nil {
		out.StaleCiters = []staleCiter{}
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, out)
	}

	fmt.Fprintf(ctx.Stdout, "✓ Recorded %s\n", d.Ref)
	fmt.Fprintln(ctx.Stdout, d.Raw)
	for _, ref := range nd.Replaces {
		fmt.Fprintf(ctx.Stdout, "Replaced %s.", ref)
		var lines []string
		for _, s := range stale {
			if s.Cites == ref {
				lines = append(lines, fmt.Sprintf("  %s (%s)", s.Slug, s.Status))
			}
		}
		if len(lines) == 0 {
			fmt.Fprintln(ctx.Stdout, " No open issue cites it.")
			continue
		}
		fmt.Fprintln(ctx.Stdout, " Open issues still citing it:")
		fmt.Fprintln(ctx.Stdout, strings.Join(lines, "\n"))
	}
	if len(stale) > 0 {
		fmt.Fprintf(ctx.Stdout, "Next: revise those issues to cite %s, or leave them for the human (they show as stale in %s).\n", d.Ref, out.Decisions)
	}
	fmt.Fprintf(ctx.Stdout, "Cite this decision from dependent issues as %s.\n", d.Ref)
	return nil
}

type decisionRow struct {
	tracker.Decision
	State       string   `json:"state"`
	System      string   `json:"system"`
	Status      string   `json:"issue_status"`
	ReplacedBy  string   `json:"replaced_by,omitempty"`
	CitedOpen   []string `json:"cited_by_open"`
	CitedClosed int      `json:"cited_by_closed"`
}

func runDecisions(ctx *Context, args []string) error {
	fs := newFlagSet("decisions", ctx)
	systemFlag := fs.String("system", "", "only decisions recorded in issues of this system")
	stateFlag := fs.String("state", "", "active | deferred | replaced | stale")
	issueFlag := fs.String("issue", "", "only decisions recorded in this issue")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	state := strings.ToLower(strings.TrimSpace(*stateFlag))
	switch state {
	case "", tracker.DecisionActive, tracker.DecisionDeferred, tracker.DecisionReplaced, tracker.DecisionStale:
	default:
		return fmt.Errorf("--state %q: want active, deferred, replaced or stale", *stateFlag)
	}

	issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	onlyIssue := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*issueFlag), ".md"))
	if onlyIssue != "" {
		found := false
		for _, i := range issues {
			if i.Slug == onlyIssue {
				found = true
				break
			}
		}
		if !found {
			if _, _, err := findIssueOrErr(ctx, onlyIssue); err != nil {
				return err
			}
		}
	}

	index := tracker.BuildDecisionIndex(issues, tracker.ClosedIssueFunc(ctx.Project))
	rows := []decisionRow{}
	for _, e := range index.Entries {
		if *systemFlag != "" && !strings.EqualFold(e.Issue.System, *systemFlag) {
			continue
		}
		if onlyIssue != "" && e.Slug != onlyIssue {
			continue
		}
		if !matchesDecisionState(e, state) {
			continue
		}
		row := decisionRow{Decision: e.Decision, State: e.State, System: e.Issue.System, Status: e.Issue.Status, CitedOpen: []string{}, CitedClosed: len(e.CitedClosed)}
		if e.ReplacedBy != nil {
			row.ReplacedBy = e.ReplacedBy.Ref
		}
		for _, c := range e.CitedOpen {
			row.CitedOpen = append(row.CitedOpen, c.Slug)
		}
		rows = append(rows, row)
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, rows)
	}
	if len(rows) == 0 {
		if len(index.Entries) == 0 {
			fmt.Fprintln(ctx.Stdout, "No decisions recorded yet. Record one with: issue-cli decide <slug> --text \"...\"")
		} else {
			fmt.Fprintln(ctx.Stdout, "No decisions match these filters.")
		}
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "== Decisions (%d) ==\n", len(rows))
	for _, r := range rows {
		state := r.State
		if state == tracker.DecisionStale {
			state = "replaced · stale"
		}
		fmt.Fprintf(ctx.Stdout, "%s · %s · %s · cited %d open/%d closed\n", r.Ref, r.Date.Format("2006-01-02"), state, len(r.CitedOpen), r.CitedClosed)
		fmt.Fprintf(ctx.Stdout, "    %s\n", truncateRunes(r.Text, 120))
		if r.ReplacedBy != "" {
			fmt.Fprintf(ctx.Stdout, "    replaced by %s\n", r.ReplacedBy)
		}
		if r.State == tracker.DecisionStale {
			fmt.Fprintf(ctx.Stdout, "    still cited by: %s\n", strings.Join(r.CitedOpen, ", "))
		}
	}
	return nil
}

func matchesDecisionState(e *tracker.DecisionEntry, state string) bool {
	switch state {
	case "":
		return true
	case tracker.DecisionReplaced:
		return e.ReplacedBy != nil
	default:
		return e.State == state
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// decisionsURL links the viewer's Decisions tab.
func decisionsURL(proj *tracker.Project) string {
	base := strings.TrimRight(viewerBaseURL(), "/")
	if proj != nil {
		return fmt.Sprintf("%s/p/%s/decisions", base, proj.Slug)
	}
	return base + "/decisions"
}

// printReplacedCitations warns when the issue builds on decisions that have
// since been replaced, so a re-dispatched agent knows the issue needs revision.
func printReplacedCitations(ctx *Context, issue *tracker.Issue, issues []*tracker.Issue) {
	if !strings.Contains(issue.BodyRaw, "#D-") {
		return
	}
	index := tracker.BuildDecisionIndex(issues, nil)
	var lines []string
	for _, e := range index.Cited(issue.Slug) {
		if e.ReplacedBy != nil && e.Slug != issue.Slug {
			lines = append(lines, fmt.Sprintf("  %s → replaced by %s: %s", e.Ref, e.ReplacedBy.Ref, truncateRunes(e.ReplacedBy.Text, 100)))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(ctx.Stdout, "== Cites replaced decisions (%d) ==\n", len(lines))
	fmt.Fprintln(ctx.Stdout, strings.Join(lines, "\n"))
	fmt.Fprintln(ctx.Stdout, "Revise this issue to follow the newer decisions and cite them instead.")
	fmt.Fprintln(ctx.Stdout)
}
