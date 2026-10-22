---
title: "UI Overview"
order: 1
---

## Scope

The UI system covers HTML templates, CSS styling, and client-side JavaScript for all web views.

## Key Files

- `templates/list.html` — filterable issue list
- `templates/board.html` — kanban board with drag-and-drop
- `templates/detail.html` — issue detail with sidebar, comments, dispatch
- `templates/graph.html` — workflow status graph
- `templates/agents.html` — agents canvas: lead/worker tree, unfinished jobs, selected-agent panel (handlers in `handlers_agents.go`)
- `templates/canvas.html` — issues canvas: version columns × epic or system rows, pan and zoom (`handlers_canvas.go` builds it)
- `templates/docs.html` — documentation viewer with sidebar navigation
- `templates/_sidebar.html` — the project sidebar on every project page (see [Navigation and theme](#navigation-and-theme)); `nav.go` lists the views
- `templates/_create_modal.html` — shared "New issue" modal partial included by list and board
- `templates/_project_actions.html` — the project-action dispatch modal, included by the sidebar
- `templates/balance.html` — balance board (`handlers_balance.go`, board built by `tracker.BuildBalanceBoard`). Its sidebar entry shows only when `workbench.balance` is set
- `templates/status.html` — the project Status page (`handlers_status.go`; the model is `buildStatus` in `status.go`, the "waiting on you" rule is `attention.go`)
- `templates/decisions.html` — generated decisions index (`handlers_decisions.go`, index built by `tracker.BuildDecisionIndex`)
- `templates/metrics.html` — Metrics view: guard metrics, knobs, branches (`handlers_metrics.go`; readers in `internal/tracker/metrics.go`, `knobs.go`, `branches.go`, `deps.go`)
- `templates/_metrics_panels.html` — the `system-guard-metrics` and `system-knobs` partials a system page embeds, fed by `systemMetricsPanels` (`handlers_metrics_panels.go`)
- `static/style.css` — all CSS: theme tokens for dark, Dracula and light, then the shared pieces and each view
- `static/fonts/` — Archivo and JetBrains Mono (woff2, OFL), served by the viewer itself
- `internal/testids/testids.go` — every `data-testid` the templates carry (playtest targets)

## Navigation and theme

Every project page has the left sidebar from `templates/_sidebar.html`:

- **All projects** link and the **project block**: name, version (projects.yaml `version`) and open-issue count.
- **New issue**. On list and board it opens the create modal; elsewhere it links to `/?new=1`, which opens the modal on the list.
- **Views**: Status (with an amber count of what waits on the owner), Board, List, Balance (when `workbench.balance` is set), Decisions and Docs, then a **More** group with Graph, Canvas, Agents, Retros, Stats and GitHub (when `supports_github` is set).
  - The current view has `aria-current="page"`.
  - More opens on load when the current view is in it; otherwise the browser remembers it open or closed (`localStorage` key `nav.more`).
- **N agents running**, linking to Agents. It is hidden at 0, and `auto-refresh.js` updates it.
- A **screen panel**. The Board shows a Systems list here (open counts, each a `?system=` filter).
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

## Views

| Route             | Template      | Description                                     |
|:------------------|:--------------|:------------------------------------------------|
| `/`               | `list.html`   | Filterable list with status, system, priority    |
| `/board`          | `board.html`  | Kanban with configurable columns and card fields |
| `/graph`          | `graph.html`  | Workflow DAG with stale highlighting             |
| `/agents`         | `agents.html` | Agents canvas: leads, workers, their issues (see below) |
| `/canvas`         | `canvas.html` | Issues canvas: versions × epics (or systems), see [Issues canvas](#issues-canvas) |
| `/docs`           | `docs.html`   | Docs with collapsible sidebar sections           |
| `/issue/<slug>`   | `detail.html` | Detail with edit, approve, dispatch, comments    |
| `/stats`          | `stats.html`  | Workflow token-cost estimates (see [Workflow Stats](../workflow-stats.md)) |
| `/balance`        | `balance.html` | Typed balance notes grouped by metric (state chip, hypothesis / change / evidence / verdict, earlier notes), joint passes from `balance_pass:` issues with **Start pass** (the existing dispatch endpoint), free-text notes under Unsorted. Shown only with `workbench.balance`; otherwise its sidebar entry is hidden and the page explains how to turn it on. Reloads when `/hash` changes, like `/decisions` |
| `/status`         | `status.html` | Where the project stands: progress, work in flight, next steps, what waits on the owner, what shipped. See [Status page](#status-page) |
| `/status.md`      | (text)        | The Status page as markdown (`text/markdown`), for pasting as a stand-up |
| `/decisions`      | `decisions.html` | Generated index of every `D-n` decision: how it was made, recording issue, citing issues, state (active / deferred / replaced / replaced · stale). System and state chips (`?system=`, `?state=`); reloads when `/hash` changes. The page renders the `/hash` value it was built from as the polling baseline, so a change made before the first 5 s poll still triggers a reload |

| `/metrics`        | `metrics.html` | Guard metrics, knobs and branch overlap, read from the project's files and git (see [Metrics view](#metrics-view)). `/metrics.json` returns the same data; `?sig=1` only the reload signature |

In projects whose `workflow.yaml` defines `types:` ([Types of Work](../Workflow/types.md)), the list and board show a type badge and an "All types" filter (`?type=`), `type` joins the default board card fields, `/graph?type=<t>` draws that type's path, and the detail sidebar has a **Type** select (`POST /p/<project>/issue/<slug>/type`; a `409` with `statuses` asks the human which status on the new path to move to). Without `types:` none of these controls render.

## Status page

`/p/<project>/status` is the first sidebar entry. It answers "where are we" without asking the lead. It is read-only, and everything comes from repo files. `buildStatus` (`status.go`) holds the rules and is tested without HTTP.

- **Where we are:**
  - an "All issues" done/total bar (side statuses such as parked are left out)
  - a row per `version:` when any issue sets one
  - open epics with their children's done/total
- **What we are working on:**
  - every issue from in progress up to (not including) done, with its agent state from the agents model and its time in status
  - the time is the stats sidecar's last transition into the status; without one it is the file's mtime, marked `≈`
  - the time turns amber at 7 days and red at 14
  - the project lead's state
  - chips for live agents on issues not yet in progress
- **What's next:** the `## Next` section of the project lead's newest handoff note (`tracker.NewestHandoffNote`), rendered as markdown, with slugs in code spans linked to their issues. Then the backlog by priority, then parked issues with the first line of their `## Parked Record`.
- **Waiting on you** (`waitingOnOwner` in `attention.go`, shared with the board markers and the system pages):
  - gates whose approval is the only unmet requirement (`WorkflowConfig.ReadyForApproval`, which skips `command_succeeds`)
  - human-testing issues with a playtest session (`issue: <slug>`) and no `-judge` run
  - issues whose newest comment starts with `question:`
  - a footer counts gates whose checklist is still open
  - the sidebar's Status count is the same number
- **Recently shipped:** the newest 3 `## vX.Y.Z — date` entries of the changelog, then issues that reached the final status in the last 7 days.
  - The changelog is `workbench.changelog` in `workflow.yaml` (relative to the project root), or `CHANGELOG.md` when that exists.
  - Without either, the page lists the newest 3 git tags.
  - This repo sets `workbench.changelog: cmd/issue-cli/CHANGELOG.md`.

**Copy as markdown** fetches `status.md` onto the clipboard and shows "✓ Copied" for 2 s. Without a clipboard (plain http on another host) it opens `status.md` instead.

## Issues canvas

`/p/<project>/canvas` (**Canvas** under More in the sidebar, after Graph) is a read-only map of what each epic holds and what each version plans. `buildCanvas` in `handlers_canvas.go` holds every rule and is unit-tested without HTTP. The template only renders the view model.

- **Columns** are the distinct `version:` values, sorted as dotted numbers (0.2 before 0.10). Each header shows "done/total", side-status counts and a status-coloured bar.
- **The "no version" column** is hidden unless `?nover=1`, and the toolbar says how many issues it hides. When no issue has a version, the column is the only one and the toggle is disabled.
- **Closed statuses** are the workflow's final status (`done`) and its `optional: true` side statuses (obsolete, parked). Side statuses are left out of "done/total" and listed separately. Closed cards are dimmed. With `?done=collapse`, each cell folds them into a "✓ 17 done · 20 obsolete" chip; clicking the chip turns collapse off.
- **Rows** come from `?rows=`:
  - `epic` (the default) gives one row per epic. An epic is an issue with `type: epic` (EPIC), or one that another issue's `parent:` (or legacy `umbrella:`) points at (UMBRELLA).
    - The header shows status, `base:` branch, and a roll-up over all children, visible or not.
    - A sub-epic is listed after its parent, indented, with "↳ in <parent>".
    - An epic with no visible children shows one note across the row instead of cells, with a **Show no version** button when version hiding caused it.
    - "No epic" is the last row. A card whose `parent:` matches no issue lands there with an "unknown parent" marker.
    - Parent cycles list each epic once.
  - `system` gives one row per `system:`, with "No system" last. Epics appear as cards, and every card under an epic carries a **⧉ <epic>** badge.
- **Highlight** comes from `?hl=<epic slug>`, a row's Highlight button, or a card's badge. It rings the epic's cards and sorts them first, fades the rest, and shows a "Highlight ⧉ <epic> ×" chip.
  - Client-side changes are mirrored into the URL with `history.replaceState`, and `data-order` restores the original card order when the highlight is cleared.
  - An epic's detail page links to `/canvas?hl=<slug>` ("View on canvas").
- **Pan and zoom** come from `initPanZoom(viewport, world, controls)` in `canvas.html`: a CSS transform on the grid.
  - Drag the background or use the wheel to pan. Ctrl+wheel zooms around the pointer, and − / + step 10% between 20% and 200%. Fit scales the grid to the viewport width.
  - The function has no canvas-specific names, so another map view can reuse it.
  - Nothing is saved: the layout is always computed.
- **Parent lookup** is `epicParentSlug` (`parent:`, then `umbrella:`). Swap it for `tracker.ParentSlug` once the lead-engineer branch lands.
- **Test ids** are `canvas.*`: lanes are keyed by epic slug, `no_epic` or the system; columns and cells by version (`0_1`, `no_version`). The playtest sessions are `playtest/sessions/canvas-basics.yaml` and `canvas-phone.yaml`, on the `canvas` scenario.

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

- **Tree:** the hierarchy only: You → the project lead (`lead-<project>`) → its workers, plus any live sub-lead (`lead-<epic>`, started from the CLI). Epics are not drawn here; the [issues canvas](#issues-canvas) shows them. Nodes show the session name without `agent-`, cut at 32 characters; the full name is the tooltip. The epic data (base branch, children running, `main` ahead) stays in `agents.json`.
- **Worker states:**
  - *working*: live session, nothing pending;
  - *at gate*: the next transition needs a human approval not yet given (the gate's status is shown);
  - *question*: the issue's newest comment starts with `question:`;
  - *stopped*: no live session, but a worktree or an active status (`in progress` up to, not including, `done`). "retro not swept" is added when a retro for the issue is not yet processed or archived.
- **Unfinished jobs:** every stopped worker.
- **Issue cards:** each worker links to its issue card with a dashed line in the status color.
- **Toolbar:** Tree / Radial layout (radial puts the project lead in the middle with one ring of agents around it), and the Show issues and Show stopped toggles. **Spawn lead** shows while the project lead is not running.
- **Clicking a live agent** (lead, sub-lead or worker) opens its tmux session in a terminal: `POST /p/<project>/agents/open` `{"session"}` runs the same attach as a dispatch re-attach (the project's `terminal`, or in `tmux_session` mode a `select-window` plus a terminal only when no client is attached). A session that already has a terminal attached never gets a second window: on i3 the window titled with the session name is focused (`focused`; the default i3 + alacritty terminal titles its windows that way), otherwise the panel says it is already open (`already-open`). With `terminal: none` the panel shows the attach command instead (prefixed `TMUX_TMPDIR=…` when the board runs on a private tmux server). The page never shows the pane and has no message box (D-12); you watch and type in tmux.
- **Selected-agent panel** (shown for the clicked node): kind, state, tmux location, the issue, then:
  - **Approve gate** (at-gate workers) calls the issue's existing `/approve` endpoint, a direct human action;
  - **Spawn lead** when the project lead is not running (there is no sub-lead button: one lead per project for now, workbench umbrella D-13);
  - **Open in tmux** (live sessions) does the same as clicking the node again: it opens the terminal, or brings the open one to the front, and a status line says which;
  - **Copy attach** copies the `tmux attach` command;
  - the issue's last 5 comments and the branch's `git diff --stat` against its base (or `main`), from `GET /p/<project>/agents/detail?session=<name>&issue=<slug>`.

Spawn buttons POST `{}` to `/p/<project>/lead/spawn` and show the launch steps in a modal. The endpoint still accepts `{"epic": "<slug>"}` for a sub-lead, but no button sends it. It runs the same `agentrun.LeadSpec` as `issue-cli lead spawn`; a non-epic slug returns `422`.

The detail page's sidebar has a **Lead** section: on an epic, its sub-lead, whether it is live and its base branch; on any other issue, the lead it reports to and its epic.

## Project actions

The sidebar's **Actions** group has **Spawn lead** (`POST /p/<project>/lead/spawn`) on every project page, followed by one button per `project_actions:` entry. Each button POSTs to `POST /p/<project>/action/<id>` (handler `handleProjectAction` in `handlers_dispatch.go`), which dispatches a one-shot agent with no issue context — prompts template with `{{project}}` only. The dispatch result modal is `project-actions-modal` in `templates/_project_actions.html`. This mirrors the per-issue custom action buttons on `detail.html`, which come from `issue_actions:` (legacy alias: `actions:`). The sidebar reads the actions from the project's workflow (`Nav.ProjectActions`).
