---
title: "Workflow Overview"
order: 1
---

## Scope

The Workflow system covers the workflow engine, transition logic, validation rules, system overlays, and `workflow.yaml` configuration.

## Key Files

- `workflow.yaml` — project workflow definition (statuses, transitions, actions, board config, system overlays)
- `internal/tracker/workflow_config.go` — `WorkflowConfig`, `LoadWorkflow`, `DefaultWorkflow`, status/transition accessors, prompts/templates
- `internal/tracker/workflow_transition.go` — `ApplyTransition*`, `StartIssueOnce`, `MarkIssueDoneOnce`, `IsValidTransition`, `Next*Status`
- `internal/tracker/workflow_validate.go` — `ValidateTransition`, `Validate`, `checkRule` (legacy colon-string rules), `DescribeAction`
- `internal/tracker/workflow_validators.go` — dispatcher for structured rules: translates `WorkflowAction` + `Issue` into the narrow types accepted by the validations sub-package
- `internal/tracker/validations/` — leaf package with one file per structured validator (`field_in.go`, `has_section.go`, `command_succeeds.go`, …); each file registers its `CheckFn` in the `Registry` map. Add a new validator by dropping a file in here and ensuring `init()` calls `register(name, fn)`
- `internal/tracker/workflow_preview.go` — `PreviewTransition` and the `TransitionPreview*` types
- `internal/tracker/workflow_merge.go` — `Clone`, `ForSystem`, `Merge` (per-system overlay handling; `replace: true` edges)
- `internal/tracker/workflow_types.go` — `ForType`, `ForIssue` (base → type → system), `ResolveType`, `RetypeStatus`, `Lint` for types of work
- `internal/tracker/workflow_schema.go` — reflection-based YAML schema docs
- `internal/tracker/heading.go` — shared section/heading helpers used by both workflow and issue code
- `internal/tracker/lead.go`, `lead_brief.go` — the epic link (`parent:`, `umbrella:` alias), the lead's session name, `LeadFor` routing, issue worktrees, and the `leads:` brief
- `cmd/issue-cli/templates/workflow/*.yaml` — bundled templates (`development`, `review`, `writing`), each stamped `template: <name>@<release>` and carrying its own `features:` record
- `cmd/issue-cli/templates/workflow-features.yaml` — the feature catalog: every `workflow.yaml` capability, with detection probes, `sources:` and the text `upgrade` proposes
- `cmd/issue-cli/templates/embed.go` — package `templates`: embeds the bundled templates and the catalog for issue-cli and the viewer
- `internal/workflowupgrade/` — catalog loading, probe walker, `features:` record read/write (`Scan`, `WriteRecord`), stamp parsing and `BuildPlan` (catalog.go, plan.go); `CheckBranch` for `workflow catalog-check` (check.go); the guard tests (guard_test.go)
- `cmd/issue-cli/cmd_upgrade.go`, `cmd_scan.go`, `cmd_catalog_check.go` — the `workflow upgrade` / `scan` / `catalog-check` commands; `updates.go` — the viewer's Updates tab

## Workflow Structure

A workflow defines:

- **Statuses** — ordered lifecycle stages with descriptions and prompts
- **Transitions** — allowed moves between statuses with ordered actions
- **Board config** — which columns and card fields appear on the kanban board
- **System overlays** — per-system prompt and transition overrides
- **Types of work** — per-type paths through the statuses, picked by the issue's `type:` ([Types of Work](types.md))
- **Lead briefs** — `leads:` with the `project` and `epic` prompts lead agents are spawned with (see below)
- **Template stamp and feature record** — which bundled template the file came from, and which catalog features it has installed or declined ([below](#template-stamp-and-feature-record)). The engine never acts on these; only `issue-cli workflow upgrade` and `workflow scan` read them

## Transition Validity

A transition `from → to` is allowed when any of:

- An explicit `transitions:` entry with matching `from` and `to` is declared in YAML — this covers backward edges (e.g. `waiting-for-team-input → in progress`) and forward edges that skip indices in the `statuses:` list.
- No explicit edge is declared and `to` is the next status in the `statuses:` list (the linear `+1` fallback).
- `to` is ahead of `from` and every status strictly between them in the `statuses:` list is marked `optional: true`. Optional statuses are skippable on forward transitions.

Any `from → to` that matches none of those is rejected by both `ApplyTransitionToFile` and the transition-preview endpoint. Error messages point at the next *required* status via `NextRequiredStatus`, so hints never tell you to go into a status you can skip.

## Optional Statuses

A status marked `optional: true` remains part of the lifecycle but can be skipped on forward transitions. Useful for parking states (e.g. `waiting-for-team-input`) that only apply when a specific condition fires.

```yaml
statuses:
  - name: "in progress"
  - name: "waiting-for-team-input"
    optional: true
    description: "Parked — blocked on another team (skip if not blocked)"
  - name: "testing"
```

Behavior:

- `in progress → testing` is valid (skips the optional status).
- `in progress → waiting-for-team-input` is still valid (ordinary forward step).
- Backward transitions into an optional status still require an explicit `transitions:` edge.
- `issue-cli process workflow`, `issue-cli process transitions`, `issue-cli stats`, and `issue-cli show <slug>` all render `(optional)` next to the status name.
- In the web UI, optional board columns show an `optional` badge and italic title; on the graph view the node uses a dashed border and the incoming arrow is dashed.
- The default `== Next ==` hint printed by `issue-cli transition` and `issue-cli start` points at the first non-optional status after the current one, with any intervening optional statuses listed under an `Optional side-paths:` block. This keeps agents on the required path by default while still surfacing the sidesteps.
- Below the transition command line, the hint also prints the next transition's contract: a `Requires:` sub-block listing every validation rule and approval gate that blocks the transition, and a `Will:` sub-block listing every side-effect (appended sections, frontmatter changes, prompt injection). Wording matches `issue-cli process transitions` exactly (both come from `tracker.DescribeAction`). Either bucket is omitted when empty so transitions with no rules render unchanged. Agents should read the contract before attempting the transition rather than discover requirements via validation errors. The `transition --json` output carries the same data as `next_requires` / `next_side_effects` (omitempty).
- When the next step requires a human approval (other than `done`), the hint shows the plain command (`issue-cli start <slug>` from a handoff status, otherwise `issue-cli transition …`) followed by a one-line note: tell the human what to approve, then stop and end the turn. JSON carries it as `next_command` / `next_command_note`. Agents don't block or poll; the approval notification resumes them.

`WorkflowConfig.DefaultNextStatus(current)` returns `(required, optionals)` — the first non-optional status after `current`, plus the optional statuses skipped to get there. If every remaining status is optional, `required` is empty and `optionals` holds all of them so callers render alternatives rather than silently picking one. The JSON shape from `issue-cli transition --json` carries these as `next_status` (required) and `optional_next_statuses` (skipped optionals).

Side statuses are never suggested as the next step. `WorkflowConfig.IsSideStatus(name)` is true for an optional status that is `global` or reached only by a `from: "*"` edge, such as `parked`. When no required status is left, `issue-cli transition` and `start` drop side statuses from the remaining optionals. An optional status with its own lifecycle edge (say `done → archived`) is still offered. If nothing is left and the status is not optional, `transition` prints `== Done ==` instead of `== Next ==`. `--json` sets `done: true`, and the next status's entry guidance (parked's "Don't work on it") is never printed. Parking an issue is not done: a move to `parked` prints parked's guidance.

### Approvals on Optional Side-Paths

When a transition targets an `optional: true` status **and** has a `require_human_approval` action, the detail view does not render the approval checkbox inline alongside the required-path approval — that would put two pending approvals side-by-side and obscure the default next step. Instead:

- Every required-path approval (target is non-optional) renders as a normal checkbox. If a status has more than one non-optional approval transition, each one renders its own approve button — they are not collapsed into a single control, so every required approval is reachable from the detail view.
- Each optional-path approval renders as a CTA button. Clicking the CTA reveals the same approval checkbox for that specific transition (with a Cancel link to collapse back). If the issue's `human_approval` already matches the optional target, the widget starts revealed and the CTA is suppressed.

The CTA button label is configurable via a `cta_label` field on the transition:

```yaml
transitions:
  - from: "in progress"
    to: "waiting-for-team-input"
    cta_label: "Block on another team — park until they respond"
    actions:
      - type: "require_human_approval"
        status: "waiting-for-team-input"
```

When `cta_label` is unset the template falls back to `Divert to <status> — <description>`. System overlays may override `cta_label` per-system via the standard transition merge.

## Transition Actions

Each transition can have ordered actions:

| Action                     | Description                                              |
|:---------------------------|:---------------------------------------------------------|
| `validate`                 | Check a rule (body not empty, checkboxes checked, etc.)  |
| `require_human_approval`   | Block until human approves in the web UI                 |
| `append_section`           | Add a `## <title>` section with its checklist to the issue body. See [Gate sections](#gate-sections-append_section) for when it creates the section and when it tops up an existing one. |
| `inject_prompt`            | Add extra guidance for this specific transition          |
| `set_fields`               | Update a frontmatter field. Supported `field` values: `assignee`, `priority`, `status`, `human_approval` (alias `approved_for`). An empty `value` clears the field. |

### Gate sections (`append_section`)

A gate such as `section_checkboxes_checked: Testing` reads the boxes under a level-2 `## Testing` heading, so `append_section` uses the same heading to decide whether the section is there:

- **Only `## <title>` counts.** A `### Testing` inside `## Design`, the text `## Testing` in a paragraph, or a heading inside a fenced code block (``` or `~~~`) is not the section. The title compares case- and spacing-insensitively.
- **Missing:** `## <title>` and the action's body are appended at the end of the issue.
- **Present:** each checkbox of the action's body that no `## <title>` section holds yet (same text, ticked or not, case- and spacing-insensitive) is added to the first one. Boxes already there keep their ticks, so a section written by hand, or left from an earlier round after a backward move, still gets the gate's boxes, and a second pass adds nothing. Prose lines of the body are not re-added.
- **Sub-headings:** boxes under a `### X` in the action's body go under the section's own `### X` (appended at the section's end when it has none). Boxes before the body's first sub-heading go before the section's first sub-heading. This keeps the bundled `## Test Plan` (`### Automated` / `### Manual`) intact.

`transition` and `start` name each section: `## Testing appended`, or `## Testing existed — added 2 missing boxes` (`sections` in `--json`). `transition --dry-run` replaces `appends ## Testing section` with `## Testing exists — adds 2 missing boxes` or `exists with all its boxes — left as is` when the issue already has the section. A legacy status `template:` that opens with a heading follows the same rules.

## Declarative Fields (v0.5.0+)

Transitions can declare `fields[]` — prompts the web UI collects via a modal before the move commits. Both the CLI and the board use the same engine, so dragging a card is equivalent to `issue-cli transition`.

```yaml
- from: "*"
  to: "deferred"
  fields:
    - name: "deferred_to"
      prompt: "Deferred to whom?"
      target: "frontmatter"   # writes deferred_to: "<answer>" to frontmatter
      required: true
    - name: "deferral_reason"
      prompt: "Reason for deferral"
      target: "section:Deferred Record"  # appends "- **Reason...:** <answer>" under ## Deferred Record
      type: "multiline"
      required: true
```

- `target: frontmatter` writes an arbitrary scalar key (protected keys like `status`, `title`, `human_approval` are always refused).
- `target: section:<Title>` appends `- **<prompt>:** <answer>` under the section, creating it if missing.
- `type: multiline` hints the UI to render a textarea.
- `required: true` blocks the transition when the answer is empty.

For `target: frontmatter` fields the validator falls back to the issue's existing frontmatter when no answer is supplied. This means `issue-cli set-meta <slug> --key X --value Y` followed by `issue-cli transition --to <status>` succeeds without re-supplying `X` — the frontmatter is the source of truth. `target: section:<Title>` fields do not fall back; they record a fresh line on each transition and so always require an explicit answer.

From the CLI, declarative answers can also be passed inline with repeatable `--field key=value` flags:

```bash
issue-cli transition <slug> --to "deferred" --field deferred_to=alice --field deferral_reason="capacity"
```

Explicit `--field` values win over frontmatter fallback when both are present.

## Wildcard Source (`from: "*"`)

A transition with `from: "*"` matches every source status that lacks a more specific edge. Useful for "defer from anywhere" or similar fork-off rules. Exact `from: <status>` edges always win.

## Global Status (`global: true`)

A status marked `global: true` is an escape hatch: transitions out of it to any other status are allowed, regardless of the linear lifecycle. The board column renders a `global` badge next to the existing `optional` badge. Combine with `from: "*"` to build parked states (deferred, blocked, on-hold) without having to enumerate every edge.

## Side-Effects

Statuses support `side_effects` that run automatically after a transition:

- `clear_assignee` — clears the assignee field

```yaml
- name: backlog
  side_effects: [clear_assignee]
```

## Custom Actions

Custom actions are one-shot agent buttons rendered on the issue detail view, below the built-in Claude/Codex dispatch buttons. Unlike a transition, an action does **not** validate or mutate the issue — it just opens a tmux agent session briefed with a fixed prompt. Use them for lightweight coordination tasks ("defer to team", "summarize for standup", "draft a release note").

Top-level `issue_actions:` list in `workflow.yaml`:

```yaml
issue_actions:
  - id: "defer-to-team"        # stable id; used in the URL and tmux session name
    label: "Defer to team"     # button text
    agent: "claude"            # claude (default) or codex
    prompt: |
      Issue "{{title}}" ({{slug}}, status: {{status}}) is being deferred to the team.
      Draft a short handoff note and add it as a comment:
      issue-cli comment {{slug}} --text "deferred: <summary>"
```

> **Backward compatibility:** `issue_actions` was originally named `actions`. The old `actions:` key still works as a deprecated alias — existing configs need no change. If both are present, `issue_actions` entries win on an id clash and legacy-only ids are merged in. Prefer `issue_actions` in new configs; it parallels `project_actions`.

- Each action becomes a button on every issue's detail view.
- Clicking it POSTs to `/p/<project>/issue/<slug>/action/<id>`, which dispatches a fresh agent.
- The session name is `agent-<slug>-<id>`, separate from the main `agent-<slug>` dispatch session, so an action runs alongside (not on top of) a working agent.
- Actions run in the project checkout, not a per-issue worktree — they are lightweight prompts, not code-change sessions.
- Prompts are templated with `{{slug}}`, `{{title}}`, `{{status}}`, `{{system}}`, `{{priority}}`, and `{{number}}`. Unrecognized `{{...}}` tokens are left verbatim.

### Project-Level Actions

Project-level actions are the same one-shot agent buttons, but **not bound to a single issue**. They render on every project page, under **Actions** in the sidebar, below **Spawn lead**. Use them for project-wide chores ("triage the backlog", "summarize all in-progress work for standup").

Top-level `project_actions:` list in `workflow.yaml`, with the same fields as `actions:`:

```yaml
project_actions:
  - id: "triage-backlog"       # stable id; used in the URL and tmux session name
    label: "Triage backlog"    # button text
    agent: "claude"            # claude (default) or codex
    prompt: |
      Review every backlog issue in {{project}} and propose a priority order.
```

- Each action renders as a button on the list, board, and graph views.
- Clicking it POSTs to `/p/<project>/action/<id>`, which dispatches a fresh agent with no issue slug.
- The session name is `agent-<project-slug>-<id>`.
- Like issue actions, they run in the project checkout (never a worktree).
- Because there is no issue context, prompts are templated with `{{project}}` (the project name) only. Unrecognized `{{...}}` tokens are left verbatim.
- The bar always starts with **Spawn lead**, even when `project_actions:` is empty: it starts (or re-attaches to) the project lead, `lead-<project>`. See the next section.

### Lead Brief (`leads:`)

`issue-cli lead spawn` and the Spawn lead button start the [project lead](../patterns/lead-session.md). Its brief comes from a top-level `leads:` block:

```yaml
leads:
  project: |
    You are the project lead of {{project}}, running in {{lead}}.
```

- Placeholders are `{{project}}` and `{{lead}}`. `issue-cli` commands get `--project` injected.
- A missing key uses the built-in brief, so a workflow without `leads:` still gets a working lead.
- The CLI appends a fixed footer: role, what the lead may tick, how to message other agents, and its handoff note under `sessions/`.
- `leads:` is project-wide: type, system and per-issue overlays keep it as is (`Clone` copies it). `issue-cli process schema` lists it.
- An old `leads.epic` key still loads but does nothing; `Lint` warns about it, so `issue-cli process workflow` shows it under Warnings.
- The lead changes no statuses or transitions. An epic runs the normal workflow (or its `types:` path); children link to it with `parent:` and get worktrees like any other issue.

## Per-Issue Worktrees

The dispatch handler creates a per-issue git worktree before launching the agent's tmux session, so concurrent issues cannot stomp on each other's uncommitted state.

Top-level toggle in `workflow.yaml`:

```yaml
worktree: true   # opt in
```

Defaults to `false` when absent — projects keep their existing dispatch behaviour until they explicitly opt in. Recommended for projects that dispatch concurrent agents on different issues at the same time.

Behavior when enabled:

- Branch: `work/<issue-slug>`, created off the project workdir's current HEAD. Epics and their children are no exception; an old `base:` on an epic is ignored.
- Path: `<project.workdir>/.worktrees/<issue-slug>` (gitignored)
- Re-dispatch on the same issue reuses the existing worktree directory rather than re-running `git worktree add`.
- The tree is materialized with `git checkout HEAD` after sparse-checkout patterns are configured. Paths in `worktree_sparse_exclude` (default `["issues/"]`) are kept out of the worktree, so a dispatched agent sees the source but not the issue store. If an old worktree was created empty (every file shown as staged-for-deletion), recover it with `git -C .worktrees/<slug> checkout HEAD`.
- The dispatched tmux session opens with `cwd` set to the worktree path, and the agent's prompt includes a worktree-aware section pointing at the path and branch.
- Worktrees are not cleaned up: they stay after the issue ships, and no checklist asks anyone to remove them. Agents never remove a worktree themselves, since that would risk discarding uncommitted state.

If `git worktree add` fails (dirty tree, branch already exists outside the worktrees dir, etc.), the dispatch surfaces an error step and aborts rather than silently falling back to the primary checkout.

Dispatches without an issue slug (retros review, project-wide commands) never create a worktree regardless of the toggle.

## Local CI (`ci:`)

The optional `ci:` block (`tracker.CIConfig` in `internal/tracker/ci.go`) turns on local CI: its commands run on the integration branch after every move. It is project-wide like `workbench:`, so system and type overlays can't change it, and `Clone` copies it into scoped workflows. `Lint` checks it. A `ci:` entry is a plain string or `{cmd, tier, failures}`.

Nothing about statuses, transitions or overlays changes. Without the block nothing runs and nothing shows. The runner is `internal/ci/` (run records, lock, persistent worktree, blame, watcher, one-liner); the viewer's watcher is `ci_watch.go` and the page model is `ci_view.go`. `tracker.CIStatusLine` is a hook `internal/ci` sets at init, so the dispatch prompt (built in `tracker`) can carry the CI line while the server and the CLI build the same prompt. User guide: [Workflow: Local CI](../workflow.md#local-ci).

## Health snapshots (`health:`)

The optional `health:` block (`tracker.HealthConfig` in `internal/tracker/health.go`) tunes project health snapshots: `big_file_lines`, `test_globs`, `exclude`, the per-family `timeout`, and `off`. Every git project gets snapshots without it. It is project-wide like `ci:`, so system and type overlays can't change it, and `Clone` copies it into scoped workflows. `Lint` checks it.

Nothing about statuses, transitions or overlays changes. The collector is `internal/health/`: the family registry, the `.health/snapshots/` files, retention and the codebase family. The server's daily job is `health_watch.go`, and `issue-cli ci run` and `issue-cli health snapshot` take the others. User guide: [Workbench files: Health snapshots](../workbench-files.md#health-snapshots).

## Release candidates (`workbench.release_candidates`)

`workbench.release_candidates: true` (`tracker.WorkbenchConfig.ReleaseCandidates`) puts a release candidate before each release. When the milestone is ready, a local tag `<version>-rc.N` is cut and run on this machine's projects, and the release is published only on the owner's ok. The setting is project-wide like the rest of `workbench:`.

Nothing about statuses, transitions or overlays changes, and no issue needs migrating. The setting changes three things:
- The Waiting for release card (`release_candidate.go`) and `status.md`.
- One line in the lead brief (`releaseCandidateBrief`).
- What `report-bug` does with a report from a candidate build: the bug joins the milestone (`rcMilestone`).

The version stamp (`make install`) and the usage log's `ver` apply to every build, whether the setting is on or not. User guide: [Roadmap: Cutting a release](../roadmap.md#cutting-a-release-candidate-soak-publish).

**On a release branch.** `issue-cli release cut` cuts `release/vX.Y` with its worktree and tags the candidates there; once that branch exists, the card counts the commits picked onto it after the newest rc (not main's) and adds the `picked` check (every done milestone issue's `work/<slug>` commits are on the branch, by patch). `release pick` cherry-picks them; `release publish` publishes from the branch (with `privacy.code: snapshots`). `cmd/issue-cli/cmd_release.go` reads the card from the viewer's `/p/<project>/release.json` (`buildReleaseJSON`), so the CLI refuses on exactly the checks the page shows.

## Privacy (`privacy:`)

`privacy:` (`tracker.PrivacyConfig`, `internal/tracker/privacy.go`) records the owner's choice: `work_files` (tracker | in_repo), `work_paths`, `code` (snapshots | public), `public_remote`, `public_branch` and `decided` (their words). It is project-wide; `Lint` warns about unknown values, a work path outside the project, and an empty `decided:`. No status, transition or overlay changes, and no issue needs migrating; a project without the block shows only the facts git gives (a red Work badge when it tracks work files).

What reads it:
- `internal/privacy`: the tracker repo (`Tracker.Init`, `Sync`, the sync record), releases (`Setup`, `Cut`, `Pick`, `Publish`, `IsSnapshot`), the badges (`WorkBadge`, `CodeBadge`) and the pre-push guard (`InstallHook`, `PrePush`, `DecisionQuotes`).
- `issue-cli tracker`, `privacy` and `release`; `issue-cli init` (asks both questions on a terminal).
- The viewer: `tracker_watch.go` (sync every 10 min), `privacy_view.go` (Status badges and Health lines, projects list, `/stats` overview), the Waiting for release card's publish step.
- The lead brief (`snapshotsBrief`, `trackerBrief`).
- `.github/workflows/privacy-guard.yml` (PRs) and `release-marker.yml` (skips private repos).

User guide: [Workflow: Private work and code](../workflow.md#private-work-and-code).

## System Overlays

System-specific overrides are defined under the `systems:` key. They merge with the base workflow — overriding status prompts and appending transition actions for that system:

```yaml
systems:
  API:
    statuses:
      - name: "in design"
        prompt: |
          Extra API-specific design guidance here.
    transitions: []
```

Overlays can only add guidance and actions; they cannot drop a status or a gate. For a different path per kind of
work (tweak, bugfix, epic), use `types:` — see [Types of Work](types.md). Resolution is base → type → system.

## Template Stamp and Feature Record

Two optional top-level keys tie a project's `workflow.yaml` to the templates and the feature catalog bundled in `issue-cli`:

```yaml
template: development@v0.36.0     # bundled template + the release that last changed it; "custom" opts out
features:
  installed:                      # catalog features this file has, as <id>@<version>
    - types-of-work@v0.36.0
    - lead-handoff-prompts@v0.36.0
  declined:                       # features the human said no to; upgrade stops proposing them
    - scoring
```

- `template:` is `WorkflowConfig.Template` and `features:` is `WorkflowConfig.Features` (`FeatureRecord{Installed, Declined}`). Both are declared on the struct so a marshal round trip keeps them, and `process schema` lists them. The engine never acts on either.
- `template: custom` marks a hand-built workflow: no proposals and no Updates tab. A file with no `template:` is **unstamped**; `upgrade` compares it with `development` (or `--template <name>`).
- "Latest" is the stamp of the template bundled in the running binary, not `issue-cli version`. Releases that don't change a template don't make projects look behind, and `dev` builds work.
- Older binaries ignore both keys (yaml.v3 decoding is not strict).

### Feature catalog

`cmd/issue-cli/templates/workflow-features.yaml` (embedded) lists every capability a `workflow.yaml` can use, grouped by category: lifecycle (status prompts, optional and parked statuses, wildcard transitions, transition fields, types, system overlays), gates (one entry per validator family), automation (`append_section`, `inject_prompt`, `set_fields`, local CI and its test results), actions, board, workbench (the viewer's pages: health settings, the Status changelog, system pages, project pages, metrics and knobs, the balance board), dispatch (worktrees, the lead brief), and conventions (template wording such as `lead-handoff-prompts`, which tells agents to say what they need at a gate and end their turn, and `local-merge-shipping`). Each entry has `id`, `version`, `category`, `propose`, `templates`, `requires`, `sources`, `title`, `what`, `why`, `where`, `snippet`, `question`, `setup` and `detect`.

Entries added in v0.54.0, when the catalog caught up with what projects adopt:

| id | category | config | proposed |
|---|---|---|---|
| `local-ci` | automation | `ci:` (run, tier, failures, timeout, on_red) | yes |
| `ci-test-results` | automation | `ci.run[].results` | yes, once the project has `local-ci` |
| `health-tuning` | workbench | `health:` (limits, test_globs, e2e_sessions, …) | yes |
| `status-changelog` | workbench | `workbench.changelog` (without it the Status page reads CHANGELOG.md, else git tags; declined, it stops suggesting the key) | yes |
| `release-candidates` | workbench | `workbench.release_candidates` | catalog only (needs `status-changelog`) |
| `system-pages` | workbench | `workbench.systems`: summaries, design docs, families (the pages come from `system:` without it) | yes |
| `project-pages` | workbench | `workbench.status_page`, `workbench.pages` | yes |
| `metrics-and-knobs` | workbench | `workbench.metrics`, `workbench.tuning` (without them Metrics lists Branches) | yes |
| `balance-board` | workbench | `workbench.balance.comment_prefix` | catalog only |
| `lead-prompt` | dispatch | `leads.project` | yes |
| `local-merge-shipping` | conventions | the shipping prompt: `merge --ff-only`, never push | yes, `development` only; the bundled template keeps push |

- `propose: true` entries are the ones `upgrade` offers to projects that lack them. `propose: false` entries are catalog-only: scanned and recorded, and listed only by `upgrade --all`.
- `requires: [<id>]` holds an entry back until the project has those features; `upgrade --all` lists it with `needs <id>`.
- `sources:` names the code behind the feature: repo globs anchored at the root (`internal/ci/**`), `file.go#Symbol` for one Go declaration, or `@prompt` for a convention that is prompt text only. `workflow catalog-check` reads them (see below).
- `setup:` is an optional ordered list of `{who: owner | agent, do: …}` steps beyond the snippet, for a feature that sets things up outside `workflow.yaml` (repos, hooks). The Updates card lists them ("you" on the owner's), `workflow upgrade` prints them, and the install agent's brief walks them, telling the operator each owner step and waiting, never doing it. `TestSetupStepsSayWhoDoesThem` keeps `who` to the two values. `privacy` is the first entry with steps.
- `detect` is a list of **structural probes** over the parsed YAML; the feature is present when any probe matches. A probe has a `path` (`key`, `key[]` for each list item, `*` for each map value) and at most one condition: none (the value is set: not empty, `false`, `0` or null), `equals`, `one_of`, `rule_in` (the rule name before `:`), or `contains` (case-insensitive, inside that one field only). A path that starts with `statuses[]` or `transitions[]` also searches every `types.<t>` and `systems.<s>` overlay. For example, a parked status is `statuses[].global equals true`, so `optional: true` on its own doesn't count.
- The data store, stats, retros and comments are not catalogued: they are not `workflow.yaml` configuration.

**Copy model.** Installing a feature means an agent copies its snippet, adapted to the project's statuses and sections, into `workflow.yaml`. Nothing from the catalog is merged in at load time, so features that reshape the workflow (types of work) are installed the same way as one-line ones.

### Upgrading workflow.yaml

1. The operator sees the proposals on the viewer's **Updates** tab (`/p/<project>/updates`; `/updates` compares every project). From there they decline one, re-open it, or start an install agent. No `issue-cli` command mentions upgrades unless it is asked: workers and leads are never nudged.
2. `issue-cli workflow upgrade` (read-only) lists the proposals for the project's template. With a `features:` record, the record decides: every proposed entry that is neither installed nor declined is listed, whatever the stamp says, so bumping the stamp can't hide an undecided feature. Without a record, what the file has comes from the probes and only entries newer than the stamp are listed (all of them if unstamped). Declined features are listed with the re-open command, and an installed feature with a newer catalog version is proposed as an update.
3. The agent proposes the items one at a time, backs the file up to `workflow.oldN.yaml` and writes the picked snippets. It sets `template: <name>@<latest>` only once every proposal is installed or declined.
4. `issue-cli workflow scan --installed <picked> --declined <declined>` records the result. Scan rewrites only the `features:` block.
5. `issue-cli process workflow` validates the result. Existing issues keep the checklist sections they already have.

A current stamp is `up_to_date` only when the record decides everything: `undecided` while proposals wait on the owner, `stale_record` when the file has features the record doesn't list. `process workflow`, `health lint` and the Updates tab ("not recorded") flag a stale record; undecided proposals show only on the Updates tab and in `upgrade`.

Command details: [CLI overview](../CLI/overview.md#workflow-upgrade).

### Adding or changing a catalog entry

1. Add or edit the entry in `workflow-features.yaml` with `version:` set to the next release (the one after the newest `## vX.Y.Z` in `cmd/issue-cli/CHANGELOG.md`) and its `sources:`. Write probes against parsed paths, not raw text; use `contains` only for wording conventions, scoped to the field that carries the text.
   - Put the entry in its category's section: output groups by category in catalog order, and `TestCatalogKeepsEachCategoryTogether` fails on an entry outside its section. Moving one reorders the bundled templates' `features:` records too, since they list entries in catalog order.
   - When the viewer shows something without the key (system pages from `system:`, Branches in Metrics, the default CHANGELOG.md), say so in `what:` and keep the title on what the key adds, so the entry doesn't read as a switch.
2. If a bundled template gains the feature, bump its `template: <name>@<version>` stamp and re-run `issue-cli workflow scan` on the template file so its `features:` record matches.
3. `make validate` enforces it:
   - `go test ./internal/workflowupgrade/` checks that every field is filled, ids are unique, snippets parse and are detected by their own probes, each probe has at most one condition and its path resolves in the `WorkflowConfig` structs, every source matches a tracked file (and its `#Symbol` exists), every stamp version has an entry, no entry is newer than its template's stamp, and each template's record equals what scan detects in it.
   - `TestEveryWorkflowFeatureHasACatalogEntry` fails on a `workflow.yaml` key with no entry: every top-level key, the sub-keys of `ci`, `ci.run[]`, `health`, `leads`, `workbench` and `workbench.systems.*`, every validator rule and every action type. A key counts when a probe starts at it or an entry's snippet sets it; the rest go in `nonFeatureKeys` with a reason (a stale line fails too).
   - `issue-cli workflow catalog-check` (it compares commits, so before the branch's first commit it prints "skipped: not a work branch") fails on a work branch that changes an entry's `sources:` (for `file.go#Symbol`, that declaration's code; doc comments don't count) or a bundled template without bumping its version past the last release. The message names the entry, the files and the version to set. When nothing a project adopts changed (a refactor, a fix), a commit trailer `Catalog: n/a <id>[,<id>] <why>` on the branch passes it instead. It skips on main.
4. issue-viewer's own workflow asks too: the Design checklist has a catalog box (which entry the change adds or bumps), and Documentation asks again for config, validators, actions and prompt patterns, which the checks can't see.

## Validation Rules

Validators come in two flavors:

- **Legacy colon-string rules** live in `workflow_validate.go:checkRule` (one switch case per rule name). Encoded as `rule: "name: arg"` on the action — the tracker splits at `: ` and dispatches to the switch.
- **Structured rules** live one per file under `internal/tracker/validations/`. Each file registers a `CheckFn` against a rule name in the package-level `Registry`. The tracker (`workflow_validators.go:checkAction`) translates `WorkflowAction` + `Issue` into the narrow `validations.Action` + `IssueView` and calls `validations.Check(action, view, cfg)`.

To add a new structured validator:

1. Drop a file `internal/tracker/validations/<rule_name>.go` with an `init() { register("<rule_name>", <Fn>) }` and the `CheckFn` body.
2. Add a corresponding `_test.go` in the same package (cover pass + fail).
3. Append a `SchemaNamedDoc` entry to `WorkflowValidationRules` in `workflow_schema.go` so `issue-cli process schema` lists it.
4. Extend the catalog in `docs/workflow.md` and the schema fields on `WorkflowAction` in `workflow_config.go` if the validator needs new companion fields.
5. Extend `structuredSummary` in `workflow_validators.go` so the transition preview renders a meaningful one-liner.

The `validations` package is a leaf — it does not import `tracker`. This is what makes the per-validator-per-file split possible without import cycles.

`criteria_proven` is a colon-string rule whose check lives outside `tracker`: it needs git and playtest runs, so `internal/proof` registers it with `tracker.SetProofChecker` in `init`, and the binaries import that package (see [Proof Links](proof.md)). A transition hands validators the issue's full slug (`folderSlug`: system folder included), the same as `LoadIssues`.

`linked_issue_in_status` and `command_succeeds` need runtime context (an issue lookup and the project's working directory); both are populated automatically by `Project.LoadWorkflow` via `attachRuntime`. `command_succeeds` is gated by the top-level `allow_shell: true` flag in `workflow.yaml`.

## Playtest Sessions in the Dev Workflow

This repo's `workflow.yaml` asks for manual checks to be written as playtest sessions. The shared contract is [docs/patterns/playtest/README.md](../patterns/playtest/README.md) (`issue-cli process playtest`), and this repo's runner is described in [web-app.md](../patterns/playtest/web-app.md).

- **`backlog → in progress`:** the Test Plan's `### Manual` boxes ask for a session in `playtest/sessions/<name>.yaml` (step id = checkbox id) that passes `make playtest-check`. Checks that no session can drive (real Claude behaviour, terminal-only flows) stay written steps, each with a `.agent-logs/<name>-sandbox/`.
- **`in progress → testing`:** the `## Testing` section adds a box for `make ux-test ISSUE=<slug> REPEAT=2`, run once before handoff.
- **The `human-testing` prompt:** sends the owner to `make playtest ISSUE=<slug>` (judgement steps in an overlay) and `make playtest-report ISSUE=<slug> APPLY=1`. The report ticks the boxes that passed and posts each failure and opinion as a `playtest:` comment.
- **The UI overlay's `in design` prompt:** asks for the test ids the session will point at.
- **The bundled `development` template:** has opt-in wording, "if the project has a playtest system", so other projects aren't forced to adopt one.

Issues that already have the old boxes ("Throwaway sandbox scenario prepared") keep them; only new transitions append the new wording.

## Guided Check Scripts in the Dev Workflow

Checks a session can't drive (CLI commands, API routes, tmux, terminal-only flows) come as [guided check scripts](../patterns/guided-check-scripts.md): `scripts/checks/<name>.sh` on the issue branch, run by `issue-cli check-script <slug>`. It is the `guided-check-scripts` catalog feature (v0.54.0), in the bundled `development` template and this repo's `workflow.yaml`:

- **`backlog → in progress`** (and `in design → in progress`): a Test Plan `### Manual` box, "Manual checks that aren't playtests come as `scripts/checks/<name>.sh` (`issue-cli check-script <slug> --new <name>`), or N/A with the reason". Tweak and bugfix Manual boxes mention it next to the playtest session.
- **The `testing` prompt:** start the script with `--new`, read `check-script --lib`, name each step's box with `--box`, pass `check-script <slug> --yes` before handing over, and never tick the owner's Manual boxes.
- **The `human-testing` prompt:** the owner runs `issue-cli check-script <slug>` (▶ Run checks); it ticks the boxes that passed and comments failures as `checks:`, read like `playtest:` comments.
- **`documentation → shipping`** (and a tweak's `testing → shipping`): a Shipping box, "Check scripts removed in a last commit before the merge, or none", and the shipping prompt says so: scripts stay on the issue branch (D-2).
- When an issue has both a playtest session and a script, each Manual box belongs to one of them (a session step id, a script step's `--box`). The issue page asks for the playtest first, then the checks, then the approval; the Status page, the board card and the issue-approve band show both.

The feature is detected by `check-script` in a status prompt or a transition body. Existing issues keep their Test Plans.

## Design Considerations

When working on workflow changes:

- State which statuses, rules, templates, or overlays will change
- Consider whether existing issues need migration or compatibility handling
- For new validators, follow the steps in *Validation Rules* above
- For any new `workflow.yaml` capability (a field, action type or validator), add a feature catalog entry, and bump an entry's version when its feature changes; see *Adding or changing a catalog entry* above (`make validate` checks both)
- Test with `go test ./internal/tracker/...` (engine) and `go test ./internal/tracker/validations/...` (per-validator suite)
