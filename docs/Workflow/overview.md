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
- `internal/tracker/workflow_merge.go` — `Clone`, `ForSystem`, `Merge` (per-system overlay handling)
- `internal/tracker/workflow_schema.go` — reflection-based YAML schema docs
- `internal/tracker/heading.go` — shared section/heading helpers used by both workflow and issue code

## Workflow Structure

A workflow defines:

- **Statuses** — ordered lifecycle stages with descriptions and prompts
- **Transitions** — allowed moves between statuses with ordered actions
- **Board config** — which columns and card fields appear on the kanban board
- **System overlays** — per-system prompt and transition overrides

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

`WorkflowConfig.DefaultNextStatus(current)` returns `(required, optionals)` — the first non-optional status after `current`, plus the optional statuses skipped to get there. If every remaining status is optional, `required` is empty and `optionals` holds all of them so callers render alternatives rather than silently picking one. The JSON shape from `issue-cli transition --json` carries these as `next_status` (required) and `optional_next_statuses` (skipped optionals).

### Approvals on Optional Side-Paths

When a transition targets an `optional: true` status **and** has a `require_human_approval` action, the detail view does not render the approval checkbox inline alongside the required-path approval — that would put two pending approvals side-by-side and obscure the default next step. Instead:

- The required-path approval (target is non-optional) renders as a normal checkbox, as today.
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

Top-level `actions:` list in `workflow.yaml`:

```yaml
actions:
  - id: "defer-to-team"        # stable id; used in the URL and tmux session name
    label: "Defer to team"     # button text
    agent: "claude"            # claude (default) or codex
    prompt: |
      Issue "{{title}}" ({{slug}}, status: {{status}}) is being deferred to the team.
      Draft a short handoff note and add it as a comment:
      issue-cli comment {{slug}} --text "deferred: <summary>"
```

- Each action becomes a button on every issue's detail view.
- Clicking it POSTs to `/p/<project>/issue/<slug>/action/<id>`, which dispatches a fresh agent.
- The session name is `agent-<slug>-<id>`, separate from the main `agent-<slug>` dispatch session, so an action runs alongside (not on top of) a working agent.
- Actions run in the project checkout, not a per-issue worktree — they are lightweight prompts, not code-change sessions.
- Prompts are templated with `{{slug}}`, `{{title}}`, `{{status}}`, `{{system}}`, `{{priority}}`, and `{{number}}`. Unrecognized `{{...}}` tokens are left verbatim.

## Per-Issue Worktrees

The dispatch handler creates a per-issue git worktree before launching the agent's tmux session, so concurrent issues cannot stomp on each other's uncommitted state.

Top-level toggle in `workflow.yaml`:

```yaml
worktree: true   # opt in
```

Defaults to `false` when absent — projects keep their existing dispatch behaviour until they explicitly opt in. Recommended for projects that dispatch concurrent agents on different issues at the same time.

Behavior when enabled:

- Branch: `work/<issue-slug>` (created off the project workdir's current HEAD)
- Path: `<project.workdir>/.worktrees/<issue-slug>` (gitignored)
- Re-dispatch on the same issue reuses the existing worktree directory rather than re-running `git worktree add`.
- The dispatched tmux session opens with `cwd` set to the worktree path, and the agent's prompt includes a worktree-aware section pointing at the path and branch.
- Cleanup is the human's responsibility: the `shipping` checklist contains a reminder to merge `work/<slug>` and run `git worktree remove .worktrees/<slug>`. Agents do not remove worktrees themselves — that would risk discarding uncommitted state.

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

## Design Considerations

When working on workflow changes:

- State which statuses, rules, templates, or overlays will change
- Consider whether existing issues need migration or compatibility handling
- For new validators, follow the steps in *Validation Rules* above
- Test with `go test ./internal/tracker/...` (engine) and `go test ./internal/tracker/validations/...` (per-validator suite)
