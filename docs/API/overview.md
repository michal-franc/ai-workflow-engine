---
title: "API Overview"
order: 1
---

## Scope

The API system covers HTTP handlers, routing, JSON endpoints, and server-side logic. The handler code is split across cohesive files by responsibility (routes, list, board, detail, mutate, data, comments, workflow, dispatch, github), with shared helpers in `template_funcs.go`, `tmux.go`, and `helpers.go`.

## Key Files

- `main.go` — `run` (flags, listener, signals), `newHTTPServer` (timeouts), `serve` (graceful shutdown); `main` only calls `os.Exit` on a `run` error
- `background.go` — `background`: goroutines that outlive a request (`Go(name, fn)`, `Wait(deadline)`), with the shutdown context `ctx` and the deadline context `hard`
- `routes.go` — `Server` struct, `NewServer`, `Routes`, `handleProjectRoutes` dispatcher, project list page (logs per-project load errors with the slug)
- `origin.go` — `hostAllowed` (Host allowlist, 421), `sameOriginOnly` (cross-origin write guard), `-host` helpers (`defaultHost`, `listenAddr`, `isLoopbackHost`, `allowedHostsFor`)
- `template_funcs.go` — `funcMap`, `statusColor`/`priorityColor`/`assigneeColor`, `linkIssueRefs`, status ordering helpers
- `helpers.go` — `projectRoot`, `fileExists`, `resolveProjectWorkDir`, `valueOrDash`, `trimSnippet`
- `images.go` — `rewriteRelativeImages` (relative `<img src>` → `/files/`), `handleProjectFile` (image-only, root-confined `/p/<project>/files/` route)
- `tmux.go` — agent session listing (`listTmuxSessions`), matching (`sessionMatchesIssue`), notification (`tmuxSendKeys`), existence probe (`tmuxHasSession`)
- `handlers_list.go` — list view, filters, `IssueView`, `attachScores`, `/hash` polling (plus the `pages` value from `pagesHash`), `/issues.json`
- `handlers_pages.go` — project pages: the frame (`pageView`), `/pages/<n>`, the sandboxed `/page-raw/` route (`resolvePageFile`: page folders only, extension allowlist, no symlink escape), `pagesHash`, `/status.json` (`statusJSON`). See [Workbench Files](../workbench-files.md#project-pages-status_page-pages)
- `handlers_board.go` — board and graph views
- `handlers_detail.go` — detail page, `renderBodyWithDataTable`, `renderDataTable`, transition preview, `findIssueBySlug`
- `handlers_issue_mutate.go` — update/create/delete/approve/upload, edit-in-nvim full-file editor goroutine (writes the entire markdown file to a temp, parse-validates on save, calls `tracker.RewriteIssueFile`)
- `handlers_data.go` — `/data` endpoints (`extractDataSlugAndID` + add/set-status/set-comment/remove)
- `handlers_comments.go` — comment add/get/toggle/delete
- `handlers_workflow.go` — retros, bug status, docs pages
- `handlers_dispatch.go` — agent dispatch, `startAgentSession`, `buildAgentPrompt`, `buildRetrosReviewPrompt`
- `handlers_github.go` — github page, fetch, import
- `handlers_agents.go` — agents canvas (`buildAgentsModel`, `AgentsModel`), agent detail, `handleAgentOpen` (open a session in a terminal), `handleLeadSpawn`
- `internal/agentrun/` — the shared tmux + worktree launcher (`Start`, `Paste`, `CapturePane`) used by the server and `issue-cli`; `lead.go` holds `LeadSpec`/`RecordLead` so `POST /lead/spawn` and `issue-cli lead spawn` start identical sessions
- `internal/tracker/lead.go`, `lead_brief.go` — epic link (`ParentSlug`, `IsEpic`, `EpicChildren`, `EpicBase`), `LeadSessionName`, `LeadFor` routing, `IssueWorktree` (children start from the epic's base), lead briefs and handoff notes
- `internal/tracker/issue.go` — Issue struct, ParseIssue, LoadIssues, frontmatter update (every scalar write goes through `setFrontmatterScalar`: literal replace, `escapeYAMLScalar`), `RewriteIssueFile` (parse-validated, locked, atomic full-file write)
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
- Every request whose `Host` (port stripped, any case) is not `localhost`, `127.0.0.1`, `[::1]` or a `*.localhost` name gets `421 Misdirected Request` before anything else runs (`hostAllowed` in `origin.go`, the outermost wrapper in `Routes`). This applies to every method and route, GETs, `/static/` and `/page-raw/` included. It closes DNS rebinding: a page whose name now resolves to 127.0.0.1 is same-origin to itself, so `sameOriginOnly` would pass it, but it still sends its own name as `Host`. Other names come from `allowed_hosts:` and `viewer_url:`'s host (`configHosts`) in `projects.yaml` and, when `-host` is not loopback, from the bind address (every interface address for `0.0.0.0`, `::` or `""`); `allowedHostsFor` builds the list and the startup warning prints it. The 421 body is one line naming the allowed hosts and the config key. Tests that call `Routes()` with `httptest.NewRequest` must set `req.Host` (it defaults to `example.com`)
- Every non-GET/HEAD request a browser sends for another site gets a 403 before any handler runs (`sameOriginOnly` in `origin.go`, wrapping `Routes`): `Sec-Fetch-Site: cross-site`, or an `Origin` whose host is not the request's `Host` — another port on localhost and `Origin: null` (a sandboxed project page) included. The UI's own fetches are same-origin; curl and `issue-cli` send neither header and pass. Do not add CORS headers to write endpoints
- The server binds `127.0.0.1` by default (`-host`, `defaultHost`); a non-loopback bind prints a startup warning
- The `http.Server` has timeouts: ReadHeader 5s, Read 30s, Write 60s, Idle 120s (constants in `main.go`). `WriteTimeout` bounds a whole handler, so keep handlers well under 60s; long work belongs in the background, not in the request. There are no streaming endpoints; adding one would need its own write deadline (`http.ResponseController`)
- Shutdown: the first SIGINT/SIGTERM cancels the signal context and calls `Shutdown`. In-flight requests and `s.bg` work share a 10s grace, and the names still running at the deadline are logged. The second signal gets the default handling and kills the process
- Never start a bare `go` from a handler for work that outlives the request: use `s.bg.Go("<name>", fn)` so shutdown waits for it and can name it. Inside `fn`, wait or poll on `s.bg.ctx` (cancelled when shutdown starts), and run subprocesses that should finish during the grace period with `exec.CommandContext(s.bg.hard, …)` (cancelled at the deadline). Detached processes that are meant to outlive the server (agent tmux sessions, `issue-cli ci run --detach`) are not tracked
- Changes to issue update endpoints must preserve the atomic write + file lock pattern in `issue.go`
- JSON responses should include a `status` field for consistency
- The viewer never builds a URL from the request: no `Host` / `X-Forwarded-*` rebuild and no `ISSUE_VIEWER_URL` for agents. Absolute links come from `viewer_url` (or the running viewer's address) through issue-cli; see [CLI: `dispatch` and `status`](../CLI/overview.md#dispatch-and-status)
- Tmux-backed flows (`startAgentSession`, `startIssueBodyEditor`) probe `tmuxHasSession` before `tmux new-session`. When a session with the target name already exists they skip setup (no re-prompt of the agent, no nvim relaunch, no save-on-exit goroutine) and just open a terminal attached to the existing session. The `/dispatch` response uses `status: "reattached"` with a single step of the same status; `edit-in-nvim` adds `reattached: true`. The UI renders both as a yellow warning banner/toast distinct from the normal success/error styling. When the project sets `tmux_session`, these flows address the window `=<tmux_session>:=<name>` instead of a session (`agentTmuxTarget`), create it with `new-window` in the shared session, and only open a terminal if no client is attached to it. A new agent is created detached, running as the window's own command with its env (`-e`) and cwd (`-c`) passed to tmux (`agentrun.CreateTmux`, `agentrun.AgentCommand`); nothing is typed into its shell and dispatch never selects its window (`agentrun.ShowStep`). A re-attach still goes through `attachAgentStep`. A lead (`Spec.LeadScope` set) skips both: it follows `lead_terminal:` through `agentrun.LeadTerminalStep` (`internal/agentrun/leadview.go` for `tmux-window`), and `terminal:` placeholders are filled by `agentrun.ExpandTerminal`.

## Endpoints

| Method | Path                                          | Description                    |
|:-------|:----------------------------------------------|:-------------------------------|
| GET    | `/`                                           | Issue list view                |
| GET    | `/board`                                      | Kanban board view              |
| GET    | `/graph`                                      | Workflow status graph          |
| GET    | `/docs`                                       | Documentation viewer           |
| GET    | `/issue/<slug>`                               | Issue detail view              |
| PATCH  | `/issue/<slug>`                               | Update issue frontmatter/body  |
| POST   | `/issue/<slug>/dispatch`                      | Dispatch the issue's worker — body `{agent, force?}`. The spec comes from `agentrun.DispatchSpec`, shared with `issue-cli dispatch`. `409` with `{status: "waiting-on-gate", gate}` when the issue only waits on the owner's approval and no worker is live; `force: true` dispatches anyway |
| POST   | `/issue/<slug>/dispatch/reprompt`             | Re-send the current briefing into the live worker (`agentrun.Reprompt`, as `issue-cli dispatch <slug> --reprompt`); `409` with `status: "no-session"` when none runs |
| POST   | `/issue/<slug>/edit-in-nvim`                  | Open the full markdown file (frontmatter + body) in nvim via tmux. See [Edit in nvim](#edit-in-nvim) below. |
| POST   | `/issue/<slug>/check-script`                  | ▶ Run checks: start the owner's run of the issue's [guided check scripts](../patterns/guided-check-scripts.md) in a tmux window named `check-script-<slug>` (`checks.WindowName`: the slug's `/` and `.` become `-`), in the issue's worktree, running `issue-cli --project <p> check-script <slug>` then a "Press Enter to close" pause, with the viewer's `ISSUE_VIEWER_CONFIG`. It is created like an agent's home (`agentrun.CreateTmux`: a window in `tmux_session`, else its own session) and opened like one (`AttachStep`: the terminal, or the window selected; headless with `terminal: none`). Returns the dispatch response: `status` `started`, `already-running` (the window is live: only brought back), `error`; `404` with `no-scripts` when no script names the issue (`handlers_checks.go`) |
| POST   | `/issue/<slug>/checkbox`                      | Tick or untick one task-list box — body `{line, checked, base_body_hash}`, `line` being the box's `data-line` (1-based in the body). Flips only that box (`tracker.ToggleIssueTaskBox`, under the issue lock) and returns `{body, body_hash}`; `409` when the body changed since `base_body_hash` or the box is already `checked`; `400` when the line holds no task-list box (a `[ ]` in code or prose) or a field is missing. See [Checkbox clicks](../UI/overview.md#checkbox-clicks) |
| POST   | `/issue/<slug>/approve`                       | Toggle human approval; timestamps it in the stats sidecar (`last_approval`) and nudges the agent's tmux session (failures are logged) |
| GET    | `/issue/<slug>/comments`                      | List issue comments            |
| POST   | `/issue/<slug>/comments`                      | Add comment                    |
| POST   | `/issue/<slug>/data`                          | Add a row to the per-issue data store — body `{description, status, tier?}`, returns `{id}` |
| POST   | `/issue/<slug>/data/<id>/status`              | Set a row's status — body `{status}` |
| POST   | `/issue/<slug>/data/<id>/tier`                | Set a row's tier (second axis; pass empty string to clear) — body `{tier}` |
| POST   | `/issue/<slug>/data/<id>/comment`             | Set a row's comment — body `{comment}` |
| DELETE | `/issue/<slug>/data/<id>`                     | Remove a row (id is not reused) |
| GET    | `/hash`                                       | Content hash for polling: `{hash}`, plus `pages` when the project has project pages (changes when a page file changes) |
| GET    | `/status.json`                                | The Status model project pages receive by postMessage |
| GET    | `/pages/<n>`                                  | A `workbench.pages` entry in the viewer frame |
| GET    | `/page-raw/<path>`                            | A project page or asset under a configured page's folder, with CSP `sandbox`, `nosniff`; 404 otherwise |
| GET    | `/agents`                                     | Agents canvas (see [UI](../UI/overview.md#agents-canvas)) |
| GET    | `/agents.json`                                | Canvas model: `project_lead`, `epics` (each open epic's issue and children running/total), `workers`, `unfinished`, `others`, `listed_at` |
| GET    | `/agents/detail?session=<name>&issue=<slug>`  | Selected-agent panel: the issue's last 5 comments, `branch`/`against`/`diff_stat`. `400` unless `session` is an `agent-*`/`lead-*` name |
| POST   | `/agents/open`                                | Open a live session in a terminal, like a dispatch re-attach — body `{session}`. Returns `{status: "opened", steps, attach_cmd, switch_cmd}`. When a terminal is already attached it opens none: `status: "focused"` when i3 brought the window titled with the session name to the front, else `"already-open"`. With `terminal: none` it opens nothing: in `tmux_session` mode it selects the agent's window (`"selected"`). A `lead-*` follows `lead_terminal:` instead: unset or `none` is that same headless answer even when `terminal:` is set; otherwise it runs `agentrun.LeadTerminalStep` (`status: "opened"`; with `tmux-window`, an existing tab is left as it is). `409` with `status: "no-session"` when it isn't running, `400` for a name that isn't `agent-*`/`lead-*` |
| GET    | `/updates`                                    | The Updates tab: workflow features to adopt (see [UI](../UI/overview.md#updates-tab)); `404` for a built-in or `template: custom` workflow |
| POST   | `/updates/decline`, `/updates/reopen`         | Form field `id`: record the feature as declined, or drop it from the record (`workflow scan --declined` / `--forget`); only the `features:` block changes. Redirects to `/updates?declined=<id>` / `?reopened=<id>`, or `?error=` |
| POST   | `/updates/install`                            | Form fields `id`, `words`: start the install agent `agent-<project>-install-<id>` with a brief built from the proposal and the answer. Redirects to `/updates?installing=<id>`; an id that isn't proposed redirects with `?error=` |
| POST   | `/lead/spawn`                                 | Start (or re-attach) the project lead; optional `agent`. Returns the dispatch `{status, session, steps}`. What opens follows `lead_terminal:` like `issue-cli lead spawn`, never `terminal:`: unset is headless and the response carries `attach_cmd`/`switch_cmd`; `tmux-window` adds a detached tab to `lead_window_session` (a `warning` step plus the hints when it can't). A body with `epic` gets `422`: one lead per project |

## Edit in nvim

`POST /issue/<slug>/edit-in-nvim` launches a tmux + nvim session over the full markdown file — frontmatter and body — so keyboard-driven users can edit metadata (labels, priority, custom fields, body) without leaving the editor. The endpoint is fire-and-forget: it returns `{status: "launched", session, message}` once nvim is running, and the actual write-back happens asynchronously after the editor exits.

**Save path.** When nvim exits with code 0, a goroutine in `handlers_issue_mutate.go` runs:

1. Parse the temp file via `tracker.ParseIssue`. On parse error, the change is rejected.
2. Hash-check the on-disk file against the snapshot taken at launch time. If a sidebar save raced, the nvim write loses (existing behavior).
3. Persist via `tracker.RewriteIssueFile`, which holds `withIssueLock`, validates parseability again, and writes atomically through `writeFileAtomically`. The user's exact byte sequence is preserved — comment placement, key order, and blank lines all survive.

**Shutdown.** The save-on-exit goroutine runs through `s.bg` (`awaitBodyEdit`), and its `tmux wait-for` is bound to the shutdown context. If the server stops while nvim is open, the edit session is killed and the issue gets `edit-in-nvim aborted: server shut down; nvim session closed, changes discarded`. A write-back that has already started finishes within the grace period.

**Failure surfacing.** Each rejection path drops a comment on the issue with source `edit-in-nvim` (e.g. `edit-in-nvim aborted: invalid issue file: …`). Users discover failures on next page reload via the existing comments timeline — no polling endpoint or new toast plumbing.

**Trust model.** nvim is treated as a power-user free-form editor: every frontmatter field — including workflow-managed ones like `status`, `started_at`, `human_approval`, `number`, `repo` — can be changed. Bypassing workflow validators is the explicit point of the feature; users who want guarded edits should use the sidebar UI or `issue-cli set-meta` (which enforces `ProtectedFrontmatterFields`). The frontend (`templates/detail.html:editInNvim`) is unchanged; it shows a "launched" toast and walks away.
