---
title: "Workflow Overview"
order: 1
---

## Scope

The Workflow system covers the workflow engine, transition logic, validation rules, system overlays, and `workflow.yaml` configuration.

## Key Files

- `workflow.yaml` — project workflow definition (statuses, transitions, actions, board config, system overlays)
- `internal/tracker/workflow.go` — WorkflowConfig, transition engine, validation, approval checks

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
| `append_section`           | Add a titled section with checklist to the issue body    |
| `inject_prompt`            | Add extra guidance for this specific transition          |
| `set_fields`               | Update frontmatter fields (e.g., clear assignee)         |

## Side-Effects

Statuses support `side_effects` that run automatically after a transition:

- `clear_assignee` — clears the assignee field

```yaml
- name: backlog
  side_effects: [clear_assignee]
```

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

## Design Considerations

When working on workflow changes:

- State which statuses, rules, templates, or overlays will change
- Consider whether existing issues need migration or compatibility handling
- Validation rules are defined in `workflow.go` — new rules need both the rule implementation and yaml support
- Test with `go test ./internal/tracker/...` — workflow tests are comprehensive
