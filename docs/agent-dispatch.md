---
title: "Agent Dispatch"
order: 6
---

## Overview

The board and detail views can dispatch issues to AI agents (Claude or Codex) via tmux sessions. Dispatch creates a session, opens a terminal, and pastes a generated prompt.

## How to Dispatch

- **Board view** — hover a card, click the play button, pick Claude or Codex
- **Detail view** — two buttons in the sidebar (Claude / Codex)
- **API** — `POST /p/<project>/issue/<slug>/dispatch` with `{"agent": "claude"}` or `{"agent": "codex"}`

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

## Human Approval Notifications

When a human approves a status transition in the web UI, the server sends a natural-language message to the active agent's tmux session. The message is randomized from a set of conversational templates so the agent receives a human-like prompt rather than a structured signal.

## Approval-gate Deep Links

When a CLI command (`start`, `transition`) fails because a human approval is missing, the error includes a clickable URL pointing at the issue's approve button:

```
Error: cannot start <slug>: human approval for "in progress" is missing; no changes were made

A human must approve this in the issue viewer:
  http://localhost:8080/p/<project>/issue/<slug>#approve-in-progress
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
