package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const upgradeLongHelp = `Show which newer workflow.yaml features this project could adopt.

Read-only: it never edits workflow.yaml. It compares the project's
` + "`template: <name>@<version>`" + ` stamp with the template bundled in this
issue-cli and lists each missing feature as a numbered proposal: what it is,
why it helps, where it goes, a YAML snippet, and the question to ask the human.
Then it prints the upgrade steps (back up to workflow.oldN.yaml, apply the
picked snippets, bump the stamp, validate).

Agents: only upgrade when the operator asks. When start or process workflow
print an upgrade hint, tell the operator and carry on with your task.

Usage:
  issue-cli workflow upgrade                      # proposals and steps
  issue-cli upgrade                               # same (alias)
  issue-cli workflow upgrade --template review    # compare an unstamped file with this template (default development)
  issue-cli --json workflow upgrade               # machine-readable plan

Stamp values:
  template: development@v0.34.0   compared with the bundled development template
  template: custom                hand-built workflow: no hints, no proposals
  (missing)                       unstamped: every feature the file lacks is proposed`

var upgradeCommand = &Command{
	Name:      "upgrade",
	ShortHelp: "Propose newer workflow.yaml template features (alias for `workflow upgrade`)",
	LongHelp:  upgradeLongHelp,
	Run:       runWorkflowUpgrade,
}

func init() {
	registerCommand(upgradeCommand)
}

func runWorkflowUpgrade(ctx *Context, args []string) error {
	fs := newFlagSet("workflow upgrade", ctx)
	templateFlag := fs.String("template", "", "template to compare an unstamped workflow.yaml with (default development)")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	path := projectWorkflowFile(ctx.Project)
	if path == "" {
		return fmt.Errorf("no workflow.yaml found: this project runs on the built-in default workflow\n\nCreate one from a bundled template:\n  issue-cli workflow init --template <%s>", strings.Join(availableWorkflowTemplates(), "|"))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	plan, err := buildUpgradePlan(path, raw, *templateFlag)
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, plan)
	}
	printUpgradePlan(ctx.Stdout, plan)
	return nil
}

func printUpgradePlan(w io.Writer, plan *upgradePlan) {
	fmt.Fprintln(w, "== Workflow upgrade ==")
	fmt.Fprintf(w, "File:     %s\n", plan.WorkflowFile)
	current := plan.Current
	if current == "" {
		current = "(no stamp)"
	}
	if plan.Latest != "" {
		fmt.Fprintf(w, "Template: %s   current: %s   bundled: %s\n", plan.Template, current, plan.Latest)
	} else {
		fmt.Fprintf(w, "Template: %s\n", plan.Template)
	}
	fmt.Fprintf(w, "Status:   %s\n", plan.Status)
	fmt.Fprintln(w)
	fmt.Fprintln(w, plan.Message)

	if len(plan.Proposals) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "== Proposals ==")
		for _, p := range plan.Proposals {
			origin := "opt-in"
			if p.InTemplate {
				origin = "in the bundled template"
			}
			fmt.Fprintf(w, "[%d] %s — %s (%s, %s)\n", p.Number, p.ID, p.Title, p.Version, origin)
			fmt.Fprintf(w, "  What:  %s\n", indentContinuation(p.What, "         "))
			fmt.Fprintf(w, "  Why:   %s\n", indentContinuation(p.Why, "         "))
			fmt.Fprintf(w, "  Where: %s\n", p.Where)
			fmt.Fprintf(w, "  Ask:   %s\n", p.Question)
			fmt.Fprintln(w, "  Snippet:")
			for _, line := range strings.Split(p.Snippet, "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
			fmt.Fprintln(w)
		}
	}
	if len(plan.Present) > 0 {
		fmt.Fprintf(w, "Already present (not proposed): %s\n", strings.Join(plan.Present, ", "))
	}
	if len(plan.Steps) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "== Steps ==")
		for i, s := range plan.Steps {
			fmt.Fprintf(w, "  %d. %s\n", i+1, s)
		}
	}
}

// indentContinuation joins a multi-line text so continuation lines line up
// under the first one.
func indentContinuation(s, indent string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n"+indent)
}
