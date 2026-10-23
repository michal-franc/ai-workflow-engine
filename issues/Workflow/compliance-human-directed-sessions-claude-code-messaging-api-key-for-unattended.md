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

## 1. Lead → worker messaging: use Claude Code's built-in messaging

- Replace the tmux paste in `issue-cli lead message` (`pasteIntoSession` = `agentrun.Paste`,
  `cmd/issue-cli/cmd_lead.go:323`) with Claude Code's cross-session messaging (ListAgents / SendMessage).
  `agentrun.LaunchCommand` already starts claude with `-n agent-<slug>` / `-n lead-<project>`, so those names are
  already the addresses.
- Keep the tmux paste only as the fallback for codex sessions, which have no `-n` name.
- Leave the approval nudge and Re-send prompt (`handlers_dispatch.go`, `agentrun.Paste`) as they are. A human click in
  the UI starts both.

## 2. Lead prompt: recall decisions, never invent them

Add to the lead's prompt (`tracker.LeadPromptSection` / the lead brief):

- When a worker asks for clarification, answer only from recorded decisions (issue body, comments, design docs,
  approvals), and cite the source.
- If no recorded decision covers the question, don't decide. Escalate to Michal (a `question:` comment or a message)
  and tell the worker to wait.
- Dispatch workers only when Michal says so in the lead's chat (e.g. "dispatch agents for this epic"). Never
  dispatch automatically when planning finishes, and never pick the next issue on its own.
- Tick gates only with Michal's quoted approval (the existing `approve --quote`).

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
- [ ] Nothing new starts or prompts an interactive session without a human action
- [ ] (If built) the headless mode runs `claude -p` with the key from `api_key_env`, the key passed via process env only
- [ ] `docs/agent-dispatch.md` documents the interactive / unattended boundary

## Open questions

- How does `issue-cli` (a Go binary outside Claude Code) send a Claude Code SendMessage? Find the supported entry
  point (CLI subcommand, local socket, SDK) before building section 1. If there is none, the lead sends it with its
  own SendMessage tool and `lead message` only prints the address.
- Is the headless mode in this PR, or only the rule and the docs, with the mode built when the first unattended job
  (Toadie) needs it?
