package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// ciReporter tells the system about a run of the default tier (slow tiers
// are asked for by someone who watches them):
//
//   - green → red, or a red run failing a different command: a ci: comment
//     on every issue merged since the last green run; with on_red:
//     file_issue the streak's bugfix issue is filed (first red) or
//     commented.
//   - still red, same command: only the streak's bugfix issue hears about it.
//   - red → green: the bugfix issue gets a comment.
//
// CI runs on a timer, so it never pastes into or prompts a session: the lead
// reads the state (issue-cli ci status, the Status page) when the human asks
// or before a push. The Status page, status.md and the ci-red card markers
// need nothing here: the viewer reads them from .ci/state.json.
func ciReporter(ctx *Context, proj *tracker.Project, wf *tracker.WorkflowConfig, root string) func(prev *ci.State, run *ci.Run, next *ci.State) {
	return func(prev *ci.State, run *ci.Run, next *ci.State) {
		if run.Tier != tracker.CIDefaultTier {
			return
		}
		prevState, prevFailed := "", ""
		if prev != nil {
			prevState, prevFailed = prev.State, prev.Failed
		}
		issues, _ := tracker.LoadIssues(proj.IssueDir)
		branch := run.Branch
		switch {
		case run.State == ci.Red && (prevState != ci.Red || prevFailed != run.Failed):
			if wf.CI.OnRedMode() == tracker.CIOnRedFileIssue {
				if next.RedIssue == "" {
					if slug, err := fileRedIssue(ctx, proj, issues, run, next); err != nil {
						fmt.Fprintf(ctx.Stderr, "ci: could not file the bugfix issue: %v\n", err)
					} else {
						next.RedIssue = slug
						if err := ci.WriteState(root, next); err != nil {
							fmt.Fprintf(ctx.Stderr, "ci: %v\n", err)
						}
						issues, _ = tracker.LoadIssues(proj.IssueDir)
					}
				} else {
					ciComment(ctx, issues, next.RedIssue, fmt.Sprintf("ci: %s still red at %s, now failing `%s`%s. Log: %s", branch, ci.Short(run.Sha), run.Failed, failuresNote(run.Failures), run.Log))
				}
			}
			text := tracker.InjectProject(proj, redComment(run, next))
			for _, slug := range next.Blamed {
				ciComment(ctx, issues, slug, text)
			}
		case run.State == ci.Red:
			if next.RedIssue != "" {
				ciComment(ctx, issues, next.RedIssue, fmt.Sprintf("ci: %s still red at %s (`%s`). Log: %s", branch, ci.Short(run.Sha), run.Failed, run.Log))
			}
		case run.State == ci.Green && prevState == ci.Red:
			if prev.RedIssue != "" {
				ciComment(ctx, issues, prev.RedIssue, fmt.Sprintf("ci: %s green again at %s. Close this issue once its fix has shipped.", branch, ci.Short(run.Sha)))
			}
		}
	}
}

func failuresNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return " (" + ci.FailureList(names) + ")"
}

// redComment is the ci: comment on an issue merged since the last green run.
func redComment(run *ci.Run, st *ci.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ci: local CI red on %s at %s: `%s` failed%s. ", run.Branch, ci.Short(run.Sha), run.Failed, failuresNote(run.Failures))
	var landed []string
	for _, m := range st.Merged {
		landed = append(landed, mergedLabel(m))
	}
	fmt.Fprintf(&b, "Landed since the last green run (%s): %s", ci.Short(st.GreenSha), strings.Join(landed, ", "))
	fmt.Fprintf(&b, ". Log: %s. The lead decides whether to revert, fix forward or dispatch a fix", run.Log)
	if st.RedIssue != "" {
		fmt.Fprintf(&b, "; tracked in %s", st.RedIssue)
	}
	b.WriteString(". `issue-cli ci status`")
	return b.String()
}

func ciComment(ctx *Context, issues []*tracker.Issue, slug, text string) {
	issue := tracker.FindIssue(issues, slug)
	if issue == nil {
		return
	}
	if err := tracker.AddComment(issue.FilePath, 0, text, "ci"); err != nil {
		fmt.Fprintf(ctx.Stderr, "ci: comment on %s: %v\n", slug, err)
	}
}

// fileRedIssue creates the red streak's bugfix issue (on_red: file_issue)
// through `create`, so it gets the workflow's template, and puts the details
// in a ci: comment. It returns the new slug.
func fileRedIssue(ctx *Context, proj *tracker.Project, issues []*tracker.Issue, run *ci.Run, st *ci.State) (string, error) {
	cmdName := run.Failed
	if len(cmdName) > 60 {
		cmdName = cmdName[:60]
	}
	title := fmt.Sprintf("Fix red CI on %s at %s: %s", run.Branch, ci.Short(run.Sha), cmdName)
	system := ""
	for _, slug := range st.Blamed {
		if issue := tracker.FindIssue(issues, slug); issue != nil && issue.System != "" {
			system = issue.System
			break
		}
	}
	args := []string{"--title", title}
	if system != "" {
		args = append(args, "--system", system)
	}
	if wf := proj.LoadWorkflow(); wf.HasTypes() {
		if _, ok := wf.Types["bugfix"]; ok {
			args = append(args, "--type", "bugfix")
		}
	}
	var out bytes.Buffer
	sub := *ctx
	sub.Stdout, sub.JSONOutput = &out, false
	if err := runCreate(&sub, args); err != nil {
		return "", err
	}
	slug := tracker.Slugify(title)
	if system != "" {
		slug = strings.ToLower(system) + "/" + slug
	}
	created, _ := tracker.LoadIssues(proj.IssueDir)
	issue := tracker.FindIssue(created, slug)
	if issue == nil {
		return "", fmt.Errorf("created %q but cannot find it as %s", title, slug)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ci: filed by local CI. %s went red at %s: `%s` failed%s. ", run.Branch, ci.Short(run.Sha), run.Failed, failuresNote(run.Failures))
	if st.GreenSha != "" {
		fmt.Fprintf(&b, "Range: %s..%s. ", ci.Short(st.GreenSha), ci.Short(run.Sha))
	}
	if len(st.Merged) > 0 {
		var landed []string
		for _, m := range st.Merged {
			landed = append(landed, mergedLabel(m))
		}
		fmt.Fprintf(&b, "Landed since green: %s. ", strings.Join(landed, ", "))
	}
	fmt.Fprintf(&b, "Log: %s. Log tail:\n```\n%s\n```", run.Log, strings.Join(run.LogTail, "\n"))
	if err := tracker.AddComment(issue.FilePath, 0, b.String(), "ci"); err != nil {
		fmt.Fprintf(os.Stderr, "ci: %v\n", err)
	}
	return issue.Slug, nil
}
