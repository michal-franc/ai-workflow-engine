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

## Project Structure

```
main.go          — entry point, CLI flags, starts HTTP server
handlers.go      — HTTP handlers, routing, template functions, Server struct
issue.go         — Issue struct, ParseIssue, LoadIssues (walks subdirs)
docs.go          — DocPage struct, ParseDocPage, LoadDocs
templates/       — Go HTML templates (list.html, board.html, detail.html, docs.html)
static/style.css — all CSS (dark GitHub theme)
sync-issues.sh   — downloads issues from GitHub Project into ./issues/<System>/
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

Per-system docs:

- [API](docs/API/overview.md) — endpoints, handlers, server logic
- [CLI](docs/CLI/overview.md) — issue-cli commands, output contracts
- [UI](docs/UI/overview.md) — templates, views, client-side behavior
- [Workflow](docs/Workflow/overview.md) — workflow engine, transitions, overlays

## Adding New Statuses

Status colors are defined in the `statusColor` template function in `handlers.go` `funcMap`. Board column order and descriptions come from `workflow.yaml` statuses.
