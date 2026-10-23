package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var balanceCommand = &Command{
	Name:      "balance",
	ShortHelp: "Add a typed balance note to an issue, or list balance notes (--list)",
	LongHelp: `Write a typed balance note: a comment on the source issue naming the metric
it moves and its state. The viewer's Balance tab (/p/<project>/balance) groups
these notes by metric. A later note on the same issue, metric and change
replaces the earlier one on the board (the history stays).

  --metric      metric id, one token (e.g. full-holds, lanes.blocks.time_fighting)
  --state       open | kept | reverted | needs-pieces | not-this-branch
  --hypothesis  why the metric is off
  --change      the knob and direction (e.g. "ScoutAccuracy 1.0 → 0.7")
  --evidence    probe numbers before and after
  --verdict     what was decided and why
  --pass        joint pass number (an issue with balance_pass: <n>)

--list prints the current notes instead (filters: --metric, --state, --pass).

The comment prefix comes from workbench.balance.comment_prefix in
workflow.yaml (default "balance:"). Without a workbench.balance block the
note is still saved, but the Balance tab is hidden.

Examples:
  issue-cli balance ai/scouting-raids-use-perfect-intel --metric dominant-plays --state kept \
    --hypothesis "Perfect scouting makes the reader club pick the counter too often." \
    --change "ScoutAccuracy 1.0 → 0.7" --evidence "read-edge 72% → 63%" \
    --verdict "Inside band. Kept in pass 2." --pass 2
  issue-cli balance --list --state open --json`,
	Run: runBalance,
}

func init() {
	registerCommand(balanceCommand)
}

// balancePrefix is the project's balance comment prefix.
func balancePrefix(ctx *Context) string {
	if ctx.Project != nil {
		if p := strings.TrimSpace(ctx.Project.LoadWorkflow().Workbench.Balance.CommentPrefix); p != "" {
			return p
		}
	}
	return tracker.DefaultBalancePrefix
}

func balanceEnabled(ctx *Context) bool {
	return ctx.Project != nil && ctx.Project.LoadWorkflow().Workbench.HasBalance()
}

type balanceOutput struct {
	Note     tracker.BalanceNote  `json:"note"`
	Replaces *tracker.BalanceNote `json:"replaces,omitempty"`
	Board    string               `json:"board_url"`
	Enabled  bool                 `json:"board_enabled"`
}

func runBalance(ctx *Context, args []string) error {
	var slug string
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		slug, rest = args[0], args[1:]
	}
	fs := newFlagSet("balance", ctx)
	listFlag := fs.Bool("list", false, "list current balance notes instead of adding one")
	metric := fs.String("metric", "", "metric id the note is about")
	state := fs.String("state", "", "open | kept | reverted | needs-pieces | not-this-branch")
	hypothesis := fs.String("hypothesis", "", "why the metric is off")
	change := fs.String("change", "", "knob and direction")
	evidence := fs.String("evidence", "", "probe numbers")
	verdict := fs.String("verdict", "", "what was decided and why")
	pass := fs.Int("pass", 0, "joint pass number")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}
	if *listFlag {
		if slug != "" {
			return fmt.Errorf("--list lists the whole project; drop the slug %q", slug)
		}
		return listBalance(ctx, *metric, *state, *pass)
	}
	if slug == "" {
		return fmt.Errorf("balance requires an issue slug (or --list)\n\nExample:\n  issue-cli balance <slug> --metric full-holds --state open --hypothesis \"...\" --change \"Knob ↑\"")
	}

	prefix := balancePrefix(ctx)
	text, err := tracker.RenderBalanceNote(prefix, tracker.NewBalanceNote{
		Metric: *metric, State: *state, Hypothesis: *hypothesis, Change: *change,
		Evidence: *evidence, Verdict: *verdict, Pass: *pass,
	})
	if err != nil {
		return fmt.Errorf("%w\n\nExample:\n  issue-cli balance %s --metric full-holds --state open --hypothesis \"...\" --change \"Knob ↑\"", err, slug)
	}
	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	note, _, _ := tracker.ParseBalanceComment(prefix, text)
	note.Slug = issue.Slug

	// The note it replaces, for the "Updates" line.
	var replaces *tracker.BalanceNote
	if existing, err := tracker.LoadComments(issue.FilePath); err == nil {
		for _, c := range existing {
			prev, typed, _ := tracker.ParseBalanceComment(prefix, c.Text)
			if !typed {
				continue
			}
			prev.Slug, prev.CommentID, prev.Date = issue.Slug, c.ID, c.Date
			if prev.Key() == note.Key() && (replaces == nil || prev.CommentID > replaces.CommentID) {
				p := prev
				replaces = &p
			}
		}
	}

	if err := tracker.AddComment(issue.FilePath, 0, text, "cli"); err != nil {
		return fmt.Errorf("failed to add balance note: %w", err)
	}
	if comments, err := tracker.LoadComments(issue.FilePath); err == nil && len(comments) > 0 {
		last := comments[len(comments)-1]
		note.CommentID, note.Date = last.ID, last.Date
	}

	out := balanceOutput{Note: note, Replaces: replaces, Board: boardURL(ctx.Project), Enabled: balanceEnabled(ctx)}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, out)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Balance note on %s: %s · %s\n", issue.Slug, note.Metric, note.State)
	if replaces != nil {
		fmt.Fprintf(ctx.Stdout, "Updates the note from %s (was %s).\n", tracker.CommentDay(replaces.Date), replaces.State)
	}
	fmt.Fprintf(ctx.Stdout, "Board: %s\n", out.Board)
	if !out.Enabled {
		fmt.Fprintln(ctx.Stdout, "Note: the Balance tab is hidden until workflow.yaml has a workbench.balance block.")
	}
	return nil
}

type balanceListRow struct {
	tracker.BalanceNote
	Earlier int `json:"earlier_notes"`
}

type balanceListOutput struct {
	Notes    []balanceListRow           `json:"notes"`
	Unsorted []tracker.LooseBalanceNote `json:"unsorted"`
	Planned  int                        `json:"planned_pass,omitempty"`
}

func listBalance(ctx *Context, metric, state string, pass int) error {
	if state != "" {
		if s := tracker.NormalizeBalanceState(state); s == "" {
			return fmt.Errorf("--state %q: want one of open, kept, reverted, needs-pieces, not-this-branch", state)
		} else {
			state = s
		}
	}
	issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	board := tracker.BuildBalanceBoard(issues, loadIssueComments, balancePrefix(ctx), tracker.ClosedIssueFunc(ctx.Project))

	out := balanceListOutput{Notes: []balanceListRow{}, Unsorted: board.Unsorted}
	if out.Unsorted == nil {
		out.Unsorted = []tracker.LooseBalanceNote{}
	}
	if board.Planned != nil {
		out.Planned = board.Planned.Number
	}
	for _, g := range board.Groups {
		for _, n := range g.Notes {
			if metric != "" && !strings.EqualFold(n.Metric, metric) {
				continue
			}
			if state != "" && n.State != state {
				continue
			}
			if pass > 0 && board.PlannedPassFor(n) != pass {
				continue
			}
			row := balanceListRow{BalanceNote: *n, Earlier: len(n.History)}
			row.History = nil
			out.Notes = append(out.Notes, row)
		}
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, out)
	}
	if len(out.Notes) == 0 {
		fmt.Fprintln(ctx.Stdout, "No typed balance notes match. Add one with: issue-cli balance <slug> --metric <id> --state open ...")
	} else {
		fmt.Fprintf(ctx.Stdout, "== Balance notes (%d) ==\n", len(out.Notes))
		current := ""
		for _, n := range out.Notes {
			if !strings.EqualFold(n.Metric, current) {
				current = n.Metric
				fmt.Fprintf(ctx.Stdout, "%s\n", n.Metric)
			}
			passLabel := ""
			if p := board.PlannedPassFor(&n.BalanceNote); p > 0 {
				passLabel = fmt.Sprintf(" · pass %d", p)
			}
			fmt.Fprintf(ctx.Stdout, "  [%s] %s · %s%s\n", n.State, n.Slug, tracker.CommentDay(n.Date), passLabel)
			for _, f := range []struct{ k, v string }{{"change", n.Change}, {"evidence", n.Evidence}, {"verdict", n.Verdict}} {
				if f.v != "" {
					fmt.Fprintf(ctx.Stdout, "      %s: %s\n", f.k, truncateRunes(f.v, 110))
				}
			}
		}
	}
	if len(out.Unsorted) > 0 {
		fmt.Fprintf(ctx.Stdout, "Unsorted (free-text) balance comments: %d — see %s\n", len(out.Unsorted), boardURL(ctx.Project))
	}
	return nil
}

func loadIssueComments(issue *tracker.Issue) []tracker.Comment {
	comments, _ := tracker.LoadComments(issue.FilePath)
	return comments
}

// boardURL links the viewer's Balance tab.
func boardURL(proj *tracker.Project) string {
	base := strings.TrimRight(viewerBaseURL(), "/")
	if proj != nil {
		return fmt.Sprintf("%s/p/%s/balance", base, proj.Slug)
	}
	return base + "/balance"
}
