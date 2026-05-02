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

When a CLI transition fails on a missing approval, the error includes a deep link to the corresponding approve button (`/p/<project>/issue/<slug>#approve-<status>`). See [Agent Dispatch — Approval-gate Deep Links](agent-dispatch.md#approval-gate-deep-links) for the URL contract.

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

## Validation Rules

Each `transitions[].actions[]` entry of `type: validate` runs one rule against the issue. There are two flavors of rule encoding:

- **Legacy colon-string** — the rule and (optional) argument live in a single `rule:` string, e.g. `rule: "section_checkboxes_checked: Design"`. Used by the original 10 rules: `body_not_empty`, `has_checkboxes`, `section_has_checkboxes`, `has_assignee`, `all_checkboxes_checked`, `section_checkboxes_checked`, `has_test_plan`, `has_comment_prefix`, `approved_for` / `human_approval`.
- **Structured action params** — the rule name in `rule:` plus companion fields on the same action (e.g. `field`, `values`, `pattern`, `section`, `min`, `max`, `command`, `ref_key`, `linked_status`, `hint`). All new validators use this form.

Both flavors can mix freely in the same workflow. New validators should use the structured form for clarity; existing legacy rules keep working unchanged.

### Frontmatter validators

```yaml
- type: validate
  rule: field_in
  field: priority
  values: [low, medium, high, critical]
  hint: "set with: issue-cli set-meta {{slug}} --key priority --value high"
```

- `field_present` — frontmatter `action.field` exists, regardless of value.
- `field_not_empty` — frontmatter `action.field` exists and is non-blank.
- `field_in` — frontmatter `action.field` value is one of `action.values`.
- `field_matches` — frontmatter `action.field` value matches the Go RE2 regex `action.pattern` (no backreferences or lookarounds).
- `has_label` — issue labels contain the name in `action.field`.
- `has_any_label` — issue has at least one label.

### Linkage validators

```yaml
# Block shipping until the parent issue is done.
- type: validate
  rule: linked_issue_in_status
  ref_key: parent
  linked_status: done
```

- `has_pr_url` — frontmatter `pr` is a github pull request URL (`https://github.com/<org>/<repo>/pull/<N>`).
- `linked_issue_in_status` — the issue referenced by `action.ref_key` (a frontmatter key whose value is another issue's slug) has the status `action.linked_status`.

### Body-structure validators

```yaml
- type: validate
  rule: section_min_length
  section: Design
  min: 200
- type: validate
  rule: no_todo_markers
```

- `has_section` — body contains `## <action.section>`.
- `section_min_length` — section `## <action.section>` has at least `action.min` non-whitespace chars.
- `section_max_length` — section `## <action.section>` has at most `action.max` non-whitespace chars (a missing section passes; pair with `has_section` if presence matters).
- `no_todo_markers` — body contains no whole-word `TODO` or `FIXME` (case-sensitive).

### Shell / external

`command_succeeds` runs a shell command and passes when the exit code is 0. It is **opt-in** — set `allow_shell: true` at the top of `workflow.yaml` to enable it; otherwise every `command_succeeds` action fails with a fix-it hint.

```yaml
allow_shell: true

transitions:
  - from: shipping
    to: done
    actions:
      - type: validate
        rule: command_succeeds
        command: "gh pr view {{number}} --json state -q .state | grep -q MERGED"
        timeout_seconds: 15
        hint: "land or close PR #{{number}} before marking done"
```

- The command is run via `/bin/sh -c <command>` with `text/template` substitution of `{{slug}}`, `{{number}}`, `{{repo}}`, `{{system}}` from the issue's frontmatter.
- Working directory is the project's issue root.
- Environment is scrubbed to `PATH`, `HOME`, `GH_TOKEN` only.
- Timeout: `timeout_seconds` (default 10s). On non-zero exit or timeout, captured stdout/stderr (truncated to 400 chars) is included in the failure message.

### Failure hints

Every validator returns a failure message of the form `<problem> — <hint>`, where `<hint>` is a concrete `issue-cli` command bots can run to fix the gate. Set `action.hint` on any structured validator to override the default hint with project-specific guidance (the override is templated with the same `{{slug}}/{{number}}/{{repo}}/{{system}}` vars as `command_succeeds`).

## Scoring

The `scoring` block in `workflow.yaml` turns on a score that ranks issues by urgency + importance + staleness. Scores are computed server-side on every page load from frontmatter — no cache, no background job — and surface in three places:

- Board cards: a small `⚡ N` badge in the corner, color-graded (green → yellow → orange → red)
- List view: a score chip on each row and a `Sort: score ↓` toggle at the top
- Detail view: a sidebar breakdown showing each component's contribution

The block is opt-in; omit it or set `enabled: false` to hide all scoring UI.

### Formula

```
score = priority_weight
      + min(overdue_cap, max(0, 30 - days_until_due) * urgency_weight)
      + age_days * staleness_weight
      + sum(label_weights for each label on the issue)
      + score_boost          # optional manual override
```

Missing fields contribute 0 — an issue with no `priority`, no `due`, and no `created` simply scores 0 (unless `score_boost` is set).

### Frontmatter fields that participate

| Field         | Role                                                                  |
|:--------------|:----------------------------------------------------------------------|
| `priority`    | Looked up in `scoring.formula.priority`. Frontmatter values are normalized to lowercase before lookup, so map keys should also be lowercase (`p0`, `critical`) — uppercase keys still match via a case-insensitive fallback but lowercase is the canonical form |
| `due`         | `YYYY-MM-DD` or RFC3339 date. Triggers urgency under a 30-day horizon |
| `created`     | Accumulates staleness at `staleness_weight` per day since that date    |
| `labels`      | Summed against `scoring.formula.labels`; unlisted labels add 0         |
| `score_boost` | Integer added as a separate component. Negative values work            |

### Example config

```yaml
scoring:
  enabled: true
  formula:
    priority:
      critical: 40
      high: 20
      medium: 10
      low: 0
    due_date:
      urgency_weight: 2     # points per day under the 30-day horizon
      overdue_cap: 60       # max from the due-date term (keeps ancient overdue from dominating)
    age:
      staleness_weight: 0.1
    labels:
      bug: 5
      blocker: 25
      enhancement: 0
  default_sort: score_desc   # initial sort on list + board; overrideable via ?sort=
```

### Sorting

With scoring enabled:

- `default_sort: score_desc` makes the list and board sort by score descending on first load.
- Append `?sort=score` to any list or board URL to force score ordering regardless of the default.
- Drop the query param (or use `/?sort=` cleared) to return to the original order (most-recently-modified first on list, insertion order per column on board).

Existing URL filters (`?status=`, `?system=`, `?label=`, etc.) compose with `?sort=`.

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
