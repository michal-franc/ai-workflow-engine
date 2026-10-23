# Issue page tabs: what data we have and what agents must record

Snapshot of the Claude doc https://claude.ai/artifact/PcYZzYRactTqGtC9xLLugw (private) as of 2026-10-22. Later
decisions are folded in: the agent heartbeat, actual tokens and tokens by step were dropped, and `issue-cli prove` was
redesigned (see `issues/Workflow/issue-cli-prove-link-each-criterion-to-its-proof.md`).

## Summary

24 of the 33 elements on the three issue-page tabs can be built from data the engine already records. That data comes
mostly from the clilog every `issue-cli` call writes, plus the stats sidecar, comments, decisions, git and playtest
runs. The rest splits into these kinds of work:

| Bucket | Elements | What it means |
| --- | --- | --- |
| Out of the box | 24 | The viewer reads files that exist today. No agent or CLI change. |
| issue-cli change, no new ask | 2 | Existing commands record one more field (exit code, merge sha). Agents keep doing what they do. |
| Viewer change | 2 | The approve button takes optional words, shown on the Timeline and Issue tabs. |
| New agent ask | 4 | Agents link each criterion to its proof and list what to watch. One new command: `issue-cli prove`. |
| New telemetry | 1 | Dropped 2026-10-22: working vs idle and actual tokens. |

The biggest plug-in point is the clilog. It already holds every call's time and full arguments, including comment
text, decisions and ticked boxes. The biggest gap is proof: nothing links a criterion to the test, playtest step or
screenshot that proves it, so the Evidence tab's matrix needs a new ask.

Checked against `origin/main` at v0.52.0 (`ade2b90`). Local CI (v0.53) is on the owner's machine only, so its
`.ci/runs` data is taken from the mockup, not the code.

## Where the data lives today

Every `issue-cli` call an agent makes is already logged twice: once with full arguments before it runs (clilog), once
with the result after it runs (telemetry). Neither is joined to the other today.

| Source | Written by | Holds | Gap for the tabs |
| --- | --- | --- | --- |
| clilog: `.agent-logs/<assignee>/<assignee>.clilog` and `$TMPDIR/issue-cli-logs/actions.jsonl` | `logAction` in `cmd/issue-cli/main.go`, on every call, before it runs | ts (seconds), pid, ppid, full args, so comment text, decision text, box ids, `--to` | No exit code or result. The tmp copy is lost on reboot. |
| Telemetry: `.agent-logs/telemetry.jsonl` and `~/.local/state/issue-cli/telemetry.jsonl` | `cmd/issue-cli/telemetry.go`, after the call | ts, `dur_ms`, command, flag names, issue, `to`, `exit`, `err_class` (approval_missing, validation, invalid_transition…), caller, `args_hash` | Names only, no values. Can be turned off by env. |
| Stats sidecar: `<issue>.stats.json` | `transition`; `RecordApproval` from the viewer button and `issue-cli approve` | Each transition's from, to, ts; token estimates; `approved_at` | `actual_tokens` is null. No dispatch time, no merge sha. |
| Comments block in the issue file | `issue-cli comment`, viewer | id, text, source, status; prefixes by convention (`progress:`, `tests:`, `question:`, `approval:`) | `date` is the day only (`2006-01-02`); the time comes from the clilog. |
| `## Decisions` (D-n lines) | `issue-cli decide` | date, how, text, quote | None for the tabs. |
| Body sections and checkboxes | agents via `append`, `check`, `replace` | Section text, box ids, ticked state | Criteria are often plain bullets, not boxes with ids. |
| Git: `work/<slug>` branch, `.worktrees/<slug>` | agents | Commits with times, diff stats | The branch may be deleted after the merge. |
| Playtest runs: `.playtest/runs/<session>/<time>-<mode>/results.yaml` | `cmd/playtest` (ux-test, playtest) | Per step: status, checks per viewport, screenshot path, judge result | The viewer only checks that a judge run exists (`attention.go`). |
| Session dir: `.agent-logs/<session>/` | dispatch (`internal/agentrun`) | `dispatch-prompt.txt`, `rawlog` (being removed) | — |
| Agent state | `agent_state.go`, `attention.go` | question, at-gate, idle (15 min with no CLI call), working; what waits on the owner | Idle is guessed from CLI calls only. |

## Timeline tab

11 of 15 elements work today.

| Element | Source today | Bucket | What it takes |
| --- | --- | --- | --- |
| Life bar cut by status | stats transitions `ts` | Out of the box | Read the sidecar. |
| Dispatch start | first clilog record; `dispatch-prompt.txt` mtime | Out of the box | Works; a `dispatched_at` in stats would make it exact. |
| Waiting on you, per gate | stats `approved_at` minus the `progress: … Waiting on` comment's clilog ts | Out of the box | Works, but leans on comment wording. A recorded `waiting_since` would harden it. |
| Agent working vs idle inside a band | CLI call gaps only | New telemetry | Dropped 2026-10-22: gaps of 15 min or more with no CLI call show as idle. |
| Rework loops | backward transitions in stats; failed playtest runs in `results.yaml`; `correction:` comments | Out of the box | Read and group them. |
| Commits in their band | `git log work/<slug>` with commit times | Out of the box | Works while the branch exists; a recorded merge sha covers deleted branches. |
| Decisions in their band | clilog `decide` ts + D-n text and quote | Out of the box | Read both. |
| Comments in their band (`progress:`, `tests:`, `question:`) | comment text; time from the clilog `comment` call | Out of the box | Join on text, or store the time on the comment. |
| Your approvals, with your words | `issue-cli approve --quote` writes an `approval:` comment; the viewer button writes only `approved_at` | Viewer change | Optional words box on the button, saved like `approve --quote`. |
| Fold repeated reads | clilog verbs `show`, `checklist`, `context`, `--dry-run` | Out of the box | Group them. |
| CLI calls filter, with the workflow actions each call saw | today's Agent Timeline (`agent_timeline.go`) | Out of the box | Move it in as a filter. |
| Gate refusals and retries | telemetry `exit` and `err_class`; clilog has the args | issue-cli change | Log the exit code and error class in the clilog too, so one record has both. |
| Tokens by step | stats static and dynamic estimates | Out of the box | Dropped 2026-10-22: no tokens card on the tab. |
| Left open at done | checkbox state per section | Out of the box | Parse at done. |
| CI after the merge | local CI `.ci/runs/<sha>.json` (v0.53, not pushed) | issue-cli change | Record the merge sha at `shipping → done`, then look up its CI run. |

## Evidence tab

6 of 10 elements work today. The tab's centre, criterion to proof, needs agents to say which test, step or commit
proves which criterion: nothing records that link now.

| Element | Source today | Bucket | What it takes |
| --- | --- | --- | --- |
| Images from the body and playtest runs | body `![…]` links (`images.go`); `screenshot` per step in `results.yaml` | Out of the box | Read `results.yaml`, not only check it exists. |
| Images from the worktree (bench shots, destruction shots) | files in `screenshots/` folders, unlinked | New agent ask | Decided 2026-10-22: shots come from playtest runs, one per step a criterion points to; a hand-picked shot only where no playtest runner exists. |
| "Proves MV1 / AC1" tag on each image | nothing | New agent ask | Part of `issue-cli prove`. |
| Verdict (shipped, merge, suite count, time, wait) | status, stats, `tests:` comment text | Out of the box | Works; the suite count is free text in the `tests:` comment. |
| Path with your checkpoints inside a status | stats transitions; D-n and `approval:` comments dated inside a status | Out of the box | Place them on the path by time (moved to the Timeline tab). |
| Criteria → proof matrix | criteria text; `tests:` comments; session step ids | New agent ask | `issue-cli prove <slug> <criterion> --test / --step / --commit / --value`; the engine checks each link (a test needs a `tests:` comment after the last commit, a step a pass in the latest `results.yaml`). Asked in the testing prompt. |
| Change summary (commits by area, files, +/−) | `git log` and `diff --stat` on `work/<slug>` or the merge | Out of the box | Works; the recorded merge sha covers deleted branches. |
| Playtest verdict per step, yours and the machine's | `results.yaml` per step; `approval:` / `playtest:` comments | Out of the box | Works where the playtest runner exists. |
| Stale text in the body | `Open …` / `pending` lines vs D-n answers | Out of the box | A lint over the body. |
| Watch list (risks left at ship) | nothing | New agent ask | Agents add `risk:` comments at shipping. |

## Issue tab and phone view

7 of 8 elements work today; the phone view reuses the same data. Retro lines and "since your last visit" are dropped,
so the owner last-seen record is no longer needed.

| Element | Source today | Bucket | What it takes |
| --- | --- | --- | --- |
| Needs-you bar with the exact action | `ownerAttention`, `waitingOnOwner`, `playtestsToRun`, open `question:` | Out of the box | Same item and wording as the Status page. |
| One-line brief | `## Idea` "In short", `## Bug` first line, `## Why` owner quote | Out of the box | Lift the first match into the header. |
| What you're approving (since the last gate) | clilog `append` / `check` / `decide` / `comment` between two transitions; git log | Out of the box | Group the clilog window by verb. |
| Decisions in your words, "took the recommendation" mark | D-n quote; `(Recommended)` in the quote text | Out of the box | Read and mark. |
| Fold the body | sections, box ids, gate names from `workflow.yaml` | Out of the box | Template change. |
| Agent card | `workerState`, `lastCliCall`, tmux, dispatch prompt | Out of the box | Gather what the sidebar already shows. |
| Relations | `depends_on`, `parent`, `#slug#D-n` citations | Out of the box | Gather. |
| Approve with your words | button writes `approved_at` only | Viewer change | Same change as on the Timeline tab. |

## Plug-in points in commands agents already run

Agents already run these commands at the right moments. Each one can record more without changing a single prompt.

| Command (when agents run it) | Records today | Can also record, no new ask |
| --- | --- | --- |
| Every call (`logAction`) | ts, pid, args, before the call runs | Exit code, error class and duration, by writing the clilog record after the call (telemetry already computes them). |
| `transition --to <gate status> --dry-run` (agent reaches a gate) | clilog; telemetry `err_class: approval_missing` | `waiting_since` per gate in the stats sidecar, the exact start of "waiting on you". |
| `transition` (every status move) | from, to, ts, token estimates, `approved_at` | Branch head sha at each move; the merge sha at `shipping → done`. |
| `comment` | text, day | Full timestamp on the comment itself, so viewer-made comments get one too. |
| `check` / `append` / `replace` | clilog args (box id, section) | Nothing new needed: the clilog window between gates is "what you're approving". |
| `decide` | D-n with date, how, quote | Nothing new needed. |
| `approve --quote` | `approval:` comment + `approved_at` | Nothing new needed. |
| Dispatch (viewer, `internal/agentrun`) | prompt file, clilog path | `dispatched_at` in stats. |
| `make ux-test` / `make playtest` (`cmd/playtest`) | `results.yaml` per step with shots | Nothing new needed: the viewer just has to read it. |

## What to add

| Change | Kind | Feeds | Effort |
| --- | --- | --- | --- |
| Write exit code, error class and duration into the clilog record | issue-cli | Gate refusals, retries, rework | S |
| Record `waiting_since` when a transition hits a missing approval | issue-cli | Waiting on you, without relying on comment wording | S |
| Record branch head per transition and the merge sha at done | issue-cli | Commits after the branch is gone, change summary, CI after merge | S |
| Store a full timestamp on comments | issue-cli | Comments placed in time without the clilog; `prove --test` freshness | S |
| `dispatched_at` in the stats sidecar | Viewer | Life bar start | S |
| Optional words on the approve button, saved as an `approval:` comment | Viewer | Your words at every approval | S |
| `issue-cli prove <slug> <criterion> --test / --step / --commit / --value`, stored in the data sidecar and checked by the engine; asked in the testing prompt | New command + agent ask | Criteria → proof matrix, image tags, verdict | M |
| Criteria written as checkboxes with ids, checked at the design gate | Agent ask (validation rule) | Gives `prove` something to point at | S |
| `risk:` comments at shipping for what to watch | Agent ask | Watch list | S |

Local CI (v0.53) also has to reach `origin/main` before "CI after the merge" can read `.ci/runs`.

## Order

1. Timeline tab from today's data, with the CLI calls filter so nothing in the old list is lost.
2. The four small issue-cli changes and the two viewer changes. They make the timeline exact and need no prompt change.
3. Issue tab: Needs-you bar, brief, what you're approving, decisions, folded body.
4. `issue-cli prove` and the criteria-with-ids rule, then the Evidence tab on top.

Open question: should the clilog stay in `$TMPDIR` at all? It is lost on reboot, and the timeline now depends on it.
