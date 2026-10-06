---
title: "Types of Work"
order: 2
---

> **Status: slice 1 implemented** (issue `workflow/types-of-work-epic-new-feature-bugfix-tweak-each-with-its-own-workflow`).
> Slice 2 (see "Slices") is not built yet. The code is in `internal/tracker/workflow_types.go`,
> `cmd/issue-cli/cmd_set_type.go`, `cmd/issue-cli/types.go` and `types_view.go`.

## Why

A project has one lifecycle. Every issue walks it, including a five-minute tweak the owner already decided in a
playtest. Types let a project give each kind of work (for example `feature`, `tweak`, `bugfix`, `epic`) its own
path through the statuses, with its own gates, validators, prompts and appended sections.

What already exists, and why it isn't enough:

| Feature | What it can do | Why it isn't types |
|:--|:--|:--|
| `systems:` overlays | override prompts, append actions | cannot drop a status or a gate |
| `optional: true` | skip a status going forward | applies to every issue, not one kind |
| explicit skip edges | e.g. `design → in progress` | open to every issue; hints still point at the linear next |
| `field_in` on a frontmatter key | gate a skip edge to `type: tweak` | hints, `process` and approve buttons still show the feature path |
| conventions in prompt text | "Umbrella issues: …" paragraphs | every agent reads every branch; nothing is enforced |

Parent/child links (epic ↔ children, `base` branch inheritance) are **not** part of this design. They belong to
`umbrella-issue-grouping-multiple-issues`. Here, `epic` is only a type with its own path and prompts.

## Owner's choices (2026-10-06)

1. **Type overlays with a path** (not one full workflow per type, not per-status tags, not just gated skip edges).
2. **Retype**: an agent may change the type only while the issue is at its type's first status. After that, it's a
   human action in the viewer.
3. **Inherit + lint**: base edges whose two ends are both on the type's path are kept. A lint flags gaps.
4. **Epic scope**: type only. Parent links stay with the umbrella issue.

## Schema

```yaml
default_type: feature          # required when types: is present

types:
  <name>:
    description: "..."         # shown in process output, the create form and the type badge tooltip
    path: [s1, s2, ...]        # optional; the base statuses this type walks. Omitted = every base status
    statuses: [...]            # same as systems.<X>.statuses: prompt/description/template overrides
    transitions:               # same as systems.<X>.transitions, plus:
      - from: a
        to: b
        replace: true          # optional; this edge's actions and fields replace the base edge's instead of appending
        actions: [...]
```

- `path` entries must be names from the base `statuses:` list. An unknown entry is dropped and reported by the lint (not a
  load error: a failed load silently falls back to the default workflow). The base order wins: `path` picks statuses,
  it doesn't reorder them (the lint warns when `path` is listed out of base order).
- A type's `statuses` overrides and `transitions` that leave its `path` are ignored, and the lint reports them.
- A status only one type uses (say `repro`) still goes in the base `statuses:` list. The other types leave it out of
  their `path`. The base list is the **vocabulary and the board's column order**.
- `replace: true` is also accepted on `systems.<X>.transitions`, but only types need it.

## Resolution: `ForIssue`

One resolver builds the per-issue config. `Project.LoadWorkflowForIssue` calls it, and so does every per-issue call
site that calls `ForSystem` today (`handlers_workflow.go`, `cmd_process.go`):

```
base ──ForType(type)──▶ scoped ──Merge(system overlay)──▶ per-issue config
```

`ForType(t)`:

1. Clone the base. If `t` names no type (the project has no `types:`, or the type is unknown), return the clone.
2. If the type has a `path`, keep only those statuses in `Statuses` (base order), and keep only base transitions whose
   `from` (or `*`) and `to` are both on the path.
3. Merge the type's `statuses` and `transitions` with the existing `Merge` (descriptions and prompts override, actions
   append and dedupe). A transition with `replace: true` overwrites the base edge's `Actions` and `Fields`.

Then the system overlay merges as it does today. Its edges with an end off the path are dropped.

Because `Statuses` and `Transitions` are rewritten, everything downstream follows the type with no further changes:
`IsValidTransition` (the linear fallback runs on the path, so `idea → in progress` is the tweak's +1),
`NextRequiredStatus`, `DefaultNextStatus`, `RequiredHumanApproval`, `TransitionPrompts`, the approve buttons in the
detail view, and the dispatch prompt.

**Off-path rescue.** If the issue's current status isn't on its type's path (a hand edit, or a human retyped it
without picking a status), `ForIssue` keeps that status at its base position and marks it `global: true`. The issue
can then move to any status on its path. `show`, `start` and `transition` print a warning naming the path.

**The type of an issue** is the `type:` frontmatter key. It's empty when unset, and the issue then gets
`default_type`. An unknown value also gets `default_type`, plus a warning:
`type "chore" is not defined (types: feature, tweak, bugfix, epic); using default "feature". Fix: issue-cli set-type <slug> <type>`.

### Combining with system overlays

The order is base → type → system, so a system's status `prompt` replaces the type's prompt for that status (the
`Merge` rules as they are today). Systems that add guidance should use `inject_prompt` on an edge, which appends.
The lint flags a status prompt that both a type and a system set.

### Lint

`WorkflowConfig.Lint() []string`. Its output shows in `issue-cli process workflow` under
`== Warnings (workflow.yaml) ==` (the viewer's workflow designer page doesn't show it yet). Checks:

- `default_type` is missing or names no type; a `path` entry isn't a base status; a `path` is out of base order.
- **Section gaps**: an edge on a type's path validates `section_checkboxes_checked: X`, `section_has_checkboxes: X`,
  `has_section` or `section_min_length` on a section the base workflow appends somewhere, but no edge on the type's
  path appends it. Sections the base never appends (written by people, like `Repro`) are not flagged. Example: `type tweak: shipping → done checks section "Shipping", but nothing on tweak's
  path appends it — add an append_section to the edge into "shipping"`.
- A type and a system both override the prompt of the same status.

## CLI

Every change is a hint in CLI output, so agents in any project find types without prompt text.

| Command | Change |
|:--|:--|
| `create --type <t>` | New flag. The issue starts at the type's first status. Omitted: `default_type`, and the output says `type: feature (default; also: tweak, bugfix, epic — issue-cli set-type <slug> <type> while at idea)`. Unknown type: an error listing the types. |
| `set-type <slug> <type>` | New command. Allowed while the issue is at its current type's first status. The new type must contain that status; otherwise it sets the new type's first status. Later: refused with `type changes after "idea" are the human's call: ask them to change Type in the issue viewer (detail sidebar)`. |
| `set-meta --key type` | Refused in projects with `types:`, with a hint pointing to `set-type`. Projects without `types:` keep `type` as a plain custom field. |
| `process workflow [--type t]` | No `--type`: the base lifecycle, then `== Types ==` with one line per type: name, description and path (`idea → in progress → playtest → shipping → done`). With `--type`: that type's lifecycle. Lint warnings at the end. |
| `process transitions [--type t] [<slug>]` | Scoped with `ForType`, then the system. The header names both: `== Transition Rules — type "tweak", system "UI" (issue ui/x) ==`. |
| `show`, `start`, `transition` | The header shows `Type: tweak`. The `Workflow lifecycle` line is the type's path. `== Next ==` comes from the scoped config. A bad-order error names the path: `cannot transition from "idea" to "discussion" — type "tweak" goes idea → in progress → … (must go to "in progress" next)`. |
| `list` | A `--type` filter (untyped issues count as `default_type`). Rows show the type column only when the project has types. `next` and `search` don't filter by type yet. |
| `--json` outputs | A `type` field (omitempty) on issue objects, plus `type_path` on `show` and `transition`. |

## Viewer

- **Board**: columns stay the base `statuses:` list (one board for every type). Cards get a type badge; `type` joins
  the default `card_fields` when `types:` is defined. There's a `?type=` filter next to `?system=`. Dragging a card
  already goes through the per-issue transition endpoint, so the type is enforced there.
- **Graph**: `?type=t` draws that type's path, and its approval markers come from the scoped config.
- **Detail**: the sidebar shows Type. The human can change it with a select (`POST /p/<proj>/issue/<slug>/type`).
  When the current status is off the new path, the form asks which status on the new path to move to. The approve
  buttons and the transition preview already use `LoadWorkflowForIssue`, so they follow the type.
- **Create form**: a type select, defaulting to `default_type`.
- **Dispatch**: the prompt builders already load the per-issue config. The briefing adds one line:
  `Type: tweak (idea → in progress → playtest → shipping → done)`.

## Migration and compatibility

- **Projects without `types:`**: `ForType` returns the base clone, so `ForIssue` returns exactly what `ForSystem`
  returns today. A `type:` key in frontmatter stays a plain custom field (still shown in the sidebar, still settable
  with `set-meta`). Tests: a golden test over every issue in this repo and in a fixture copy of Raid League's
  `workflow.yaml` asserts `ForIssue(i)` deep-equals today's `ForSystem(i.System)`, plus snapshot tests of
  `process workflow`, `process transitions` and `show` output for a project with no types.
- **Adding `types:` to an existing project**: issues without `type:` get `default_type`. Make that the type with no
  `path` (the full base) and nothing changes for them. No file rewrites are needed.
- **Stats**: transitions recorded before types carry no type. In slice 2 the `/stats` tab groups them under the
  default type.

## Slices

1. **MVP** (this issue's acceptance criteria): the schema (`types`, `default_type`, `path`, `replace`), `ForType` /
   `ForIssue` with the off-path rescue, `Lint`, `Issue.Type`, `create --type`, `set-type`, `set-meta` refusal,
   `process --type`, the `show` / `start` / `transition` hints, list/next filters, the board badge and filter, the
   graph `?type=`, the detail Type select, the create form, and the dispatch line.
2. **Later**: per-type rows on `/stats` and `issue-cli stats`; a commented `types:` block in the `development` init
   template; parent links (`umbrella-issue-grouping-multiple-issues`), which can then replace the "Umbrella children"
   prompt paragraphs with an overlay keyed on having a parent.

## Worked example: Raid League (`wipe-manager`)

The base `workflow.yaml` keeps its lifecycle (idea → discussion → design → backlog → in progress → balance → playtest →
documentation → shipping → done, plus `parked` and `obsolete`). `feature` is the base. The "Umbrella issues: …"
paragraphs move out of the base prompts into `epic`, and the base `documentation → shipping` append gets a
`&shipping_section` anchor so the tweak and the bugfix reuse it.

```yaml
default_type: feature

types:
  feature:
    description: "New rules, numbers or content: the full path"
    # no path: every base status, every base edge

  tweak:
    description: "Already decided by the owner (who, when, in the body); no new rules or numbers; about half a day"
    path: [idea, in progress, playtest, shipping, done, obsolete]
    statuses:
      - name: "in progress"
        prompt: |
          Tweak: build exactly what the body says the owner decided. If it needs a new rule or number, stop and ask the
          human to change the type to feature in the viewer. Take a `--shot` screenshot before changing anything.
      - name: "playtest"
        prompt: |
          Post the before and after screenshots in one `shots:` comment with the `make run ARGS="..."` to see it,
          and ask the owner to approve them.
    transitions:
      - from: "idea"                       # the tweak's one human gate to start
        to: "in progress"
        actions:
          - type: validate
            rule: body_not_empty
          - type: require_human_approval
            status: "in progress"
          - type: validate
            rule: has_assignee
          - type: append_section
            title: Implementation
            body: |
              - [ ] Before screenshot taken (path in a comment)
              - [ ] Change made as decided, nothing more
              - [ ] `make test` green
      - from: "in progress"
        to: "playtest"
        actions:
          - type: validate
            rule: "section_checkboxes_checked: Implementation"
          - type: validate
            rule: "has_comment_prefix: shots:"
            hint: 'issue-cli comment {{slug}} --text "shots: before <png>, after <png>; make run ARGS=\"...\""'
          - type: validate
            rule: "has_comment_prefix: tests:"
          - type: append_section
            title: Playtest
            body: |
              - [ ] Owner approved the before/after screenshots
      - from: "playtest"                   # the owner's approval of the screenshots ships it
        to: "shipping"
        actions:
          - type: validate
            rule: "section_checkboxes_checked: Playtest"
          - type: require_human_approval
            status: shipping
          - *shipping_section              # add &shipping_section to the base documentation → shipping append_section
    # Inherited from the base: idea/in progress/playtest → obsolete, shipping → done.

  bugfix:
    description: "Something that worked is broken: repro and a failing test first, then the fix"
    path: [idea, in progress, playtest, shipping, done, obsolete]
    statuses:
      - name: "in progress"
        prompt: |
          Bugfix: first write a test that fails on the bug and commit it on its own (sha in a `repro:` comment), then fix
          it. No design doc. If the fix changes a rule or a number, stop and ask the human to make it a feature.
    transitions:
      - from: "idea"                       # no human gate: a confirmed bug can be picked up directly
        to: "in progress"
        actions:
          - type: validate
            rule: has_section
            section: Repro
            hint: "Add the steps, seed and expected vs actual: issue-cli append {{slug}} --body \"## Repro\\n...\""
          - type: validate
            rule: has_assignee
          - type: append_section
            title: Implementation
            body: |
              - [ ] Failing test committed first (sha in a `repro:` comment)
              - [ ] Fix makes it pass; no other behaviour changes
              - [ ] `make test` green
      - from: "in progress"
        to: "playtest"
        actions:
          - type: validate
            rule: "section_checkboxes_checked: Implementation"
          - type: validate
            rule: "has_comment_prefix: repro:"
          - type: append_section
            title: Playtest
            body: |
              - [ ] Human confirmed the repro no longer happens (`make run ARGS="..."` given)
      - from: "playtest"
        to: "shipping"
        actions:
          - type: validate
            rule: "section_checkboxes_checked: Playtest"
          - type: require_human_approval
            status: shipping
          - *shipping_section

  epic:
    description: "Umbrella: owns the design and the base branch; children ship into it; balance and polish playtest happen once, here"
    # no path: the full lifecycle, with the umbrella's own meaning per status
    statuses:
      - name: "in progress"
        prompt: |
          Epic: nothing is built on this issue. Children (issues with `umbrella: <this slug>`) build on the `base` branch.
          Tick Implementation when every child has shipped into the base branch, and list them.
      - name: "balance"
        prompt: |
          The joint pass: fold the children's `balance:` comments into a list, tune to the targets this design names,
          and keep the tuning history here.
      - name: "playtest"
        prompt: "The polish playtest for the whole feature, with cases as usual."
      - name: "shipping"
        prompt: "Take the base branch to master when the human decides; run `make test` on master after the merge."
    transitions:
      - from: "backlog"
        to: "in progress"
        replace: true                      # drop the feature's per-issue build checklist
        actions:
          - type: require_human_approval
            status: "in progress"
          - type: validate
            rule: has_assignee
          - &epic_implementation
            type: append_section
            title: Implementation
            body: |
              - [ ] Base branch created and set as the `base` meta
              - [ ] Every child shipped into the base branch (listed with #slug)
              - [ ] `make test` green on the base branch
      - from: "design"                     # the fast lane needs the same replacement
        to: "in progress"
        replace: true
        actions:
          - type: validate
            rule: "section_checkboxes_checked: Design"
          - type: validate
            rule: "section_has_checkboxes: Acceptance Criteria"
          - type: require_human_approval
            status: "in progress"
          - type: validate
            rule: has_assignee
          - *epic_implementation
      - from: "in progress"
        to: "balance"
        replace: true                      # no per-issue tests: comment; children carry those
        actions:
          - type: validate
            rule: "section_checkboxes_checked: Implementation"
          - type: validate
            rule: field_not_empty
            field: base
          - type: append_section
            title: Balance
            body: |
              - [ ] Children's `balance:` comments folded into one list
              - [ ] Targets from this design met, or the gap logged on #sim/balance
              - [ ] Tuning history recorded (change → result)
```

What the lint reports for this file: nothing. Each type's `shipping → done` checks `Shipping`, and the tweak's and
the bugfix's `playtest → shipping` append it. A tweak's `in progress → playtest` and a bugfix's `idea → in progress`
have no base edge, so they are the type's own.

What an agent sees on a tweak:

```
$ issue-cli create --title "Bigger play cards, no book, no card bans" --system UI --type tweak
✓ Created ui/bigger-play-cards-no-book-no-card-bans (type: tweak, status: idea)
  Path: idea → in progress → playtest → shipping → done

$ issue-cli transition ui/bigger-play-cards-no-book-no-card-bans --to discussion
✗ cannot transition from "idea" to "discussion" — type "tweak" goes idea → in progress → playtest → shipping → done (must go to "in progress" next)
```
