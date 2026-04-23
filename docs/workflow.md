---
title: "Workflow"
order: 3
---

## Issue Lifecycle

Every issue follows this flow from left to right on the board:

1. **Idea** — new feature or bug lands here. Just a title and rough description is enough.
2. **In Design** — flesh out the idea: requirements, approach, edge cases.
3. **Backlog** — design is done, ready to be picked up for implementation.
4. **In Progress** — actively being worked on.
5. **Testing** — implementation done, verifying it works correctly.
6. **Documentation** — update docs to reflect the change.
7. **Done** — shipped and documented.

## Optional Statuses

A status can be marked `optional: true` in `workflow.yaml`. Optional statuses remain part of the lifecycle but can be skipped on forward transitions — useful for parking states (e.g. `waiting-for-team-input`) that only apply when a specific condition fires.

```yaml
statuses:
  - name: "in progress"
  - name: "waiting-for-team-input"
    optional: true
    description: "Parked — blocked on another team (skip if not blocked)"
  - name: "testing"
```

With this config, `in progress → testing` is valid (skips the optional status). `in progress → waiting-for-team-input` is also valid (ordinary forward step). Backward transitions into an optional status still require an explicit `transitions:` edge. See [Workflow Overview](Workflow/overview) for full semantics.

If a transition into an optional status also has `require_human_approval`, the detail view hides the approval checkbox behind a CTA button so the default (required) next step stays unambiguous. Customise the button text with `cta_label` on the transition:

```yaml
transitions:
  - from: "in progress"
    to: "waiting-for-team-input"
    cta_label: "Block on another team — park until they respond"
    actions:
      - type: "require_human_approval"
        status: "waiting-for-team-input"
```

When `cta_label` is unset, the button falls back to `Divert to <status> — <description>`.

## Updating Status

Edit the `status` field in the issue's frontmatter:

```yaml
status: "in progress"
```

The board view updates automatically on page refresh.

## Workflow Prompts

Workflow YAML supports two different prompt concepts:

- `statuses[].prompt`
  Baseline guidance for working in that status. This is shown when an agent starts work in the status or enters it from the previous status.
- `transitions[].actions[].type: inject_prompt`
  Additional transition-specific guidance. This is emitted only for that exact `from -> to` move.

Use `status.prompt` for persistent stage expectations, for example:

```yaml
statuses:
  - name: "in design"
    description: "Being designed and specced out"
    prompt: |
      Review the relevant docs and code before proposing changes.
      Turn requirements into explicit checklists.
      Surface assumptions, open questions, and required human input clearly.
```

Use `inject_prompt` for extra context that should only apply to one transition, for example:

```yaml
transitions:
  - from: "idea"
    to: "in design"
    actions:
      - type: inject_prompt
        prompt: |
          Focus on unresolved scope and design questions from the raw idea.
```

In practice:

- `status.prompt` defines how an agent should behave while in a status
- `inject_prompt` adds extra guidance triggered by a specific transition

For the full end-to-end agent flow from dispatch through approvals, retrospectives, and bug reporting, see [Agent Workflow Flow](agent-workflow-flow).

## Side-Effects

Statuses support a `side_effects` field — actions that run automatically after entering a status:

- `clear_assignee` — clears the assignee field

```yaml
- name: backlog
  side_effects: [clear_assignee]
```

This is used so design agents are unassigned when an issue moves to backlog, before a different agent picks it up for implementation.

## System Overlays

Each system (API, CLI, UI, Workflow) can override status prompts and add transition actions. Overlays are defined under `systems:` in `workflow.yaml` and merge with the base workflow:

```yaml
systems:
  API:
    statuses:
      - name: "in design"
        prompt: |
          Extra API-specific design guidance.
    transitions: []
```

System-specific documentation lives in `docs/<System>/` and agents should reference it during the documentation status. See:

- [API Overview](API/overview)
- [CLI Overview](CLI/overview)
- [UI Overview](UI/overview)
- [Workflow Overview](Workflow/overview)
