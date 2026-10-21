---
title: "Agent Dispatch"
order: 6
---

## Overview

The board and detail views can dispatch issues to AI agents (Claude or Codex) via tmux sessions. Dispatch creates a session, opens a terminal, and starts the agent with a generated prompt.

## Requirements

- **tmux** is mandatory. The whole dispatch lifecycle is tmux: creating the session, injecting the environment, logging via `pipe-pane`, and launching the agent in the session.
- **The agent CLI**: `claude` or `codex` on the `PATH` of the server.
- **git**, when the workflow sets `worktree: true`.
- **A terminal command**, or `terminal: "none"` to attach yourself (see [Terminal Configuration](#terminal-configuration)). The default, i3 + alacritty, is only needed if you leave `terminal` unset.

> **Tip.** Not sure which `terminal` value fits your setup? Ask your coding agent: *"Look at my OS, window manager and installed terminals, then set the `terminal:` field in `projects.yaml` to something that works here."* It can detect i3, GNOME, macOS or WSL, check what's on `$PATH`, and pick the right command, or fall back to `"none"`.

## How to Dispatch

- **Board view** — hover a card, click the play button, pick Claude or Codex
- **Detail view** — two buttons in the sidebar (Claude / Codex)
- **API** — `POST /p/<project>/issue/<slug>/dispatch` with `{"agent": "claude"}` or `{"agent": "codex"}`

## How the prompt is delivered

The prompt is saved to `<workdir>/.agent-logs/<session>/dispatch-prompt.txt`, and the agent starts with it as its positional argument (`claude "<prompt>"`, the same as codex). It is never pasted into a booting agent. Claude is also started with `-n <session name>` (`claude -n agent-<slug> "<prompt>"`, `-n lead-<scope>` for leads), so its tmux name is its address for Claude Code's built-in messaging between sessions (`ListAgents` / `SendMessage`); codex gets no name. Earlier versions typed `claude`, waited a fixed 3 s and pasted the prompt. When Claude took longer to start, the paste was dropped, yet every step still reported ok.

Dispatch then polls the pane for the prompt's last line. It reports the step **Prompt delivered**, or a failed step after 20 s. Nothing is re-sent automatically.

### Re-send prompt

If an agent came up without its briefing, use **Re-send prompt** in the issue sidebar, or the link in the reattach banner. It sends `POST /p/<project>/issue/<slug>/dispatch/reprompt`, which pastes the current briefing into the issue's live session through a per-session named tmux buffer and submits it. It asks for confirmation first. If no agent session is alive, it fails with "No live agent session" and creates nothing; dispatch instead.

### Read the prompt without dispatching

`GET /p/<project>/issue/<slug>/dispatch-prompt` (`?format=json` for `{"prompt","slug","status","worktree","branch","session"}`) and `issue-cli dispatch-prompt <slug> [--json]` print the prompt a dispatch would send right now. Both share one builder (`tracker.BuildDispatchPrompt`), so the two outputs are byte-identical. Neither creates a worktree, session or file. The prompt follows the issue's current status, so it can differ from the saved `dispatch-prompt.txt` of an earlier dispatch.

## Re-dispatching to a live session

Dispatching to an issue whose tmux session is still alive does **not** error out and does **not** re-prompt the agent. The handler probes `tmux has-session -t <name>` first; if the session exists it skips `new-session`, all logging/env setup, and the prompt, and just opens a terminal attached to the existing session.

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

The dispatch modal renders this with a yellow warning banner. The banner links **Re-send prompt** for an agent that never got its briefing. Kill the existing session manually (`tmux kill-session -t agent-<slug>`) if you want a fresh dispatch instead.

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
| Linux + i3 + alacritty     | `i3-msg exec "alacritty -T {{session}} -e tmux attach -t {{session}}"`       |
| macOS + iTerm2             | `osascript -e 'tell app "iTerm2" to create window with default profile command "tmux attach -t {{session}}"'` |
| macOS + Terminal.app       | `osascript -e 'tell app "Terminal" to do script "tmux attach -t {{session}}"'` |
| Headless (attach manually) | `none`                                                                       |

If `terminal` is unset, defaults to i3 + alacritty, with the window titled after the session (`alacritty -T <session>`). On i3, opening an agent from the agents canvas when its session already has a terminal focuses the window with that exact title instead of opening another, so give your own `terminal` command `-T {{session}}` (or your terminal's equivalent) to get the same. Set to `none` to only create the tmux session (the response includes the `attach_cmd`).

When the viewer itself runs with `TMUX_TMPDIR` set (a sandbox or playtest scenario on a private tmux server), `attach_cmd` starts with `TMUX_TMPDIR=<dir>`, so it reaches that server from any shell. A `terminal` command inherits the variable through `sh -c`; the i3 default passes it on with `env`. The board waits for the `terminal` command to exit, so use one that returns at once (`i3-msg exec …`, or `setsid -f alacritty -e … >/dev/null 2>&1`).

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

"Edit in nvim" follows the same rule: it opens as an `agent-<slug>-edit` window, and only that window is closed when the edit finishes. Agent windows are listed as `work:agent-<slug>` and receive approval notifications like per-agent sessions. Lead windows (`lead-<project>`, `lead-<epic>`) live in the same shared session and are listed as `work:lead-<…>`. With `terminal: none`, `attach_cmd` is `tmux attach -t work \; select-window -t agent-<slug>`.

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
- Worktrees stay after the issue ships. Nobody has to remove them; `git worktree remove` is there if you want the space back.

Epics change where a worktree starts:

- A child of an epic (`parent: <epic>`, or the legacy `umbrella:`) whose epic has `base:` gets its worktree branched from that base branch instead of `HEAD`. Its prompt gets a `## Base branch` section: build on the base, merge the base (never `main`), ship into the base.
- The epic issue itself reuses the base-branch worktree its sub-lead runs in (`.worktrees/<epic-slug>` on `base:`); it gets no `work/<slug>` branch.

See [The lead session](patterns/lead-session.md#epics).

## Lead Sessions

Lead agents run in tmux like workers, under fixed names: `lead-<project>` for the project lead (in the project checkout) and `lead-<epic-slug>` for an epic's sub-lead (in the epic's base-branch worktree). Start them with `issue-cli lead spawn [--epic <slug>]`, the **Spawn lead** button in the Actions bar, or **Spawn sub-lead** on the agents canvas or the epic's detail page (`POST /p/<project>/lead/spawn`, body `{"epic": "<slug>"}` or `{}`). A live lead is re-attached, never started twice.

- Leads use the same launcher, terminal, `tmux_session` and `agent_model_source` settings as dispatched workers.
- Their brief comes from `leads:` in `workflow.yaml`, or the built-in defaults.
- A `lead-<epic>` session is not counted as an active agent on the epic issue, although its name contains the slug.
- Every worker's dispatch prompt has a `## Your lead` section naming who it reports to: the epic's sub-lead while it is live, otherwise the project lead.
- Agents message each other with Claude Code's own `ListAgents` / `SendMessage`, addressed by tmux name. The tool builds nothing on top of it; the briefs just say it is there. A session in a different permission mode from the sender holds incoming messages for its human's approval, as Claude Code does out of the box.

See [The lead session](patterns/lead-session.md) for roles, routing and authority.

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

This nudge is the hand-off: at a gate the agent asks in chat, then stops and ends its turn, and the approval message resumes it. Agents don't block on `--wait` or poll (the flag remains for scripts, see [CLI overview](CLI/overview.md#blocking-on-a-human-approval---wait)). If an agent wasn't reachable (no matching tmux session), tell it yourself that the approval landed. The approve handler also timestamps the approval in the issue's stats sidecar (`last_approval`), and logs nudges that fail (`agent not nudged: …`) to the server log, so their failure rate can be measured.

## Approval-gate Deep Links

When a CLI command (`start`, `transition`) fails because a human approval is missing, the error includes a clickable URL pointing at the issue's approve button:

```
Error: cannot start <slug>: human approval for "in progress" is missing; no changes were made

A human must approve this in the issue viewer:
  http://localhost:8080/p/<project>/issue/<slug>#approve-in-progress

Tell the human what needs approving, then stop and end your turn.
Do not wait or poll: re-run this command only after the human says it is approved.
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
- `ISSUE_VIEWER_LEAD_SCOPE` — lead sessions only: `project` for the project lead, or the epic slug for a sub-lead. `issue-cli lead whoami`, `approve` and `lead spawn` read it to know who is asking.
- `ISSUE_VIEWER_URL` — base URL of the dispatching server, reconstructed from the inbound request's `Host` (and `X-Forwarded-Host`/`X-Forwarded-Proto` if behind a proxy). The CLI uses it to build approval-gate deep links so dispatched bots' errors point back at the same host the human just clicked from.
