package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

const scanUsage = `Usage:
  issue-cli workflow scan --dry-run                         # report detected features and the record it would write
  issue-cli workflow scan                                   # write the features: record into workflow.yaml
  issue-cli workflow scan --installed types-of-work,scoring # record features you installed (sets the catalog version)
  issue-cli workflow scan --declined worktree               # record features the human declined
  issue-cli workflow scan --forget worktree                 # drop ids from the record (re-opens a declined feature)
  issue-cli --json workflow scan --dry-run                  # machine-readable report`

// runWorkflowScan detects catalog features in workflow.yaml and writes the
// features: record. It only ever rewrites the features: block.
func runWorkflowScan(ctx *Context, args []string) error {
	fs := newFlagSet("workflow scan", ctx)
	dryRun := fs.Bool("dry-run", false, "print the report and the record without writing workflow.yaml")
	installed := fs.String("installed", "", "comma-separated feature ids to record as installed at the catalog version")
	declined := fs.String("declined", "", "comma-separated feature ids to record as declined")
	forget := fs.String("forget", "", "comma-separated feature ids to drop from the record (re-opens a declined feature)")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("workflow scan takes no arguments, got %q\n\n%s", strings.Join(fs.Args(), " "), scanUsage)
	}
	path := projectWorkflowFile(ctx.Project)
	if path == "" {
		return fmt.Errorf("no workflow.yaml found: this project runs on the built-in default workflow\n\nCreate one from a bundled template:\n  issue-cli workflow init --template <%s>", strings.Join(workflowupgrade.Templates(), "|"))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	res, err := workflowupgrade.Scan(path, raw, workflowupgrade.ScanOptions{
		Installed: splitIDList(*installed),
		Declined:  splitIDList(*declined),
		Forget:    splitIDList(*forget),
	})
	if err != nil {
		return err
	}
	res.DryRun = *dryRun
	if !res.DryRun && (len(res.Changes) > 0 || !res.HadRecord) {
		out, err := workflowupgrade.WriteRecord(raw, res.Record)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		res.Written = true
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, res)
	}
	printScanResult(ctx.Stdout, res)
	return nil
}

func printScanResult(w io.Writer, res *workflowupgrade.ScanResult) {
	fmt.Fprintln(w, "== Workflow scan ==")
	fmt.Fprintf(w, "File:   %s\n", res.WorkflowFile)
	if res.HadRecord {
		fmt.Fprintln(w, "Record: features: present")
	} else {
		fmt.Fprintln(w, "Record: none yet")
	}
	switch {
	case res.DryRun:
		fmt.Fprintln(w, "Mode:   dry run, nothing written")
	case res.Written:
		fmt.Fprintln(w, "Mode:   wrote the features: record (nothing else in the file changed)")
	default:
		fmt.Fprintln(w, "Mode:   record already up to date, nothing written")
	}

	detected := 0
	for _, f := range res.Features {
		if f.Detected {
			detected++
		}
	}
	fmt.Fprintf(w, "\nDetected %d of %d catalog features (✓ detected, + recorded only, ✗ declined, · absent):\n", detected, len(res.Features))
	category := ""
	for _, f := range res.Features {
		if f.Category != category {
			category = f.Category
			fmt.Fprintf(w, "  %s\n", category)
		}
		mark, detail := "·", ""
		switch {
		case f.Detected:
			mark, detail = "✓", f.Evidence
			if f.Matches > 1 {
				detail += fmt.Sprintf(" (+%d more)", f.Matches-1)
			}
		case f.Recorded == "declined":
			mark, detail = "✗", "declined"
		case f.Recorded != "":
			mark, detail = "+", "recorded, not detected"
		}
		fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf("    %s %-31s %s", mark, f.ID, detail), " "))
	}

	if len(res.Changes) > 0 {
		fmt.Fprintln(w, "\nChanges to the record:")
		for _, c := range res.Changes {
			fmt.Fprintf(w, "  %s\n", c)
		}
	} else {
		fmt.Fprintln(w, "\nChanges to the record: none")
	}
	for _, n := range res.Notes {
		fmt.Fprintf(w, "Note: %s\n", n)
	}
	fmt.Fprintln(w, "\nRecord after this scan:")
	for _, line := range strings.Split(strings.TrimRight(res.Block, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
	if res.DryRun {
		fmt.Fprintln(w, "\nRun without --dry-run to write it. Only the features: block is changed.")
	}
}

// splitIDList parses a comma/space separated --installed/--declined value.
func splitIDList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
