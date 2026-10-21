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
- `internal/tracker/lead.go`, `lead_brief.go` — the epic link (`parent:`, `umbrella:` alias), lead session names, `LeadFor` routing, base-branch worktrees, and the `leads:` briefs
- `cmd/issue-cli/templates/workflow/*.yaml` — bundled templates (`development`, `review`, `writing`), each stamped `template: <name>@<release>` and carrying its own `features:` record
- `cmd/issue-cli/templates/workflow-features.yaml` — the feature catalog: every `workflow.yaml` capability, with detection probes and the text `upgrade` proposes
- `cmd/issue-cli/workflow_features.go` — catalog loading, probe walker, `features:` record read/write, `scanWorkflowFeatures`
- `cmd/issue-cli/workflow_upgrade.go` — stamp parsing, `buildUpgradePlan`, the `start`/`process workflow` upgrade hint

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
- When the next step requires a human approval (other than `done`), the hint shows the plain command (`issue-cli start <slug>` from a handoff status, otherwise `issue-cli transition …`) followed by a one-line note: tell the human what to approve, then stop and end the turn. JSON carries it as `next_command` / `next_command_note`. Agents don't block on `--wait` or poll; the approval notification resumes them.

`WorkflowConfig.DefaultNextStatus(current)` returns `(required, optionals)` — the first non-optional status after `current`, plus the optional statuses skipped to get there. If every remaining status is optional, `required` is empty and `optionals` holds all of them so callers render alternatives rather than silently picking one. The JSON shape from `issue-cli transition --json` carries these as `next_status` (required) and `optional_next_statuses` (skipped optionals).

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
| `append_section`           | Add a titled section with checklist to the issue body. Fence-aware: section headings inside fenced code blocks (``` or `~~~`) are not treated as existing sections, so quoting workflow YAML in the body does not block appends. |
| `inject_prompt`            | Add extra guidance for this specific transition          |
| `set_fields`               | Update a frontmatter field. Supported `field` values: `assignee`, `priority`, `status`, `human_approval` (alias `approved_for`). An empty `value` clears the field. |

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

Project-level actions are the same one-shot agent buttons, but **not bound to a single issue**. They render on the project views — list, board, and graph — in an "Actions" bar below the header. Use them for project-wide chores ("triage the backlog", "summarize all in-progress work for standup").

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

### Lead Briefs (`leads:`)

`issue-cli lead spawn [--epic <slug>]` and the Spawn lead / Spawn sub-lead buttons start [lead agents](../patterns/lead-session.md). Their brief comes from a top-level `leads:` block:

```yaml
leads:
  project: |
    You are the project lead of {{project}}, running in {{lead}}.
  epic: |
    You are the sub-lead of {{epic}} ("{{title}}") on base branch {{base}}. Report to {{project_lead}}.
```

- Placeholders are `{{project}}`, `{{lead}}`, `{{project_lead}}`, `{{epic}}`, `{{title}}` and `{{base}}`. `issue-cli` commands get `--project` injected.
- A missing key uses the built-in brief, so a workflow without `leads:` still gets working leads.
- The CLI appends a fixed footer to every brief: role, who the lead reports to, what it may tick, how to message other agents, and its handoff note under `sessions/`.
- `leads:` is project-wide: type, system and per-issue overlays keep it as is (`Clone` copies it). `issue-cli process schema` lists it.
- Leads change no statuses or transitions. An epic runs the normal workflow (or its `types:` path); children link to it with `parent:`, and their worktrees branch from its `base:` (see Per-Issue Worktrees).

## Per-Issue Worktrees

The dispatch handler creates a per-issue git worktree before launching the agent's tmux session, so concurrent issues cannot stomp on each other's uncommitted state.

Top-level toggle in `workflow.yaml`:

```yaml
worktree: true   # opt in
```

Defaults to `false` when absent — projects keep their existing dispatch behaviour until they explicitly opt in. Recommended for projects that dispatch concurrent agents on different issues at the same time.

Behavior when enabled:

- Branch: `work/<issue-slug>`, created off the project workdir's current HEAD, or off the epic's `base:` branch for a child of an epic. An epic with `base:` uses `.worktrees/<epic-slug>` on the base branch itself (see [The lead session](../patterns/lead-session.md#epics)).
- Path: `<project.workdir>/.worktrees/<issue-slug>` (gitignored)
- Re-dispatch on the same issue reuses the existing worktree directory rather than re-running `git worktree add`.
- The tree is materialized with `git checkout HEAD` after sparse-checkout patterns are configured. Paths in `worktree_sparse_exclude` (default `["issues/"]`) are kept out of the worktree, so a dispatched agent sees the source but not the issue store. If an old worktree was created empty (every file shown as staged-for-deletion), recover it with `git -C .worktrees/<slug> checkout HEAD`.
- The dispatched tmux session opens with `cwd` set to the worktree path, and the agent's prompt includes a worktree-aware section pointing at the path and branch.
- Worktrees are not cleaned up: they stay after the issue ships, and no checklist asks anyone to remove them. Agents never remove a worktree themselves, since that would risk discarding uncommitted state.

If `git worktree add` fails (dirty tree, branch already exists outside the worktrees dir, etc.), the dispatch surfaces an error step and aborts rather than silently falling back to the primary checkout.

Dispatches without an issue slug (retros review, project-wide commands) never create a worktree regardless of the toggle.

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

- `template:` is `WorkflowConfig.Template` and `features:` is `WorkflowConfig.Features` (`FeatureRecord{Installed, Declined}`). Both are declared on the struct so the web designer's marshal round trip keeps them, and `process schema` lists them. The engine never acts on either.
- `template: custom` marks a hand-built workflow: no upgrade hint, no proposals. A file with no `template:` is **unstamped**; `upgrade` compares it with `development` (or `--template <name>`).
- "Latest" is the stamp of the template bundled in the running binary, not `issue-cli version`. Releases that don't change a template don't make projects look behind, and `dev` builds work.
- Older binaries ignore both keys (yaml.v3 decoding is not strict).

### Feature catalog

`cmd/issue-cli/templates/workflow-features.yaml` (embedded) lists every capability a `workflow.yaml` can use, grouped by category: lifecycle (status prompts, optional and parked statuses, wildcard transitions, transition fields, types, system overlays), gates (one entry per validator family), automation (`append_section`, `inject_prompt`, `set_fields`), actions, board, dispatch (worktrees), and conventions (template wording such as `lead-handoff-prompts`, which tells agents to say what they need at a gate and end their turn). Each entry has `id`, `version`, `category`, `propose`, `templates`, `title`, `what`, `why`, `where`, `snippet`, `question` and `detect`.

- `propose: true` entries are the ones `upgrade` offers to projects that lack them. `propose: false` entries are catalog-only: scanned and recorded, and listed only by `upgrade --all`.
- `detect` is a list of **structural probes** over the parsed YAML; the feature is present when any probe matches. A probe has a `path` (`key`, `key[]` for each list item, `*` for each map value) and at most one condition: none (the value is set: not empty, `false`, `0` or null), `equals`, `one_of`, `rule_in` (the rule name before `:`), or `contains` (case-insensitive, inside that one field only). A path that starts with `statuses[]` or `transitions[]` also searches every `types.<t>` and `systems.<s>` overlay. For example, a parked status is `statuses[].global equals true`, so `optional: true` on its own doesn't count.
- The data store, stats, retros and comments are not catalogued: they are not `workflow.yaml` configuration.

**Copy model.** Installing a feature means an agent copies its snippet, adapted to the project's statuses and sections, into `workflow.yaml`. Nothing from the catalog is merged in at load time, so features that reshape the workflow (types of work) are installed the same way as one-line ones.

### Upgrading workflow.yaml

1. `start` and `process workflow` print a one-line hint when `upgrade` has at least one proposal. Agents tell the operator and carry on; they upgrade only when asked.
2. `issue-cli workflow upgrade` (read-only) lists the proposals: catalog entries newer than the stamp (all of them if unstamped), for the project's template, that the file doesn't have. What the file has comes from its `features:` record, or from the probes if there is no record. Declined features are listed with the re-open command, and an installed feature with a newer catalog version is proposed as an update.
3. The agent proposes the items one at a time, backs the file up to `workflow.oldN.yaml`, writes the picked snippets, and sets `template: <name>@<latest>`.
4. `issue-cli workflow scan --installed <picked> --declined <declined>` records the result. Scan rewrites only the `features:` block.
5. `issue-cli process workflow` validates the result. Existing issues keep the checklist sections they already have.

Command details: [CLI overview](../CLI/overview.md#workflow-upgrade).

### Adding or changing a catalog entry

1. Add or edit the entry in `workflow-features.yaml` with `version:` set to the release in `cmd/issue-cli/CHANGELOG.md`. Write probes against parsed paths, not raw text; use `contains` only for wording conventions, scoped to the field that carries the text.
2. If a bundled template gains the feature, bump its `template: <name>@<version>` stamp and re-run `issue-cli workflow scan` on the template file so its `features:` record matches.
3. `go test ./cmd/issue-cli/` checks that every field is filled, ids are unique, snippets parse, each probe has at most one condition and its path resolves in the `WorkflowConfig` structs, every stamp version has an entry, no entry is newer than its template's stamp, and each template's record equals what scan detects in it.

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

`linked_issue_in_status` and `command_succeeds` need runtime context (an issue lookup and the project's working directory); both are populated automatically by `Project.LoadWorkflow` via `attachRuntime`. `command_succeeds` is gated by the top-level `allow_shell: true` flag in `workflow.yaml`.

## Playtest Sessions in the Dev Workflow

This repo's `workflow.yaml` asks for manual checks to be written as playtest sessions. The shared contract is [docs/patterns/playtest/README.md](../patterns/playtest/README.md) (`issue-cli process playtest`), and this repo's runner is described in [web-app.md](../patterns/playtest/web-app.md).

- **`backlog → in progress`:** the Test Plan's `### Manual` boxes ask for a session in `playtest/sessions/<name>.yaml` (step id = checkbox id) that passes `make playtest-check`. Checks that no session can drive (real Claude behaviour, terminal-only flows) stay written steps, each with a `.agent-logs/<name>-sandbox/`.
- **`in progress → testing`:** the `## Testing` section adds a box for `make ux-test ISSUE=<slug> REPEAT=2`, run once before handoff.
- **The `human-testing` prompt:** sends the owner to `make playtest ISSUE=<slug>` (judgement steps in an overlay) and `make playtest-report ISSUE=<slug> APPLY=1`. The report ticks the boxes that passed and posts each failure and opinion as a `playtest:` comment.
- **The UI overlay's `in design` prompt:** asks for the test ids the session will point at.
- **The bundled `development` template:** has opt-in wording, "if the project has a playtest system", so other projects aren't forced to adopt one.

Issues that already have the old boxes ("Throwaway sandbox scenario prepared") keep them; only new transitions append the new wording.

## Design Considerations

When working on workflow changes:

- State which statuses, rules, templates, or overlays will change
- Consider whether existing issues need migration or compatibility handling
- For new validators, follow the steps in *Validation Rules* above
- For any new `workflow.yaml` capability (a field, action type or validator), add a feature catalog entry; see *Adding or changing a catalog entry* above
- Test with `go test ./internal/tracker/...` (engine) and `go test ./internal/tracker/validations/...` (per-validator suite)
