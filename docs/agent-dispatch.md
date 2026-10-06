---
title: "Agent Dispatch"
order: 6
---

## Overview

The board and detail views can dispatch issues to AI agents (Claude or Codex) via tmux sessions. Dispatch creates a session, opens a terminal, and pastes a generated prompt.

## Requirements

- **tmux** is mandatory. The whole dispatch lifecycle is tmux: creating the session, injecting the environment, logging via `pipe-pane`, and delivering the prompt via `send-keys`.
- **The agent CLI**: `claude` or `codex` on the `PATH` of the server.
- **git**, when the workflow sets `worktree: true`.
- **A terminal command**, or `terminal: "none"` to attach yourself (see [Terminal Configuration](#terminal-configuration)). The default, i3 + alacritty, is only needed if you leave `terminal` unset.

> **Tip.** Not sure which `terminal` value fits your setup? Ask your coding agent: *"Look at my OS, window manager and installed terminals, then set the `terminal:` field in `projects.yaml` to something that works here."* It can detect i3, GNOME, macOS or WSL, check what's on `$PATH`, and pick the right command, or fall back to `"none"`.

## How to Dispatch

- **Board view** — hover a card, click the play button, pick Claude or Codex
- **Detail view** — two buttons in the sidebar (Claude / Codex)
- **API** — `POST /p/<project>/issue/<slug>/dispatch` with `{"agent": "claude"}` or `{"agent": "codex"}`

## Re-dispatching to a live session

Dispatching to an issue whose tmux session is still alive does **not** error out and does **not** re-prompt the agent. The handler probes `tmux has-session -t <name>` first; if the session exists it skips `new-session`, all logging/env setup, and the prompt-paste, and just opens a terminal attached to the existing session.

The response in this case is:

```json
{
  "status": "reattached",
  "session": "agent-<slug>",
  "steps": [
    {"name": "Existing session — attaching", "status": "reattached"},
    {"name": "Open terminal", "status": "ok"}
  ]
}
```

The dispatch modal renders this with a yellow warning banner. Kill the existing session manually (`tmux kill-session -t agent-<slug>`) if you want a fresh dispatch with a re-pasted prompt.

The same pattern applies to **Edit in nvim**: re-triggering it while the previous edit session is still alive returns `{"status": "reattached", "reattached": true, ...}` and opens a terminal attached to the existing nvim instance. The reattach request does NOT register a new save-on-exit handler — the original request's goroutine still owns the sync-back when nvim exits.

## Terminal Configuration

The terminal that opens for the agent session is configurable via the `terminal` field in `projects.yaml`:

```yaml
- name: "My Project"
  terminal: "alacritty -e tmux attach -t {{session}}"
```

The handler substitutes `{{session}}` with the tmux session name and runs the command via `sh -c`.

### Examples

| Platform                   | Config                                                                       |
|:---------------------------|:-----------------------------------------------------------------------------|
| Linux + alacritty          | `alacritty -e tmux attach -t {{session}}`                                    |
| Linux + i3 + alacritty     | `i3-msg exec "alacritty -e tmux attach -t {{session}}"`                      |
| macOS + iTerm2             | `osascript -e 'tell app "iTerm2" to create window with default profile command "tmux attach -t {{session}}"'` |
| macOS + Terminal.app       | `osascript -e 'tell app "Terminal" to do script "tmux attach -t {{session}}"'` |
| Headless (attach manually) | `none`                                                                       |

If `terminal` is unset, defaults to i3 + alacritty. Set to `none` to only create the tmux session (the response includes the `attach_cmd`).

## Shared tmux Session

By default every dispatch gets its own tmux session (`agent-<slug>`) and its own terminal window. Set `tmux_session` on a project to run all its agents as **windows inside one shared session** instead:

```yaml
- name: "My Project"
  tmux_session: "work"
  terminal: "alacritty -e tmux attach -t {{session}}"
```

On dispatch:

- If the `work` session doesn't exist, it is created with the agent as its first window (`agent-<slug>`).
- If it exists, a new `agent-<slug>` window is added to it and selected.
- A terminal (via `terminal`, where `{{session}}` becomes the shared session name) is opened **only when no client is attached** to the shared session. If you already have it open, the new window just appears there.
- Re-dispatching an issue whose window is still alive reattaches (selects the window) instead of re-prompting.

"Edit in nvim" follows the same rule: it opens as an `agent-<slug>-edit` window, and only that window is closed when the edit finishes. Agent windows are listed as `work:agent-<slug>` and receive approval notifications like per-agent sessions. With `terminal: none`, `attach_cmd` is `tmux attach -t work \; select-window -t agent-<slug>`.

## Per-issue Worktrees

Set `worktree: true` at the top of `workflow.yaml` to give every dispatched issue its own git worktree:

```yaml
worktree: true
worktree_setup: "make"            # optional: runs once inside a new worktree
worktree_sparse_exclude: ["issues/"]   # the default; [] keeps everything
```

- The worktree is `<workdir>/.worktrees/<slug>` on a new branch `work/<slug>`, created from the current `HEAD` of the project's workdir. An existing worktree is reused, so re-dispatching after a restart picks up where the agent left off.
- `issues/` is left out with sparse-checkout, so issues are only ever edited on the main checkout. The dispatch prompt tells the agent it is in a worktree and that its `issue-cli` commands already point at the main checkout.
- `worktree_setup` runs on creation only. If it fails, the dispatch stops and reports the error.
- Removing the worktree after the issue ships is left to you (`git worktree remove`).

To branch a worktree from somewhere other than `HEAD` (for example a staging branch for a large feature), create it yourself before dispatching; the viewer will reuse it. See [The lead session](patterns/lead-session.md#umbrella-features).

## Agent Model

Each project chooses whether dispatched agents get a pinned model or use their own global settings:

```yaml
- name: "My Project"
  agent_model_source: project   # or "global" (default)
  agent_models:
    claude: claude-opus-5-5
    codex: gpt-5
```

- `global` (default) — no `--model` flag is passed; the agent uses its own configured model.
- `project` — the model in `agent_models` for that agent type is passed as `--model <name>`. An agent type with no entry still launches without a flag.

Model names may contain only letters, digits and `._:/[]-`; anything else is ignored (it is typed into a shell). Any other `agent_model_source` value fails config loading.

## Human Approval Notifications

When a human approves a status transition in the web UI, the server sends a natural-language message to the active agent's tmux session. The message is randomized from a set of conversational templates so the agent receives a human-like prompt rather than a structured signal.

This nudge is now the fallback. The primary hand-off is the agent blocking on `issue-cli transition|start --wait` (see [CLI overview](CLI/overview.md#blocking-on-a-human-approval---wait)). That picks up the approval from the issue file within about 2s, even in sessions the nudge cannot reach (no matching tmux session, or an agent that is busy). The approve handler also timestamps the approval in the issue's stats sidecar (`last_approval`), and logs nudges that fail (`agent not nudged: …`) to the server log, so their failure rate can be measured.

## Approval-gate Deep Links

When a CLI command (`start`, `transition`) fails because a human approval is missing, the error includes a clickable URL pointing at the issue's approve button:

```
Error: cannot start <slug>: human approval for "in progress" is missing; no changes were made

A human must approve this in the issue viewer:
  http://localhost:8080/p/<project>/issue/<slug>#approve-in-progress

To block until it is approved instead of retrying:
  issue-cli start <slug> --wait --timeout 9m
  (exit 3 = still waiting, nothing changed; re-run it)
```

The fragment `#approve-<status>` matches the `id` on the approve button in the detail view, so clicking the link scrolls to and visually flashes the right control. For optional approvals (the ones hidden behind a "Divert to..." CTA), the page also auto-reveals the widget when the URL fragment matches.

The base URL comes from:

1. `ISSUE_VIEWER_URL` env var (set automatically in dispatched sessions — see below)
2. Default `http://localhost:8080`

## Environment Variables

Dispatched sessions export:

- `ISSUE_CLI_LOG` — logging path
- `ISSUE_VIEWER_SERVER_PWD` — server working directory
- `ISSUE_VIEWER_ISSUE_SLUG` — slug of the dispatched issue (attached to bug reports automatically)
- `ISSUE_VIEWER_URL` — base URL of the dispatching server, reconstructed from the inbound request's `Host` (and `X-Forwarded-Host`/`X-Forwarded-Proto` if behind a proxy). The CLI uses it to build approval-gate deep links so dispatched bots' errors point back at the same host the human just clicked from.
