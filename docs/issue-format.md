---
title: "Issue File Format"
order: 2
---

## Frontmatter Fields

Every issue is a markdown file with YAML frontmatter.

| Field      | Required | Description                                        |
|:-----------|:---------|:---------------------------------------------------|
| `title`    | Yes      | Issue title                                        |
| `status`   | No       | Workflow stage (see below)                         |
| `system`   | No       | Categorization tag, also used as subdirectory name |
| `version`  | No       | Version string, filterable on the board            |
| `labels`   | No       | List of label strings                              |
| `priority` | No       | `low`, `medium`, `high`, or `critical`             |
| `created`  | No       | Date string for sorting (newest first)             |
| `number`   | No       | GitHub issue number (links to GitHub with `repo`)  |
| `repo`     | No       | GitHub repo in `owner/repo` format                 |
| `assignee` | No       | Free text; dispatched agents use `agent-<slug>`    |
| `type`     | No       | Type of work (`feature`, `tweak`, …) when `workflow.yaml` defines `types:`; picks the issue's path. Unset = `default_type`. Change with `issue-cli set-type` (see [Types of Work](Workflow/types.md)). In projects without types it is a plain custom field. |

`score_boost` and `due` take part in scoring when it's enabled (see [Scoring Fields](#scoring-fields)).

## Status Values

`status` must be one of the statuses defined in the project's `workflow.yaml`; there is no fixed list. The bundled `development` template uses:

- `idea`: raw idea, needs exploration
- `in design`: being designed and specced out
- `backlog`: ready to work on
- `in progress`: actively being implemented
- `testing`: automated tests written and run
- `human-testing`: waiting for a human to verify by hand
- `documentation`: being documented
- `shipping`: being merged and released
- `done`: completed

`issue-cli process workflow` prints the statuses of the current project.

## Example

```markdown
---
title: "Add dark mode toggle"
status: "idea"
system: "UI"
priority: "low"
labels:
  - enhancement
  - ux
---

Allow users to switch between dark and light themes.

## Requirements

- [ ] Toggle button in header
- [ ] Persist preference in localStorage
- [ ] Respect system preference by default
```

## Custom Fields

Any frontmatter key not listed above is preserved as a custom field and displayed in the detail view sidebar. URL values render as clickable links, list values render as bullet lists:

```yaml
pr: "https://github.com/org/repo/pull/456"   # renders as a link
pr_author: "jsmith"                            # renders as text
participants:                                  # renders as a list
  - alice
  - bob
```

Set or clear a custom field with `issue-cli set-meta`:

```bash
issue-cli set-meta <slug> --key waiting --value "blocked on design review"
issue-cli set-meta <slug> --key waiting --clear
```

Workflow-managed fields (`title`, `status`, `human_approval`, `queued_approval`, `approved_for`, `started_at`, `done_at`, `number`, `repo`, `created`, `labels`) are refused — use the dedicated commands (`transition`, `update --title`, etc.) for those.

`queued_approval` is written by the detail page's **Approved for backlog and in progress** button next to `human_approval: backlog`. When the `backlog` approval is consumed by its transition, the queued approval becomes the new `human_approval`, so the agent's next `start` continues into `in progress` without a second wait. Any other transition drops it.

## Scoring Fields

When `workflow.yaml` has `scoring.enabled: true`, the viewer computes a per-issue score from these frontmatter fields:

| Field         | Contributes                                                            |
|:--------------|:-----------------------------------------------------------------------|
| `priority`    | Points from `scoring.formula.priority` map (e.g. `critical: 40`)        |
| `due`         | Urgency under a 30-day horizon, capped by `scoring.formula.due_date.overdue_cap` |
| `created`     | Staleness points at `scoring.formula.age.staleness_weight` per day      |
| `labels`      | Sum of per-label weights from `scoring.formula.labels`                  |
| `score_boost` | Flat additive override (integer, may be negative)                       |

Missing fields contribute 0. See [Workflow](workflow.md#scoring) for the full formula and example config.

## Inline Data Table Marker

A `<!-- data -->` HTML comment in the body marks where the per-issue [data table](data-store.md) renders. The marker can declare the dropdown statuses for that issue:

```markdown
## Findings

<!-- data statuses=🔥 must-fix,👍 nice-to-have,✅ resolved,❌ wontfix tiers=🔴 critical,🟢 nice -->
```

The marker accepts `statuses=` (the row status dropdown) and an optional `tiers=` (a second-axis dropdown such as severity / priority / blast-radius). Both are comma-separated; spaces and emojis are allowed inside a token. Without `statuses=`, the dropdown defaults to `open, resolved`; without `tiers=`, the tier column / dropdown / filter are hidden entirely so existing tables render unchanged. With no marker at all, the table renders below the body when entries exist.

The table reads from a sidecar JSON file (`<slug>.data.json`) — manage rows via `issue-cli data add | list | set-status | set-tier | set-comment | remove`, never by editing the JSON directly.

## Questions

`issue-cli ask` writes structured questions for the owner under `## Questions`, which is created when missing. The issue file is the mailbox: the question and its answer survive even when the asking session ends.

```markdown
## Questions

### Q-3 · open · 2026-10-02 14:05
Does unspent gold carry over between stages?

At PROBE_N=200 teams bank about 140 gold at a stage lock.

- **A** Reset to the stage's starting gold — Simplest. Stage 3 buys look like stage 1.
- **B** (recommended) Carry over, capped at 300 — Rewards saving without snowballing.

Links: docs/economy.md
```

- `Q-n` numbers are per issue. Options are one letter, with the label and tradeoff split by ` — `.
- `issue-cli answer <slug> Q-3 --option B` replaces the whole entry with a pointer, `### Q-3 · answered 2026-10-02 14:46 → B · D-7` followed by the question line, and appends the `D-7` line under [Decisions](#decisions).
- Questions never gate a transition. Approvals stay the gate.

## Decisions

Decisions live as numbered lines under an issue's `## Decisions` section. `issue-cli answer` writes them for answered [questions](#questions), and `issue-cli decide` writes the rest. The viewer's **Decisions** tab (`/p/<project>/decisions`) is generated from these lines, so nobody edits the index by hand.

```markdown
## Decisions
- **D-1** · 2026-10-02 · answered in lead chat (Q-1): Does gold carry over? → **B. Carry over, capped at 300**, "revisit after the economy pass" (also considered: A Reset; C Carry over in full)
- **D-2** · 2026-10-04 · deferred · chat: Balance classes once more elements are in place
- **D-3** · 2026-10-06 · chat: The raid becomes an ET-style objective format, 6v6, "no readable tension" (replaces D-1, sim/gdd-match-phases#D-1)
```

The line format is `- **D-<n>** · <YYYY-MM-DD>[ · deferred] · <how>: <text>[, "<quote>"][ (also considered: …)][ (replaces <ref>, …)]`.

- **IDs are per issue.** `D-n` restarts in each issue, and the global ref is `<slug>#D-n`. Inside the same issue, `D-n` alone also works in `replaces`.
- **Citations.** Any `<slug>#D-n` (or `#<slug>#D-n`, or a viewer URL ending in it) in another issue's body is a citation. Refs inside fenced code, in the recording issue itself, or inside `(replaces …)` don't count.
- **States:**
  - `active`, or `deferred` (marked with `· deferred`).
  - `replaced` once another line names it in `(replaces …)`. The newest replacer wins.
  - `stale` when it is replaced but an open issue still cites it. Open means the issue's status sorts before `done`; `done` and anything after it (e.g. `obsolete`) count as closed.
- **What is skipped.** Only lines under a level-2 `## Decisions` heading and outside fenced code are parsed. Free-form notes in that section are left alone and not listed.
- **Revising.** `issue-cli show` prints `== Cites replaced decisions ==` for an issue that still builds on a replaced decision.

## Balance notes

A balance note is a comment on the issue whose change it reports on, starting with the project's balance prefix (`workbench.balance.comment_prefix` in `workflow.yaml`, default `balance:`). `issue-cli balance` writes the typed form:

```
balance: full-holds · open
hypothesis: Defenders revive too freely because nobody finishes a downed attacker.
change: FinishChance ↑ (proposed)
evidence: finish rate 0% across 200 matches
verdict: Needs the finish action first.
pass: 3
```

- **First line:** `<prefix> <metric> · <state>`. The metric is one token (letters, digits, `.`, `_`, `-`). The state is `open`, `kept`, `reverted`, `needs pieces` or `not this branch`.
- **Other lines:** optional `hypothesis`, `change`, `evidence`, `verdict` and `pass` (an integer). Any other line is shown as a note.
- **Latest wins:** notes with the same issue, metric and change (case- and space-insensitive) are one experiment. The newest comment is current, and the older ones are its history.
- **Free text:** a balance comment that doesn't match the first-line shape is an *unsorted* note. A leading `[#slug]` names the source issue.
- **Joint passes:** an issue with frontmatter `balance_pass: <n>` is a pass. It is past once it is `done` (or later), and the lowest open pass is the planned one. A note counts towards the pass in its `pass:` line; open and needs-pieces notes without one count towards the planned pass.

The viewer's **Balance** tab (`/p/<project>/balance`) is generated from these comments and is shown only when `workflow.yaml` has a `workbench.balance` block.

## Images

Standard markdown images render in issue bodies and doc pages:

```markdown
![Circuit map](../../docs/artifacts/road-between-tourneys/preview.png)
![Remote](https://example.com/shot.png)
```

Relative paths resolve against the markdown file's own directory, the same as on GitHub or in an editor. They are served from `/p/<project>/files/<path>`, where `<path>` is relative to the project root (`workdir`, or the parent of the issues directory). Only image files (`png`, `jpg`, `jpeg`, `gif`, `webp`, `svg`, `avif`) inside the project root are served; anything else returns 404 and the `src` is left as written. Images pasted into the create-issue form are stored under `<issues>/attachments/`.

## File Organization

Issues can live flat in the issues directory or in subdirectories by system:

```
issues/
  UI/
    add-dark-mode.md
  API/
    rate-limiting.md
  fix-typo.md
```
