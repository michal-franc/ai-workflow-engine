---
title: "API Overview"
order: 1
---

## Scope

The API system covers HTTP handlers, routing, JSON endpoints, and server-side logic. The handler code is split across cohesive files by responsibility (routes, list, board, detail, mutate, data, comments, workflow, dispatch, github), with shared helpers in `template_funcs.go`, `tmux.go`, and `helpers.go`.

## Key Files

- `routes.go` — `Server` struct, `NewServer`, `Routes`, `handleProjectRoutes` dispatcher, project list page
- `template_funcs.go` — `funcMap`, `statusColor`/`priorityColor`/`assigneeColor`, `linkIssueRefs`, status ordering helpers
- `helpers.go` — `projectRoot`, `fileExists`, `workflowFileTarget`, `resolveProjectWorkDir`, `valueOrDash`, `trimSnippet`
- `tmux.go` — agent session listing (`listTmuxSessions`), matching (`sessionMatchesIssue`), notification (`tmuxSendKeys`), existence probe (`tmuxHasSession`)
- `handlers_list.go` — list view, filters, `IssueView`, `attachScores`, `/hash` polling, `/issues.json`
- `handlers_board.go` — board and graph views
- `handlers_detail.go` — detail page, `renderBodyWithDataTable`, `renderDataTable`, transition preview, `findIssueBySlug`
- `handlers_issue_mutate.go` — update/create/delete/approve/upload, edit-in-nvim full-file editor goroutine (writes the entire markdown file to a temp, parse-validates on save, calls `tracker.RewriteIssueFile`)
- `handlers_data.go` — `/data` endpoints (`extractDataSlugAndID` + add/set-status/set-comment/remove)
- `handlers_comments.go` — comment add/get/toggle/delete
- `handlers_workflow.go` — workflow designer (data/preview/save), retros, bug status, docs pages
- `handlers_dispatch.go` — agent dispatch, `startAgentSession`, `buildAgentPrompt`, `buildRetrosReviewPrompt`
- `handlers_github.go` — github page, fetch, import
- `internal/tracker/issue.go` — Issue struct, ParseIssue, LoadIssues, frontmatter update, `RewriteIssueFile` (parse-validated, locked, atomic full-file write)
- `internal/tracker/doc.go` — DocPage struct, LoadDocs
- `internal/tracker/workflow_config.go` — `WorkflowConfig`, `LoadWorkflow`, `DefaultWorkflow`, status/transition accessors, prompt + template helpers
- `internal/tracker/workflow_transition.go` — `ApplyTransition*`, `StartIssueOnce`, `MarkIssueDoneOnce`, `IsValidTransition`, `Next*Status`
- `internal/tracker/workflow_validate.go` — `ValidateTransition`, `Validate`, `checkRule`, `DescribeAction`
- `internal/tracker/workflow_preview.go` — `PreviewTransition` and the `TransitionPreview*` types
- `internal/tracker/workflow_merge.go` — `Clone`, `ForSystem`, `Merge`, `appendUnique*` (system overlay merging)
- `internal/tracker/workflow_schema.go` — reflection-based YAML schema docs (`WorkflowActionTypes`, `WorkflowValidationRules`, `WorkflowSchemaSections`)
- `internal/tracker/heading.go` — shared section/heading helpers (`appendToSection`, `findHeadingMatches`, `parseHeadingLine`, …) used by both the workflow and issue files
- `internal/tracker/data.go` — `DataStore` sidecar (`<slug>.data.json`); the four `/data` endpoints are thin wrappers over `AddEntry` / `SetEntryStatus` / `SetEntryComment` / `RemoveEntry`. See [Per-issue Data Store](../data-store.md).

## Design Considerations

When working on API changes:

- If the change affects UI-visible state, identify the source of truth (server-side file vs. client state)
- Server-side polling uses the `/hash` endpoint for cache invalidation — check whether your change affects the hash
- Changes to issue update endpoints must preserve the atomic write + file lock pattern in `issue.go`
- JSON responses should include a `status` field for consistency
- Tmux-backed flows (`startAgentSession`, `startIssueBodyEditor`) probe `tmuxHasSession` before `tmux new-session`. When a session with the target name already exists they skip setup (no re-prompt of the agent, no nvim relaunch, no save-on-exit goroutine) and just open a terminal attached to the existing session. The `/dispatch` response uses `status: "reattached"` with a single step of the same status; `edit-in-nvim` adds `reattached: true`. The UI renders both as a yellow warning banner/toast distinct from the normal success/error styling.

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
| POST   | `/issue/<slug>/edit-in-nvim`                  | Open the full markdown file (frontmatter + body) in nvim via tmux. See [Edit in nvim](#edit-in-nvim) below. |
| POST   | `/issue/<slug>/approve`                       | Toggle human approval          |
| GET    | `/issue/<slug>/comments`                      | List issue comments            |
| POST   | `/issue/<slug>/comments`                      | Add comment                    |
| POST   | `/issue/<slug>/data`                          | Add a row to the per-issue data store — body `{description, status, tier?}`, returns `{id}` |
| POST   | `/issue/<slug>/data/<id>/status`              | Set a row's status — body `{status}` |
| POST   | `/issue/<slug>/data/<id>/tier`                | Set a row's tier (second axis; pass empty string to clear) — body `{tier}` |
| POST   | `/issue/<slug>/data/<id>/comment`             | Set a row's comment — body `{comment}` |
| DELETE | `/issue/<slug>/data/<id>`                     | Remove a row (id is not reused) |
| GET    | `/hash`                                       | Content hash for polling       |

## Edit in nvim

`POST /issue/<slug>/edit-in-nvim` launches a tmux + nvim session over the full markdown file — frontmatter and body — so keyboard-driven users can edit metadata (labels, priority, custom fields, body) without leaving the editor. The endpoint is fire-and-forget: it returns `{status: "launched", session, message}` once nvim is running, and the actual write-back happens asynchronously after the editor exits.

**Save path.** When nvim exits with code 0, a goroutine in `handlers_issue_mutate.go` runs:

1. Parse the temp file via `tracker.ParseIssue`. On parse error, the change is rejected.
2. Hash-check the on-disk file against the snapshot taken at launch time. If a sidebar save raced, the nvim write loses (existing behavior).
3. Persist via `tracker.RewriteIssueFile`, which holds `withIssueLock`, validates parseability again, and writes atomically through `writeFileAtomically`. The user's exact byte sequence is preserved — comment placement, key order, and blank lines all survive.

**Failure surfacing.** Each rejection path drops a comment on the issue with source `edit-in-nvim` (e.g. `edit-in-nvim aborted: invalid issue file: …`). Users discover failures on next page reload via the existing comments timeline — no polling endpoint or new toast plumbing.

**Trust model.** nvim is treated as a power-user free-form editor: every frontmatter field — including workflow-managed ones like `status`, `started_at`, `human_approval`, `number`, `repo` — can be changed. Bypassing workflow validators is the explicit point of the feature; users who want guarded edits should use the sidebar UI or `issue-cli set-meta` (which enforces `ProtectedFrontmatterFields`). The frontend (`templates/detail.html:editInNvim`) is unchanged; it shows a "launched" toast and walks away.
