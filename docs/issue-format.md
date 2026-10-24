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
| `created`  | No       | `YYYY-MM-DD`, written when the issue is created; sorts newest first, and the `criteria_proven` gate exempts issues dated before its date |
| `number`   | No       | GitHub issue number (links to GitHub with `repo`)  |
| `repo`     | No       | GitHub repo in `owner/repo` format                 |
| `assignee` | No       | Free text; dispatched agents use `agent-<slug>`    |
| `slug`     | No       | Pins the issue's slug. Normally the slug comes from the title (slugified, with the system folder lower-cased in front), not from the file name. `issue-cli update --title` and the web title editor write the old slug here, so a retitle keeps links and references valid. `update --rename` removes it. |
| `type`     | No       | Type of work (`feature`, `tweak`, …) when `workflow.yaml` defines `types:`; picks the issue's path. Unset = `default_type`. Change with `issue-cli set-type` (see [Types of Work](Workflow/types.md)). In projects without types it is a plain custom field. |

Epic links are custom fields that the [issues canvas](UI/overview.md#issues-canvas) reads:

| Field      | Description |
|:-----------|:------------|
| `parent`   | Slug of the epic or umbrella this issue belongs to, e.g. `core/search-everywhere`. Slugs come from the title (or a `slug:` pin), with the system folder lower-cased, not from the file name. |
| `umbrella` | Legacy spelling of `parent`; read when `parent` is unset. |
| `base`     | An epic's staging branch (children branch from it and ship into it). Shown on the epic's canvas row. |

Dependency links are a custom field too:

| Field        | Description |
|:-------------|:------------|
| `depends_on` | The issues this one waits on, as a YAML list or a comma-separated string: `issue-cli set-meta <slug> --key depends_on --value "economy/shop-prices, ui/other"`. `set-meta` writes two or more slugs as a YAML list and one as a string; the value replaces the old one, so pass every slug the issue waits on. It refuses unknown slugs (and suggests close ones) and the issue itself. The detail page shows **Depends on** and, on the other issue, **Depended on by**; `issue-cli show` prints both lines, plus **Decided by** for the `slug#D-n` decisions the body cites. |

Blocking markers are custom fields (and one label) that the [workboard](UI/overview.md#workboard) shows as red chips on the card and on system-page rows:

| Field / label    | Description |
|:-----------------|:------------|
| `blocked_on`     | The issue (or a YAML list of issues) this one can't move without: `issue-cli set-meta <slug> --key blocked_on --value ui/shop-screen`. A red **blocked on <slug>** chip links the target while it is open. Once the target is done, it turns into a grey struck-through **<slug> done**, and you can remove the field. An unknown slug shows **(not found)**. |
| `needs_revision` | Why the issue must be revised before it moves on, e.g. a decision ref: `needs_revision: "D-5"`. Shows a red **needs revision · D-5** chip. |
| `needs-revision` label | The same chip, without a reason. |

They don't block transitions yet. A validator and CLI commands for them are `workflow/blocking-markers-needs-revision-blocked-on-and-deferred`.

Metric and knob files, and the `metrics:` / `knobs:` lists that tie them to a system, are configured in `workflow.yaml`'s `workbench:` block: see [Workbench Files](workbench-files.md).

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

`created` is the one exception: `set-meta <slug> --key created --value YYYY-MM-DD` dates an issue that has none (what `issue-cli health lint`'s undated warning asks for), and refuses to change or clear an existing date. New issues get `created: "<today>"` from `issue-cli create`, `report-bug`, CI's red issue and the viewer's New issue form.

Every single-value write (`set-meta`, `update --title`, the detail page edits, transition fields) stores the value as one double-quoted YAML line, escaping `\`, `"`, newlines, carriage returns and tabs. Any value reads back exactly as written, including `$1`, `${x}`, `: #` or a leading `-`.

Setting or clearing a key replaces its whole old value, whatever its shape: the indented list items, block scalar (`|`, `>`) or nested map under it all go, up to the next top-level key. List writes (`labels`, a multi-slug `depends_on`) store each item as a quoted `  - "…"` line, in place.

Every frontmatter write parses the result before it touches the file. If the YAML would not parse, nothing is written and the command fails, naming the keys it was setting:

```
failed to set frontmatter: <file>: not written: frontmatter would not parse after setting depends_on: yaml: …
```

The same check covers `issue-cli create`, the web create form and the GitHub import (`not written: frontmatter would not parse: …`). A file whose frontmatter is already broken is skipped on load (with a `warning: skipping …` line), so the CLI and the viewer can't find it; fix it in an editor.

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

## The brief

The issue page's Issue tab leads with one paragraph, the brief, and `issue-cli show` prints it as `Brief:`. `tracker.Brief` takes the first of these:
1. a paragraph opening with `**In short:**` (the label is dropped);
2. the first prose line under `## Bug`;
3. the body's first prose paragraph before any `##` heading;
4. the first prose paragraph of the first section.

Tables, lists, quotes, images, code and HTML comments are skipped. Past 280 characters the brief ends at the last sentence end inside the limit, else at a word with "…". With none of these, the tab shows the title alone. To steer what the owner reads first, open the body with a lead paragraph, or start one with `**In short:**`.

## Decisions

Decisions live as numbered lines under an issue's `## Decisions` section, written by `issue-cli decide`. Questions themselves stay in chat; only the outcome is recorded. The viewer's **Decisions** tab (`/p/<project>/decisions`) is generated from these lines, so nobody edits the index by hand.

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

## Comments

Comments live in an `<!-- issue-viewer-comments … -->` block at the end of the file, written by `issue-cli comment` and the issue page. Each has an id, text, status, source and `date`. Since v0.54 `date` is a full RFC3339 timestamp (`2026-10-23T14:05:07+02:00`); older comments have the day only (`2026-10-23`). Both forms are read, and pages and `issue-cli show` show the day.

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

## Systems and families

The `system` field groups issues into a **system page** (`/p/<project>/systems/<System>`), and every distinct value gets one even when nothing is configured. To add a summary, a design doc and **families**, give the system an entry under `workbench.systems` in `workflow.yaml`. A family is a group of mechanics inside a system: an umbrella issue, whose children (`parent: <umbrella>`) in that system make up the family page.

```yaml
workbench:
  systems:
    UI:                                  # matches system: "UI" (case-insensitive)
      summary: "Templates, views and client-side behaviour"
      design_doc: docs/UI/overview.md    # relative to the project root
      metrics: [page-load]               # guard metrics for this page (metric views)
      knobs: ["Theme.*"]                 # tuning knobs, globs allowed
      families:
        Workbench:
          umbrella: ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index   # required
          summary: "The workbench redesign"
          design_doc: ""                 # empty: the system's doc applies
```

Paths must stay inside the project, and a family without an `umbrella` is rejected. `issue-cli process schema` lists every key. A design-doc section that cites a replaced decision (`<slug>#D-n`) is marked stale on the system page. See [System pages](UI/overview.md#system-pages).

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
