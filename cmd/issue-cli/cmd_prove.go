package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/proof"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var proveCommand = &Command{
	Name:      "prove",
	ShortHelp: "Link an acceptance criterion to its proof; list each criterion's proof and its check",
	LongHelp: `Link each acceptance criterion (AC1, AC2, … from issue-cli checklist) to
the proof that it works. A proof is a link to something that exists, never a
copy, and the engine checks every link each time it reads it.

Most links need no prove call: declare them where you write the proof.

  playtest step   proves: [AC2]                 on the step in the session YAML
  test            Proves: AC2 TestProveList     trailer on the commit that adds or changes it
  the change      Proves: AC2                   trailer on its commit
                  (several: "Proves: AC1, AC3 TestA TestB"; only the work
                  branch's own commits count, never commits merged from main)

prove is the fallback, and the only way to record a measured value:

  issue-cli prove <slug> <AC-id> --test <TestName>      repeatable, like every link flag
  issue-cli prove <slug> <AC-id> --step <TP3>           a playtest session step id
  issue-cli prove <slug> <AC-id> --commit <sha>
  issue-cli prove <slug> <AC-id> --value "690 vs 380 (1.82×)"
  issue-cli prove <slug> <AC-id> --shot <path>          only without a playtest runner,
                                                        for a criterion ending in (visual)
  issue-cli prove <slug> <AC-id> --clear                removes this criterion's prove links
  issue-cli prove <slug> [--list] [--json]              every criterion, its links and checks

Checks:
  test    a test file on the branch has the name, and a tests: comment is
          newer than the branch's last non-merge commit
  step    the step's latest playtest result passed
  commit  reachable from the work branch or its merge
  value   none: shown as "reported"
  shot    the file exists

A criterion is proven when one of its links passes. prove, prove --list and
the transition that checks proof (criteria_proven) tick the proven boxes;
issue-cli check refuses to tick a criterion by hand while the gate covers
the issue.`,
	Run: runProve,
}

func init() {
	registerCommand(proveCommand)
}

type proveOutput struct {
	proof.Report
	Proven int      `json:"proven"`
	Total  int      `json:"total"`
	Ticked []string `json:"ticked"`
}

func runProve(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "prove")
	if err != nil {
		return err
	}
	fs := newFlagSet("prove", ctx)
	var links []tracker.ProofLink
	for _, kind := range []string{tracker.ProofTest, tracker.ProofStep, tracker.ProofCommit, tracker.ProofValue, tracker.ProofShot} {
		kind := kind
		fs.Func(kind, "repeatable: link the criterion to this "+kind, func(v string) error {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("--%s needs a value", kind)
			}
			links = append(links, tracker.ProofLink{Kind: kind, Ref: strings.TrimSpace(v)})
			return nil
		})
	}
	clearFlag := fs.Bool("clear", false, "remove this criterion's prove links")
	fs.Bool("list", false, "list every criterion with its links and checks (the default without a criterion)")
	if err := parseFlagsArgs(ctx, fs, rest); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return unexpectedArgError(fs, fs.Arg(1))
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	criteria := tracker.Criteria(issue.BodyRaw)
	if len(criteria) == 0 {
		return fmt.Errorf("%s has no checkboxes under ## %s — write each criterion as a checkbox first:\n  issue-cli replace %s --section %q --body \"- [ ] ...\"", issue.Slug, tracker.CriteriaSection, issue.Slug, tracker.CriteriaSection)
	}

	if fs.NArg() == 1 {
		id := fs.Arg(0)
		it, st := tracker.ResolveCheckboxRef(criteria, id)
		if st != tracker.RefFound {
			return fmt.Errorf("%s is not an acceptance criterion of %s; criteria: %s", id, issue.Slug, criteriaIDs(criteria))
		}
		switch {
		case *clearFlag && len(links) > 0:
			return fmt.Errorf("--clear removes links; don't combine it with link flags")
		case !*clearFlag && len(links) == 0:
			return fmt.Errorf("say where the proof is\n\nExamples:\n  issue-cli prove %s %s --test TestName\n  issue-cli prove %s %s --step TP3\n  issue-cli prove %s %s --value \"690 vs 380\"", issue.Slug, it.ID, issue.Slug, it.ID, issue.Slug, it.ID)
		}
		if err := saveProofLinks(ctx, issue, it, links, *clearFlag); err != nil {
			return err
		}
	} else if *clearFlag || len(links) > 0 {
		return fmt.Errorf("name the criterion: issue-cli prove %s <AC-id> ... (criteria: %s)", issue.Slug, criteriaIDs(criteria))
	}

	return printProof(ctx, issue)
}

func criteriaIDs(items []tracker.CheckboxItem) string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ", ")
}

// saveProofLinks adds links to criterion it (or clears its links) in the
// data sidecar, refusing links that can't be right.
func saveProofLinks(ctx *Context, issue *tracker.Issue, it tracker.CheckboxItem, links []tracker.ProofLink, clear bool) error {
	root := projectRoot(ctx.Project)
	for i, l := range links {
		switch l.Kind {
		case tracker.ProofStep:
			if !tracker.IsCheckboxRefShaped(l.Ref) {
				return fmt.Errorf("--step %q is not a step id: use the session step's id (a checkbox id such as TP3)", l.Ref)
			}
		case tracker.ProofCommit:
			full, err := proof.ResolveCommit(root, l.Ref)
			if err != nil {
				return fmt.Errorf("--commit: %w", err)
			}
			links[i].Ref = full
		case tracker.ProofShot:
			if wf := ctx.Project.LoadWorkflow(); wf.HasFeature("playtest-sessions") {
				return fmt.Errorf("--shot is refused: this project has a playtest runner, so its screenshots come from playtest runs. Add `proves: [%s]` to the step that shows it", it.ID)
			}
			if !tracker.IsVisualCriterion(it.Text) {
				return fmt.Errorf("--shot is refused: %s is not marked visual. Only a criterion ending in \"(visual)\" takes a screenshot:\n  issue-cli check-edit %s %s --text \"%s (visual)\"", it.ID, issue.Slug, it.ID, it.Text)
			}
			if !proof.InsideCheckout(root, issue.Slug, l.Ref) {
				return fmt.Errorf("--shot %s: the file must be in the issue's worktree or the project checkout", l.Ref)
			}
		}
		links[i].At = ctx.now().UTC()
		links[i].Criterion = it.Text
	}

	store, err := tracker.LoadData(issue.FilePath)
	if err != nil {
		return err
	}
	if store.Proofs == nil {
		store.Proofs = map[string][]tracker.ProofLink{}
	}
	id := strings.ToUpper(it.ID)
	if clear {
		delete(store.Proofs, id)
	} else {
		existing := store.Proofs[id]
		for _, l := range links {
			if l.Kind == tracker.ProofShot {
				// One hand-picked shot per criterion: a new one replaces it.
				kept := existing[:0]
				for _, e := range existing {
					if e.Kind != tracker.ProofShot {
						kept = append(kept, e)
					}
				}
				existing = kept
			}
			dup := false
			for _, e := range existing {
				if e.Kind == l.Kind && e.Ref == l.Ref {
					dup = true
					break
				}
			}
			if !dup {
				existing = append(existing, l)
			}
		}
		store.Proofs[id] = existing
	}
	if len(store.Proofs) == 0 {
		store.Proofs = nil
	}
	return tracker.SaveData(issue.FilePath, store)
}

func printProof(ctx *Context, issue *tracker.Issue) error {
	comments, _ := tracker.LoadComments(issue.FilePath)
	report := proof.Collect(projectRoot(ctx.Project), issue, comments)
	ticked, err := proof.TickProven(issue.FilePath, report)
	if err != nil {
		return fmt.Errorf("ticking proven criteria: %w", err)
	}
	for i := range report.Criteria {
		for _, id := range ticked {
			if report.Criteria[i].ID == id {
				report.Criteria[i].Checked = true
			}
		}
	}
	out := proveOutput{Report: report, Total: len(report.Criteria), Ticked: ticked}
	out.Proven = out.Total - len(report.Unproven())
	if out.Ticked == nil {
		out.Ticked = []string{}
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, out)
	}
	writeProofReport(ctx.Stdout, out)
	wf := ctx.Project.LoadWorkflowForIssue(issue)
	if _, ok := wf.ProofGateSince(); ok && !wf.ProofRequired(issue) {
		fmt.Fprintln(ctx.Stdout, "The proof gate doesn't cover this issue (created before the gate's date): links are shown, not required.")
	}
	fmt.Fprintf(ctx.Stdout, "Evidence (with the playtest shots): %s?tab=evidence\n", issueURL(ctx.Project, issue.Slug))
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}

func writeProofReport(w io.Writer, out proveOutput) {
	fmt.Fprintf(w, "== Proof: %s ==\n", out.Slug)
	if out.RefName != "" {
		fmt.Fprintf(w, "Branch: %s\n", out.RefName)
	}
	for _, m := range out.Missing {
		fmt.Fprintf(w, "Not read: %s\n", m)
	}
	for _, c := range out.Criteria {
		box, state := "[ ]", "UNPROVEN"
		if c.Checked {
			box = "[x]"
		}
		if c.Proven() {
			state = "proven"
		}
		fmt.Fprintf(w, "%s %s %s  %s\n", c.ID, box, state, c.Text)
		for _, l := range c.Links {
			mark := "✗"
			if l.Check.OK {
				mark = "✓"
			}
			fmt.Fprintf(w, "    %s %s — %s\n", mark, proof.Describe(l.Link), l.Check.State)
			if !l.Check.OK && l.Check.Fix != "" {
				fmt.Fprintf(w, "      → %s\n", l.Check.Fix)
			}
		}
	}
	if unproven := out.Unproven(); len(unproven) > 0 {
		fmt.Fprintf(w, "Prove each UNPROVEN criterion with any of (shown for %s):\n", unproven[0].ID)
		for _, way := range proof.Ways(out.Slug, unproven[0].ID) {
			fmt.Fprintf(w, "  - %s\n", way)
		}
	}
	for _, s := range out.Strays {
		fmt.Fprintf(w, "Unknown criterion %s: %s — the issue has no such box; fix the trailer or proves: line\n", s.ID, proof.Describe(s.Link))
	}
	for _, id := range out.Ticked {
		fmt.Fprintf(w, "✓ Ticked %s (proven)\n", id)
	}
	fmt.Fprintf(w, "%d/%d criteria proven\n", out.Proven, out.Total)
}
