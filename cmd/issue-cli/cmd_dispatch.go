package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// exitWaitingOnGate is dispatch's exit code when the issue only waits on the
// owner's approval and nothing was started.
const exitWaitingOnGate = 3

var dispatchCommand = &Command{
	Name:      "dispatch",
	ShortHelp: "Start (or re-attach to) the issue's worker, exactly like ▶ on the board",
	LongHelp: `Start the issue's worker: the same prompt, worktree and tmux session as ▶
on the board (both build it with agentrun.DispatchSpec). Runs here, through
tmux: the viewer does not need to be running. A live worker is re-attached,
never started twice.

When everything the issue's next transition needs is done except the
owner's approval, dispatch refuses and names the gate: a new worker would
only stop there. --force dispatches anyway.

A human starts the work: --quote is required and carries the human's words
that ask for this worker (from the lead's chat, or your own). Without it
nothing starts. Each dispatch (and --reprompt) leaves a comment on the issue:
  dispatch: "<words>" (<how>, <date>)
One slug per call; nothing expands an epic into its children.

Workers can't dispatch: post a question: comment for your lead instead.

Flags:
  --quote "<words>"      the human's words that ask for this worker (required)
  --how "<how>"          how the words reached you (default: "lead chat" for
                         the lead, "owner's shell" otherwise)
  --agent claude|codex   agent to run (default claude)
  --force                dispatch even though the issue waits on a gate
  --reprompt             re-send the current briefing into the live worker
                         instead of starting one (the Re-send prompt button)
  --json                 print the same object POST .../dispatch returns

Exit codes:
  0  started, re-attached, or re-prompted
  1  the launch failed, or --reprompt found no live worker
  2  usage error (also: no --quote)
  3  waiting on the owner's approval; nothing was started

Examples:
  issue-cli dispatch api/my-issue --quote "start the api one"
  issue-cli dispatch api/my-issue --agent codex --quote "try codex on it"
  issue-cli dispatch api/my-issue --reprompt --quote "re-send its prompt"`,
	Run: runDispatch,
}

func init() {
	registerCommand(dispatchCommand)
}

func runDispatch(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "dispatch")
	if err != nil {
		return err
	}
	fs := newFlagSet("dispatch", ctx)
	agentFlag := fs.String("agent", "claude", "agent to run: claude or codex")
	force := fs.Bool("force", false, "dispatch even though the issue waits on the owner's approval")
	reprompt := fs.Bool("reprompt", false, "re-send the current briefing into the live worker")
	quoteFlag := fs.String("quote", "", "the human's words that ask for this worker (required)")
	howFlag := fs.String("how", "", "how the words reached you")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}
	quote := strings.TrimSpace(normalizeEscapedText(*quoteFlag))
	if quote == "" {
		return &usageError{msg: fmt.Sprintf("dispatch needs --quote: the human's words that ask for this worker. Nothing was started.\n\nA human starts the work; never dispatch on your own (after planning, or the next issue).\n\nExample:\n  issue-cli dispatch %s --quote \"<the human's words>\"", slug)}
	}
	if *agentFlag != "claude" && *agentFlag != "codex" {
		return &usageError{msg: fmt.Sprintf("--agent must be claude or codex, got %q", *agentFlag)}
	}
	if *reprompt && (*force || flagSet(fs, "agent")) {
		return &usageError{msg: "--reprompt re-sends the briefing into the live worker; --agent and --force don't apply to it"}
	}

	issue, issues, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	proj := ctx.Project
	me := whoAmI(proj, issues)
	if me.Role == tracker.RoleWorker {
		return fmt.Errorf("workers can't dispatch (you are %s, detected from %s).\n\nPost a question: comment on your issue instead; your lead relays it:\n  %s", valueOr(me.Name, "a worker"), me.Signal,
			tracker.InjectProject(proj, fmt.Sprintf("issue-cli comment %s \"question: ...\"", valueOr(me.Slug, "<slug>"))))
	}

	how := strings.TrimSpace(*howFlag)
	if how == "" {
		how = "owner's shell"
		if me.Role == tracker.RoleProjectLead {
			how = "lead chat"
		}
	}
	comment := tracker.DispatchComment(quote, how, ctx.now().Format("2006-01-02"))
	if me.Name != "" {
		comment += " — recorded by " + me.Name
	}

	if *reprompt {
		resp := repromptWorker(proj, issue, projectRoot(proj))
		return reportDispatch(ctx, issue, resp, comment)
	}

	spec, refused := agentrun.DispatchSpec(proj, issue, *agentFlag, projectRoot(proj), *force)
	if refused != nil {
		if ctx.JSONOutput {
			if err := writeJSON(ctx.Stdout, refused); err != nil {
				return err
			}
			return &exitCodeError{Code: exitWaitingOnGate}
		}
		return &exitCodeError{Code: exitWaitingOnGate, Msg: fmt.Sprintf(
			"✗ Not dispatched: %s waits on the owner's approval for %q.\nEverything else %s → %s needs is done, so a new worker would only stop at the gate.\n\nA human approves at: %s\nDispatch anyway:     %s",
			issue.Slug, refused.Gate, issue.Status, refused.Gate,
			approvalURL(proj, issue.Slug, refused.Gate),
			tracker.InjectProject(proj, fmt.Sprintf("issue-cli dispatch %s --force --quote %q", issue.Slug, quote)))}
	}
	resp := startAgent(proj, spec)
	if resp.AttachCmd == "" && resp.Status != "error" {
		resp.AttachCmd, resp.SwitchCmd = agentrun.AttachCmd(proj, spec.Session), agentrun.SwitchCmd(proj, spec.Session)
	}
	return reportDispatch(ctx, issue, resp, comment)
}

// repromptWorker re-sends the issue's briefing into its live worker. A var
// so tests need no tmux.
var repromptWorker = agentrun.Reprompt

// reportDispatch prints a dispatch or reprompt response: JSON, or the
// outcome line, the steps and how to reach the session. A worker started or
// re-prompted gets comment (the dispatch: line) on its issue. A failed launch
// or a reprompt with no live worker exits 1.
func reportDispatch(ctx *Context, issue *tracker.Issue, resp agentrun.Response, comment string) error {
	failed := resp.Status == "error" || resp.Status == "no-session"
	commented := false
	if resp.Status == "dispatched" || resp.Status == "reprompted" {
		if err := tracker.AddComment(issue.FilePath, 0, comment, "cli"); err != nil {
			fmt.Fprintf(ctx.Stderr, "warning: writing the dispatch comment: %v\n", err)
		} else {
			commented = true
		}
	}
	if ctx.JSONOutput {
		if err := writeJSON(ctx.Stdout, resp); err != nil {
			return err
		}
		if failed {
			return &exitCodeError{Code: 1}
		}
		return nil
	}
	switch resp.Status {
	case "dispatched":
		fmt.Fprintf(ctx.Stdout, "✓ Dispatched %s for %s\n", resp.Session, issue.Slug)
	case "reattached":
		fmt.Fprintf(ctx.Stdout, "✓ Already live: %s (re-attached, prompt not re-sent)\n", resp.Session)
	case "reprompted":
		fmt.Fprintf(ctx.Stdout, "✓ Re-sent the briefing to %s\n", resp.Session)
	}
	if commented {
		fmt.Fprintf(ctx.Stdout, "  comment: %s\n", comment)
	}
	printSteps(ctx, resp.Steps)
	if resp.Status == "no-session" {
		return fmt.Errorf("no live worker for %s; nothing was re-sent (see the step above)", issue.Slug)
	}
	if failed {
		return fmt.Errorf("dispatching %s failed (see the steps above)", issue.Slug)
	}
	if resp.AttachCmd != "" {
		fmt.Fprintf(ctx.Stdout, "\nInside tmux:  %s\nFrom a shell: %s\n", resp.SwitchCmd, resp.AttachCmd)
	}
	return nil
}

// flagSet reports whether the flag called name was given on the command line.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
