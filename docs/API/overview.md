---
title: "API Overview"
order: 1
---

## Scope

The API system covers HTTP handlers, routing, JSON endpoints, and server-side logic in `handlers.go`.

## Key Files

- `handlers.go` — all HTTP handlers, template functions, Server struct, agent dispatch
- `internal/tracker/issue.go` — Issue struct, ParseIssue, LoadIssues, frontmatter update
- `internal/tracker/doc.go` — DocPage struct, LoadDocs
- `internal/tracker/workflow.go` — WorkflowConfig, transition engine, validation

## Design Considerations

When working on API changes:

- If the change affects UI-visible state, identify the source of truth (server-side file vs. client state)
- Server-side polling uses the `/hash` endpoint for cache invalidation — check whether your change affects the hash
- Changes to issue update endpoints must preserve the atomic write + file lock pattern in `issue.go`
- JSON responses should include a `status` field for consistency

## Endpoints

| Method | Path                                          | Description                    |
|:-------|:----------------------------------------------|:-------------------------------|
| GET    | `/`                                           | Issue list view                |
| GET    | `/board`                                      | Kanban board view              |
| GET    | `/graph`                                      | Workflow status graph          |
| GET    | `/docs`                                       | Documentation viewer           |
| GET    | `/issue/<slug>`                               | Issue detail view              |
| PATCH  | `/issue/<slug>`                               | Update issue frontmatter/body  |
| POST   | `/issue/<slug>/dispatch`                      | Dispatch agent for issue       |
| POST   | `/issue/<slug>/approve`                       | Toggle human approval          |
| GET    | `/issue/<slug>/comments`                      | List issue comments            |
| POST   | `/issue/<slug>/comments`                      | Add comment                    |
| GET    | `/hash`                                       | Content hash for polling       |
