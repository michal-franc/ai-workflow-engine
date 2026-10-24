package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/checks"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var checkScriptCommand = &Command{
	Name:      "check-script",
	ShortHelp: "Run an issue's guided check scripts (scripts/checks/<name>.sh) from its worktree",
	LongHelp: `Guided check scripts walk the owner through an issue's manual checks that
aren't playtest sessions (CLI commands, API routes, tmux, scripts): each step
says what it checks, why, what to look at and what to expect, runs the
commands, checks what a machine can and asks the owner about the rest. The
owner's run from a terminal offers to tick the boxes that passed and to
comment the failures as "checks:".

A script is scripts/checks/<name>.sh on the issue's branch, with a
"# issue: <slug>" line in its first lines, sourcing the helper library
issue-cli ships ($CHECKS_LIB). Scripts stay on the branch: remove them in a
last commit before the merge.

Usage:
  issue-cli check-script <slug>               run the issue's scripts (the owner, from a terminal)
  issue-cli check-script <slug> <name>        run one of them
  issue-cli check-script <slug> --yes         machine run: machine checks only, nothing ticked
                                              (agents: pass this before handing over)
  issue-cli check-script <slug> --list        list the issue's scripts (exit 1 when none)
  issue-cli check-script <slug> --new <name>  write a starting script, one step per unticked
                                              Manual box, in the issue's worktree
  issue-cli check-script --lib                print the helper library and its API

The scripts run from the issue's worktree (or the project folder when the
workflow has no worktrees). A machine run is also what you get when stdin
isn't a terminal.`,
	Run: runCheckScript,
}

func init() {
	registerCommand(checkScriptCommand)
}

func runCheckScript(ctx *Context, args []string) error {
	fs := newFlagSet("check-script", ctx)
	libFlag := fs.Bool("lib", false, "print the helper library")
	yesFlag := fs.Bool("yes", false, "machine run: machine checks only, nothing ticked")
	listFlag := fs.Bool("list", false, "list the issue's scripts")
	newFlag := fs.String("new", "", "write a starting script with this name")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if *libFlag {
		if fs.NArg() > 0 {
			return unexpectedArgError(fs, fs.Arg(0))
		}
		_, err := ctx.Stdout.Write(checks.Lib)
		return err
	}
	if fs.NArg() == 0 {
		return &usageError{msg: "check-script requires <slug>\n\nExample:\n  issue-cli check-script <slug>\n  issue-cli check-script --lib"}
	}
	if fs.NArg() > 2 {
		return unexpectedArgError(fs, fs.Arg(2))
	}
	slug := fs.Arg(0)
	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	root, rootErr := checkScriptRoot(ctx.Project, issue)

	if *newFlag != "" {
		if rootErr != nil {
			return rootErr
		}
		return newCheckScript(ctx, issue, root, *newFlag)
	}

	var found []checks.Script
	if rootErr == nil {
		found = checks.Find([]string{root}, issue.Slug)
	}
	if fs.NArg() == 2 {
		found = filterScripts(found, fs.Arg(1))
	}
	if *listFlag {
		return listCheckScripts(ctx, issue, root, found)
	}
	if rootErr != nil {
		return rootErr
	}
	if len(found) == 0 {
		return noCheckScripts(issue, root, fs.Arg(1))
	}
	return runCheckScripts(ctx, issue, found, *yesFlag)
}

// checkScriptRoot is where the issue's scripts live and run: its worktree,
// or the project folder when the workflow has no worktrees.
func checkScriptRoot(proj *tracker.Project, issue *tracker.Issue) (string, error) {
	wf := proj.LoadWorkflowForIssue(issue)
	path, branch, enabled := tracker.ResolveWorktree(projectRoot(proj), issue.Slug, wf)
	if !enabled {
		return projectRoot(proj), nil
	}
	if !pathExists(path) {
		return "", fmt.Errorf("no worktree for %s at %s\n\nCheck scripts live on the issue's branch (%s) and run from its worktree.\nCreate it with: git worktree add %s %s", issue.Slug, path, branch, path, branch)
	}
	return path, nil
}

func filterScripts(found []checks.Script, name string) []checks.Script {
	name = strings.TrimSuffix(name, ".sh")
	for _, s := range found {
		if s.Name == name {
			return []checks.Script{s}
		}
	}
	return nil
}

func noCheckScripts(issue *tracker.Issue, root, name string) error {
	what := "no check scripts"
	if name != "" {
		what = "no check script " + name
	}
	return &exitCodeError{Code: 1, Msg: fmt.Sprintf("%s for %s under %s\n\nA script is scripts/checks/<name>.sh with a \"# issue: %s\" line near the top.\nStart one: issue-cli check-script %s --new <name>", what, issue.Slug, filepath.Join(root, checks.Dir), issue.Slug, issue.Slug)}
}

type checkScriptEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	OwnerRun bool   `json:"owner_run"`
}

func listCheckScripts(ctx *Context, issue *tracker.Issue, root string, found []checks.Script) error {
	if ctx.JSONOutput {
		entries := []checkScriptEntry{}
		for _, s := range found {
			entries = append(entries, checkScriptEntry{Name: s.Name, Path: s.Path, OwnerRun: checks.OwnerRun([]string{s.Root}, s.Name)})
		}
		if err := writeJSON(ctx.Stdout, entries); err != nil {
			return err
		}
		if len(found) == 0 {
			return &exitCodeError{Code: 1}
		}
		return nil
	}
	if len(found) == 0 {
		if root == "" {
			return &exitCodeError{Code: 1, Msg: "no check scripts for " + issue.Slug + " (no worktree)"}
		}
		return noCheckScripts(issue, root, "")
	}
	for _, s := range found {
		run := "no owner run yet"
		if checks.OwnerRun([]string{s.Root}, s.Name) {
			run = "owner has run it"
		}
		fmt.Fprintf(ctx.Stdout, "%s  %s  (%s)\n", s.Name, s.Path, run)
	}
	return nil
}

func runCheckScripts(ctx *Context, issue *tracker.Issue, found []checks.Script, yes bool) error {
	lib, err := checks.LibPath()
	if err != nil {
		return fmt.Errorf("writing the check library: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		self = "issue-cli"
	}
	project := ctx.ProjectSlug
	if ctx.Project != nil && ctx.Project.Slug != "" {
		project = ctx.Project.Slug
	}
	var failed []string
	for i, s := range found {
		if i > 0 {
			fmt.Fprintln(ctx.Stdout)
		}
		cmd := exec.Command("bash", s.Path)
		cmd.Dir = s.Root
		cmd.Stdin, cmd.Stdout, cmd.Stderr = ctx.Stdin, ctx.Stdout, ctx.Stderr
		cmd.Env = append(os.Environ(),
			"CHECKS_LIB="+lib,
			"CHECKS_SLUG="+issue.Slug,
			"CHECKS_PROJECT="+project,
			"CHECKS_ROOT="+s.Root,
			"CHECKS_NAME="+s.Name,
			"CHECKS_SCRIPT="+s.Path,
			"CHECKS_ISSUE_CLI="+self,
		)
		if ctx.Config.Path != "" {
			cmd.Env = append(cmd.Env, "ISSUE_VIEWER_CONFIG="+ctx.Config.Path)
		}
		if yes {
			cmd.Env = append(cmd.Env, "CHECKS_YES=1")
		}
		err := cmd.Run()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr) && exitErr.ExitCode() == 130:
			return &exitCodeError{Code: 130, Msg: "stopped in " + s.Name}
		case errors.As(err, &exitErr):
			failed = append(failed, s.Name)
		default:
			return fmt.Errorf("running %s: %w", s.Path, err)
		}
	}
	if len(failed) > 0 {
		return &exitCodeError{Code: 1, Msg: "check script failed: " + strings.Join(failed, ", ")}
	}
	return nil
}

func newCheckScript(ctx *Context, issue *tracker.Issue, root, name string) error {
	name = strings.TrimSuffix(name, ".sh")
	if !checks.ValidName(name) {
		return &usageError{msg: fmt.Sprintf("script name %q: use lowercase letters, digits and dashes, e.g. issue-cli check-script %s --new lead-tab", name, issue.Slug)}
	}
	path := filepath.Join(root, checks.Dir, name+".sh")
	if pathExists(path) {
		return fmt.Errorf("%s already exists; edit it, or pick another name", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(checks.Skeleton(issue.Slug, name, manualBoxes(issue.BodyRaw))), 0o755); err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "✓ Wrote %s\n", path)
	fmt.Fprintln(ctx.Stdout, "  Fill in each step (the helpers: issue-cli check-script --lib), then:")
	fmt.Fprintf(ctx.Stdout, "  issue-cli check-script %s --yes    machine run, before you hand over\n", issue.Slug)
	fmt.Fprintf(ctx.Stdout, "  The owner runs: issue-cli check-script %s (or ▶ Run checks in the viewer)\n", issue.Slug)
	fmt.Fprintln(ctx.Stdout, "  Commit it on the issue branch; remove it in a last commit before the merge.")
	return nil
}

// manualBoxes are the unticked boxes under the Test Plan's ### Manual
// heading: one starting step each.
func manualBoxes(body string) []checks.Box {
	lines := strings.Split(body, "\n")
	var boxes []checks.Box
	for _, item := range tracker.ListCheckboxes(body) {
		if item.Checked || item.ID == "" || !strings.EqualFold(item.Section, "Test Plan") {
			continue
		}
		if subheading(lines, item.Line) != "manual" {
			continue
		}
		boxes = append(boxes, checks.Box{ID: item.ID, Text: item.Text})
	}
	return boxes
}

// subheading is the nearest "### " heading above line, lowercased.
func subheading(lines []string, line int) string {
	for i := line - 1; i >= 0 && i < len(lines); i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "### ") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "### ")))
		}
		if strings.HasPrefix(t, "## ") {
			return ""
		}
	}
	return ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// usesCheckScripts reports whether the workflow has the guided-check-scripts
// feature: its testing or human-testing prompt names check-script.
func usesCheckScripts(wf *tracker.WorkflowConfig) bool {
	return strings.Contains(wf.StatusPrompt("testing"), "check-script") || strings.Contains(wf.StatusPrompt("human-testing"), "check-script")
}

// printCheckScriptHint says where the issue's guided check scripts stand, at
// testing and human-testing, when the workflow uses them: the scripts and
// who runs what, or how to start one while Manual boxes are open.
func printCheckScriptHint(w io.Writer, proj *tracker.Project, wf *tracker.WorkflowConfig, issue *tracker.Issue) {
	status := strings.ToLower(issue.Status)
	if (status != "testing" && status != "human-testing") || !usesCheckScripts(wf) {
		return
	}
	root, err := checkScriptRoot(proj, issue)
	if err != nil {
		return
	}
	found := checks.Find([]string{root}, issue.Slug)
	if len(found) == 0 {
		if len(manualBoxes(issue.BodyRaw)) == 0 {
			return
		}
		fmt.Fprintln(w, "== Guided checks ==")
		fmt.Fprintln(w, "  None yet. Manual checks that aren't playtests come as a guided check script:")
		fmt.Fprintf(w, "  issue-cli check-script %s --new <name>    (helpers: issue-cli check-script --lib)\n\n", issue.Slug)
		return
	}
	names := make([]string, len(found))
	for i, s := range found {
		names[i] = s.Name
	}
	fmt.Fprintln(w, "== Guided checks ==")
	fmt.Fprintf(w, "  Scripts: %s\n", strings.Join(names, ", "))
	if status == "testing" {
		fmt.Fprintf(w, "  Before handing over: issue-cli check-script %s --yes    (machine checks only; never tick the owner's boxes)\n", issue.Slug)
	}
	fmt.Fprintf(w, "  The owner runs: issue-cli check-script %s    (▶ Run checks in the viewer; ticks the boxes that pass)\n\n", issue.Slug)
}

// checkScriptLibOnly reports whether args are `check-script --lib`, which
// needs no project.
func checkScriptLibOnly(args []string) bool {
	return len(args) == 1 && (args[0] == "--lib" || args[0] == "-lib")
}
