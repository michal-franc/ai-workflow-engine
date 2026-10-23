# AI Workflow Engine (issue-viewer + issue-cli)

A Go web app that renders markdown issue files as a GitHub-style project tracker with list, kanban board, and documentation views, plus `issue-cli`, the CLI agents use to walk issues through `workflow.yaml`. The product name is **AI Workflow Engine** (repo `michal-franc/ai-workflow-engine`); the binaries and the Go module keep the `issue-viewer` name.

## Running

```bash
go build && ./issue-viewer -dir ./issues -docs ./docs -port 8080
```

Flags:

- `-dir` — issue markdown directory (default `./issues`), supports subdirectories
- `-docs` — documentation markdown directory (default `./docs`)
- `-host` — interface to listen on (default `127.0.0.1`; anything else is an opt-in and warns at startup). Requests whose `Host` isn't loopback, `*.localhost`, the bind address or in `allowed_hosts:` (projects.yaml) get 421
- `-port` — HTTP port (default `8080`)

## Validation

`make validate` runs `go vet ./...`, the full test suite, and a coverage gate on
`cmd/issue-cli` (floor: `CLI_COVERAGE_FLOOR`, default 70), then
`make playtest-check`. Use it before proposing a change that touches
`cmd/issue-cli/`.

UI checks run as playtest sessions:

- `make ux-test [ISSUE=<slug>] [S=<session>] [REPEAT=2]` runs them in headless
  Chrome against a throwaway scenario board.
- `make playtest ISSUE=<slug>` shows the owner the judgement steps in an
  overlay.
- `make playtest-report ISSUE=<slug> [APPLY=1]` ticks the boxes that passed.

See [docs/patterns/playtest/web-app.md](docs/patterns/playtest/web-app.md).

## Project Structure

```
main.go                     — entry point: run() (flags, listener), server timeouts, graceful shutdown on SIGINT/SIGTERM
background.go               — goroutines that outlive a request (s.bg.Go), waited for and named at shutdown
routes.go                   — Server struct, NewServer, Routes, dispatcher, project list
origin.go                   — hostAllowed (421 for a Host not loopback/allowed_hosts: DNS rebinding), sameOriginOnly (403 for cross-origin writes), -host / listen-address helpers
nav.go                      — sidebar views (navViews) and the Nav the sidebar partial renders
template_funcs.go           — funcMap, status/priority colors, link rewriters
helpers.go                  — projectRoot, fileExists, small utilities
images.go                   — relative image src rewrite, /files/ image route
tmux.go                     — agent session listing/matching/notification
handlers_list.go            — list view, filters, /hash, /issues.json
handlers_board.go           — classic board + graph views
handlers_workboard.go       — /workboard: the sidebar Board, cards with markers, epic progress, agent footer
markers.go                  — card markers (cardMarkers, blocked_on, needs-revision) and epic progress, shared by workboard and system pages
agent_state.go              — worker state for the agents canvas and card footers (workerState, idle from the clilog's last call)
handlers_canvas.go          — issues canvas (/canvas): version columns × epic or system rows
handlers_detail.go          — detail page, transition preview, data-table render
issue_tab.go                — the detail page's Issue tab model: brief, Needs-you band, path, what you're approving, folded body, side column
timeline.go                 — the detail page's Timeline tab model: where the time went (agent, waiting on you, idle, rework), life by status, filters
timeline_sources.go         — what the Timeline tab reads: internal/timeline.Load plus comments, decisions, prompt, local CI; lists what it could not read
handlers_issue_mutate.go    — update/create/delete/approve/upload, body editor
handlers_data.go            — /data sidecar endpoints
handlers_comments.go        — comment add/get/toggle/delete
handlers_workflow.go        — retros, bug status, docs handlers
handlers_dispatch.go        — agent dispatch + prompt builders
handlers_github.go          — github page, fetch, import
handlers_stats.go           — /stats: the dashboard over the tables (time tables, workflow token cost, CLI usage)
stats_dashboard.go          — the stats dashboard model: options, gauges, trends from snapshots, time bars, drill-downs
stats_cache.go              — per-project cache of the aggregate and CLI usage report, warmed at start
stats_overview.go           — /stats without a project: every project compared from its snapshots
handlers_agents.go          — /agents canvas, agents.json, agent detail, open-in-tmux and lead spawn endpoints
handlers_systems.go         — /systems index and system / family pages (model in systems.go, design-doc panel in docsections.go)
handlers_metrics.go         — /metrics view (guard metrics, knobs, branches), metrics.json; handlers_metrics_panels.go feeds the system-page partials
handlers_status.go          — /status page and /status.md (model in status.go: buildStatus, statusMarkdown)
handlers_pages.go           — project pages (workbench.status_page / pages): frame, /pages/<n>, sandboxed /page-raw/, /status.json
ci_watch.go                 — local CI watcher: polls each project's CI branch, starts detached `issue-cli ci run`
health_watch.go             — daily project health snapshot: every 10 min, one per repository when none is from today
release_wait.go             — Status "Waiting for release" card: milestone readiness, ## Unreleased entries, unpushed commits (one git call)
ci_view.go                  — local CI on the pages: Status card (last runs boxes), waiting entry, ci-red markers
attention.go                — what waits on the owner (ownerAttention, waitingOnOwner): Status page, sidebar count, system pages, board markers
issue.go                    — Issue struct, ParseIssue, LoadIssues (walks subdirs)
docs.go                     — DocPage struct, ParseDocPage, LoadDocs
internal/timeline/          — the Timeline's derivation (Load, Split: agent / waiting on you / idle / rework per status), the project Aggregate, the `workflow` health family
internal/health/            — project health snapshots: family registry (Register), .health/snapshots/ files, retention, Range, Backfill, the codebase family (git objects at the main head)
internal/milestone/         — the current milestone (Pick, Released), shared by the Status page and the `milestone` health family
internal/chart/             — server-rendered SVG charts (line, stacked bars, columns, gauge, sparkline) with table views
internal/ci/                — local CI runner: .ci/ state and run records, lock, persistent .worktrees/.ci, blame, watcher, one-liner
internal/agentrun/          — shared tmux/worktree launcher used by the server and issue-cli (leads, dispatch)
internal/clilog/            — the clilog: one record per issue-cli call (result, no free text), paths, Redact, Scrub (tools/clilog-scrub)
internal/viewerstate/       — viewer.json: the running viewer's config/URL/pid, how issue-cli finds the config from any folder
templates/                  — Go HTML templates (list.html, board.html, detail.html, docs.html); _sidebar.html on every project page
internal/testids/           — every data-testid the templates carry; TestTemplateTestIDs keeps them in step
cmd/playtest/               — playtest runner: check | ux-test | playtest (overlay) | report (chromedp)
playtest/sessions/          — playtest sessions (step id = issue checkbox id)
playtest/scenarios/         — scenario fixtures (issues under tracker/, never issues/)
static/style.css            — all CSS: theme tokens (dark workbench, Dracula, light), shared classes, views
static/fonts/               — Archivo and JetBrains Mono woff2 (OFL)
sync-issues.sh              — downloads issues from GitHub Project into ./issues/<System>/
```

## Issue File Format

Issues are markdown files with YAML frontmatter stored in `./issues/` or `./issues/<System>/` subdirectories.

```markdown
---
title: "Issue title"
status: "in progress"
system: "Combat"
version: "0.1"
labels:
  - bug
  - enhancement
priority: "high"
created: "2025-01-15"
number: 42
repo: "owner/repo"
---

Markdown body here. Supports `[x]` checkboxes.
```

### Required fields

- `title` — issue title

### Optional fields

- `status` — one of the statuses in the project's `workflow.yaml` (the bundled `development` template: `idea`, `in design`, `backlog`, `in progress`, `testing`, `human-testing`, `documentation`, `shipping`, `done`)
- `system` — categorization tag (also used as subdirectory name by sync script)
- `version` — version string, filterable on the board view
- `labels` — list of label strings
- `priority` — one of: `low`, `medium`, `high`, `critical`
- `created` — date string for sorting (newest first)
- `number` — GitHub issue number (used in board card display as `#number`)
- `repo` — GitHub repo in `owner/repo` format
- `parent` — slug of the epic this issue belongs to (`umbrella` is a legacy alias). Old `base`/`lead` fields on epics are plain custom fields; nothing reads or writes them
- `type` — type of work when `workflow.yaml` defines `types:` (picks the issue's path; see docs/Workflow/types.md)

### Custom fields

Any other frontmatter key is preserved as a custom field and displayed in the detail view sidebar. String values that start with `http://` or `https://` render as clickable links. List values render as bullet lists. Example:

```yaml
pr: "https://github.com/org/repo/pull/456"   # renders as a link
pr_author: "jsmith"                            # renders as text
participants:                                  # renders as a list
  - alice
  - bob
```

### Filename convention

- For GitHub issues: `<number>.md` (e.g., `42.md`)
- For draft issues: slugified title (e.g., `my-feature-idea.md`)
- The slug used in URLs (`/issue/<slug>`) comes from the title (with the system folder in front), not the filename; a `slug:` field pins it. `issue-cli update --title` keeps the slug by writing that pin

## Doc Page Format

Documentation pages are markdown files in the docs directory.

```markdown
---
title: "Page Title"
order: 1
---

Page content in markdown.
```

- `title` — defaults to titlecased filename if omitted
- `order` — numeric sort order (lower first), pages with same order sort alphabetically
- Frontmatter is optional; a plain markdown file works too

## Documentation

Detailed documentation lives in `docs/` and is viewable at `/docs` in the web UI:

- [Why a Workflow Harness](docs/why.md) — the argument behind the tool
- [Roadmap](docs/roadmap.md) — goals, epics and phase order (set 2026-10-22); the board's epics are the source of truth
- [Getting Started](docs/getting-started.md) — install, server flags, first issue, docs page format
- [Issue File Format](docs/issue-format.md) — frontmatter fields, custom fields, file organization
- [Workflow](docs/workflow.md) — lifecycle, prompts, side-effects, system overlays
- [Agent Workflow Flow](docs/agent-workflow-flow.md) — full dispatch-to-done agent flow
- [Board Configuration](docs/board-configuration.md) — columns, card fields
- [Agent Dispatch](docs/agent-dispatch.md) — terminal config, approval notifications, the lead session, worktrees
- [GitHub Integration](docs/github-integration.md) — sync, issue reference, auto-close
- [Per-issue Data Store](docs/data-store.md) — sidecar JSON, `<!-- data -->` marker, `issue-cli data` commands
- [Workbench Files](docs/workbench-files.md) — metric runs, targets and knob exports the Metrics view reads, branch matching, hot files, `depends_on`; project pages (`status_page`, `pages`: sandboxed HTML, postMessage status); health snapshots (`.health/snapshots/`, `health:` config and `limits`, metric names, `issue-cli health backfill`, adding a family)
- [Local CI](docs/workflow.md#local-ci) — opt-in `ci:` block: commands run on local main after every merge in `.worktrees/.ci`, results in `.ci/`, red reported to Status, markers, `ci:` comments and the lead; `issue-cli ci status|run|reset|watch`
- [Workflow Stats](docs/workflow-stats.md) — the Stats page (`/p/<project>/stats`: gauges, trends from health snapshots, where the time goes, options, drill-downs; `/stats` across projects), per-issue stats sidecar, token-cost estimation; the issue page's Timeline tab (`?tab=timeline`), its time rules and the project aggregate
- [The Lead Session](docs/patterns/lead-session.md) — the project lead, one per project (`issue-cli lead`, `approve`), routing, authority, handoff, epics, agents canvas
- [Playtest Sessions](docs/patterns/playtest/README.md) — an issue's manual checks as YAML sessions: shared contract, [web app example](docs/patterns/playtest/web-app.md) (this repo, chromedp), [game example](docs/patterns/playtest/game.md); `issue-cli process playtest`

Per-system docs:

- [API](docs/API/overview.md) — endpoints, handlers, server logic
- [CLI](docs/CLI/overview.md) — issue-cli commands, output contracts
- [CLI Usage Telemetry](docs/CLI/telemetry.md) — local names-only usage log, `issue-cli telemetry report`, opt-out, `parseFlags` rule for new commands
- [UI](docs/UI/overview.md) — templates, views, client-side behavior
- [Workflow](docs/Workflow/overview.md) — workflow engine, transitions, overlays
- [Types of Work](docs/Workflow/types.md) — per-type paths (feature/tweak/bugfix/epic) as overlays with `path:`, `set-type`, lint

## Sessions

`sessions/` holds lead-session handoff notes, one file per handoff (`YYYY-MM-DD-lead-session.md`). Each note records running agents, issue and branch state, owner decisions and rules, and how to resume after a restart. When resuming lead work, read the newest file first. Before a shutdown or a long break, write a new one.

## Adding New Statuses

Status colors are defined in the `statusColor` template function in `template_funcs.go` `funcMap`. Board column order and descriptions come from `workflow.yaml` statuses.
