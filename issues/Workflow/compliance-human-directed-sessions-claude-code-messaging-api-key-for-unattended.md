---
title: "Compliance: human-directed sessions, Claude Code messaging, API key for unattended runs"
status: "backlog"
system: "Workflow"
priority: "high"
type: "feature"
version: "0.59"
created: "2026-10-23"
---

Goal: every agent session stays human-directed. Agent-to-agent messages go through Claude Code's own features,
and anything unattended runs on an API key, not the subscription login.

Owner, 2026-10-23: added for a new PR, to start on 2026-10-24.

Owner, 2026-10-23: "This is just a precaution to control the agents a bit, especially leads." Nothing is broken
today. This puts limits on what agents can do on their own, and most of all on the lead. The lead rules in section 2
are the core of this issue.

## 1. Lead → worker messaging: use Claude Code's built-in messaging

- Replace the tmux paste in `issue-cli lead message` (`pasteIntoSession` = `agentrun.Paste`,
  `cmd/issue-cli/cmd_lead.go:323`) with Claude Code's cross-session messaging (ListAgents / SendMessage).
  `agentrun.LaunchCommand` already starts claude with `-n agent-<slug>` / `-n lead-<project>`, so those names are
  already the addresses.
- Keep the tmux paste only as the fallback for codex sessions, which have no `-n` name.
- Leave the approval nudge and Re-send prompt (`handlers_dispatch.go`, `agentrun.Paste`) as they are. A human click in
  the UI starts both.

State today (2026-10-23): most of this is already in place. The brief's role section already says to use
ListAgents / SendMessage and calls `lead message` "the tmux-paste fallback, e.g. for Codex"
(`internal/tracker/lead_brief.go:64`). Left to do: the default brief still says "Talk to a worker: issue-cli lead
message <slug>" (`lead_brief.go:22`), and `lead message` could refuse claude targets.

## 2. Lead prompt: recall decisions, never invent them

Add to the lead's prompt (`tracker.LeadPromptSection` / the lead brief):

- When a worker asks for clarification, answer only from recorded decisions (issue body, comments, design docs,
  approvals), and cite the source.
- If no recorded decision covers the question, don't decide. Escalate to Michal (a `question:` comment or a message)
  and tell the worker to wait.
- Dispatch workers only when Michal says so in the lead's chat (e.g. "dispatch agents for this epic"). Never
  dispatch automatically when planning finishes, and never pick the next issue on its own.
- Tick gates only with Michal's quoted approval (the existing `approve --quote`).
- Fix the default brief's opening line, which says "You … dispatch workers" without the condition.

### Lead dispatch, on the human's word

Owner, 2026-10-23: "we would make it so that lead can dispatch workers but only when humans tells it".

Today the lead can't dispatch at all: `lead spawn` starts only the lead, and workers start only from the board
(`POST /issue/<slug>/dispatch`). Add a dispatch command built like `approve --quote`:

```
issue-cli lead dispatch <slug> [<slug>...] --quote "<the human's words>" [--how "lead chat"]
```

- `--quote` is required. With no quote, nothing is dispatched.
- Only the lead can run it (`whoAmI` role check, like `lead message`). Workers get an error pointing at a
  `question:` comment.
- Each dispatched issue gets a `dispatch: "<quote>" (<how>, <date>)` comment, the same shape as `approval:`, so the
  trail shows whose words started the worker.
- It goes through the same dispatch path as the board button (same worktree, prompt, session name), not a second
  launcher.
- One quote can cover several slugs ("dispatch agents for this epic" → the children Michal named or agreed to). The
  lead lists the slugs in its chat before running the command.
- The brief: "Dispatch only with `issue-cli lead dispatch … --quote` and the human's words from this chat. Never after
  planning on your own, never the next issue on your own."

As with `approve --quote`, the quote is the lead's report of what Michal said. The tool records it but can't check it;
the trail makes it reviewable.

## 3. No unattended work on the subscription login

- Add no cron, timers or auto-dispatch that start or prompt interactive sessions without a human action.
- If unattended runs are needed (scheduled jobs, overnight batches), add a separate headless mode: run `claude -p`
  (or the Agent SDK) with `ANTHROPIC_API_KEY` injected through per-project config (e.g. an `api_key_env` field in
  `projects.yaml`). That bills the linked Console org's API credits, not the Max plan.
- This headless mode is also where the Toadie automations should run, on those API credits.

Notes from the code (2026-10-23):

- `projects.yaml` has no free-form env field yet. Sessions get a fixed set of exports through
  `tmux send-keys "export K=V"` (`internal/agentrun/agentrun.go:208`). `api_key_env` is a new `tracker.Project` field.
- `api_key_env` should name the environment variable that holds the key (read by the server/CLI), never hold the key
  itself, so `projects.yaml` stays free of secrets.
- Headless runs should pass the key via `exec.Cmd.Env`, not `send-keys`, so it never lands in tmux scrollback or shell
  history. See `cli/agent-process-safety-prompt-off-the-command-line-clean-stop-env-allowlist` (secrets via stdin/env,
  never argv).

## 4. Docs

- Document the boundary in `docs/agent-dispatch.md`: interactive sessions = human-triggered starts and approvals;
  unattended = headless + API key.

## Acceptance criteria (draft)

- [ ] `issue-cli lead message` to a claude session sends via Claude Code messaging, not a tmux paste
- [ ] `issue-cli lead message` to a codex session still uses the tmux paste fallback
- [ ] Approval nudge and Re-send prompt are unchanged
- [ ] The lead prompt carries the four rules in section 2, and a test pins them
- [ ] The default brief no longer contradicts itself on messaging (line 22) or dispatch (opening line)
- [ ] `issue-cli lead dispatch <slug> --quote "..."` starts the worker through the board's dispatch path
- [ ] `lead dispatch` without `--quote` fails and starts nothing
- [ ] `lead dispatch` from a worker fails and starts nothing
- [ ] Each dispatch leaves a `dispatch: "<quote>"` comment on the issue
- [ ] Nothing new starts or prompts an interactive session without a human action
- [ ] (If built) the headless mode runs `claude -p` with the key from `api_key_env`, the key passed via process env only
- [ ] `docs/agent-dispatch.md` documents the interactive / unattended boundary

## Open questions

- `lead dispatch`: call the running viewer's `POST /issue/<slug>/dispatch` (one code path, needs the server up), or
  share the launcher through `internal/agentrun` (works offline, two callers)?

- How does `issue-cli` (a Go binary outside Claude Code) send a Claude Code SendMessage? Find the supported entry
  point (CLI subcommand, local socket, SDK) before building section 1. If there is none, the lead sends it with its
  own SendMessage tool and `lead message` only prints the address.
- Is the headless mode in this PR, or only the rule and the docs, with the mode built when the first unattended job
  (Toadie) needs it?
