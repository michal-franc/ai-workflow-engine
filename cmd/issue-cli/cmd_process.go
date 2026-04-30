package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var processCommand = &Command{
	Name:      "process",
	ShortHelp: "Learn how this project works (run this first)",
	LongHelp: `Topic-based help.

Topics:
  (none)         Show high-level overview
  workflow       Status lifecycle
  transitions    Transition rules (--system <name> or <issue-slug>)
  format         Issue file format
  testing        Test plan convention
  docs           Documentation convention
  systems        Available systems
  schema         workflow.yaml schema
  changes        Release history (changes / changelog / versions)
  references     Issue references`,
	Run: runProcess,
}

func init() {
	registerCommand(processCommand)
}

func runProcess(ctx *Context, args []string) error {
	topic := ""
	var topicArgs []string
	if len(args) > 0 {
		topic = args[0]
		topicArgs = args[1:]
	}
	switch topic {
	case "", "all":
		fmt.Fprint(ctx.Stdout, processOverviewText)
		return nil
	case "workflow":
		return runProcessWorkflow(ctx, topicArgs)
	case "transitions":
		return runProcessTransitions(ctx, topicArgs)
	case "format":
		fmt.Fprint(ctx.Stdout, processFormatText)
		return nil
	case "testing":
		fmt.Fprint(ctx.Stdout, processTestingText)
		return nil
	case "docs":
		fmt.Fprint(ctx.Stdout, processDocsText)
		return nil
	case "systems":
		return runProcessSystems(ctx)
	case "schema":
		return runProcessSchema(ctx)
	case "changes", "changelog", "versions":
		return runProcessChanges(ctx)
	case "references":
		fmt.Fprint(ctx.Stdout, processReferencesText)
		return nil
	default:
		return fmt.Errorf("unknown topic: %s\n\nAvailable: workflow, format, transitions, schema, changes, testing, docs, systems, references", topic)
	}
}

func runProcessWorkflow(ctx *Context, _ []string) error {
	wf := ctx.Project.LoadWorkflow()
	statusOrder := wf.GetStatusOrder()
	statusDescs := wf.GetStatusDescriptions()
	fmt.Fprintln(ctx.Stdout, "== Status Lifecycle ==")
	for i, s := range statusOrder {
		desc := statusDescs[s]
		if i > 0 {
			fmt.Fprint(ctx.Stdout, "  → ")
		} else {
			fmt.Fprint(ctx.Stdout, "  ")
		}
		name := s
		if st := wf.GetStatus(s); st != nil && st.Optional {
			name = s + " (optional)"
		}
		if desc != "" {
			fmt.Fprintf(ctx.Stdout, "%-24s  %s\n", name, desc)
		} else {
			fmt.Fprintf(ctx.Stdout, "%s\n", name)
		}
	}
	return nil
}

func runProcessSystems(ctx *Context) error {
	proj := ctx.Project
	issues, _ := tracker.LoadIssues(proj.IssueDir)
	_, systems, _, _, _ := tracker.CollectFilterValues(issues)
	subdirSystems := tracker.CollectSubdirSystems(proj.IssueDir)
	seen := map[string]bool{}
	for _, s := range systems {
		seen[s] = true
	}
	for _, s := range subdirSystems {
		if !seen[s] {
			systems = append(systems, s)
			seen[s] = true
		}
	}
	sort.Strings(systems)
	fmt.Fprintln(ctx.Stdout, "== Available Systems ==")
	for _, s := range systems {
		fmt.Fprintf(ctx.Stdout, "  %s\n", s)
	}
	return nil
}

func runProcessTransitions(ctx *Context, args []string) error {
	proj := ctx.Project
	wf := proj.LoadWorkflow()

	var (
		system       string
		systemSource string
		issueRef     string
		hasIssueRef  bool
	)

	systemFlag := flagValue(args, "--system")
	workflowFlag := flagValue(args, "--workflow")
	if systemFlag == "" {
		systemFlag = workflowFlag
	}

	for _, a := range args {
		if a == "--system" || a == "--workflow" {
			break
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		issueRef = a
		hasIssueRef = true
		break
	}

	var issueSlug string
	switch {
	case hasIssueRef:
		issue, _, err := findIssueOrErr(proj, issueRef)
		if err != nil {
			return err
		}
		system = issue.System
		issueSlug = issue.Slug
		systemSource = fmt.Sprintf("issue %s", issue.Slug)
	case systemFlag != "":
		system = systemFlag
		systemSource = fmt.Sprintf("--system %s", systemFlag)
	}

	scoped := wf
	if strings.TrimSpace(system) != "" {
		scoped = wf.ForSystem(system)
	}

	header := "== Transition Rules =="
	switch {
	case hasIssueRef && system != "":
		header = fmt.Sprintf("== Transition Rules — system %q (%s) ==", system, systemSource)
	case hasIssueRef:
		header = fmt.Sprintf("== Transition Rules — issue %s (no system overlay; project default) ==", issueSlug)
	case system != "":
		header = fmt.Sprintf("== Transition Rules — system %q ==", system)
	}
	fmt.Fprintln(ctx.Stdout, header)
	fmt.Fprintln(ctx.Stdout)

	statusOrder := scoped.GetStatusOrder()
	if len(statusOrder) > 0 {
		first := statusOrder[0]
		fmt.Fprintf(ctx.Stdout, "  → %-26s  Initial state — title only\n", first)
	}

	rendered := map[string]bool{}
	for _, s := range statusOrder {
		for _, t := range scoped.Transitions {
			if t.To != s {
				continue
			}
			key := t.From + "→" + t.To
			if rendered[key] {
				continue
			}
			rendered[key] = true
			renderTransition(ctx.Stdout, t, scoped)
		}
	}
	for _, t := range scoped.Transitions {
		key := t.From + "→" + t.To
		if rendered[key] {
			continue
		}
		rendered[key] = true
		renderTransition(ctx.Stdout, t, scoped)
	}

	fmt.Fprintln(ctx.Stdout)
	fmt.Fprintln(ctx.Stdout, "Transitions are strict — you cannot skip required statuses.")

	var optional []string
	var globalStatuses []string
	for _, st := range scoped.Statuses {
		if st.Optional {
			optional = append(optional, st.Name)
		}
		if st.Global {
			globalStatuses = append(globalStatuses, st.Name)
		}
	}
	if len(optional) > 0 {
		fmt.Fprintf(ctx.Stdout, "\nOptional statuses (skippable on forward transitions): %s\n", strings.Join(optional, ", "))
	}
	if len(globalStatuses) > 0 {
		fmt.Fprintf(ctx.Stdout, "Global statuses (transitions from them to any status are allowed): %s\n", strings.Join(globalStatuses, ", "))
	}

	if system == "" && len(scoped.Systems) > 0 {
		var names []string
		for name := range scoped.Systems {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintln(ctx.Stdout)
		fmt.Fprintln(ctx.Stdout, "This is the project's default workflow. Per-system overlays are configured for:")
		fmt.Fprintf(ctx.Stdout, "  %s\n", strings.Join(names, ", "))
		fmt.Fprintln(ctx.Stdout, "Run 'issue-cli process transitions --system <name>' or 'issue-cli process transitions <issue-slug>' to see the rules for a specific workflow.")
	}
	return nil
}

func renderTransition(w io.Writer, t tracker.WorkflowTransition, wf *tracker.WorkflowConfig) {
	from := t.From
	if from == "" {
		from = "(any)"
	}
	if from == "*" {
		from = "* (any)"
	}
	label := fmt.Sprintf("%s → %s", from, t.To)
	descs := transitionActionDescriptions(t, wf)
	if len(descs) == 0 {
		fmt.Fprintf(w, "  %-28s  (no rules)\n", label)
		return
	}
	fmt.Fprintf(w, "  %-28s  %s\n", label, descs[0])
	for _, d := range descs[1:] {
		fmt.Fprintf(w, "  %-28s    %s\n", "", d)
	}
}

func transitionActionDescriptions(t tracker.WorkflowTransition, wf *tracker.WorkflowConfig) []string {
	var descs []string
	for _, action := range t.Actions {
		desc := tracker.DescribeAction(action, t.To)
		if strings.TrimSpace(desc) == "" {
			continue
		}
		descs = append(descs, desc)
	}
	for _, f := range t.Fields {
		target := strings.TrimSpace(f.Target)
		if target == "" {
			target = "frontmatter"
		}
		qualifier := "Prompts for"
		if f.Required {
			qualifier = "Required"
		}
		switch {
		case target == "frontmatter":
			descs = append(descs, fmt.Sprintf("%s frontmatter field %q (set via `--field %s=…` or `set-meta`)", qualifier, f.Name, f.Name))
		case strings.HasPrefix(target, "section:"):
			section := strings.TrimSpace(strings.TrimPrefix(target, "section:"))
			descs = append(descs, fmt.Sprintf("%s field %q appended to ## %s (set via `--field %s=…`)", qualifier, f.Name, section, f.Name))
		default:
			descs = append(descs, fmt.Sprintf("%s field %q (target: %s)", qualifier, f.Name, target))
		}
	}
	return descs
}

func runProcessSchema(ctx *Context) error {
	w := ctx.Stdout
	fmt.Fprintln(w, "== workflow.yaml schema ==")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Driven off Go struct tags — every field the parser honors appears here.")
	fmt.Fprintln(w, "Edit workflow.yaml at the project root (or demo/workflow.yaml for the demo).")

	for _, section := range tracker.WorkflowSchemaSections() {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "== %s  (struct: %s) ==\n", section.Path, section.Title)
		width := maxFieldWidth(section.Fields)
		for _, f := range section.Fields {
			name := f.Name
			if f.Optional {
				name += "?"
			}
			desc := f.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(w, "  %-*s  %-24s  %s\n", width, name, f.Type, desc)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Action types (transitions[].actions[].type) ==")
	wd := maxNamedWidth(tracker.WorkflowActionTypes)
	for _, a := range tracker.WorkflowActionTypes {
		fmt.Fprintf(w, "  %-*s  %s\n", wd, a.Name, a.Description)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Validation rules (actions[].rule when type=validate) ==")
	wd = maxNamedWidth(tracker.WorkflowValidationRules)
	for _, r := range tracker.WorkflowValidationRules {
		fmt.Fprintf(w, "  %-*s  %s\n", wd, r.Name, r.Description)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fields marked with ? are optional (yaml omitempty).")
	fmt.Fprintln(w, "Run 'issue-cli process changes' to see when features were added.")
	return nil
}

func runProcessChanges(ctx *Context) error {
	releases, err := fetchReleases(releasesRepo)
	if err == nil && len(releases) > 0 {
		printReleases(ctx.Stdout, releases)
		return nil
	}
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "warning: could not fetch releases from github (%v); using embedded CHANGELOG.md\n", err)
	}
	printEmbeddedChangelog(ctx.Stdout)
	return nil
}

func printReleases(w io.Writer, releases []githubRelease) {
	fmt.Fprintln(w, "== issue-cli / workflow release history ==")
	fmt.Fprintf(w, "(from https://github.com/%s/releases)\n\n", releasesRepo)
	printed := 0
	for _, r := range releases {
		if r.Draft {
			continue
		}
		title := r.Name
		if title == "" {
			title = r.TagName
		}
		date := r.PublishedAt
		if t, perr := time.Parse(time.RFC3339, date); perr == nil {
			date = t.Format("2006-01-02")
		}
		fmt.Fprintf(w, "## %s — %s\n\n", title, date)
		if body := strings.TrimSpace(r.Body); body != "" {
			fmt.Fprintln(w, body)
			fmt.Fprintln(w)
		}
		printed++
		if printed >= 20 {
			break
		}
	}
	fmt.Fprintln(w, "Run 'issue-cli process schema' to see the current workflow.yaml schema.")
}

func printEmbeddedChangelog(w io.Writer) {
	if strings.TrimSpace(changelogMD) == "" {
		fmt.Fprintln(w, "(no changelog embedded)")
		return
	}
	fmt.Fprintln(w, "== issue-cli / workflow release history (offline) ==")
	fmt.Fprintln(w)
	trimmed, omitted := trimChangelogToVersions(changelogMD, 20)
	fmt.Fprint(w, trimmed)
	if !strings.HasSuffix(trimmed, "\n") {
		fmt.Fprintln(w)
	}
	if omitted > 0 {
		fmt.Fprintf(w, "\n(%d older version entries omitted; see cmd/issue-cli/CHANGELOG.md for the full history)\n", omitted)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'issue-cli process schema' to see the current workflow.yaml schema.")
}

// trimChangelogToVersions keeps the preamble (everything before the first
// "## v" heading) plus the first `max` version sections.
func trimChangelogToVersions(md string, max int) (string, int) {
	lines := strings.Split(md, "\n")
	var preamble, kept []string
	total, seen := 0, 0
	seenFirst := false
	dropping := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## v") {
			total++
			if !seenFirst {
				seenFirst = true
			}
			if seen < max {
				seen++
				dropping = false
			} else {
				dropping = true
				continue
			}
		} else if dropping {
			continue
		}
		if !seenFirst {
			preamble = append(preamble, line)
		} else {
			kept = append(kept, line)
		}
	}
	omitted := total - seen
	if omitted < 0 {
		omitted = 0
	}
	return strings.Join(append(preamble, kept...), "\n"), omitted
}

func maxFieldWidth(fields []tracker.SchemaFieldDoc) int {
	w := 0
	for _, f := range fields {
		n := len(f.Name)
		if f.Optional {
			n++
		}
		if n > w {
			w = n
		}
	}
	return w
}

func maxNamedWidth(items []tracker.SchemaNamedDoc) int {
	w := 0
	for _, i := range items {
		if len(i.Name) > w {
			w = len(i.Name)
		}
	}
	return w
}

// flagValue is a small ad-hoc lookup retained only for runProcessTransitions
// because that command's positional/flag interleaving (issue-slug + --system)
// is incompatible with FlagSet's strict flag-then-positional ordering.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

const processOverviewText = `== AI-Native Project Viewer ==

You are working with a markdown-based issue tracker.
Issues are .md files in issues/<System>/ directories.

== Workflow ==
Every issue follows this lifecycle:
  idea → in design → backlog → in progress → testing → human-testing → documentation → shipping → done

== What each status means ==
  idea           Raw concept, just a title and rough description
  in design      Fleshing out requirements, approach, edge cases
  backlog        Designed and ready to implement
  in progress    Actively being worked on
  testing        Implementation done, verifying correctness
  human-testing  Manual verification by humans
  documentation  Updating docs to reflect the change
  shipping       Committing and pushing the changes
  done           Shipped, tested, documented

== Rules ==
  - Always use 'start' to begin work once the issue is approved for in progress in the viewer
  - Do NOT use 'claim' to begin work — it only sets assignee without starting
  - Never skip statuses — follow the order strictly
  - Always update docs before marking done
  - Reference other issues with #<slug> in the body
  - Use checkboxes [x] to track subtasks and acceptance criteria

== IMPORTANT: Command output ==
  - NEVER suppress stderr (no 2>/dev/null) — errors contain critical workflow guidance
  - NEVER use || true to ignore failures — non-zero exit codes mean something went wrong
  - ALWAYS read and act on the full output of every command — it contains next steps
  - If a command fails, fix the issue it describes, do not retry blindly

== Build/test failures from unrelated WIP files ==
  - If 'make test' or the build fails due to pre-existing untracked/WIP files unrelated to
    your issue, document the failure in a comment (issue-cli comment <slug> --text "..."),
    note that the cause is unrelated to your changes, and stop — do NOT modify, stash, or
    delete those files

== When you pick up an issue ==
  1. If the issue is in backlog, confirm it is approved for in-progress in the viewer
  2. issue-cli start <slug>          — starts the approved issue, claims it, and shows next steps
  3. If 'start' says approval is missing, stop and ask the human to approve it in the viewer
  4. Do the work, check off items
  5. Add ## Test Plan section with ### Automated and ### Manual
  6. issue-cli transition <slug> --to "testing"
  7. Log test results: issue-cli comment <slug> --text "tests: ..."
  8. issue-cli transition <slug> --to "documentation"
  9. Update docs: issue-cli comment <slug> --text "docs: ..."
  10. issue-cli transition <slug> --to "shipping"
  11. Commit and push the changes; check off the Shipping section
  12. issue-cli done <slug>

== Quick start ==
  issue-cli next --version 0.1    — find work for version 0.1
  issue-cli start <slug>          — pick up an issue at any status (claims + advances handoff states)
  issue-cli done <slug>           — finish when complete

Run 'issue-cli process <topic>' for details:
  workflow, format, transitions, schema, changes, testing, docs, systems, references
`

const processFormatText = `== Issue File Format ==

---
title: "Fix heat calculation overflow"
status: "idea"
system: "Combat"
version: "0.1"
assignee: "my-bot"
priority: "high"
labels:
  - bug
  - combat
---

Description in markdown. Supports checkboxes and #<slug> references.

== Required ==
  title — issue title (used to generate the URL slug)

== Optional ==
  status, system, version, assignee, priority, labels, created
`

const processTestingText = `== Test Plan Convention ==

Before transitioning from testing → documentation, the issue body
must contain a ## Test Plan section with two subsections:

## Test Plan

### Automated
List tests you wrote. Include file paths and what they verify.
- [x] Unit: path/to/test.cs — what it tests
- [x] Integration: path/to/test.go — what it tests
- [ ] E2E: not applicable

### Manual
Steps for a human to perform. You design these but cannot check them off.
- [ ] Load a mech with 5+ heat sinks, fire all weapons
- [ ] Verify heat bar stays in range

Rules:
  - ### Automated must have at least one entry
  - ### Manual must have at least one entry
  - You check off ### Automated as you write tests
  - Only humans check off ### Manual items
  - A test results comment is required:
    issue-cli comment <slug> --text "tests: 3 unit tests added, all passing"

To add a Test Plan section to an issue:
  issue-cli append <slug> --body "## Test Plan

### Automated
- description of test

### Manual
- step for human to verify"
`

const processDocsText = `== Documentation Convention ==

Before marking an issue as done, you must log what docs were updated:

  issue-cli comment <slug> --text "docs: updated combat/heat.md"
  issue-cli comment <slug> --text "docs: not needed, internal refactor"

The "docs:" prefix is required. This is an honor system — the CLI
validates that the comment exists but trusts its content.
`

const processReferencesText = `== Issue References ==

Use #<slug> in the issue body to link to other issues:
  See #combat/fix-heat-overflow for related work.
  This depends on #321 (looked up by filename).

References are resolved by:
  1. Full slug match (e.g. #combat/fix-heat-overflow)
  2. Title slug match (e.g. #fix-heat-overflow)
  3. Filename match (e.g. #321 if the file is 321.md)
`
