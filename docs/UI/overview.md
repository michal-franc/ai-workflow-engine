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
- `templates/docs.html` — documentation viewer with sidebar navigation
- `static/style.css` — all CSS (dark GitHub theme)

## Views

| Route             | Template      | Description                                     |
|:------------------|:--------------|:------------------------------------------------|
| `/`               | `list.html`   | Filterable list with status, system, priority    |
| `/board`          | `board.html`  | Kanban with configurable columns and card fields |
| `/graph`          | `graph.html`  | Workflow DAG with stale highlighting             |
| `/docs`           | `docs.html`   | Docs with collapsible sidebar sections           |
| `/issue/<slug>`   | `detail.html` | Detail with edit, approve, dispatch, comments    |
| `/stats`          | `stats.html`  | Workflow token-cost estimates (see [Workflow Stats](../workflow-stats.md)) |

## Design Considerations

When working on UI changes:

- Templates use Go's `html/template` — changes require a server restart (assets are embedded)
- For work that launches local tools or editors, decide whether to reuse existing tmux/alacritty patterns
- Handlers must not block on local processes — use goroutines for async operations
- User feedback after actions (dispatch, approve, save) goes through the toast notification system
- Board card fields and columns are driven by `workflow.yaml` — see [Board Configuration](../board-configuration)
- The detail view substitutes `<!-- data -->` markers in the rendered body with an inline data table (status dropdown + contenteditable comment + remove button). Markdown HTML comments require `goldmark/renderer/html.WithUnsafe()`, which is enabled in `internal/tracker/issue.go`. See [Per-issue Data Store](../data-store.md).
