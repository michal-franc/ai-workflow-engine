---
title: "Agent heartbeat and actual tokens"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
---

Two things the Timeline tab can't show from today's data:

- **Working vs idle between CLI calls.** `workerState` calls an agent idle after 15 minutes with no issue-cli call,
  which misses long builds and long thinking. The tab marks it "inferred".
- **Actual tokens.** `actual_tokens` is null in every stats sidecar; the tab shows estimates.

## Options

1. **`rawlog` sampling.** Dispatch already pipes each agent's pane into `.agent-logs/<session>/rawlog`. The viewer
   samples its size once a minute into `activity.jsonl`; growth means working. Ours, no agent change, no tokens.
2. **The agent's own session log.** Claude Code writes a log per session under `~/.claude/projects/` with a timestamp
   and token usage per message; Codex writes `~/.codex/sessions/`. Read at each transition: heartbeat and actual
   tokens. These are private formats that can change.

Same data as 0.54 Project health's "workflow timeline (working, waiting on owner, idle)" and "context cost": record
it once here, and let Project health aggregate it.
