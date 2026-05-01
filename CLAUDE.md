# Issue Viewer

A Go web app that renders markdown issue files as a GitHub-style project tracker with list, kanban board, and documentation views.

## Running

```bash
go build && ./issue-viewer -dir ./issues -docs ./docs -port 8080
```

Flags:

- `-dir` — issue markdown directory (default `./issues`), supports subdirectories
- `-docs` — documentation markdown directory (default `./docs`)
- `-port` — HTTP port (default `8080`)

## Validation

`make validate` runs `go vet ./...`, the full test suite, and a coverage gate on
`cmd/issue-cli` (floor: `CLI_COVERAGE_FLOOR`, default 70). Use it before
proposing a change that touches `cmd/issue-cli/`.

## Project Structure

```
main.go                     — entry point, CLI flags, starts HTTP server
routes.go                   — Server struct, NewServer, Routes, dispatcher, project list
template_funcs.go           — funcMap, status/priority colors, link rewriters
helpers.go                  — projectRoot, fileExists, workflowFileTarget, small utilities
tmux.go                     — agent session listing/matching/notification
handlers_list.go            — list view, filters, /hash, /issues.json
handlers_board.go           — board + graph views
handlers_detail.go          — detail page, transition preview, data-table render
handlers_issue_mutate.go    — update/create/delete/approve/upload, body editor
handlers_data.go            — /data sidecar endpoints
handlers_comments.go        — comment add/get/toggle/delete
handlers_workflow.go        — designer, retros, bug status, docs handlers
handlers_dispatch.go        — agent dispatch + prompt builders
handlers_github.go          — github page, fetch, import
handlers_stats.go           — /stats workflow token-cost view
issue.go                    — Issue struct, ParseIssue, LoadIssues (walks subdirs)
docs.go                     — DocPage struct, ParseDocPage, LoadDocs
templates/                  — Go HTML templates (list.html, board.html, detail.html, docs.html)
static/style.css            — all CSS (dark GitHub theme)
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

- `status` — one of: `idea`, `in design`, `backlog`, `in progress`, `testing`, `human-testing`, `documentation`, `shipping`, `done`, `none`
- `system` — categorization tag (also used as subdirectory name by sync script)
- `version` — version string, filterable on the board view
- `labels` — list of label strings
- `priority` — one of: `low`, `medium`, `high`, `critical`
- `created` — date string for sorting (newest first)
- `number` — GitHub issue number (used in board card display as `#number`)
- `repo` — GitHub repo in `owner/repo` format

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
- The filename without `.md` becomes the slug used in URLs (`/issue/<slug>`)

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

- [Getting Started](docs/getting-started.md) — installation and first issue
- [Issue File Format](docs/issue-format.md) — frontmatter fields, custom fields, file organization
- [Workflow](docs/workflow.md) — lifecycle, prompts, side-effects, system overlays
- [Agent Workflow Flow](docs/agent-workflow-flow.md) — full dispatch-to-done agent flow
- [Board Configuration](docs/board-configuration.md) — columns, card fields
- [Agent Dispatch](docs/agent-dispatch.md) — terminal config, approval notifications
- [GitHub Integration](docs/github-integration.md) — sync, issue reference, auto-close
- [Per-issue Data Store](docs/data-store.md) — sidecar JSON, `<!-- data -->` marker, `issue-cli data` commands
- [Workflow Stats](docs/workflow-stats.md) — `/stats` tab, per-issue stats sidecar, token-cost estimation

Per-system docs:

- [API](docs/API/overview.md) — endpoints, handlers, server logic
- [CLI](docs/CLI/overview.md) — issue-cli commands, output contracts
- [UI](docs/UI/overview.md) — templates, views, client-side behavior
- [Workflow](docs/Workflow/overview.md) — workflow engine, transitions, overlays

## Adding New Statuses

Status colors are defined in the `statusColor` template function in `template_funcs.go` `funcMap`. Board column order and descriptions come from `workflow.yaml` statuses.
