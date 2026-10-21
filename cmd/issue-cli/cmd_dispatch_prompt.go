package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var dispatchPromptCommand = &Command{
	Name:      "dispatch-prompt",
	ShortHelp: "Print the prompt an agent dispatch would send (read-only)",
	LongHelp: `Print the exact prompt the issue viewer's agent dispatch would send for the
issue right now: same template, status guidance, --project injection, and
worktree section. Read-only: creates no worktree, tmux session, or file, and
needs no running server. Output is the raw prompt with nothing around it.

The prompt reflects the issue's current status, so it can differ from the one
a session was dispatched with earlier (that one is saved under
<workdir>/.agent-logs/<session>/dispatch-prompt.txt).

Flags:
  --json   print {"prompt","slug","status","worktree","branch","session"}
           (same shape as GET /p/<project>/issue/<slug>/dispatch-prompt?format=json)

Uses:
  - review the briefing before dispatching
  - start an agent some other way (a subagent, another terminal)
  - recover a session that came up without its prompt. Prefer the issue
    viewer's Re-send prompt button (POST .../dispatch/reprompt); by hand:
      issue-cli dispatch-prompt <slug> | tmux load-buffer -b reprompt -
      tmux paste-buffer -d -p -b reprompt -t <session> && tmux send-keys -t <session> Enter

Examples:
  issue-cli dispatch-prompt api/my-issue
  issue-cli dispatch-prompt api/my-issue --json`,
	Run: runDispatchPrompt,
}

func init() {
	registerCommand(dispatchPromptCommand)
}

func runDispatchPrompt(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "dispatch-prompt")
	if err != nil {
		return err
	}
	fs := newFlagSet("dispatch-prompt", ctx)
	jsonOut := fs.Bool("json", false, "output as JSON")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	// TelemetryRoot is the project's workdir, else the issues dir's parent —
	// where the server resolves worktrees from in the usual layout.
	dp := tracker.BuildDispatchPrompt(ctx.Project, issue, ctx.Project.LoadWorkflowForIssue(issue), ctx.Project.TelemetryRoot())
	if ctx.JSONOutput || *jsonOut {
		return writeJSON(ctx.Stdout, dp)
	}
	_, err = fmt.Fprint(ctx.Stdout, dp.Prompt)
	return err
}
