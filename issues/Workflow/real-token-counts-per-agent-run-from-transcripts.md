---
title: "Real token counts per agent run from transcripts"
status: "idea"
system: "Workflow"
priority: "low"
type: "feature"
version: "0.57"
parent: "workflow/harness-checks-the-work-ideas-from-lights-on"
source: "https://github.com/gojko/lights-on-software-factory"

---
The Stats page estimates workflow token cost as `len(s) / 4` (`docs/workflow-stats.md`). Lights-On's `measure-run`
reads the run's transcripts instead and reports wall clock, model and tool time, tool calls and tokens per agent and
in total.

## Idea

1. **Read the agent's transcript** for a dispatched issue (Claude Code keeps it under `~/.claude/projects/…`; the
   session name is known from dispatch) and record real input, output and cache tokens and tool calls in the stats
   sidecar.
2. **Show actual next to estimate** on the Stats page (`actual_tokens` is already in the JSON, always `null` today)
   and per status on the Timeline tab.
3. **Fall back to the estimate** when no transcript is readable (Codex, another machine), and say so.

## Notes

- Read-only, local, names and counts only, same privacy rule as the clilog: no prompt or output text is stored.
- Source: `bin/measure-run.js` in gojko/lights-on-software-factory.
