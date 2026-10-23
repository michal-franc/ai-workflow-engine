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

### Lead brief (`leads:`)

A top-level `leads:` block sets the brief the [project lead](patterns/lead-session.md) is spawned with by `issue-cli lead spawn`:

```yaml
leads:
  project: |
    You are the project lead of {{project}}, running in {{lead}}.
    Dispatch workers from the board and relay their questions to the human one at a time.
```

- Placeholders: `{{project}}` and `{{lead}}` (the lead's tmux name). `issue-cli` commands get `--project` injected.
- A missing or empty `project` uses the built-in default brief, so a workflow without `leads:` still gets a working lead.
- The CLI always appends a footer: role, what the lead may tick, and its handoff note under `sessions/`.
- An old `epic` key still loads but is ignored; `issue-cli process workflow` warns about it.
- `leads:` is project-wide: type, system and per-issue overlays keep it as it is.

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

### Checkbox validators

- `has_checkboxes` — body contains at least one `- [ ]`/`- [x]` checkbox anywhere.
- `section_has_checkboxes: <Title>` — the `## <Title>` section exists and contains at least one checkbox. **Existence only** — the boxes need not be checked here; they are typically verified at a later transition. This is how the `Acceptance Criteria` section gates `in design → backlog`: it must be present and non-empty, but its boxes are checked during testing.
- `all_checkboxes_checked` — every checkbox in the body is checked.
- `section_checkboxes_checked: <Title>` — the `## <Title>` section must exist with at least one checkbox, and **every** checkbox in it must be checked. A missing or checkbox-less section **fails** (it does not pass vacuously) — this stops an issue imported without a templated section from transitioning with none of that section's checklist content. In the normal flow each gated section is appended by the prior transition's `append_section`, so it always exists when the gate runs.

`process transitions` annotates these in its output: `section_checkboxes_checked` rows read "(gates this transition)" and `section_has_checkboxes` rows read "(existence only — contents verified later)".

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

## Local CI

Agents run `make validate` on their own branch before they fast-forward main. Two branches that are green on their own can still break main together, and nobody runs the slow checks. Local CI re-checks the integration branch after every move and tells the system when it goes red. It is opt-in per project: without a `ci:` block nothing runs and nothing shows.

### Config

```yaml
ci:
  branch: main          # default: the branch checked out in the main checkout
  timeout: 20m          # for the whole run; default 10m
  nice: 10              # default 10, so CI doesn't slow the agents; 0 = off
  on_red: mark          # mark (default) or file_issue
  run:                  # in order; the first failure stops the run
    - make validate                    # a plain string = a default-tier command
    - cmd: make ux-test
      tier: slow                       # never on merge: issue-cli ci run --tier slow
      failures: '--- FAIL: (\S+)'      # optional: group 1 names a failing test
```

- `ci:` is project-wide: system and type overlays can't change it.
- `issue-cli process schema` documents every key, and `process workflow` lints the block. It flags an empty `run`, a bad timeout, an unknown `on_red`, a `failures` regex without a group, and a block with only slow commands.
- Commands run with `sh -c` in their own process group, so a timeout kills everything they started. They get `CI=true`. `ISSUE_VIEWER_*`, `AGENT_NAME` and `TMUX` are removed, so issue-cli inside a test never thinks it is an agent and never reaches the real tmux server.
- The block does not need `allow_shell`: it is its own opt-in, and its commands are the project's own.
- This repo runs `make validate` on every move of main, with `make ux-test` as the slow tier.

### How a run happens

1. The viewer polls each configured project's branch head every 5 s. When the head was not tested yet and no run is live, it starts `issue-cli ci run --detach --if-untested`. A commit from a fast-forward, a `--no-ff` merge, a `git pull` or a hotfix on main counts the same, because the viewer watches the ref, not the merge command.
2. The run lives in tmux session `ci-<project>`, or window `ci-<project>` in the project's shared session. The owner can watch it live, and it stays open with the result until the next run. Restarting the viewer never kills a run, and on startup the viewer catches up on any untested head. Without tmux the run is a background process logging to `.ci/runner.log`.
3. The run takes `.ci/lock`, so there is one run per repository at a time. A lock whose process died is replaced.
4. The run checks out the head in the CI worktree `.worktrees/.ci`. The worktree is created once and reused (`checkout --force` plus `clean -fd`), so ignored build output survives and builds stay incremental. `issue-cli ci reset` rebuilds it from scratch.
5. When the run ends it re-reads the branch. If merges landed meanwhile it tests the newest head, so three merges during a run give one more run, not three.

Without the viewer, `issue-cli ci watch` does the polling; `--once` checks once.

### Files (`.ci/` in the main checkout)

`.ci/` writes its own `.gitignore`, so no repo has to ignore it.

| File | What |
|:--|:--|
| `.ci/state.json` | Where the branch stands: last tested sha, `green`/`red`/`error`, last green sha, red since, failing command and tests, what landed since green, the issues among it, the log tail. The viewer reads only this file and the lock |
| `.ci/runs/<sha>.json` | One run: each command's exit code and duration, the failing command and tests, the commit range from the last green run, the merged branches and issues, the log tail. A non-default tier writes `<sha>.<tier>.json`. The newest 50 runs are kept |
| `.ci/runs/<sha>.log` | The full output of the run |
| `.ci/lock` | The run in progress: pid, sha, tier, start |

### When it goes red

- **Blame:** the run lists what landed between the last green commit and this one. That covers local branches whose tip is in the range (matched to issues by `work/<slug>`), branches named by merge commits, and commits made straight on the branch. A rewound branch counts from the merge-base, and the first run ever blames nothing.
- **Status page:** the Local CI card shows the state chip, the one-line state and a Last runs row (the last 12 runs as green/red/amber boxes, oldest first, with a dashed box for a live run; each box's tooltip has the commit, result, age, duration and what failed). While red it also shows the failing command and tests, the issues that landed since green and the log tail. Red CI is the first item in the Needs you tile (Waiting on you in `status.md`) and counts in the sidebar's Status number. `status.md` carries the same line.
- **Card markers:** each issue merged since the last green run carries a red `CI red · <command>` chip while main is red, on the workboard (done cards too) and the system pages.
- **`ci:` comments:** each of those issues gets a comment with the failing command, its tests, what else landed and the log path.
- **The lead:** a live lead gets a `[from ci]` message, and decides with the owner whether to revert, fix forward or dispatch a fix. CI never reverts anything.
- **`on_red: file_issue`:** the first red run after green also files one `bugfix` issue (`Fix red CI on <branch> at <sha>: <command>`) with the range, the merged issues and the log tail in a `ci:` comment. Later red runs comment on that same issue, and the `ci:` comments link it. When main is green again the issue gets a comment and stays open for whoever ships the fix.
- **Quiet while still red:** later red runs comment and message again only if the failing command changes. The lead hears once more when main is green again, and the chips go.
- **CI can't run** (git or the worktree failed): the state is `error`, not red. Nobody is blamed, the card says why, and the lead hears once.
- **Health snapshot:** after each default-tier run, red or green, `ci run` takes a [health snapshot](workbench-files.md#health-snapshots) at the tested commit and prints `health: snapshot <path>`. A failed snapshot prints one line and never changes the run's result.
- **Slow tiers:** `issue-cli ci run --tier slow` runs the default commands, then the slow ones, once. Its result is shown separately (`ci status --tier slow`) and never turns main red, blames issues or messages anyone.

### What agents and the lead see

- `issue-cli start` and the dispatch prompt print the one-line state, for example `Local CI: main red since def5678: make validate: TestFoo — inherited from main, not caused by your branch`. An agent can tell a failure it inherited from one it caused.
- After `issue-cli transition <slug> --to done`, the agent reads that CI tests main by itself and that it must not wait.
- The lead brief adds a line when `ci:` is set: check `issue-cli ci status` before any push, and never push a release while main is red.
- `issue-cli ci status` prints the one-liner first, then what landed and the recent runs; `--json` dumps the state and runs. See [CLI Overview](CLI/overview#ci).

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

## Keeping workflow.yaml up to date

Projects edit their `workflow.yaml` heavily, so a newer issue-cli never overwrites it. Instead:

- `workflow.yaml` carries a `template: <name>@<release>` stamp and a `features:` record (installed `<id>@<version>`, declined `<id>`). `issue-cli workflow init` writes both.
- `issue-cli workflow scan` detects which catalog features the file already has and writes the record. Use `--dry-run` to preview.
- `issue-cli workflow upgrade` lists the features the project could adopt, each with what it does, why, a snippet and a question for you. An agent proposes them one at a time and copies in only the ones you pick. Declined ones stay declined until you re-open them with `issue-cli workflow scan --forget <id>`.
- When there is something to propose, `start` and `process workflow` print a one-line hint. Agents mention it and upgrade only when you ask.

Details: [Workflow Overview](Workflow/overview) (stamp, record, catalog) and [CLI Overview](CLI/overview) (`workflow upgrade`, `workflow scan`).
