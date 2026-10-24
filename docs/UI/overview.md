---
title: "UI Overview"
order: 1
---

## Scope

The UI system covers HTML templates, CSS styling, and client-side JavaScript for all web views.

## Key Files

- `templates/list.html` — filterable issue list
- `templates/workboard.html` — the board (`/workboard`): read-only cards with markers, epic progress and the agent footer (`handlers_workboard.go`; see [Workboard](#workboard))
- `templates/_markers.html` — card marker partials: `markers`, `marker-chips`, `epic-progress`, `agent-footer`, `card-legend` (data from `markers.go`; agent state from `agent_state.go`)
- `templates/board.html` — the classic kanban board with drag-and-drop (More › Classic board)
- `templates/detail.html` — issue page: title, the Issue · Timeline · Evidence tabs (`?tab=`), and on the Issue tab the folded body with comments and the sidebar with dispatch (see [Issue page tabs](#issue-page-tabs))
- `templates/_issue_tab.html` — the Issue tab's band, main-column cards and side-column cards (`issue_tab.go` builds them; see [The Issue tab](#the-issue-tab))
- `templates/_timeline.html` — the Timeline tab (`timeline.go` builds the view on `internal/timeline`'s `Split`, `timeline_sources.go` reads its files; rules in [Workflow Stats](../workflow-stats.md#timeline-tab))
- `templates/graph.html` — workflow status graph
- `templates/agents.html` — agents canvas: lead/worker tree, unfinished jobs, selected-agent panel (handlers in `handlers_agents.go`)
- `templates/canvas.html` — issues canvas: version columns × epic or system rows, pan and zoom (`handlers_canvas.go` builds it)
- `templates/docs.html` — documentation viewer with sidebar navigation
- `templates/_sidebar.html` — the project sidebar on every project page (see [Navigation and theme](#navigation-and-theme)); `nav.go` lists the views
- `templates/_create_modal.html` — shared "New issue" modal partial included by list and board
- `templates/_project_actions.html` — the project-action dispatch modal, included by the sidebar
- `templates/balance.html` — balance board (`handlers_balance.go`, board built by `tracker.BuildBalanceBoard`). Its sidebar entry shows only when `workbench.balance` is set
- `templates/status.html` — the project Status page (`handlers_status.go`; the model is `buildStatus` in `status.go`, the "waiting on you" rule is `attention.go`)
- `templates/page.html`, `templates/_status_tabs.html` — the frame around a project's own HTML page (`workbench.status_page` / `pages`), and the Project status / Built-in tabs (`handlers_pages.go`). See [Project pages](#project-pages)
- `templates/updates.html`, `templates/updates_overview.html` — the Updates tab and `/updates` (`updates.go`; the plan is `internal/workflowupgrade.BuildPlan`). See [Updates tab](#updates-tab)
- `templates/decisions.html` — generated decisions index (`handlers_decisions.go`, index built by `tracker.BuildDecisionIndex`)
- `templates/systems.html`, `templates/system.html` — systems index and the system / family pages (`handlers_systems.go`; the model is built in `systems.go`, the design doc panel in `docsections.go`). See [System pages](#system-pages)
- `templates/metrics.html` — Metrics view: guard metrics, knobs, branches (`handlers_metrics.go`; readers in `internal/tracker/metrics.go`, `knobs.go`, `branches.go`, `deps.go`)
- `templates/_metrics_panels.html` — the `system-guard-metrics` and `system-knobs` partials a system page embeds, fed by `systemMetricsPanels` (`handlers_metrics_panels.go`)
- `static/style.css` — all CSS: theme tokens for dark, Dracula and light, then the shared pieces and each view
- `static/fonts/` — Archivo and JetBrains Mono (woff2, OFL), served by the viewer itself
- `internal/testids/testids.go` — every `data-testid` the templates carry (playtest targets)

## Navigation and theme

Every project page has the left sidebar from `templates/_sidebar.html`:

- **All projects** link and the **project block**: name, version (projects.yaml `version`) and open-issue count.
- **New issue**. On list and board it opens the create modal; elsewhere it links to `/?new=1`, which opens the modal on the list.
- **Views**: Status (with an amber count of what waits on the owner), Systems (when any issue has a `system`, or `workbench.systems` is set), Board (the [workboard](#workboard), `/workboard`), List, Balance (when `workbench.balance` is set), Metrics, Decisions, Updates (when the project's `workflow.yaml` is based on a bundled template, with an amber count of features to decide) and Docs, then a **More** group with Classic board (`/board`, drag-and-drop), Graph, Canvas, Agents, Retros, Stats and GitHub (when `supports_github` is set).
  - The current view has `aria-current="page"`.
  - More opens on load when the current view is in it; otherwise the browser remembers it open or closed (`localStorage` key `nav.more`).
- **N agents running**, linking to Agents. It is hidden at 0, and `auto-refresh.js` updates it.
- A **screen panel**. The Board shows the card legend, then a Systems list (open counts, each a `?system=` filter on that board); the Classic board shows the Systems list. The systems pages show the same list linking to the system pages, with configured families indented under their system and the current one marked.
- **Actions** (Spawn lead and the `project_actions:` buttons) and the **theme swatches**, at the bottom.

Below 900px the sidebar stacks above the page. There is no phone menu yet.

**Adding a view.** Add one entry to `navViews` in `nav.go` (key, label, path, `More`, optional `Show`), then start the template with:

```html
<body>
<div class="app">
{{template "sidebar" (sidebar .Prefix "<key>")}}
<div class="app-main">
  …the page…
</div>
</div>
</body>
```

The `sidebar` template func builds the `Nav` for the page's project, so page data only needs `Prefix`. A page with its own screen panel renders `sidebar-open`, the panel, then `sidebar-close`. `TestProjectPagesRenderSidebar` fails for a project page without the sidebar.

**Theme tokens.** `static/style.css` starts with the tokens. The dark default is the workbench look; Dracula and light set the same names under `[data-theme]`:

- surfaces: `--bg`, `--bg-sidebar`, `--bg-secondary` (cards), `--bg-tertiary` (hover), `--bg-active`
- lines: `--border`, `--border-subtle`
- text: `--text`, `--text-nav`, `--text-secondary`, `--text-link`, `--accent`
- four tones for chips and state: `--amber`, `--blue`, `--green`, `--red`, each with `-bg` and `-border`
- fonts: `--font-sans` (Archivo), `--font-mono` (JetBrains Mono)

Use the shared classes rather than new colours:

- `.page-head`, with `.page-title` and `.page-sub`
- `.label`
- `.chip`, plus `.chip-amber`, `.chip-blue`, `.chip-green` or `.chip-red`
- `.card`
- `.btn` and `.btn-primary`

Status colours stay per status (`statusColor`). A status chip tints its colour with `.status-chip` and `--c`.

Charts are inline SVG from `internal/chart`, drawn on the server: marks use `currentColor`, and a series class (`s-agent`, `s-waiting`, `s-idle`, `s-feature`, `s-bugfix`, `s-tweak`, `s-none`, …) maps to the theme's `--chart-*` token. Each chart has `role="img"`, an `aria-label`, a `<title>` per mark and a table view. The stats page is the first user; see [Workflow Stats: Stats page](../workflow-stats.md#stats-page).

## Views

| Route             | Template      | Description                                     |
|:------------------|:--------------|:------------------------------------------------|
| `/`               | `list.html`   | Filterable list with status, system, priority    |
| `/workboard`      | `workboard.html` | The board: cards with markers, epic progress and the agent footer, read-only. See [Workboard](#workboard) |
| `/board`          | `board.html`  | Classic kanban with configurable columns, card fields and drag-and-drop (More › Classic board) |
| `/graph`          | `graph.html`  | Workflow DAG with stale highlighting             |
| `/agents`         | `agents.html` | Agents canvas: leads, workers, their issues (see below) |
| `/canvas`         | `canvas.html` | Issues canvas: versions × epics (or systems), see [Issues canvas](#issues-canvas) |
| `/docs`           | `docs.html`   | Docs with collapsible sidebar sections           |
| `/issue/<slug>`   | `detail.html` | Issue page with tabs: Issue (brief and Needs-you band, folded body, edit, approve, dispatch, comments), Timeline (`?tab=timeline`, `&show=`), Evidence (`?tab=evidence`: is it proven?). See [Issue page tabs](#issue-page-tabs) |
| `/stats`          | `stats.html`  | The stats dashboard: gauges (waiting on you, milestone, big files), trend lines from the health snapshots, where the time goes by status and group, issues done per week, drill-downs to the issues; options `range`, `group` (remembered per project by `static/stats.js`). The time tables, token-cost estimates and CLI usage are folded below. See [Workflow Stats: Stats page](../workflow-stats.md#stats-page) |
| `/stats` (top level, no `/p/<project>`) | `stats_overview.html` | Every project compared from its health snapshots: one row each with waiting on you, cycle p50, rework share, issues done, lines of code, test ratio and big files, with sparklines; `range` (7/30/90 days) and `sort` links |
| `/balance`        | `balance.html` | `?system=` scopes it to one system's issues (the system pages link here). Typed balance notes grouped by metric (state chip, hypothesis / change / evidence / verdict, earlier notes), joint passes from `balance_pass:` issues with **Start pass** (the existing dispatch endpoint), free-text notes under Unsorted. Shown only with `workbench.balance`; otherwise its sidebar entry is hidden and the page explains how to turn it on. Reloads when `/hash` changes, like `/decisions` |
| `/systems`        | `systems.html` | One card per system, most open first: summary and design doc (from `workbench.systems`), open / done counts, status bar, waiting-on-you count and family chips, plus a "No system" line. See [System pages](#system-pages) |
| `/systems/<System>[/<Family>]` | `system.html` | One page per system or configured family. Unknown names get a 404 listing the systems. See [System pages](#system-pages) |
| `/health`         | `health.html` | The Health view, right below Status in the sidebar (with the derived score beside it). See [Health view](#health-view) |
| `/status`         | `status.html` | Where the project stands: progress, work in flight, next steps, what waits on the owner, what shipped. See [Status page](#status-page) |
| `/status`         | `page.html`   | With `workbench.status_page`: the project's own page, with Project status / Built-in tabs. `?view=builtin` shows `status.html`. See [Project pages](#project-pages) |
| `/status.md`      | (text)        | The Status page as markdown (`text/markdown`), for pasting as a stand-up |
| `/status.json`    | (JSON)        | The Status model the frame posts to project pages (shape in [Workbench Files](../workbench-files.md#live-status-postmessage)) |
| `/pages/<n>`      | `page.html`   | The `n`th `workbench.pages` entry in the viewer frame; 404 when out of range |
| `/page-raw/<path>` | (file)       | A project page or one of its assets, sandboxed by CSP; only files under a configured page's folder |
| `/decisions`      | `decisions.html` | Generated index of every `D-n` decision: how it was made, recording issue, citing issues, state (active / deferred / replaced / replaced · stale). System and state chips (`?system=`, `?state=`); reloads when `/hash` changes. The page renders the `/hash` value it was built from as the polling baseline, so a change made before the first 5 s poll still triggers a reload |

| `/updates`        | `updates.html` | Workflow features the project could adopt, with Decline, Re-open and Install with an agent. See [Updates tab](#updates-tab) |
| `/updates` (top level, no `/p/<project>`) | `updates_overview.html` | Every project's template stamp, state, features to decide, record and newest proposal, each linking its Updates tab. The project list shows each project's count and links here |
| `/metrics`        | `metrics.html` | Guard metrics, knobs and branch overlap, read from the project's files and git (see [Metrics view](#metrics-view)). `/metrics.json` returns the same data; `?sig=1` only the reload signature |

In projects whose `workflow.yaml` defines `types:` ([Types of Work](../Workflow/types.md)), the list and board show a type badge and an "All types" filter (`?type=`), `type` joins the default board card fields, `/graph?type=<t>` draws that type's path, and the detail sidebar has a **Type** select (`POST /p/<project>/issue/<slug>/type`; a `409` with `statuses` asks the human which status on the new path to move to). Without `types:` none of these controls render.

## Workboard

`/p/<project>/workboard` is the sidebar's **Board**. It is a read-only kanban whose cards carry coordination state, rebuilt from the repo files on each request; it reloads when `/hash` changes (not while the search box has text). Writes stay where they were: the classic board (drag-and-drop), the detail page and issue-cli.

**Card.**
- **Head:** system chip, `epic` chip, and the time in the current status. That time comes from the stats sidecar's last transition into the status (tooltip "since"), else the file mtime ("last file change"). It turns amber after 24 h, except in the first status, done and global statuses (parked).
- **Title:** links to the issue.
- **Epic progress:** a child shows "N of M · in <epic>"; an epic shows "N of M children done", with a bar.
- **Markers** (`cardMarkers` in `markers.go`):

  | Chip | Tone | When |
  |:--|:--|:--|
  | `approve <status> · 2 h` | amber | the next transition's approval is the only thing missing (`GateInfo.Ready` from `attention.go`); hidden while a playtest or check script is due |
  | `playtest · 30 m` | amber | human-testing with a playtest session to judge |
  | `run checks · 30 m` | amber | human-testing with a [guided check script](../patterns/guided-check-scripts.md) and no owner run (`Attention.ChecksDue`); the tooltip names `issue-cli check-script <slug>` |
  | `question` | amber | the newest comment starts with `question:` |
  | `blocked on <slug>` / `<slug> done` / `(not found)` | red / grey struck / red | `blocked_on` (see [Issue format](../issue-format.md)) |
  | `needs revision[ · why]` | red | the `needs-revision` label or the `needs_revision` field |
  | `parked · <reason>` | grey | status parked; the reason is the first line of `## Parked Record` |

- **Agent footer** (`agent_state.go`, the same rules as the [agents canvas](#agents-canvas)): links to `/agents?agent=<session>`, and its tooltip names the last issue-cli command.
  - green **agent working · last call N ago**;
  - grey **agent idle**: live, but no issue-cli call for 15 minutes;
  - amber **waiting on your approval / playtest / checks**, **asked you a question**;
  - hollow **agent stopped · not running**: unfinished work with no session.

  An unready gate counts as working, not waiting.

**Scale** (D-10: readable with hundreds of issues):
- **Idle piles fold.** The first status, global statuses and done become count tiles that link to the list filtered on that status (`/?status=…`). A tile shows "N waiting on you" when any of its issues wait on the owner.
- **Needs attention by default.** A card is *quiet* when it has no marker and no agent, is not an epic, and is not old in an active status (`in progress` up to done). Quiet cards are hidden. Each column links them as **+N quiet**, and **Show all** (`all=1`) draws them as one-line rows (title and age).
- **Cap.** A column draws 12 cards, then links to the rest: **N more in the list** (or in the epic, in the epic view).

**Controls.**
- **Group by:** Status, System, or Epic (`group=`). The epic view gives one column per open epic with open children, the most first, then "No epic". System and epic views show only the waiting cards of the idle piles.
- **Filters:** the version select (only when an issue has a version), `system=` (from the sidebar), **Waiting on you · N** (`waiting=1`), Show all, and an in-page search.
- **Order in a column:** waiting on you first, then blocked, then the longest in status.

**Reuse.** A view that lists issues builds a `markerCtx` (`newMarkerCtx`, or `newMarkerCtxFor` for a subset) and renders `{{template "markers" .Markers}}` (a row) or `{{template "marker-chips" .Markers}}` (bare chips; the [system pages](#system-pages) use it).

Test ids: `workboard.*`, `marker.<kind>` and `nav.panel.legend`. The sessions `workboard-markers` (the `workboard` scenario) and `workboard-scale` (~520 generated issues) cover it.

## System pages

`/systems/<System>` joins one system's issues, families, design doc, decisions and balance notes on one read-only page. A family page, `/systems/<System>/<Family>`, is the same page scoped to an umbrella's children inside that system (ui/workbench-sidebar-and-system-pages D-1). The whole page is rebuilt from the repo files on each request, and it reloads when `/hash` changes.

- **Which systems.** A system is every distinct issue `system` value, matched case-insensitively, plus every key under `workbench.systems`. A project without the block still gets a page per system (D-2). Summary, design doc and families need the block, and an unconfigured page shows a hint naming `workbench.systems.<name>`.
- **Header.**
  - the summary;
  - chips: open / done counts (side statuses such as `obsolete` are counted separately), one chip per family (or the system's `umbrella`), and **N waiting on you** from `waitingOnOwner` in `attention.go` (the status page's rule: gate ready, open question, playtest to run);
  - links: the design doc, Board and List filtered by `?system=`, and the decisions count.
- **Issues.**
  - The status bar runs in workflow order without side statuses.
  - Chips are toned by position: the final status green, the first neutral, the first half blue, the second half amber.
  - Rows are ordered furthest along first, then by the newest status change. A system page shows 8 rows and a family page 30, with a footer counting the rest and linking to the list.
  - Each row carries the [workboard](#workboard)'s marker chips (waiting on you, blocked on, needs revision, parked).
- **Families and epics.** Configured families come first, with progress over their children in this system. Then come the epics that the system's issues name in `parent:` (with the whole-epic count when an epic spans systems), and finally the number of open issues in neither.
- **Design doc sections.** The doc is read from the project root and split on `## ` headings (frontmatter dropped, fences ignored). Each section is marked:
  - **stale** when it cites a `<slug>#D-n` that the decisions index marks replaced;
  - **changed / new / removed on branch** when an open branch (`tracker.OpenBranches`) edited it since its merge-base. The branch's copy is compared with the merge-base copy, not with main's, so an old branch that is behind main marks nothing;
  - **current** otherwise.

  Results are cached by branch head. Section names link to the heading ids that doc pages now render (goldmark `WithAutoHeadingID`). A doc that exists only on branches is reported as such.
- **Recent decisions.** The 3 newest indexed decisions of the members, with "All N" linking to `/decisions?system=`. When none are indexed but members keep free-form `## Decisions` sections, the panel points at `issue-cli decide`.
- **Balance notes.** Up to 3 open or unsorted `balance:` notes. They link to `/balance?system=` when the Balance view exists, and the panel is hidden without notes.
- **Guard metrics and knobs.** `systemMetricsPanels` (`handlers_metrics_panels.go`) builds them from the system's (or family's) `metrics:` / `knobs:` lists. They render through the `system-guard-metrics` / `system-knobs` partials in `templates/_metrics_panels.html`, and are left out when the project has no metric or tuning source or the system lists none.

Config, in `workflow.yaml`:

```yaml
workbench:
  systems:
    UI:
      summary: "Templates, views and client-side behaviour"
      design_doc: docs/UI/overview.md
      families:
        Workbench:
          umbrella: ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index
```

`issue-cli process schema` lists every key (`WorkbenchSystem`, `WorkbenchFamily`).

## Status page

`/p/<project>/status` is the first sidebar entry. It answers "where are we" without asking the lead. It is read-only, and everything comes from repo files. `buildStatus` (`status.go`) holds the rules and is tested without HTTP; the grouping and the caps are in `status_groups.go`.

**Layout.** A row of three tiles, then three rows on one shared grid (`.status-grid`, gap 20 px), so card edges line up down the page:

- **1200 px and up:** the tiles' grid, `repeat(3, minmax(0, 1fr))` (workflow/health-scores-…#D-10). Widths per row, normalised to 3: tiles 1 1 1, row 1 1 1 1, rows 2 and 3 2 1.
- **901–1199 px:** a third would be too narrow for the side cards, so the grid is a main column and a 320 px side one. Local CI takes the side for both rows of row 1 at its own height, and Health goes under Current milestone.
- **900 px and under:** one column, in DOM order.

A row with a card missing (no `ci:` block, no health snapshot, nothing waiting for release) gives the free column to the first card, and a lone card spans the row. What needs the owner or is running comes first.

| Row | 1200 px and up | 901–1199 px |
|:--|:--|:--|
| Tiles | Needs you · Working on · What's next (`status.strip`) | the same |
| 1 | Current milestone · Health · Local CI | main: Current milestone, then Health; side: Local CI |
| 2 | Epics (2) · Waiting for release (1) | main · side (320 px) |
| 3 | Recently shipped (2) · Versions, Recently released (1) | main · side (320 px) |

**Health card** (`status.health`, `healthCard` in `health_view.go`). Only shown with a health snapshot; it reads the newest one, however old. It holds:

- **Derived** and **Agent opinion:** each the number, a thin bar (blue / amber) and one line, either the 7-day change (`▲ +2 in 7 days`, `no change yet`) or the review's age (`reviewed 3 days ago`; over 7 days `review 9 days old` in amber with a clock icon).
- **Weakest:** the lowest-scoring component with its measured value.
- **Recommendations:** "N recommendations wait for you", linking to the Health view's review.
- **Lint:** the snapshot's lint count (`3 lint warnings`, `status.health.lint`), linking to the Health view's Lint section. It is missing when the snapshot predates the lint family.
- **Privacy lines** (`status.privacy`, `privacy_view.go`, `templates/_privacy.html`): one line per badge, Work files then Code (`status.privacy.line.<kind>`, `data-level`). Green and grey are one quiet line; amber and red a tinted box with the sentence and its fix, as code or as a link to the Updates tab (`status.privacy.setup.<kind>`). Without a health snapshot they get their own small Privacy card (`status.privacy_card`).
- **Open Health →**

**Privacy badges after the title** (`status.privacy.badge.work`, `.code`, `data-level` green / amber / red / grey; a dashed border while not set up). Each links to its Health card line, and its `title` and `aria-label` carry the full sentence ("Work files private: ai-workflow-engine-tracker, last backup 12m ago"). The states are in [Workflow: Private work and code](../workflow.md#what-status-shows). All local (`git ls-files`, the tracker's git folder and sync file, the public branch's remote-tracking ref), cached a minute per project. The projects list shows the same two markers on each card (`projects.privacy.<kind>`), and the `/stats` overview a Privacy column (`stats.overview.privacy.<kind>`) and the counts under its title (`stats.overview.privacy`). `status.md` gets one line per badge after the summary line.

`status.md` gets a `## Health` section with the same facts, including `- Lint: 3 lint warnings · issue-cli --project <slug> health lint`, and `issue-cli health review --spawn` when the review is missing or old.

**Tiles** (D-6). Each of Needs you, Working on and What's next is a `<details class="status-pop">` (`status.<card>.pop`). Its summary shows the title, a count and one line (`NeedsSummary`: "7 to approve · 1 question"; `WorkingSummary`: "3 agents working · 9 without an agent · lead not running"; `NextSummary`: "the lead's ## Next · 8 backlog · 6 parked"). Needs you is amber while anything waits. Clicking a tile opens the full card in a panel positioned over the page (absolute, `z-index` 40, at most 70vh, scrolls), so nothing below moves. Only one is open at a time. A click outside, focus leaving it, or Escape folds it back. The open state is not saved.

**Side cards fit their row** (D-7). In the rows under the milestone the main card sets the row's height (at least 240 px); the side column takes that height and adds none (`height: 0; min-height: 100%`), so there are no gaps. The last side card fills what is left; when space is short the cards above it shrink too, to 120 px at least. A card whose content doesn't fit is cut off (`overflow: clip`, so focus can't scroll it) under a fade and **Show all** (`status.<card>.fit`). That opens the whole card over the page (at most 80vh, scrolls), ending in **Show less**; a click outside, focus leaving it, or Escape folds it back. A `ResizeObserver` re-measures when a fold opens or the window resizes. On a phone the cards take their natural height.

**Every list is capped** (the `cap…` constants in `status_groups.go`, read by the template as `.Cap`): it shows its first N rows, then the rest sit in a `<details class="status-more">` "+K more" that opens in place, without JS. The caps: Needs you 5 per kind, Working on 8, in-design agent chips 6, backlog and parked 5 each, epics per area 8, Versions 8, Unreleased 5, and the release-wait open list folds whole past 5 (10 rows inside, then "+K more"). The template splits a list with `head N list` / `rest N list` (`template_funcs.go`).

**Remembered per project.** Every fold (`details[data-fold]`) and the Epics tab, version and text filter are kept in `localStorage` under `status:<project>` as `{tab, version, q, open}`. Every read and write is in try/catch; without storage the page uses the defaults (Active, the current milestone, no text, the server's open state). A `<details open>` fires `toggle` while it is parsed, so the script records the state it set in `data-was` and saves only a real click.

- **Header:** "<project> · done/total done · N epics in K areas · as of …".
- **Current milestone** (`status.milestone`, `MilestoneView`), the version Waiting for release cuts (`pickMilestone`):
  - done/total and the percentage
  - one bar split by status (`status.milestone.segment.<status>`): done green, then each open status furthest along first in its `statusColor`; the workflow's first status (idea) is the empty track
  - a legend with counts, and "Open by area" chips (top 5, then "+N areas" with the rest in its tooltip)
  - with no milestone (no `version:` set) the card is "Where we are": the All issues bar and the no-versions hint (`status.no_versions`)
- **Epics** (`status.epics`, `EpicsView`): open epics grouped by **area**, the epic's `system` lower-cased (else its slug prefix, else `other`).
  - One panel per version that has epic children (`status.epics.panel.<version>`), the current milestone first, then "all". A version's panel counts **only the children in that version** (D-3), so the area totals add up to the milestone's. The server renders every panel and hides all but the current one; the Version select swaps them.
  - Tabs: **Active** (an epic with a done child; the default), **Not started**, **All**, with counts per panel. Under Active an area with nothing started stays as a folded row, so the page still shows what exists.
  - The text filter matches area names, epic slugs and titles, and opens matching areas without saving that.
  - Area rows (`status.epics.area.<area>`) show a chevron, a bar, done/total and the epic count; areas with progress come first (most done first) and start open, the rest by size, folded. Inside, epics are furthest along first.
  - `status.md` shows the current panel: one line per active area with its epics (5), then the not-started areas on one line (8).
- **Recently shipped** (`status.shipped`), on the same column grid as the Epics rows: issues that reached the final status in the last 7 days, grouped by area, largest first, with a bar per area. The largest group starts open with its newest 3 and "Show K more"; past 5 areas the rest merge into "N more areas". **Full list** links `/?status=done`.
- **Versions** (`status.versions`): a bar per `version:`, the current milestone marked "· now".
- **Recently released** (`status.released`): the newest 3 releases from the changelog (or git tags), each with its version in green mono, the date and the summary clamped to 3 lines. Summaries render their inline markdown (bold, code, links; raw HTML dropped) as `Release.SummaryHTML`; `status.md` and `status.json` get plain text (`SummaryText`, clipped at 140). With none: "No releases yet: vX.Y.0 will be the first", then a hint to point `workbench.changelog` at the changelog (`status.released.hint`). The hint is left out when the `features:` record declines `status-changelog`.
- **Working on** (`status.working`, in its tile's panel):
  - every issue from in progress up to (not including) done: title, status chip, time in status and agent state from the agents model
  - the time is the stats sidecar's last transition into the status; without one it is the file's mtime, marked `≈`
  - the time turns amber at 7 days and red at 14
  - the project lead's state
  - chips for live agents on issues not yet in progress
- **What's next** (`status.next`, in its tile's panel): the `## Next` section of the project lead's newest handoff note (`tracker.NewestHandoffNote`), rendered as markdown, with slugs in code spans linked to their issues. Then the backlog by priority, then parked issues with the first line of their `## Parked Record`.
- **Needs you** (`status.waiting`, in its tile's panel; `waitingOnOwner` in `attention.go`, shared with the board markers and the system pages), amber while anything waits, each gate with an amber Review button:
  - gates whose approval is the only unmet requirement (`WorkflowConfig.ReadyForApproval`, which skips `command_succeeds`)
  - human-testing issues with a playtest session (`issue: <slug>`) and no `-judge` run
  - human-testing issues with a guided check script (`# issue: <slug>`, in the worktree or the project) and no owner run record (`.agent-logs/checks/<name>/*-owner.txt`): the **Checks** list, each row with ▶ Run checks linking to the issue page; "N to check" in the tile
  - issues whose newest comment starts with `question:`
  - issue-cli bug reports queued on this machine because `report-bug` found no engine project (`status.waiting.cli_bugs`, `clibug.PendingCount`): one row naming the count and `issue-cli report-bug --send`, one more on the waiting count and the sidebar's Status number, and a line in `status.md`
  - a footer counts gates whose checklist is still open
  - the sidebar's Status count is the same number
- **Local CI** (only with a `ci:` block; `ci_view.go`), in the side column beside Current milestone:
  - a state chip (green, red, amber when CI can't run) and the one-liner `issue-cli ci status` prints
  - **Last runs:** the last 12 default-tier runs as boxes, oldest first (`status.ci.run.<sha>`, `data-state`), with a dashed box for a live run; each tooltip has the commit, result, age, duration and what failed
  - while red: the failing command and tests, the issues that landed since the last green run (and how many commits had no issue), the bugfix issue with `on_red: file_issue`, and the log tail in a `<details>`
  - red CI is the first entry under Waiting on you (`status.waiting.ci`) and adds one to the waiting count and the sidebar's Status number; `status.md` gets a `Local CI:` line, the tail while red, and the waiting entry
  - the same state gives the merged issues a red `ci-red` card marker (`markers.go`, `ciMarkers`), which done workboard cards get too
- **Waiting for release** (`release_wait.go`), in the side column beside Epics:
  - **Milestone:** the lowest issue `version:` after the newest released version (the top `## vX.Y.Z` changelog heading, else the newest git tag). `0.53` and `v0.53.0` compare equal. Parked issues don't count, and a version with only parked issues is skipped. With no release yet, it is the lowest version with an open issue. It shows done/total, its open issues (furthest along first, with status and assignee), and a chip: amber `vX.Y.0 not ready` or green `ready to cut`.
  - **Checks** (`status.release_wait.check.<key>`, `data-ok`):
    - `issues`: every milestone issue done.
    - `ci`: only with a `ci:` block. Local CI is green and its green commit is the branch head, read from `.ci/state.json` (nothing re-runs). When it fails, a red second line says why (red since, head not tested yet, can't run, no run yet), with a link to the Local CI card.
    - `changelog`: only with a changelog. At least one entry under `## Unreleased`.
  - **Ready to cut vX.Y.0** (`status.release_wait.ready`), when every check passes: rename `## Unreleased` to `## vX.Y.0 — <today>` with a focus summary, then push with the owner's go-ahead.
  - **Release candidates** (`release_candidate.go`), only with `workbench.release_candidates: true` ([Roadmap: Cutting a release](../roadmap.md#cutting-a-release-candidate-soak-publish)). The card never shows Ready to cut vX.Y.0. Instead:
    - **Candidate** (`status.release_wait.candidate`), folded to one summary line (`.candidate.summary`: tag, cut age, merges since, ran by N of M, open reports, a red "not the candidate"); a click opens the rows (`.candidate.fold`, remembered like the card's other folds). The newest local tag `vX.Y.0-rc.N` (`git for-each-ref refs/tags/vX.Y.0-rc.*`, numeric order), when it was cut, and the commits after it that touch more than the changelog (`git rev-list --count <tag>..<head> -- . ':(exclude)<changelog>'`). Both calls are vars for tests.
    - **Check `soaked`:** nothing merged after the candidate but the changelog. When it fails, it says `N commits since rc.K: cut rc.K+1`. Once `release/vX.Y` exists (`issue-cli release cut`), the count is on that branch ("Nothing picked onto release/vX.Y after rc.K") and the hint adds `issue-cli release cut vX.Y.0`.
    - **Check `picked`:** only with the release branch. Every done milestone issue's `work/<slug>` commits are on it, by patch (`git branch --no-merged`, then `git cherry` for those; `gitUnpicked`, a var for tests). When it fails it lists `issue-cli release pick <slugs>`.
    - **Ready to cut release candidate …** (`status.release_wait.cut_rc`, chip `ready to cut vX.Y.0-rc.N`): every other check passes and there is no candidate yet, or merges came after the newest one. The steps: `issue-cli release cut vX.Y.0` (the branch and its worktree the first time, the rc tag, never pushed), install from the release worktree, restart, tell the other leads; fixes are picked onto the branch.
    - **Soaking** (`status.release_wait.publish`, chip `vX.Y.0-rc.N soaking`): a candidate with every check met. It says to publish only on the owner's ok, then, with `privacy.code: snapshots`, `cd <release worktree> && issue-cli release publish vX.Y.0`; otherwise rename `## Unreleased` and push.
    - **On the engine project** (`engine_project:`, else the project whose repo is the engine's; `Server.engine`), the card adds `status.release_wait.candidate.running`, which shows the viewer's own build and the newest issue-cli `ver` in any usage log, `data-stale` when either is not the tag. It also adds `.projects`: each configured project's calls on the exact tag from its `.agent-logs/telemetry.jsonl` (`telemetry.ScanVersion`), as Ran, Not yet or No usage log. `.reports` lists the open `cli-bug` issues whose body or report: comments carry `- issue-cli: vX.Y.0-rc.N…`, with the newest rc they were found in. `.fixed` lists those closed after the cut, whose fixes soak in the next candidate. The check `reports` passes when none is open.
    - **Silver badge:** while a candidate exists, the card shows `vX.Y.0-rc.N · release candidate · soaking <age>` in silver under its title (`status.rc_badge`): something is being tested, and for how long.
    - `status.md` puts the verdict on the milestone line and adds `- Candidate …`, `- Running: …`, `- Ran <tag>: …`, `- Open reports from candidates (N):` and `- Fixed since <tag>, soak in <next>: …`.
  - **Unreleased:** each top-level bullet under `## Unreleased` (wrapped lines joined, nested bullets left out, clipped at 140 characters), with a count. Hidden without a changelog, or when the changelog has no `## Unreleased` and there is no milestone.
  - **Not pushed:** commits on the branch (`ci.branch`, else the checked-out one) that aren't on its upstream, and the age of the newest. Hidden without an upstream (or when it is gone). It costs one `git for-each-ref` per render (`gitBranchState`, a var for tests).
  - **Folds:** past 5 open issues the list folds behind "N open: 1 in progress, 2 in design, …" (`status.release_wait.open.fold`), 10 rows inside, then "+K more"; Unreleased shows 5, then "+K more". Unreleased lines render their inline markdown.
  - A row with no data is hidden; with nothing to show, the card is hidden. `status.md` gets a `## Waiting for release` block after the current milestone, with the verdict and reasons on the milestone line, the open list capped at 10 with `+K more: issue-cli list --version <v> --status open`.
- **Where releases come from:**
  - The changelog is `workbench.changelog` in `workflow.yaml` (relative to the project root), or `CHANGELOG.md` when that exists.
  - Without either, the page lists the newest 3 git tags.
  - This repo sets `workbench.changelog: cmd/issue-cli/CHANGELOG.md`.

**`status.md`** follows the page, for the lead and for pasting as a stand-up: the summary line, `## Waiting on you`, `## Working on`, `## Current milestone` (by status, open by area, the Local CI line) or `## Where we are`, `## Health`, `## Waiting for release`, `## Epics (<version>)`, `## Next`, `## Recently shipped: N in 7 days` (one line per area: newest 3 slugs, then `+K more (issue-cli list --status done --system <S>)`), `## Versions` (one line) and `## Recently released`. Every list stops at its cap (10 for Waiting on you, Working on and the open list) with "+K more" and, where one exists, the `issue-cli list` command for the rest.

**Copy as markdown** fetches `status.md` onto the clipboard and shows "✓ Copied" for 2 s. Without a clipboard (plain http on another host) it opens `status.md` instead.

## Health view

`/p/<project>/health` (`health_view.go`, `templates/health.html`) shows the derived health score and the agent opinion, from the health snapshots and `.health/opinions/` only. Every number and metric is in [Workbench files: Scores](../workbench-files.md#scores) and [The agent opinion](../workbench-files.md#the-agent-opinion).

- **Now:** two cards side by side (stacked on a phone).
  - **Derived health:** a 0–100 gauge, its 7- and 30-day change and the share of the weight it comes from. Under it, the component table: what each part measures and its good → bad range, the measured value, a sub-score bar and the weight; a part without data says why. Below it, the newest snapshot's family warnings (`health.warnings`), one line each with the family in mono: a glob that matched no file, say. Session `catalog-truth` checks it.
  - **Agent opinion:** the gauge, the review's age (amber with a clock after 7 days) and **Run review**. Run review POSTs `/health/review` and starts the review agent `review-<project>` headless, like `issue-cli health review --spawn`. Below come the assessment and the recommendations. Proposed ones have **File as issue** (`/health/recommendation/<R#>/file`) and **Dismiss** with an optional reason (`…/dismiss`). Filed ones link their issue. The owner's click is the decision recorded on the new issue (`how: viewer`).
- **Lint** (`health.lint`, `#lint`): `issue-cli health lint` run live over workflow.yaml and the issues (`lint_view.go`), between Now and Trends. The heading counts the warnings. Each warning is one numbered line (`health.lint.row`): check and where in mono, the message, and the fix in muted text. 50 are listed, then "N more: … --all" (`health.lint.more`), and the command closes the section. The `lint` row in the component table scores the same count from the snapshot ([The lint family](../workbench-files.md#the-lint-family)). Session `health-lint` runs on `playtest/scenarios/lint`.
- **Trends** (`?range=7d|30d|90d|all`, one point per snapshot):
  - **Health scores:** both scores on a 0–100 axis.
  - **Unit tests: count vs runtime** and **End-to-end tests: count vs runtime:** each draws the count above and the wall time below, in two aligned panels on one time axis, count blue (`--chart-count`) and runtime orange (`--chart-runtime`). Never a second y-axis (D-6).
  - Without results for a kind the chart says how to get them.
- **Where to look:** the top 5 hotspots, longest functions, slowest tests and parts, and systems without a playtest.

After a POST the page shows a one-line flash (`health.flash`), an error in red. Test ids are `health.*` in `internal/testids`. Sessions:

- `health-view`: desktop and phone
- `health-layout`: 1440 px geometry
- `health-layout-medium`: 1100 px

They run on `playtest/scenarios/health`, whose `health.off: true` keeps the fixture snapshots the newest.

## Updates tab

`/p/<project>/updates` (`updates.go`, `templates/updates.html`) shows the operator what `issue-cli workflow upgrade` would propose: catalog features the project's `workflow.yaml` (the file `Project.LoadWorkflow` reads) lacks and nobody declined. It is the only place upgrades surface: no `issue-cli` command and no lead nudges about them. The rules are in [Workflow: Upgrading workflow.yaml](../Workflow/overview.md#upgrading-workflowyaml).

- **Sidebar:** Updates after Decisions, with an amber count of what is undecided. Hidden for a built-in or `template: custom` workflow.
- **Template strip:** the stamp → the bundled version with a state chip (`behind`, `N undecided` and `not recorded` amber, `up to date` green, `no stamp`). `not recorded` means nothing is left to propose but the file has features its record doesn't list (`issue-cli workflow scan`); the `/updates` overview uses the same states, the record's installed / declined counts, and the terminal command.
- **Category chips** (`?category=`) with counts.
- **Cards:** newest catalog version first, then by category. Each shows the title, `id@version`, the category, and the entry's first sentence. **Details** unfolds What, Why, Where, Ask (the question) and the snippet. An update to an installed feature is titled "Update: …" with the installed version.
- **Decline** and **Re-open** are form POSTs (`/updates/decline`, `/updates/reopen`, field `id`) that rewrite only the `features:` block, through the same writer as `workflow scan`. After a decline the flash offers **Undo** (re-open), and the row is marked "just now" in Declined.
- **Install with an agent** opens a panel in the card: what the agent will do, an optional answer to the entry's question, and **Start the agent** (`POST /updates/install`, fields `id`, `words`). It starts `agent-<project>-install-<id>` through the project-action dispatch in the project checkout. Its brief: read the entry, ask the question (or use the answer), adapt the snippet, `process workflow`, `workflow scan --installed <id>`, leave the stamp while others are undecided, show the diff, commit only when told. While that session runs, the card shows "installing", the attach command and a link to Agents, and no Decline.
- **Installed** ids as chips, and **Declined** rows with Re-open.
- Plans are cached per workflow file by mtime and size, so the sidebar count costs nothing on most pages.

Test ids are `updates.*` (and `projects.updates*`, `nav.tab.updates.count`). Session `updates` on `playtest/scenarios/updates` (a v0.36.0 development workflow with a record and ci): the install step fakes a live agent with a `cat` session, so nothing is launched.

## Project pages

With `workbench.status_page` or `workbench.pages` in `workflow.yaml`, the viewer shows the project's own HTML pages: config, sandbox rules and the postMessage shape are in [Workbench Files](../workbench-files.md#project-pages-status_page-pages).

- **Frame** (`page.html`): the sidebar, a header with the title, the page's path, its age and **Open raw**, then a full-height `<iframe>` (only the iframe scrolls). It is sandboxed without `allow-same-origin`, and the iframe gets a white background.
  - On `/status` the header also has the tabs (`status.tabs`); `status.html` shows the same tabs, Built-in selected, when `status_page` is set.
  - A missing file or a bad path shows the `page.missing` card in place of the iframe.
- **Sidebar:** `Nav.Pages` render as `.nav-sub` rows right after Status (`nav.page.<n>`); the active one is highlighted and Status is not.
- **Refresh:** the frame polls `/hash`. When its `pages` value changes, the frame reloads only the iframe and shows "· reloaded" (`page.source.reloaded`). When `hash` changes, it re-posts the status.
- **Origin guard:** `sameOriginOnly` (`origin.go`) answers 403 to any non-GET/HEAD request from another origin, `Origin: null` included, so a page may read but never write.

## Issues canvas

`/p/<project>/canvas` (**Canvas** under More in the sidebar, after Graph) is a read-only map of what each epic holds and what each version plans. `buildCanvas` in `handlers_canvas.go` holds every rule and is unit-tested without HTTP. The template only renders the view model.

- **Columns** are the distinct `version:` values, sorted as dotted numbers (0.2 before 0.10). Each header shows "done/total", side-status counts and a status-coloured bar.
- **The "no version" column** is hidden unless `?nover=1`, and the toolbar says how many issues it hides. When no issue has a version, the column is the only one and the toggle is disabled.
- **Closed statuses** are the workflow's final status (`done`) and its `optional: true` side statuses (obsolete, parked). Side statuses are left out of "done/total" and listed separately. Closed cards are dimmed. With `?done=collapse`, each cell folds them into a "✓ 17 done · 20 obsolete" chip; clicking the chip turns collapse off.
- **Rows** come from `?rows=`:
  - `epic` (the default) gives one row per epic. An epic is an issue with `type: epic` (EPIC), or one that another issue's `parent:` (or legacy `umbrella:`) points at (UMBRELLA).
    - The header shows status and a roll-up over all children, visible or not.
    - A sub-epic is listed after its parent, indented, with "↳ in <parent>".
    - An epic with no visible children shows one note across the row instead of cells, with a **Show no version** button when version hiding caused it.
    - "No epic" is the last row. A card whose `parent:` matches no issue lands there with an "unknown parent" marker.
    - Parent cycles list each epic once.
  - `system` gives one row per `system:`, with "No system" last. Epics appear as cards, and every card under an epic carries a **⧉ <epic>** badge.
- **Folding:** every lane, in both row modes, starts folded: the header shrinks to three lines (title on one line), and each cell shows a count per open status (coloured dot, workflow order) and one "✓ n" for closed cards instead of the cards.
  - A click anywhere on a lane head but its title link (or the ▸/▾ button) unfolds or folds it. **Expand all** / **Fold all** sit in the toolbar.
  - Open lanes go in `?open=<key>,<key>` (lane keys: epic slug, `no epic`, or the system) via `history.replaceState` and the toolbar's hidden `open` field, so a reload or a toolbar change keeps them. The server drops keys that match no lane.
  - The pan starts capturing the pointer only after it moves 4px, so a plain click reaches the head and a drag pans without folding.
- **Epic filter** (`?epic=<slug>`, epic rows only): the toolbar's **Epic** select lists every epic in lane order, sub-epics indented. Picking one shows its lane and its sub-epics, unfolded, without "No epic". Folding there is not written to the URL, and **All** brings every lane back folded.
- **Toolbar changes** submit through `submit()`, which leaves empty fields out of the URL.
- **Highlight** comes from `?hl=<epic slug>`, a row's Highlight button, or a card's badge. It rings the epic's cards and sorts them first, fades the rest, and shows a "Highlight ⧉ <epic> ×" chip.
  - Client-side changes are mirrored into the URL with `history.replaceState`, and `data-order` restores the original card order when the highlight is cleared.
  - An epic's detail page links to `/canvas?hl=<slug>` ("View on canvas").
- **Pan and zoom** come from `initPanZoom(viewport, world, controls)` in `canvas.html`: a CSS transform on the grid.
  - Drag the background or use the wheel to pan. Ctrl+wheel zooms around the pointer, and − / + step 10% between 20% and 200%. Fit scales the grid to the viewport width.
  - The function has no canvas-specific names, so another map view can reuse it.
  - Nothing is saved: the layout is always computed.
- **Parent lookup** is `epicParentSlug` (`parent:`, then `umbrella:`). Swap it for `tracker.ParentSlug` once the lead-engineer branch lands.
- **Test ids** are `canvas.*`: lanes are keyed by epic slug, `no_epic` or the system; columns and cells by version (`0_1`, `no_version`). The playtest sessions are `playtest/sessions/canvas-basics.yaml` (its steps click Expand all first), `canvas-fold.yaml` and `canvas-phone.yaml`, on the `canvas` scenario.

## Metrics view

`/p/<project>/metrics` is read-only over files the project writes and over git. The file formats, branch matching and overlap rules are in [Workbench Files](../workbench-files.md).

- **Sidebar entry:** "Metrics" after Balance when `workbench.metrics` or `workbench.tuning` is set. When any master metric is off it carries a red "N off" note, read from the metric files with no git call. A git project with neither key gets the entry as "Branches", with only the Branches tab. A project that has neither key nor git gets no entry.
- **Tabs** (`?tab=metrics|knobs|branches`, each with its own sidebar screen panel through `Nav.Panel`):
  - **Guard metrics** (panel: Sources):
    - OK / Near / Off counts;
    - the Master run card (file, date, commit, N, "N commits to … since this run") and the Branch runs card;
    - filters: `?system=`, `?group=`, `?only=off`, `?q=`;
    - the table: metric, system, target, master, branch values with their markers, verdict chip (✓ ok, ≈ near, ✗ off), n and probe;
    - a banner for file problems (no band, unreadable file).
  - **Knobs** (panel: Knob files):
    - filters: `?system=`, `?file=`, `?edited=1`, `?recent=1`, `?q=`;
    - one card per file with each knob's value, comment and last change;
    - a click on a row opens its history on master and the edits on open branches.
  - **Branches** (panel: Legend):
    - **Open branches:** branch, issue and status, or "no issue", or "base of N open branches"; base; commits ahead; files changed; depends on / decided by; hints with a fix command (a cited decision that was replaced; commits shared with no `depends_on`).
    - **Hot files:** `?hot=2` switches to two-branch files; `?shared=1` shows shared-commit and base overlap.
- **Detail page:** the Fields card gains **Depends on**, **Depended on by**, **Decided by** (with the decision's state) and **Shared files** ("2 hot files with 2 other branches"). Each row shows only when it has something. `depends_on` is not repeated among the custom fields.
- **Balance board:** a metric group whose id is in the metric files shows its band and master value next to the heading.
- **Refresh:** the page polls `/hash` and `/metrics.json?sig=1` every 10 s and reloads when either changes: the issues, the metric or knob files (master and worktrees), or a branch head.
- **Cost:** a cold load runs about 1 s of git on rust-manager (11 branches) and a warm one about 0.2 s. Git calls time out after 5 s. Without git, the file values render and the other parts say "git not available for this project".

## Design Considerations

When working on UI changes:

- Templates use Go's `html/template` — changes require a server restart (assets are embedded)
- For work that launches local tools or editors, decide whether to reuse existing tmux/alacritty patterns
- Handlers must not block on local processes — use goroutines for async operations
- User feedback after actions (dispatch, approve, save) goes through the toast notification system
- The dispatch modal (board ▶, the issue page's Claude/Codex buttons `detail.dispatch.*`) answers a `409 waiting-on-gate` with a banner instead of steps (`gateBanner`, `dispatch.gate`): "Waiting on your approval for <status> … Nothing was started", an **Approve** link to `#approve-<status>` (`dispatch.gate.approve`) and **Dispatch anyway** (`dispatch.gate.force`), which re-posts with `force: true`. "Prompt sent" is hidden then. Balance's **Start pass** toasts the gate. See [Agent Dispatch](../agent-dispatch.md#waiting-on-a-gate)
- Status colours come from `statusColor` in `template_funcs.go`. Its map keys are in `canonicalStatusKey` form (lower case, spaces as hyphens), so `in-progress`, not `in progress`.
- Board card fields and columns are driven by `workflow.yaml` — see [Board Configuration](../board-configuration)
- The detail view substitutes `<!-- data -->` markers in the rendered body with an inline data table (status dropdown + contenteditable comment + remove button). Markdown HTML comments require `goldmark/renderer/html.WithUnsafe()`, which is enabled in `internal/tracker/issue.go`. See [Per-issue Data Store](../data-store.md).

## Mockups in design

UI issues get mockups before the written design. The UI overlay in `workflow.yaml` adds a `## Mockups` section when an issue enters `in design`, and its boxes must be ticked before `backlog` (or before `in progress`, when the human approves starting straight from design).

1. Build the mockups as a Claude artifact: a Design canvas with one artboard per screen, or an HTML page for a single view or a comparison of options. Use real project data.
2. Link the artifact in frontmatter with `issue-cli set-meta <slug> --key mockups --value <url>`. It renders as a link in the detail sidebar.
3. Artifacts are private, so save the sources under `issues/attachments/<issue-name>/`.
4. Make screenshots with `tools/mockup-shots/shots.py issues/attachments/<issue-name>/` (Python Playwright with Chromium). It writes `<screen>.png` next to each source:
   - **Design artboards** (`*.dc.html`) need the claude.ai runtime, so they render through a small offline shim (`tools/mockup-shots/dc-shim.js`) covering `{{holes}}`, `<sc-for>`, `<sc-if>` and `renderVals()`. Each screen shows its initial state; event handlers are ignored.
   - **HTML pages** saved from an artifact have no `<!doctype>` or `<head>` (claude.ai adds them at publish time); the script adds them so the page doesn't render in quirks mode.
   - Captures are full height, wait for web fonts, and pin the colour scheme (`--scheme dark|light`, default dark). A page with a box that scrolls sideways (a board) is widened until nothing is cut off; `--width` sets the width yourself.
   - `--shot "<css>=<name>"` (repeatable) adds a close-up of one element as `<name>.png`; `--selector "<css>"` shoots every match as `<screen>-<n>.png`. Sticky and fixed bars are pinned in place for these so they don't cover the shot.
5. Under `## Mockups`, add one subsection per screen with its image (path relative to the issue file) and a description of the layout, regions, states and interactions, detailed enough to build from without opening the artifact.

Issues with no visible change write why under `## Mockups` and tick the boxes.

Images saved on an agent's worktree branch don't show on the board until that branch is merged, because the viewer reads `issues/attachments/` from the main checkout. Example: `ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index`.

## Test ids and playtest sessions

A UI issue's manual checks are written as a playtest session (`playtest/sessions/<name>.yaml`). `make ux-test` runs the session in headless Chrome against a throwaway scenario board, and `make playtest` shows the owner the steps that need their judgement in an overlay. The full guide is [docs/patterns/playtest/web-app.md](../patterns/playtest/web-app.md).

- **Labelling elements:** an element a step points at gets a `data-testid`:
  - The id is `page.area.element`, lower snake case.
  - Dynamic segments use the `tid` template func: `data-testid="board.column.{{tid .Status}}"`.
  - From JS, set it as `el.dataset.testid = 'detail.body.checkbox.' + i`.
- **The id list:** every id or pattern (`*` any one segment, `#` an index) goes in `internal/testids/testids.go`. `TestTemplateTestIDs` fails when templates and that list drift apart, and `make playtest-check` rejects session targets that aren't listed.
- **Containers:** label containers too (`nav.sidebar`, `board.filters`). A session's `layout_ignore` on a container waives a known layout bug inside it.
- **Known bug:** at phone width (390px) the page headers and the board filter bar don't wrap. Sessions waive them with `layout_ignore`.

## Issue page tabs

`/p/<project>/issue/<slug>` has the back link, the title (click to edit), a chip row (status, type, priority, system) and a tab bar. The bar reuses the Status page's `status-tabs` look, with links instead of JS:

- **Issue** (default; any unknown `?tab=`): the band (what this is, Needs you, where it is), then what you're approving, mockups, decisions and the body folded by section; the side column has the agent, acceptance criteria and relations above the sidebar's fields. See [The Issue tab](#the-issue-tab).
- **Timeline** (`?tab=timeline`): where the issue's time went. Its label carries the total ("5h 0m", or "… so far" in flight). It is built only when this tab is open and has no issue sidebar (its own side column instead):
  - **Missing sources:** an amber "Could not read N of 5 sources" box, when a source is missing.
  - **Where the time went:** the total, then agent working (with its rework), waiting on you, idle, rework loops and commits. Under them, a bar cut by time in blue (agent), amber (waiting), purple (rework) and grey (idle), with the statuses and clock ticks under it.
  - **Life by status:** one band per status, its header with the agent time (blue), waiting, idle and rework inside it, then its events, amber wait rows, grey idle rows and purple loop rows. The filters are links (`&show=you|commits|tests|cli`); **CLI calls** is the old Agent Timeline, with the workflow actions each transition saw.
  - **Side column:** Your part, Your words, Left open at done (done issues), CI after merge.

  The rules (what counts as waiting, idle and rework) are in [Workflow Stats](../workflow-stats.md#timeline-tab). It wraps to one column at phone width.
- **Evidence** (`?tab=evidence`): is it proven? See [The Evidence tab](#the-evidence-tab). Its label carries the count on every tab.

Each tab is a URL, so a lead or an agent can hand the owner a link: `issue-cli show` prints `Timeline: <url>?tab=timeline` and `Evidence: <url>?tab=evidence` (and `Brief: …`, the sentence the Issue tab leads with), and `issue-cli prove --list` ends with the Evidence link. Tab and filter links carry `aria-current="true"` (`page` stays the sidebar's). Test ids: `detail.pagetab.*`, `evidence.*` (see the Evidence tab), `timeline.*` (`timeline.total`, `.working`, `.waiting`, `.idle`, `.rework`, `.commits`, `.bar`, `.filter.<key>`, `.band`, `.wait`, `.idlegap`, `.reworkloop`, `.folded`, `.cli.call`, `.cli.actions`, `.side.*`, `.missing`). The playtest session is `playtest/sessions/timeline-tab.yaml` on scenario `playtest/scenarios/timeline`.

## The Issue tab

The tab answers in ten seconds what the issue is, where it is and what it needs from you (`ui/issue-tab-brief-needs-you-bar-and-a-folded-body`). `IssueTab` in `issue_tab.go` is its model: `buildIssueTab` reads nothing from disk, and `loadIssueTab` (`handlers_detail.go`) gathers its inputs. It is built only on this tab. It reads the Timeline's sources (`loadTimelineSources` and `buildTimeline`), `ownerAttention`, the board card's agent line (`cardAgents.footer`), `lastCliCall` and `epicProgress`, so every part says what the other views say.

**The band** (`detail.brief`) is one card in three columns. Under 720 px it stacks as brief, Needs you, then where it is.
- **What this is:** the brief (`tracker.Brief`, see [Issue File Format](../issue-format.md#the-brief)), then your words: the first decision quote on the issue that you said, then the newest `approval:` words, then the parent epic's first such quote ("epic D-n"). A quote with `how` "recorded by agent", or a "(Recommended)" pick, is not your words. The epic's progress follows.
- **Where it is** (`detail.path`): the type's path, with the time spent in each status (from the Timeline's bands). It marks "your gate" on each status entered through an approval and "your playtest" on human-testing. A time strip (`detail.time`: total, agent, waiting on you) links to the Timeline; on a phone it sits right under Needs you.
- **Needs you** (`detail.needs`, `data-state`) shows the same items as Status's Needs you, with the same wait ("waiting 2m · since 12:31", "≈" when the time is the file's mtime). An open question wins over a due playtest, and a playtest over a ready gate, as on the board cards; the rest show as "Also waiting" (`detail.needs.also`).
  - **gate:** one approve button per required approval, primary first and a handoff status (backlog) last (`detail.needs.approve.<status>`). Beside them sit the words box (`approve_words.band`) and the lead's `issue-cli approve <slug> --to "…" --quote "…"` line. A click posts to `/approve` like the sidebar's buttons, toasts, then reloads.
  - **playtest:** `make playtest ISSUE=<slug>` with a Copy button.
  - **question:** the question and an answer box. Send posts `answer: <text>` as a block-0 comment, which clears the question.
  - **Quiet** (card ground, nothing amber): it says who has the issue. With the agent: working, with the last call; idle, with Open in tmux. Also: stopped, queued, parked with its reason, done, or nobody. A gate whose checklist is still open, or one you approved that the agent has not moved through yet, adds one line.

**Main column:**
- **What you're approving** (`detail.approving`) covers what was recorded since the newest transition that passed your gate, else since the newest dispatch. Counters for sections, decisions, boxes (their ids), comments (their prefixes) and commits appear only when non-zero (`detail.approving.count.<key>`). Then the last 10 Timeline events and status changes; refused dry runs are left out.
- **Mockups:** the first image under each `###` of `## Mockups`, linked to the `mockups` canvas.
- **Decisions:** each D-n with your quote; a "(Recommended)" quote counts toward "N took the agent's recommendation".
- **Full issue** (`detail.fold`) holds today's rendered body, with Preview / Edit / Edit in nvim / Expand all. After `initComments`, `foldBody()` puts a row before each `##` section (and "Intro" before the first one) and hides its blocks. A row (`detail.fold.section.<id>`) shows the box chip, the lead (`tracker.Lead`, plain text), subsection names, open comments and Show/Hide (`detail.fold.toggle.<id>`); the server passes these as `data-fold` JSON. The rows are siblings with no checkboxes, and no block moves, so comment `data-block` indices and `detail.body.checkbox.#` are unchanged. Expand all hides the rows and shows every block (today's body); the button then reads Fold. Open sections and Expand all are kept in `localStorage` (`issue-fold:<issue url>`), and `#<id>` opens a section. A muted line names the sections later transitions will add (`detail.fold.not_started`).

**Side column:**
- Agent (`detail.agent`): its state from the board's footer, last call, dispatch, calls, Open in tmux and the dispatch prompt.
- Acceptance criteria: the first three, then "+N more".
- Relations: the epic, then depends on, depended on by and decided by. These rows moved here from the sidebar.
- Fields (`detail.fields`): today's sidebar sections, in an open `<details>`.

Under 768 px the side column goes under the main one, and a locked sidebar stops being sticky.

The cost is the Timeline's sources on every view, about 10–20 ms on this board. Playtest sessions: `issue-tab.yaml` and `issue-tab-phone.yaml` on `playtest/scenarios/issue-tab`.

## The Evidence tab

The tab answers "is it proven?" (`ui/evidence-tab-proof-shots-and-criteria`). `EvidenceTab` in `evidence.go` is its model: `buildEvidence` reads nothing from disk, and `loadEvidenceTab` gathers its inputs. It is built only on this tab. The report behind it is `proof.Collect`, the one `issue-cli prove --list` and the `criteria_proven` gate read (see [Proof Links](../Workflow/proof.md)), so the three never disagree.

**The label** (on every tab, `evidenceLabel`):
- green "n / n proven" when every criterion has a passing link;
- amber "p / n proven" when one is short and the gate covers the issue or any link exists;
- grey "n ticked · 0 linked" when there are criteria, no links, and the gate doesn't cover the issue;
- no count without Acceptance Criteria checkboxes.

An issue with no `created:` is dated by its first recorded transition or dispatch for the colour (the gate itself counts it as covered). Building the report costs at most about 40 ms on this repo's issues: test names are looked up in one `git grep`, and `internal/timeline` keeps parsed `results.yaml` files per process (path, size, mtime).

**Main column:**
- **Shots:** one card per playtest step a criterion links to, from the latest auto run that has the step and its shot file, in `<root>/.playtest/runs` or the issue's worktree. When no step proves a criterion, one card per step of each session's latest auto run that has a shot, and the header says so. A game's runs (RogueTech.X's schema) read the same; see [What the viewer reads](../patterns/playtest/README.md#what-the-viewer-reads).
  - Each card is tagged with the criteria it proves and its result. The first card is large.
  - The widest screen shows first, with chips that swap it (`evPickView`). A failed screen is shown instead, with its failing check. A phone screen shows at 390 px, top-aligned, and on a phone the large card opens on it.
  - A shot is looked up as `shots/<file>` next to its `results.yaml` (`.png`, then `.jpg` for a reduced run), then `<file>` beside it (a game's runner), so moved runs and old absolute paths both resolve. It is served by `/files/`, and a click opens the full page.
  - A linked step with no run shows "has no run yet". A step that ran without a shot (a judgement step) stays off the grid. The latest run's other steps are folded under "N other steps … prove no criterion".
- **Criteria → proof:** each criterion, every link's kind, ref and source (`prove`, `trailer <sha>`, `proves: in <session>`) and its check state, with the fix line under a failing one. Unproven rows are amber. Then stray links ("Unknown criterion AC9") and the ways to prove one. Not covered and unlinked: "ticked · no link" rows, with nothing guessed.
- **Change:** the first-parent, non-merge commits in the range the proof checks read (`proof.LogRange`), then files by area (the first folder, two for `internal/` and `cmd/`, `(root)` at the top) with + and −, and the lines added in test files (`proof.IsTestPath`).

**Side column:**
- **Verdict:** the status, the proven count, the merge sha and time (or the branch head while live), and CI after the merge. Then the newest `tests:` comment, said to be after or before the last code commit (docs-only commits don't count), and time from dispatch and waiting on you, as on the Timeline tab.
- **Playtest, per step:** each step's latest machine result and the screens it ran at. Your verdict comes from the latest judge run, else the newest `approval:` / `playtest:` comment naming the step, whose words are quoted.
- **How links are checked:** the key.
- **Watch:** `risk:` comments; "risk: none" reads as none.
- **Stale text:** past design, the body lines under an Open questions heading, or that start "Open question:", or that use pending or TBD as a value (`stale_text.go`).
  - Skipped: code, checkboxes, struck-through lines, lines citing a D-n or saying "answered", and the Decisions, Acceptance Criteria, Test Plan, Testing, Human Testing, Documentation and Shipping sections.
  - Each line shows "Likely answered by" the D-n or `Clarifications` bullet that shares at least 3 distinctive words, else "No decision matches".

Under 720 px the cards stack as Verdict, Shots, Criteria (rows, no table), Playtest, Change, Watch, Stale text, Key, with no sideways scroll.

Test ids: `evidence.label`, `evidence.tab`, `.missing`, `.empty`, `.shots`, `.shots.empty`, `.shots.other`, `.shot.<step>`, `.shot.vp.<viewport>`, `.criteria`, `.criterion.<id>`, `.link`, `.strays`, `.change`, `.change.area`, `.verdict`, `.playtest`, `.playtest.step.<id>`, `.key`, `.watch`, `.stale`, `.stale.line`. Sessions `evidence-tab` (with the Stats page's Disk use card) and `evidence-tab-phone` on scenario `playtest/scenarios/evidence-tab`.

## Checkbox clicks

The renderer (`tracker.newIssueMarkdown`) gives each task-list box in the body `data-line`, its 1-based line in the body (`Issue.BodyRaw`, from the goldmark parse). Only real task-list items get one: a `[ ]` in inline code, a code block or prose is not a box. `initCheckboxes` enables the `input[data-line]` boxes in `detail.body` and numbers them `detail.body.checkbox.#` in document order.

A click posts `{line, checked, base_body_hash}` to `POST /issue/<slug>/checkbox`. The server re-parses the body under the issue lock and flips only the box on that line (`tracker.ToggleIssueTaskBox`), so every other byte of the body stays. Until 2026-10-24 the page found "the nth `[ ]`" with a regex over the raw markdown, which also counted code and prose, and every box below a quoted `[ ]` saved the box above it.

- 409 when the body changed since the page loaded (the stale-body message below), or when the box is already in the asked state (the page showed something else).
- 400 when the line holds no task-list box.
- On any refusal the click is undone, the error shows as a red toast, and a 409 reloads the page after it.
- A success returns `{body, body_hash}`; the page puts both on `detail.body.textarea`, so clicks in a row and a later editor Save start from the written body.

Session `checkbox-below-code` ticks a box below inline-code, prose and fenced `[ ]`.

## Stale body saves

The editor's **Save** on `/issue/<slug>` posts the whole body (`{"body": …}` to `POST /issue/<slug>`). An agent or the CLI may have written the file since the page loaded (a transition appending `## Documentation`, say), and posting the page's copy would erase that. So the page sends `base_body_hash`, the hash of the body it started from (`data-body-hash` on `detail.body.textarea`, `tracker.BodyHash`), as a checkbox click does:

- The server compares it with the file's body under the issue lock (`tracker.UpdateIssueFrontmatterIfBody`; `tracker.ToggleIssueTaskBox` for a click). When they differ it writes nothing and answers 409 with "This issue changed since the page loaded …", shown as a red toast.
- A refused checkbox click is undone and the page reloads after the toast. A refused Save keeps the text in the editor so it can be copied.
- A successful save returns `body_hash`; the page keeps it, so saves in a row keep saving.
- A body write without `base_body_hash` (a script, an older page) is applied as before.

## Detail Sidebar Toggles

The frontmatter sidebar on `/issue/<slug>` has a small toolbar at the top with two toggles:

- **Lock** — pins the sidebar with `position: sticky` so it stays visible while the body scrolls. Default: on. When the sidebar is taller than the viewport, it scrolls internally.
- **Hide** — collapses the sidebar; the body expands to full width. A "Show sidebar" button appears in the body toolbar to bring it back.

Both states persist independently in `localStorage` (`sidebar-locked`, `sidebar-hidden`). An early-load script in `<head>` reads them and sets `data-sidebar-locked` / `data-sidebar-hidden` on `<html>` before paint to avoid a flash. CSS targets `html[data-sidebar-locked="true"] .detail-sidebar` and `html[data-sidebar-hidden="true"] .detail-layout`.

Lock defaults to on (the early-load check is `localStorage.getItem('sidebar-locked') !== 'false'`), so an explicit user opt-out is required to keep the sidebar scrolling with the body.

## Approving two steps at once

The sidebar's **Human Approval** section shows one **Human-approved for <status>** toggle per approval-gated next step. When that step lands on a handoff status (`backlog`, `human-testing`) whose own next step also needs an approval, a second toggle, **Approved for backlog and in progress**, approves both in one click.
- It posts `{"status":"backlog","then":"in progress"}` to `POST /issue/<slug>/approve`, which writes `human_approval: backlog` plus `queued_approval: in progress` (see [Issue File Format](../issue-format.md#custom-fields)).
- The agent's tmux session gets one nudge naming both statuses.
- Clicking it again clears both. Clicking it on an issue already approved for `backlog` only adds the queued approval.

## Approving with your words

Under the approve buttons, the Human Approval section has one **Your words (optional)** box (`approve_words.detail`). It goes with whichever button you click next.
- **Filled:** the request carries `words`, and the approve handler writes an `approval: "<words>" (viewer, <date>) → <status>` comment (source `app`). That is the same comment `issue-cli approve --quote` writes, and an approve-then names both statuses (`→ backlog, in progress`). The agent's nudge quotes the words in place of the canned line. The box empties and the toast says "with your words".
- **Empty, blank, or un-approving** (clicking a green button): no comment and the canned nudge. Un-approving keeps the box's text.
- **Comment write fails:** the approval still stands (`words_error` in the response), the toast says the words were not saved, and the box keeps them.

The box is `templates/_approve_words.html` (`{{template "approve-words" "<place>"}}`, test id `approve_words.<place>`). `static/approve-words.js` builds the same markup for pages that render in JS (`approveWords.html(place)`), reads it (`take`) and empties it (`clear`). The agents panel and the Issue tab's Needs-you column (`approve_words.band`) use it. The Timeline shows the words beside the approval (see [Timeline tab](../workflow-stats.md#timeline-tab)).

## Create issue modal

The sidebar's **New issue** button opens a shared modal defined in `templates/_create_modal.html`, included by `list.html` and `board.html`. On other pages the button links to `/?new=1`, and the list opens the modal on load. The modal posts to `POST /p/<project>/issues/create` (handler `handleCreateIssue` in `handlers_issue_mutate.go`) and on `201` navigates the browser to the new issue's detail page.

Form fields:

- **Title** (required, autofocused)
- **Body** (required, markdown). Prefilled from the per-status body template defined in `workflow.yaml`; if the user has not edited the body, changing the Status dropdown refills it from the new status's template. A `bodyDirty` flag in the partial gates the refill.
- **System** (required). Prefilled from the currently-active `?system=` filter on the page; falls back to "No system" if no filter is set.
- **Status** (default = first status before `backlog`, e.g. `idea`). The select is populated from the workflow's "creatable" set — every status with index < `backlog` — computed by `createOptions(wf)` in `handlers_list.go`.
- **Priority** (optional: low/medium/high/critical)
- **Labels** (optional, comma-separated)
- **Type** (only in projects with `types:`; defaults to `default_type`). A status the type's path skips — for example the board's `+` on a column a tweak never visits — starts the issue at the type's first status.

The board's per-column `+` buttons keep working and pass their column status as a preset to `openCreateModal(status)`; the sidebar button calls `openCreateModal('')` for the default.

Server-side wiring: both `ListData` and `BoardData` carry `CreatableStatuses []string` and `BodyTemplates map[string]string`. The templates JSON is embedded into the page via the `toJSON` template helper (`template_funcs.go`) which returns `template.JS` to bypass HTML escaping.

Auto-refresh interaction: `static/auto-refresh.js` skips its full-page `location.reload()` (board "needs reload" path and the detail-view path) when `window.__createModalOpen` is true, so a polled refresh cannot wipe an in-flight create.

## Agents canvas

`/p/<project>/agents` (**Agents** under More in the sidebar) shows who is working on what, built from `tmux ls`, the issues' `parent:` links and git. The page polls `GET /p/<project>/agents.json` (the `AgentsModel` in `handlers_agents.go`) every 5 seconds; the toolbar shows `live · tmux ls Ns ago`, or `offline` when a poll fails.

- **Tree:** the hierarchy only: You → the project lead (`lead-<project>`) → its workers, then any other live agent session (a project action, or a leftover `lead-*` session). Epics are not drawn here; the [issues canvas](#issues-canvas) shows them. Nodes show the session name without `agent-`, cut at 32 characters; the full name is the tooltip. Each open epic's children running and total stay in `agents.json`.
- **Worker states:**
  - *working*: live session, nothing pending, an issue-cli call in the last 15 minutes (or no readable clilog);
  - *idle*: live session, but its newest issue-cli call ended 15 minutes ago or more. The time is the newest call end (`ts + dur_ms`, see [the clilog](../CLI/telemetry.md#the-clilog)) across the agent's clilogs: `<workdir>/.agent-logs/<session>/<session>.clilog`, the assignee's, and the one in its worktree written before v0.54. Records naming another `--project` are skipped, because test runs in the agent's shell log fixture projects;
  - *at gate*: the next transition needs a human approval not yet given (the gate's status is shown);
  - *question*: the issue's newest comment starts with `question:`;
  - *stopped*: no live session, but a worktree or an active status (`in progress` up to, not including, `done`). "retro not swept" is added when a retro for the issue is not yet processed or archived.
- **Unfinished jobs:** every stopped worker.
- **Issue cards:** each worker links to its issue card with a dashed line in the status color.
- **Toolbar:** Tree / Radial layout (radial puts the project lead in the middle with one ring of agents around it), and the Show issues and Show stopped toggles. **Spawn lead** shows while the project lead is not running.
- **Clicking a live agent** (the lead or a worker) opens its tmux session in a terminal: `POST /p/<project>/agents/open` `{"session"}` runs the same attach as a dispatch re-attach (the project's `terminal`, or in `tmux_session` mode a `select-window` plus a terminal only when no client is attached). A session that already has a terminal attached never gets a second window: on i3 the window titled with the session name is focused (`focused`; the default i3 + alacritty terminal titles its windows that way), otherwise the panel says it is already open (`already-open`). With `terminal: none` nothing opens: in `tmux_session` mode the agent's window is selected (`selected`). The lead follows `lead_terminal:` instead of `terminal:`: unset or `none` gives that headless answer, and `tmux-window` adds (or leaves alone) its tab in `lead_window_session` ([Agent Dispatch](../agent-dispatch.md#lead-terminal)). The panel shows `Inside tmux: <switch_cmd> · From a shell: <attach_cmd>`; the attach command is prefixed `TMUX_TMPDIR=…` when the board runs on a private tmux server. The page never shows the pane and has no message box (D-12); you watch and type in tmux.
- **`?agent=<session>`** selects that agent on load without opening its terminal (the workboard's footer links here).
- **Selected-agent panel** (shown for the clicked node): kind, state, tmux location, the last issue-cli call (`last_call` / `last_cmd` in agents.json), the issue, then:
  - **Approve gate** (at-gate workers) calls the issue's existing `/approve` endpoint, a direct human action. Above it sits the words box (`approve_words.agents_panel`, see [Approving with your words](#approving-with-your-words)). A poll re-renders the panel only when the agent's gate changes, so typed text survives it. After a click, the button reads **Approved with your words** when they were saved;
  - **Spawn lead** when the project lead is not running (one lead per project, workbench umbrella D-13);
  - **Open in tmux** (live sessions) does the same as clicking the node again: it opens the terminal, or brings the open one to the front, and a status line says which;
  - **Copy attach** copies the `tmux attach` command. With `terminal: none` (`terminal_none` in agents.json) the button is **Copy switch** and copies `tmux switch-client …`, because the owner is inside tmux;
  - the issue's last 5 comments and the branch's `git diff --stat` against `main`, from `GET /p/<project>/agents/detail?session=<name>&issue=<slug>`.

Spawn buttons POST `{}` to `/p/<project>/lead/spawn` and show the launch steps in a modal. It runs the same `agentrun.LeadSpec` as `issue-cli lead spawn`. A body naming an `epic` returns `422`: there is one lead per project.

The detail page's sidebar has a **Lead** section: the project lead, whether it is live, and the issue's epic.

## Project actions

The sidebar's **Actions** group has **Spawn lead** (`POST /p/<project>/lead/spawn`) on every project page, followed by one button per `project_actions:` entry. Each button POSTs to `POST /p/<project>/action/<id>` (handler `handleProjectAction` in `handlers_dispatch.go`), which dispatches a one-shot agent with no issue context — prompts template with `{{project}}` only. The dispatch result modal is `project-actions-modal` in `templates/_project_actions.html`. This mirrors the per-issue custom action buttons on `detail.html`, which come from `issue_actions:` (legacy alias: `actions:`). The sidebar reads the actions from the project's workflow (`Nav.ProjectActions`).
