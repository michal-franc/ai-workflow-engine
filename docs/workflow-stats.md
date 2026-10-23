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
      "approved_at": "2026-04-30T11:59:58Z",
      "branch_head": "4cbe7ea1f0…"
    },
    {
      "from": "shipping",
      "to": "done",
      "ts": "2026-04-30T16:00:00Z",
      "static_tokens": 80,
      "dynamic_tokens": 900,
      "actual_tokens": null,
      "branch_head": "a3b2e33c9d…",
      "merge_sha": "a3b2e33c9d…"
    }
  ],
  "last_approval": { "status": "testing", "at": "2026-04-30T12:20:00Z" },
  "dispatches": [
    { "at": "2026-04-30T11:58:10Z", "session": "agent-cli-foo", "agent": "claude" }
  ]
}
```

- **`static_tokens`** — sum of approximate token counts over the workflow scaffolding the bot reads on this specific transition: every `transitionActions` body (`validate` rules, `append_section` titles+bodies, `inject_prompt` prompts, `require_human_approval` markers), plus the legacy `Template` for the target status, plus the target status's entry-guidance `Prompt`. Pure function of `workflow.yaml`.
- **`dynamic_tokens`** — `static_tokens` plus the issue body and joined comment text **at the moment of transition**. Snapshotted because the body drifts after transition.
- **`actual_tokens`** — reserved for a future hybrid pass that records measured agent-run token counts. Always `null` today; nullable in the schema so adding actuals later requires no migration.
- **`approved_at`** (optional) — when the viewer recorded the human approval this transition consumed. Absent when the transition needed no approval, or when the approval did not come through the viewer's approve endpoint (for example an older server binary). It is never guessed from detection time.
- **`branch_head`** (optional) — the head of the issue's `work/<slug>` branch when the transition ran, looked up in the project's checkout (`workdir`). Absent when there is no such branch. The issue's commits stay reachable from it after the branch is deleted.
- **`merge_sha`** (optional, on the move to `done` only) — the checkout's `HEAD` when it contains `branch_head`: the commit the shipping step fast-forwarded main to, and the one [local CI](workflow.md#local-ci) tests (`.ci/runs/<sha>.json`). Absent when main does not contain the branch. A git error never fails a transition; the fields are just left out.
- **`last_approval`** — scratch state for the next transition row. The viewer's approve handler writes it, and clears it when the approval is toggled off. A transition that consumes an approval copies a matching `last_approval` into `approved_at`, then clears it.

- **`dispatches`** (optional) — every launch of an agent on the issue from the viewer (board, detail page, API), oldest first: when, the tmux session, and the agent (`claude`, `codex`). Re-attaching to a live session is not a dispatch; lead launches are not recorded (they have no issue).

Together these give exact gate latency: status entry (the previous row's `ts`) → approval (`approved_at`) → transition (`ts`). The issue page's [Timeline tab](#timeline-tab) uses them.

### Waiting on you

How long you held a gate is derived, not recorded. For each row with `approved_at`:

> waiting on you = `approved_at` − the end of the agent's last `issue-cli` call while the issue was in that row's `from` status, before `approved_at`

An agent that asks for approval stops, so its last call is when you became the blocker. If you approve while the agent is still working, the wait is about 0; time the agent spends answering your questions counts as agent time. With no agent call in the status, the wait runs from when the issue entered it. The one miss: an approval given while the agent sat idle away from a gate counts that idle time as waiting. Call times come from the [clilog](CLI/telemetry.md#the-clilog) (`ts + dur_ms`). `tracker.OwnerWaits(store, callEnds)` computes it for the Timeline tab and project health.

Sidecars written before v0.49.0 may also hold `pending_wait` and per-row `wait_started_at`, written by the removed `issue-cli transition|start --wait`. They are ignored on load and dropped the next time the sidecar is saved.

`tracker.MarkIssueDoneOnce` (the rare multi-step "mark as done" fast-path) bypasses `ApplyTransitionToFile` and so does not record per-step stats. Acceptable for now — this path is uncommon.

Issues that transitioned before the feature shipped have no sidecar. The Stats tab treats a missing sidecar as "no records", never an error.

## Stats page

`/p/<project>/stats` (sidebar: More → Stats) opens on a dashboard; the tables below it are the ones this page always had, folded. `/stats` without a project compares every project.

**Options** are query parameters, so every view is a link:

- `range`: `7d`, `30d` (default), `90d` or `all` (since the oldest snapshot or done issue). The old `?days=N` still works as a custom range and opens the time tables.
- `group`: `type` (default), `system`, `epic` (the `parent:` slug) or `version`, for the "By <group>" bars and their drill-downs.
- `p=90` switches the cycle chart to p90, `scale=share` the by-status bars to shares, `hide=<type,…>` hides series (click a legend entry), and `drill=…` opens the issues behind a bar or point.
- `static/stats.js` remembers the last options per project (`stats:<slug>` in localStorage, read and written in try/catch). A visit without options restores them. Everything works without the script.

**Now: gauges.**

| Gauge | From | Limit |
|:--|:--|:--|
| Waiting on you | the range's aggregate: waiting / (agent + waiting + idle) over the issues with CLI calls | `health.limits.waiting_share`, default 25% |
| Current milestone | `internal/milestone`, as the Status page's release card: done / total | none |
| Big files | the latest snapshot's `codebase.big_files` | `health.limits.big_files`, default 10 |
| CI green rate, static context tokens | "No data yet", naming the issue that adds the family | |

A gauge over its limit says "Over limit" with an icon, never colour alone.

**Trends** are lines from the [health snapshots](workbench-files.md#health-snapshots), one point per day: the day's last snapshot.

- **Cycle time by type:** `workflow.cycle.p50|p90{type}`; a type's point is left out on days it had no issues in its window.
- **Waiting on you:** `workflow.time.share{kind=waiting}` with the limit as a dashed line.
- **Agent working time per issue:** `workflow.working.p50|p90{type=all}`.
- **Rework and gate failures:** `workflow.rework.loops{type=all}`, and the `workflow.gate_failures` rows with `dry_run=false` summed; the dry runs are in each point's tooltip.
- **Lines of code:** `codebase.source_lines`, with the test ratio and its sparkline in the subtitle.

Each workflow point covers the issues done in the 30 days before its snapshot, so a point's issues (click it) come from that window. A line breaks across a missing day, and a vertical "data starts" marker shows where history begins. Charts whose family no snapshot has yet (CI and `make validate` duration, ux-test duration and flaky steps, tokens per issue) are dashed "No data yet" cards naming the issue that brings them. With fewer than two snapshots in the range, a hint names `issue-cli --project <slug> health backfill`. A family that failed in the latest snapshot shows its error on its charts.

**Where the time goes** is live: the range's [project aggregate](#project-aggregate), split exactly as each issue's Timeline tab splits it.

- **By status:** hours of agent working, waiting on you and idle summed per status (Hours / Share toggle). A few long waits dominate the sums, so the time tables keep the p50 / p90.
- **By <group>:** the same split per group-by value; the 12 largest are drawn, the rest summed into one bar.
- **Issues done per week by type:** stacked columns (per month for ranges over half a year).
- **Drill-downs:** a bar, segment or point lists its issues (12, then "N more"), each linking to its Timeline tab, with cycle, agent time and loops.

**Charts** are SVG drawn on the server (`internal/chart`): no chart library, no inline script, so they work under a strict CSP.

- **Accessible:** `role="img"` and an `aria-label` on each chart, a `<title>` on every mark (the hover tooltip), and a Table button per chart that swaps it for the same numbers as rows (a plain `<details>` without JavaScript).
- **Colours:** classes (`s-agent`, `s-waiting`, `s-idle`, `s-feature`, `s-bugfix`, `s-tweak`, `s-none`, …) mapped to the theme's `--chart-*` tokens in `static/style.css`, in dark, Dracula and light. A type of work keeps its colour on every chart. The dark palette passes the colour-vision checks (worst adjacent ΔE 9.4); the light one is below 3:1 contrast for aqua and amber, which the table view covers.

**Speed.** The aggregate is cached per project, keyed by the done issues (slug and file time):

- It is built once for the widest window asked for, and narrower ranges filter it (`Report.Filter`).
- It is warmed when the server starts and rebuilt in the background after 15 minutes, while the old one is served.
- The CLI usage report is cached the same way and never blocks the page; until the first one lands, its section says so.
- An 800-issue project renders in about 85 ms warm. On big projects the shared sidebar costs more than the page (see `ui/sidebar-waiting-on-owner-count-is-slow-on-big-projects-1-s-per-page-on-roguetech`).

**/stats (all projects)** reads only snapshots, never the live aggregate:

- **Rows:** one per repository, with waiting on you (a meter with the limit tick and an "over" chip), cycle p50, rework share, issues done in 30 days, lines of code, test ratio, big files and the latest snapshot. Each value has a sparkline over 7, 30 or 90 days.
- **Order:** sorted by issues done, waiting on you, cycle p50 or lines of code.
- **No snapshots yet:** projects without snapshots, with health off, or sharing another project's repository are named on one line at the end.

## What the Stats tab shows

Below the dashboard, under Tables, three folded sections hold six blocks, in order:

1. **Where the time goes (done in the last 30 days)**: the [project aggregate](#project-aggregate) of the Timeline tab's split. Tables per type of work (cycle p50/p90, rework loops, issues sent back, rework share, dispatches), time per status by type (in status, agent, waiting on you, idle, each p50 / p90; `all` open, the types folded), approval waits per gated transition, and refused transitions by error class, dry runs apart. It names the issues with no CLI calls (status time only) and counts done issues it could not read. From the dashboard's cached aggregate, over its range; `?days=N` opens it.
2. **Dispatch base prompt** — single-row table showing `tracker.AgentDispatchPromptStaticCost()`: the `len(s) / 4` cost of the constant scaffolding text in `tracker.AgentDispatchPromptTemplate` that the issue viewer writes into every fresh agent dispatch. Per-dispatch, not per-transition — kept as a separate metric so it does not over-count when a transition happens without a fresh dispatch.
3. **Static reference — by transition** — every `(from → to)` pair declared in the project's `workflow.yaml`, with its pure static cost. Wildcard `from: "*"` edges are expanded into one row per source status. Sorted by token count descending so the heaviest transitions surface first. Independent of recorded data, so this block is always populated.
4. **Recorded transitions — average cost** — averages of `static_tokens` and `dynamic_tokens` per `(from → to)` across every issue with recorded transitions, plus a count. Sorted by avg dynamic descending.
5. **Per-issue totals** — sum of `dynamic_tokens` across each issue's recorded transitions, sorted descending. Issue title links to the detail view.
6. **CLI usage (last 30 days)**: how `issue-cli` is used in this project, from the local names-only [usage telemetry](CLI/telemetry.md). It has tables for commands (calls, failures, error %, p50 ms), never-used commands, aliases, subcommands, topics and flags, errors, unknown commands and flags tried, and retry sequences.
   - The handler runs `issue-cli [--config $ISSUE_VIEWER_CONFIG --project <slug>] --json telemetry report --since 30d` with a 5 s timeout and `ISSUE_CLI_TELEMETRY_SKIP=1`, so page loads aren't counted as usage. It shells out because only the CLI binary knows its own command registry.
   - The report is cached per project for 5 minutes and refreshed in the background; the page never waits for it.
   - Every failure becomes an inline notice, never a 500: `issue-cli` not on PATH, an exec error, bad JSON, a newer report version, telemetry disabled for the project, or no calls in the window.

## Timeline tab

The issue page's second tab (`/p/<project>/issue/<slug>?tab=timeline`) shows where one issue's time went. It is built on each visit from files already on disk, by `loadTimelineSources` (`timeline_sources.go`) and `buildTimeline` (`timeline.go`), and replaces the old Agent Timeline list.

**Sources.** If one can't be read, the tab says so in a "Could not read N of 5 sources" box and hides what depends on it. It never errors.

| Source | Read from | Without it |
|:--|:--|:--|
| Stats sidecar | `<issue>.stats.json`: transitions, `approved_at`, and `dispatches`, `branch_head`, `merge_sha` when present | No status bands or approvals |
| CLI log | `.agent-logs/<agent>/<agent>.clilog` in the issue's worktree and in the project, under the assignee, `agent-<system>-<name>` and `agent-<name>`; the per-project `.agent-logs/clilog.jsonl` (the lead's and your calls too), filtered by slug; the pre-v0.54 `$TMPDIR/issue-cli-logs/actions.jsonl` while it exists, filtered by project and slug. Deduped on `ts` + args | Agent, waiting and idle are "—"; the CLI calls filter is empty |
| Work branch | `git log --first-parent` from `work/<slug>`, else the last recorded `branch_head`, else `merge_sha` (one call, 2 s timeout) | Commits and CI after merge are not shown |
| Comments | the issue file | — |
| Playtest runs | `.playtest/runs/*/*/results.yaml` in the project and in `.worktrees/<slug>/`, where `issue:` is the slug | No playtest events or playtest rework |

Local CI runs (`.ci/runs/<sha>.json`) are read from the merge sha. For a done issue with none recorded, the branch head is used, which equals the merge after a fast-forward.

**The life** runs from the first dispatch (else the agent's first call in a per-agent log, else the first transition) to the `→ done` transition, or to now while the issue is in flight. Calls on the issue before that (the lead writing it up) fold into one "before dispatch" line and count toward nothing.

**Where the time went.** Activity is an issue-cli call (its end: `ts` + `dur_ms`, or `ts` for pre-v0.54 records) or a commit on the work branch. The life is split at each activity and at each approval, and every piece is classified once:

1. Each approval's wait is [waiting on you](#what-is-recorded) as `tracker.OwnerWaits` derives it: from the agent's last call in that status to `approved_at`. It is **waiting on you** whatever the rules below say of that time.
2. In a status only you can move on (its exit needs your approval: `in design`, `human-testing` in the bundled workflow), any other gap of 15 min or more is **waiting on you**.
3. Any other gap of 15 min or more is **idle** (the same 15 min as `agentIdleAfter` in `agent_state.go`).
4. Everything else is **agent working**.

So agent working + waiting on you + idle = the total. A question answered in chat shows as idle, because nothing records it.

**Rework.** A loop opens in either of two ways:
- at a backward transition, and closes when the issue is back in the status it left;
- at a failed playtest run from `testing` onward, and closes at the next run in the same mode (judge or auto) where every failed step passes.

Failed runs before testing are the agent iterating and don't count. Agent time inside a loop is rework: it is part of agent working, drawn purple on the bar.

**Life by status** has one band per status, with its span and the agent, waiting, idle and rework time inside it. Its events:
- **The agent's writes, grouped:** decisions (matched to their D-n), sections written, boxes ticked, comments (kind = their prefix: `progress`, `tests`, `docs` …), refused calls (exit ≠ 0).
- **Approvals:** "Approved → <status>", with your words quoted under it, or "no words recorded". The words come from the `approval:` comment that pairs with the approval: the latest one made after the previous transition and up to this one, whose `→` statuses name the target. A comment written before the `→` suffix matches any target, a pre-0.54 day-only date counts when its day overlaps that window, and each comment pairs once (`approvalWords`).
- **Commits:** a merge of main into the branch is one muted line.
- **Playtest runs**, and the first CI run on main.
- **Rows for time:** amber waits, grey idle gaps and a purple loop row.
- **After done:** calls fold into one line.

Filters (`&show=`):
- `all`
- `you`: approvals, your playtest runs, decisions, waits
- `commits`
- `tests`: `tests:`/`playtest:`/`ci:` comments, every playtest run, CI
- `cli`: every call, in the old Agent Timeline's form, with the workflow actions each transition saw and the exit code; runs of reads fold into one line that opens

The clilog keeps no free text (`clilog.Placeholder`: `‹text n=… h=…›`). Comment text comes back from the issue's comments the way the old list did it (`FillRedactedComments`: the comment saved during the call, else the one whose placeholder matches). A decision is matched by its `--text` or `--quote` placeholder. A value with no match stays a placeholder. A failed call shows `(failed: <err_class>)`, and a refused gate reads "refused: approval missing".

**Side column:**
- **Your part:** each wait.
- **Your words:** decisions with your quote, and each approval with its words and how they came (`viewer`, `in the lead chat`), or "no words recorded". The count of quoted entries includes approvals with words.
- **Left open at done:** unticked boxes; a section with more than 3 open shows as a count; failed playtest steps are marked.
- **CI after merge:** the first run on main that contains the issue, and how many runs since were green.

The tab label on every tab ("5h 0m", "1h 50m so far") reads only the stats sidecar and the first line of each per-agent clilog.

### Project aggregate

`timeline.Aggregate` runs every issue done in the last 30 days through the tab's own code (`timeline.Load`, `timeline.Split`), so the project numbers are the tabs' numbers summed, never a second derivation. /stats and the `workflow` health family ([Health snapshots](workbench-files.md#health-snapshots)) both read it.

- **In the window:** the issue is `done` and its last transition to `done` in the stats sidecar is in the last 30 days. Done issues without one are counted as unread and left out.
- **Per issue:** cycle time (the tab's total), and per status (a status visited twice sums) the time in it and its agent, waiting and idle time. Approval waits are `OwnerWaits`. Rework is the tab's loops plus the count of backward transitions.
- **No CLI calls:** cycle time and time in status only; agent, waiting and idle show "—" on /stats.
- **Gate failures:** `transition` calls with exit ≠ 0, keyed by the status the call was made in, its `--to` and its `err_class`. Dry runs are counted apart: they are the agent checking before it asks.
- **Percentiles:** nearest-rank p50 and p90 over the issues in the row.
- **Cost:** each clilog and each playtest results file is read once per aggregate (`timeline.Cache`), and each issue costs two git calls. On RogueTech (65 issues in the window) it takes about half a second.

## Implementation

- `internal/tokens/estimate.go` — `Estimate(s string) int = len(s) / 4`. Single function, easy to swap for a real BPE later.
- `internal/tracker/stats.go` — `TransitionStat`, `StatsStore`, `Dispatch`, sidecar I/O (`StatsSidecarPath`, `LoadStats`, `SaveStats`, `AppendTransitionStat`, `RecordDispatch`), and the cost functions `StaticTransitionCost(wf, from, to)` and `DynamicTransitionCost(wf, from, to, body, comments)`.
- `internal/tracker/stats_wait.go` — `OwnerWaits`, the derived "waiting on you" per approved gate.
- `internal/agentrun/agentrun.go` — `Start` appends a `dispatches` entry once the agent's tmux session is created.
- `internal/tracker/dispatch.go` — `AgentDispatchPromptTemplate` constant (single source of truth shared with `handlers_dispatch.go`; named `{{slug}}`, `{{system}}`, … placeholders that `BuildAgentPrompt` fills from `agentPromptFields`) and `AgentDispatchPromptStaticCost()`.
- `internal/tracker/workflow_transition.go` — `ApplyTransitionToFileWithFields` snapshots a `TransitionStat` after a successful transition, capturing the pre-transition body and comments for the dynamic cost, and `branchShas` adds `branch_head` / `merge_sha` (`WorkflowConfig.RepoRoot`, set from the project).
- `internal/timeline/` — the Timeline tab's derivation: `Load` (stats sidecar, clilogs, work branch, playtest runs), `Split` (bands, gaps, owner waits, rework), `Aggregate` and the `workflow` health family. `timeline.go` and `timeline_sources.go` at the root build the tab's view on top.
- `handlers_stats.go` — the `/stats` route handler. Walks the project's issues and aggregates per-(from→to) and per-issue, and builds the time section with `timeline.Aggregate`. `runTelemetryReport` (package var, stubbed in tests) and `loadCLIUsage` produce the CLI usage block.
- `templates/stats.html` — page template: the dashboard (`statsChart` renders a chart card with its table and drill-down), then the tables. Reuses the existing `data-table` styling.
- `stats_dashboard.go` — the dashboard model: options, gauges, trends from the snapshots, time bars, weekly columns, drill-downs. `buildDashboard` reads nothing from disk.
- `stats_cache.go` — the per-project aggregate and CLI usage cache, and `warmStats` at server start.
- `stats_overview.go`, `templates/stats_overview.html` — `/stats` across projects.
- `internal/chart/` — line, stacked bars, columns, gauge and sparkline as SVG, each with `Table()`.
- `internal/milestone/` — the current milestone (`Pick`, `Released`) and the `milestone` health family.
- `static/stats.js` — table toggles, the group-by select, remembered options.

## Out of scope (today)

- **Actual measured tokens.** No plumbing into the agent dispatch / Claude API response path. The sidecar reserves the field; the population pass is a follow-up.
- **Per-model breakdown.** One approximation across the board.
- **Token cost on the Timeline tab.** Dropped on 2026-10-22; the Stats tab is the only place token estimates show.
- **Backfilling historical transitions.** Only transitions performed after the feature shipped are recorded.
- **Storing the verbatim input string.** Counts only; if "why is this transition expensive?" becomes a real question, snapshot text can be added later.
