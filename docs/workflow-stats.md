---
title: "Workflow Stats"
order: 9
---

## What it is

`/p/<project>/stats` is a project-level tab that surfaces the approximate token cost of the workflow itself: how much the bot reads on every transition and on every fresh agent dispatch. The intent is to make workflow weight visible so steps that are not pulling their weight can be trimmed.

Token counts use a `len(s) / 4` approximation — a well-known proxy for Claude/GPT-family BPE tokenization. Numbers are useful for **relative comparison**, not as exact counts for any specific model.

## What is recorded

A per-issue sidecar `<issue>.stats.json` is written next to the markdown file every time `tracker.ApplyTransitionToFile` runs a successful transition, and when `issue-cli start` advances a handoff status (`StartIssueOnce`).

```json
{
  "transitions": [
    {
      "from": "idea",
      "to": "in design",
      "ts": "2026-04-30T12:00:00Z",
      "static_tokens": 123,
      "dynamic_tokens": 456,
      "actual_tokens": null,
      "wait_started_at": "2026-04-30T11:40:00Z",
      "approved_at": "2026-04-30T11:59:58Z"
    }
  ],
  "pending_wait": { "to": "testing", "started_at": "2026-04-30T12:05:00Z" },
  "last_approval": { "status": "testing", "at": "2026-04-30T12:20:00Z" }
}
```

- **`static_tokens`** — sum of approximate token counts over the workflow scaffolding the bot reads on this specific transition: every `transitionActions` body (`validate` rules, `append_section` titles+bodies, `inject_prompt` prompts, `require_human_approval` markers), plus the legacy `Template` for the target status, plus the target status's entry-guidance `Prompt`. Pure function of `workflow.yaml`.
- **`dynamic_tokens`** — `static_tokens` plus the issue body and joined comment text **at the moment of transition**. Snapshotted because the body drifts after transition.
- **`actual_tokens`** — reserved for a future hybrid pass that records measured agent-run token counts. Always `null` today; nullable in the schema so adding actuals later requires no migration.
- **`wait_started_at`** (optional) — when an agent began blocking on this transition with `issue-cli transition|start --wait`. If the wait was re-run after `--timeout` expiries, this is the first start. Absent when nobody waited.
- **`approved_at`** (optional) — when the viewer recorded the human approval this transition consumed. Absent when the transition needed no approval, or when the approval did not come through the viewer's approve endpoint (for example an older server binary). It is never guessed from detection time.
- **`pending_wait`** / **`last_approval`** — scratch state for the next transition row. `issue-cli … --wait` writes `pending_wait` when it starts blocking. The viewer's approve handler writes `last_approval`, and clears it when the approval is toggled off. Any recorded transition clears `pending_wait`, which is copied into `wait_started_at` only when the target matches. A transition that consumes an approval copies a matching `last_approval` into `approved_at`, then clears it.

Together these give exact gate latency: status entry (the previous row's `ts`) → approval (`approved_at`) → transition (`ts`), plus how long an agent actually sat blocked (`ts − wait_started_at`). The Stats tab does not show them yet.

`tracker.MarkIssueDoneOnce` (the rare multi-step "mark as done" fast-path) bypasses `ApplyTransitionToFile` and so does not record per-step stats. Acceptable for now — this path is uncommon.

Issues that transitioned before the feature shipped have no sidecar. The Stats tab treats a missing sidecar as "no records", never an error.

## What the Stats tab shows

Three blocks, in order:

1. **Dispatch base prompt** — single-row table showing `tracker.AgentDispatchPromptStaticCost()`: the `len(s) / 4` cost of the constant scaffolding text in `tracker.AgentDispatchPromptTemplate` that the issue viewer writes into every fresh agent dispatch. Per-dispatch, not per-transition — kept as a separate metric so it does not over-count when a transition happens without a fresh dispatch.
2. **Static reference — by transition** — every `(from → to)` pair declared in the project's `workflow.yaml`, with its pure static cost. Wildcard `from: "*"` edges are expanded into one row per source status. Sorted by token count descending so the heaviest transitions surface first. Independent of recorded data, so this block is always populated.
3. **Recorded transitions — average cost** — averages of `static_tokens` and `dynamic_tokens` per `(from → to)` across every issue with recorded transitions, plus a count. Sorted by avg dynamic descending.
4. **Per-issue totals** — sum of `dynamic_tokens` across each issue's recorded transitions, sorted descending. Issue title links to the detail view.
5. **CLI usage (last 30 days)**: how `issue-cli` is used in this project, from the local names-only [usage telemetry](CLI/telemetry.md). It has tables for commands (calls, failures, error %, p50 ms), never-used commands, aliases, subcommands, topics and flags, errors, unknown commands and flags tried, and retry sequences.
   - The handler runs `issue-cli [--config $ISSUE_VIEWER_CONFIG --project <slug>] --json telemetry report --since 30d` with a 5 s timeout and `ISSUE_CLI_TELEMETRY_SKIP=1`, so page loads aren't counted as usage. It shells out because only the CLI binary knows its own command registry.
   - Every failure becomes an inline notice, never a 500: `issue-cli` not on PATH, an exec error, bad JSON, a newer report version, telemetry disabled for the project, or no calls in the window.

## Implementation

- `internal/tokens/estimate.go` — `Estimate(s string) int = len(s) / 4`. Single function, easy to swap for a real BPE later.
- `internal/tracker/stats.go` — `TransitionStat`, `StatsStore`, sidecar I/O (`StatsSidecarPath`, `LoadStats`, `SaveStats`, `AppendTransitionStat`), and the cost functions `StaticTransitionCost(wf, from, to)` and `DynamicTransitionCost(wf, from, to, body, comments)`.
- `internal/tracker/dispatch.go` — `AgentDispatchPromptTemplate` constant (single source of truth shared with `handlers_dispatch.go`) and `AgentDispatchPromptStaticCost()`.
- `internal/tracker/workflow_transition.go` — `ApplyTransitionToFileWithFields` snapshots a `TransitionStat` after a successful transition, capturing the pre-transition body and comments for the dynamic cost.
- `handlers_stats.go` — the `/stats` route handler. Walks the project's issues and aggregates per-(from→to) and per-issue. `runTelemetryReport` (package var, stubbed in tests) and `loadCLIUsage` produce the CLI usage block.
- `templates/stats.html` — page template. Reuses the existing `data-table` styling.

## Out of scope (today)

- **Actual measured tokens.** No plumbing into the agent dispatch / Claude API response path. The sidecar reserves the field; the population pass is a follow-up.
- **Per-model breakdown.** One approximation across the board.
- **Inline display in the timeline / detail sidebar.** The Stats tab is the only render target.
- **Backfilling historical transitions.** Only transitions performed after the feature shipped are recorded.
- **Storing the verbatim input string.** Counts only; if "why is this transition expensive?" becomes a real question, snapshot text can be added later.
