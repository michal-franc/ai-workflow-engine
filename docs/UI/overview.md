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
- `templates/_create_modal.html` — shared "New issue" modal partial included by list and board
- `templates/_project_actions.html` — shared project-level custom action bar + dispatch modal, included by list, board, and graph
- `templates/balance.html` — balance board (`handlers_balance.go`, board built by `tracker.BuildBalanceBoard`). Nav templates show its tab through `{{if workbenchTab "balance" .Prefix}}`, a template func the server binds to its projects
- `templates/decisions.html` — generated decisions index (`handlers_decisions.go`, index built by `tracker.BuildDecisionIndex`)
- `static/style.css` — all CSS (dark GitHub theme)
- `internal/testids/testids.go` — every `data-testid` the templates carry (playtest targets)

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
| `/balance`        | `balance.html` | Typed balance notes grouped by metric (state chip, hypothesis / change / evidence / verdict, earlier notes), joint passes from `balance_pass:` issues with **Start pass** (the existing dispatch endpoint), free-text notes under Unsorted. Shown only with `workbench.balance`; otherwise the tab is hidden and the page explains how to turn it on. Reloads when `/hash` changes, like `/decisions` |
| `/decisions`      | `decisions.html` | Generated index of every `D-n` decision: how it was made, recording issue, citing issues, state (active / deferred / replaced / replaced · stale). System and state chips (`?system=`, `?state=`); reloads when `/hash` changes. The page renders the `/hash` value it was built from as the polling baseline, so a change made before the first 5 s poll still triggers a reload |

In projects whose `workflow.yaml` defines `types:` ([Types of Work](../Workflow/types.md)), the list and board show a type badge and an "All types" filter (`?type=`), `type` joins the default board card fields, `/graph?type=<t>` draws that type's path, and the detail sidebar has a **Type** select (`POST /p/<project>/issue/<slug>/type`; a `409` with `statuses` asks the human which status on the new path to move to). Without `types:` none of these controls render.

## Issues canvas

`/p/<project>/canvas` (the **Canvas** tab, after Graph) is a read-only map of what each epic holds and what each version plans. `buildCanvas` in `handlers_canvas.go` holds every rule and is unit-tested without HTTP. The template only renders the view model.

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
- **Containers:** label containers too (`header.bar`, `board.filters`). A session's `layout_ignore` on a container waives a known layout bug inside it.
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

Both `list.html` and `board.html` show a **+ New issue** button in the header (next to the theme picker) that opens a shared modal defined in `templates/_create_modal.html`. The modal posts to `POST /p/<project>/issues/create` (handler `handleCreateIssue` in `handlers_issue_mutate.go`) and on `201` navigates the browser to the new issue's detail page.

Form fields:

- **Title** (required, autofocused)
- **Body** (required, markdown). Prefilled from the per-status body template defined in `workflow.yaml`; if the user has not edited the body, changing the Status dropdown refills it from the new status's template. A `bodyDirty` flag in the partial gates the refill.
- **System** (required). Prefilled from the currently-active `?system=` filter on the page; falls back to "No system" if no filter is set.
- **Status** (default = first status before `backlog`, e.g. `idea`). The select is populated from the workflow's "creatable" set — every status with index < `backlog` — computed by `createOptions(wf)` in `handlers_list.go`.
- **Priority** (optional: low/medium/high/critical)
- **Labels** (optional, comma-separated)
- **Type** (only in projects with `types:`; defaults to `default_type`). A status the type's path skips — for example the board's `+` on a column a tweak never visits — starts the issue at the type's first status.

The board's per-column `+` buttons keep working and pass their column status as a preset to `openCreateModal(status)`; the header button calls `openCreateModal('')` for the default.

Server-side wiring: both `ListData` and `BoardData` carry `CreatableStatuses []string` and `BodyTemplates map[string]string`. The templates JSON is embedded into the page via the `toJSON` template helper (`template_funcs.go`) which returns `template.JS` to bypass HTML escaping.

Auto-refresh interaction: `static/auto-refresh.js` skips its full-page `location.reload()` (board "needs reload" path and the detail-view path) when `window.__createModalOpen` is true, so a polled refresh cannot wipe an in-flight create.

## Agents canvas

`/p/<project>/agents` (the **Agents** tab) shows who is working on what, built from `tmux ls`, the issues' `parent:` links and git. The page polls `GET /p/<project>/agents.json` (the `AgentsModel` in `handlers_agents.go`) every 5 seconds; the toolbar shows `live · tmux ls Ns ago`, or `offline` when a poll fails.

- **Tree:** You → the project lead (`lead-<project>`) → one box per open epic, then workers with no epic. Each epic box shows its title, base branch, `N of M children running`, `main is N ahead` when `main` has commits the base lacks, its sub-lead node (`lead-<epic>`) and the workers on its children.
- **Worker states:**
  - *working*: live session, nothing pending;
  - *at gate*: the next transition needs a human approval not yet given (the gate's status is shown);
  - *question*: the issue's newest comment starts with `question:`;
  - *stopped*: no live session, but a worktree or an active status (`in progress` up to, not including, `done`). "retro not swept" is added when a retro for the issue is not yet processed or archived.
- **Unfinished jobs:** the stopped workers. With Group umbrellas on, an epic's stopped children appear in its box and the lane shows the rest.
- **Issue cards:** each worker links to its issue card with a dashed line in the status color.
- **Toolbar:** Tree / Radial layout (radial puts the project lead in the middle, sub-leads on an inner ring and workers outside), and the Show issues, Group umbrellas and Show stopped toggles. **Spawn lead** shows while the project lead is not running.
- **Clicking a live agent** (lead, sub-lead or worker) opens its tmux session in a terminal: `POST /p/<project>/agents/open` `{"session"}` runs the same attach as a dispatch re-attach (the project's `terminal`, or in `tmux_session` mode a `select-window` plus a terminal only when no client is attached). A session that already has a terminal attached never gets a second window: on i3 the window titled with the session name is focused (`focused`; the default i3 + alacritty terminal titles its windows that way), otherwise the panel says it is already open (`already-open`). With `terminal: none` the panel shows the attach command instead (prefixed `TMUX_TMPDIR=…` when the board runs on a private tmux server). The page never shows the pane and has no message box (D-12); you watch and type in tmux.
- **Selected-agent panel** (shown for the clicked node): kind, state, tmux location, the issue, then:
  - **Approve gate** (at-gate workers) calls the issue's existing `/approve` endpoint, a direct human action;
  - **Spawn lead** / **Spawn sub-lead** for a lead that is not running;
  - **Open in tmux** (live sessions) does the same as clicking the node again: it opens the terminal, or brings the open one to the front, and a status line says which;
  - **Copy attach** copies the `tmux attach` command;
  - the issue's last 5 comments and the branch's `git diff --stat` against its base (or `main`), from `GET /p/<project>/agents/detail?session=<name>&issue=<slug>`.

Spawn buttons POST to `/p/<project>/lead/spawn` (`{}` for the project lead, `{"epic": "<slug>"}` for a sub-lead) and show the launch steps in a modal. It runs the same `agentrun.LeadSpec` as `issue-cli lead spawn`; a non-epic slug returns `422`.

The detail page's sidebar has a **Lead** section: on an epic, its sub-lead, whether it is live, its base branch and a **Spawn sub-lead** button; on any other issue, the lead it reports to and its epic.

## Project action bar

The list, board, and graph views show an **Actions** bar below the header (rendered by `templates/_project_actions.html`, included via `{{template "project-actions-bar" .}}`). It always has **Spawn lead** (`POST /p/<project>/lead/spawn`), followed by one button per `project_actions:` entry. Each button POSTs to `POST /p/<project>/action/<id>` (handler `handleProjectAction` in `handlers_dispatch.go`), which dispatches a one-shot agent with no issue context — prompts template with `{{project}}` only. The shared dispatch result modal is defined in the same partial (`project-actions-modal`). This mirrors the per-issue custom action buttons on `detail.html`, which come from `issue_actions:` (legacy alias: `actions:`). `ListData`, `BoardData`, and `GraphData` each carry `ProjectActions []tracker.CustomAction`.
