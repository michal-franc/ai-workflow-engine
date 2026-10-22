---
title: "CLI Overview"
order: 1
---

## Scope

The CLI system covers `issue-cli`, the command-line tool agents use to interact with issues during automated workflows.

## Key Files

- `cmd/issue-cli/main.go` — entry point, global-flag parsing, top-level error printing
- `cmd/issue-cli/commands.go` — registry (`registerCommand`, `lookupCommand`, `printHelp`); top-level help is auto-generated from this list
- `cmd/issue-cli/context.go` — `Command` and `Context` types, `findIssueOrErr`, `requireSlug`, `newFlagSet`, `flagWasSet`
- `cmd/issue-cli/helpers.go` — shared helpers (`loadProjectOrErr` → `resolveBootstrap` / `resolveFromConfig`, `formatProjectList`, `parseFieldFlags`, `normalizeEscapedText`, `writeJSON`, `printCheckboxes`, etc.)
- `cmd/issue-cli/cmd_<name>.go` — one file per subcommand. Each file declares a `*Command`, registers it in `init()`, and owns its own `flag.FlagSet`. Group commands (`data`, `workflow`, `process`) dispatch internally to per-subcommand handlers in the same file.
- `cmd/issue-cli/workflow_init.go` — testable `doWorkflowInit` core (the `workflow init` subcommand wraps it from `cmd_workflow.go`)
- `cmd/issue-cli/workflow_upgrade.go` / `cmd_upgrade.go` — `buildUpgradePlan` and the `workflow upgrade` / `upgrade` command; `workflowUpgradeHint` for `start` and `process workflow`
- `cmd/issue-cli/workflow_features.go` / `cmd_scan.go` — feature catalog, structural probes, `features:` record writer, and the `workflow scan` command

### Adding a new subcommand

1. Create `cmd/issue-cli/cmd_<name>.go`.
2. Declare a `var <name>Command = &Command{Name, ShortHelp, LongHelp, Run}` and call `registerCommand(<name>Command)` in `init()`.
3. Build the `Run` function as `func(ctx *Context, args []string) error`. Construct a `flag.FlagSet` via `newFlagSet("<name>", ctx)` and parse `args` (everything after the subcommand name) with `parseFlags(ctx, fs, args)`, never `fs.Parse` directly. `parseFlags` records the flag names for [usage telemetry](telemetry.md) and lets the never-used report introspect the command's flags. Call it before doing any work. Return errors instead of calling `os.Exit`.
   - Group commands set `Subcommands` (and `SubAliases`) on the `Command`. Flags parsed by hand go in `ExtraFlags`.
4. Reach the project via `ctx.Project`, write to `ctx.Stdout` / `ctx.Stderr`, and consult `ctx.JSONOutput` for output mode. The package-global `jsonOutput` no longer exists.
5. The new command appears in `issue-cli help` automatically.

## Commands

| Command                          | Description                              |
|:---------------------------------|:-----------------------------------------|
| `issue-cli show <slug>`          | Print full issue context                 |
| `issue-cli list`                 | List issues with filters — supports `--sort score` and emits `Score`/`ScoreBreakdown` in `--json` when scoring is enabled |
| `issue-cli start <slug>`         | Pick up issue from any status — claim + auto-advance handoff states (announced with a banner) |
| `issue-cli transition <slug>`    | Attempt the next workflow transition. `--dry-run` lists every unmet requirement |
| `issue-cli comment <slug>`       | Add a comment to an issue                |
| `issue-cli decide <slug>`        | Record a decision as a `D-n` line under `## Decisions`; `--replaces <slug>#D-n` lists the open issues still citing the old one |
| `issue-cli balance <slug>`       | Add a typed balance note (`--metric`, `--state`, `--hypothesis`, `--change`, `--evidence`, `--verdict`, `--pass`); `balance --list` prints the current notes |
| `issue-cli decisions`            | List the project's decisions (active, deferred, replaced, stale) with `--system`, `--state`, `--issue`, `--json` |
| `issue-cli lead <sub>`           | Lead agents: `spawn [--epic <slug>]`, `whoami`, `list`, `message <target> "text"`. See [`lead`](#lead) |
| `issue-cli approve <slug>`       | Record a human approval with the human's words (leads only). See [`approve`](#approve) |
| `issue-cli check <slug> <id>...` | Tick checkboxes by id (several at once), whole section (`--section X --all`), text, or `--section` + `--index` |
| `issue-cli checklist <slug>`     | List checkboxes grouped by section, each with its id (`D3`, `AC2`) |
| `issue-cli append <slug>`        | Append content to issue body             |
| `issue-cli replace <slug>`       | Replace content of an existing section   |
| `issue-cli set-meta <slug>`      | Set or clear a frontmatter field (refuses `type` in projects with types — use `set-type`; checks every slug in `depends_on`) |
| `issue-cli set-type <slug> <t>`  | Change an issue's type of work; agents only while it is at its type's first status. See [Types of Work](../Workflow/types.md) |
| `issue-cli process workflow`     | Print the active workflow, the `== Types ==` block and `workflow.yaml` lint warnings; `--type <t>` prints one type's lifecycle |
| `issue-cli process transitions`  | Print transition rules (default workflow, or scoped via `--system <name>`, `--type <t>` or `<issue-slug>`) |
| `issue-cli process schema`       | Print the `workflow.yaml` schema (fields, action types, validation rules) |
| `issue-cli process changes`      | Print the release history (last 20 versions) |
| `issue-cli process playtest [web\|game]` | Print the shared playtest-session contract, or one of the two worked examples (web app with chromedp, game engine). The guides are embedded; `docs/patterns/playtest/` holds the same files |
| `issue-cli report-bug "..."`     | File a bug report about issue-cli itself |
| `issue-cli retrospective <slug>` | Save a workflow retrospective            |
| `issue-cli data <sub> <slug>`    | Per-issue structured data store — see [Per-issue Data Store](../data-store.md) |
| `issue-cli workflow init`        | Bootstrap a new project: writes `workflow.yaml` from a bundled template and scaffolds `issues/`, `docs/` |
| `issue-cli workflow upgrade`     | Read-only: propose catalog features `workflow.yaml` lacks, item by item, plus the upgrade steps. Alias `issue-cli upgrade`. `--all` lists the whole catalog |
| `issue-cli workflow scan`        | Detect which catalog features `workflow.yaml` has and write its `features:` record. `--dry-run`, `--installed`, `--declined`, `--forget` |
| `issue-cli projects`             | List configured projects (slug, name, issue dir). `--json` for scripting |
| `issue-cli telemetry report`     | Local usage report: calls per command, never-used commands/flags/topics, errors, unknown tokens, retry sequences. See [CLI Usage Telemetry](telemetry.md) |
| `issue-cli telemetry path`       | Print the active telemetry file and whether recording is enabled |

### `start`

`issue-cli start <slug>` is the single entry point for picking up an issue. It claims the issue (sets the assignee if unset), prints the checklist, status guidance, and next-transition contract, and is idempotent on re-runs.

From a **handoff status** (`backlog`, `human-testing`) it also *auto-advances* to the next work status when the matching approval is present — `backlog → in progress`, `human-testing → documentation`. The target is the next non-optional status in the workflow, not a hardcoded value; only the handoff set is fixed. Because crossing into implementation from a re-claim is surprising, the advance is **never silent**: it is announced with a prominent `⚠ AUTO-ADVANCED  <from> → <to>` banner that also notes the consumed approval. If the approval is missing, `start` fails without mutating assignee or status. From any non-handoff status, `start` only claims and reports `Status unchanged`.

When the approval is missing, the agent stops and ends its turn; the approval message from the viewer resumes it (see [At a human-approval gate](#at-a-human-approval-gate)). The advance is recorded in the stats sidecar like any other transition.

### `append`

`issue-cli append <slug> --body "..."` adds content to the issue body. Two routing modes:

- **Default** — content is appended after the existing body. Headings inside `--body` must be unique against the issue.
- **Section** — pass `--section "Name"` to append into an existing section (or create it if missing). Use `--force` to disambiguate when the same heading exists at multiple levels.

If `--body` starts with a heading that is already present in the issue (and the rest contains only deeper subheadings), the command auto-routes into that section — equivalent to passing `--section`. This means agents drafting `## Implementation\n…` style appends do not have to retry with `--section` after a duplicate-heading failure.

The duplicate-heading guard still fires when `--body` introduces a *peer* heading that collides (e.g., `--body "## New\n…\n## Existing"`); pass `--section` to disambiguate.

#### Body from a file or stdin (`--body-file`)

`--body "..."` puts the body inside a shell-quoted argument, so backticks and parentheses are interpreted by the calling shell *before* `issue-cli` runs — `` `Foo.Bar` `` becomes a command substitution and `(...)` triggers a parse error. Inline code spans and parentheticals are normal in design bodies, so this hits constantly.

Pass `--body-file <path>` (or `--body-file -` to read **stdin**) to deliver the body as raw bytes that never pass through a shell word:

```bash
issue-cli append <slug> --section "Design" --body-file design.md
cat design.md | issue-cli append <slug> --section "Design" --body-file -
```

Notes:

- File/stdin content is used **verbatim** — unlike `--body`, it is not run through `\n`-escape normalization, so literal backslash sequences in code samples are preserved.
- `--body`/`--text` and `--body-file` are mutually exclusive; supplying both is an error.
- The same `--body-file`/`-` support applies to `issue-cli comment <slug>`.

### `check`

`issue-cli check <slug>` ticks checkboxes. The preferred form is **ids, several at once**:

```bash
issue-cli check <slug> D3 D4 AC1                      # by id — several in one call
issue-cli check <slug> "Design#3"                     # long-form id
issue-cli check <slug> --section "Design" --all       # every open box in a section
issue-cli check <slug> "Code changes complete"        # by text (substring, case-insensitive)
issue-cli check <slug> --section "Design" --index 2   # by section + stable index
issue-cli check <slug> --index 5                      # by position in the whole body
```

Flags go before ids or text.

#### Checkbox ids

`checklist`, `show`, `start`, and the checklist printed after `transition` show an id in front of every box, grouped by section, followed by a hint when boxes are still open:

```
== Checklist (3/8) ==
## Design
  D1 [x] Approach documented
  D2 [ ] Dependencies identified
## Acceptance Criteria
  AC1 [ ] Parser handles empty input
## Documentation
  Do1 [ ] Docs updated
Tick done boxes by id, several at once: issue-cli check <slug> <id> [<id>...]
```

- An id is the section's **initials** (first letter of each word, uppercased) plus the box's **1-based index within that section**: `D3` is the 3rd box under `## Design`, `AC2` the 2nd under `## Acceptance Criteria`, `TP1` the 1st under `## Test Plan`.
- When two sections share initials, the section that holds a checkbox **first in the document** keeps the short form. A later one extends its first word until it is unique: with `Design` holding `D`, `Documentation` becomes `Do` and `Deployment` `De`. In the default workflow `## Idea` holds boxes and claims `I`, so a later `## Implementation` is `Im`.
- Ids are **stable**. Indexes count checked and unchecked boxes, and workflow transitions append sections at the end, so a box's id never changes as work progresses.
- Ids are case-insensitive (`d3` = `D3`). The long form `<Section>#<n>` (`Design#3`, `"Acceptance Criteria#2"`) always works.
- Boxes before any `## ` heading have no id and show as `1.`; reach them with `--index`.
- Boxes inside fenced code blocks are illustrative, not workflow state. They get no id and are not counted by `checklist`, the progress line, or transition gates.

#### Behaviour

- **Several ids are all-or-nothing.** If any id doesn't exist (`check <slug> D2 D9`), nothing is ticked: `check` prints `No checkbox with id D9 — nothing was ticked.`, lists the boxes, and exits non-zero. Duplicate ids are collapsed. A box that is already checked is reported as `Already checked`, not as an error. All boxes are ticked in one locked write.
- **Id or text?** Positional args are read as ids when every arg is shaped like an id and at least one names a section of this issue. Otherwise they are joined into a text query, so a word like `phase1` (no section abbreviates to `PHASE`) is still matched as text, and so is unquoted multi-word text.
- **`--section X --all`** ticks every open box in that section. It cannot be combined with ids, text, or `--index`. On a complete section it prints `Nothing to check: section "X" already complete (n/n)` and exits 0. A section with no boxes is an error.
- **Text queries** match a box whose label contains the query. If it matches **more than one unchecked box**, `check` errors, lists every candidate with its id and `[Section #index]` label, and suggests `issue-cli check <slug> <id>`.
- **Output.** Every form prints one line per box, then overall progress plus the progress of each touched section:

  ```
  ✓ Checked: D2 [Design #2] Dependencies identified
  ✓ Checked: I1 [Implementation #1] Parser added
    Already checked: D1 [Design #1] Approach documented
    Progress: 3/11 (Design 2/2, Implementation 1/2)
  file: …
  ```

`checklist --json` emits an `items` array (`id`, `section`, `index`, `text`, `checked`) alongside the `total`/`checked` counts. `transition --json` checklist items carry the same `id`, `section` and `index` fields.

#### Transition gate failures

When a `section_checkboxes_checked` (or `all_checkboxes_checked`) gate blocks a transition, the error says how many boxes are **still open**, lists each one with its id, and gives the command to tick them:

```
Error: failed to transition: 2 of 4 boxes still open in section "Implementation":
  I2   Automated tests added or updated where practical
  I4   Changelog line drafted

Tick the ones that are done (ids, several at once):
  issue-cli check <slug> I2 I4
```

Before v0.30.0 this read `2/4 checkboxes incomplete`, where 2 was the number of *checked* boxes.

### `list`

`issue-cli list` filters by `--status`, `--system`, `--assignee`, `--version` and, in projects with types of work, `--type` (untyped issues count as `default_type`; rows then show a type column). With `--json`, each entry is the full issue plus two scoring fields:

| Field            | Type                                | When populated                                                                                  |
|:-----------------|:------------------------------------|:------------------------------------------------------------------------------------------------|
| `Score`          | `float` (or `null`)                 | `workflow.yaml` has `scoring.enabled: true` and the issue contributes to at least one component |
| `ScoreBreakdown` | `{Total, Components[]}` (or `null`) | Same as above. `Components` is an ordered list of `{Name, Points, Detail}` entries              |

Both fields are `null` when scoring is disabled or the issue has no scoring inputs (no priority, no `due`, no `created`, no scored labels, no `score_boost`). The breakdown matches what the web viewer renders into the `⚡N` badge — same `tracker.ComputeScore` is the single source of truth.

`--sort score` orders the output by `Score` descending. When scoring is enabled and `default_sort: score_desc` is set in `workflow.yaml`, the sort is applied automatically with no flag.

```bash
issue-cli list --json | jq '.[] | select(.Score != null) | {Slug, Score}'
issue-cli list --sort score --status open
```

### `transition`

`issue-cli transition <slug> --to "<status>"` runs the workflow engine — same path the board uses for drag-and-drop, so behavior matches.

When a transition declares `fields[]` with `required: true`, supply answers in either of two ways:

```bash
# Inline: repeatable --field key=value
issue-cli transition <slug> --to "waiting-for-team-input" --field waiting="design review"

# Or set the frontmatter ahead of time; the validator reads it at transition time
issue-cli set-meta <slug> --key waiting --value "design review"
issue-cli transition <slug> --to "waiting-for-team-input"
```

`--field` only applies to the in-flight transition. `set-meta` persists the value, so subsequent transitions and views see it. Section-targeted fields (`target: section:<Title>`) ignore frontmatter and always need a fresh `--field` answer because they append a new line to the body each time.

#### Checking requirements (`--dry-run`)

`issue-cli transition <slug> --to "<status>" --dry-run` evaluates every requirement of the transition at once and changes nothing. Without it, an agent finds unmet requirements one failed call at a time. Each problem has a kind and, where possible, a fix command:

```
== Dry run: in design → backlog ==
✗ 2 unmet requirement(s):
  - [validator] Validate section Design checkboxes are checked (gates this transition)
      1 of 2 box still open in section "Design":
      D2   Dependencies identified
      Tick the ones that are done (ids, several at once):
      issue-cli check cli/sample D2
  - [approval] Must be human-approved for "backlog" in the issue viewer
      → a human approves at http://localhost:8080/p/demo/issue/cli/sample#approve-backlog
      → tell the human what to approve, then stop and end your turn; the approval nudge resumes you
Nothing was changed.
```

Kinds are `order` (not a legal next step), `field` (a required `--field` answer is missing), `validator`, and `approval`. In text output a `→` fix line is left out when the validator's message already contains that command. JSON always carries it in `fix[]`. When nothing is unmet, the output is `✓ Ready: <from> → <to>` plus the `Will:` side-effects. It exits 0 when ready and 1 otherwise. `--json` returns `{dry_run, ready, from, to, slug, problems[{kind, requirement, message, fix[]}], side_effects}`.

It is built on `WorkflowConfig.PreviewTransitionAll`. That is the engine behind the viewer's transition preview, without the stop at the first failure, so the CLI and the viewer agree on what is missing.

#### At a human-approval gate

Agents don't block at an approval gate. They tell the human what to approve, then stop and end their turn. When the human approves in the viewer, the approve handler sends a message into the agent's tmux session (see [Human Approval Notifications](../agent-dispatch.md#human-approval-notifications)), and the agent re-runs the command.

`--wait`, `--timeout` and `--interval` were removed from `transition` and `start` in v0.49.0. Passing one changes nothing and fails with:

```
Error: --wait was removed in v0.49.0: stop and end your turn; the approval nudge resumes you
```

| Exit code | Meaning |
|:--|:--|
| 0 | Transitioned (or, with `--dry-run`, would succeed) |
| 1 | Error or unmet requirement; nothing changed |

The CLI gives this guidance itself, so it does not depend on any project's `workflow.yaml`:

- When the next step needs a human approval, the `== Next ==` block from `transition` and `start` prints the plain command. That is `start <slug>` from a handoff status, otherwise `transition …`. A one-line note follows it: tell the human what to approve, then stop and end your turn. It is never shown for `done`. `--json` carries this as `next_command` / `next_command_note`.
- A missing-approval error from `transition` or `start` ends with the approve link and tells the agent to stop and end its turn, and to re-run only after the human says it is approved.
- `--dry-run` lists the approval with the approve link and the same stop-and-end-turn fix.
- When the same command fails 3+ times in a row on a missing approval, the retry hint says retrying cannot help and the agent should stop and end its turn, instead of the generic "try a different approach".

Questions for the owner stay in chat (Claude Code or Codex; agents can message the lead with SendMessage). Only the outcome is recorded, with `decide`. `ask` and `answer` were removed in v0.40.0.

### `decide` and `decisions`

`issue-cli decide <slug> --text "…"` appends a dated `D-n` line to the issue's `## Decisions` (format: [Issue File Format](../issue-format.md#decisions)).

- `--how` says how the decision was made (`chat`, `project rule`, `playtest`; default `recorded by agent`; no `:`).
- `--quote` adds the human's words.
- `--deferred` marks it as decide-later.
- `--replaces <slug>#D-n` (repeatable, or `D-n` in the same issue) marks older decisions as replaced. Each ref must exist, or nothing is written and the error points at `issue-cli decisions --issue <slug>`.

The output names the ref to cite and lists, per replaced decision, the open issues still citing it, followed by a `Next:` line. `--json` returns `{decision, stale_citers, decisions_url}`.

`issue-cli decisions [--system S] [--state active|deferred|replaced|stale] [--issue <slug>] [--json]` prints the same index as the viewer's Decisions tab, newest first. `--state replaced` includes stale ones. `answer` prints the answer's full ref (`decision_ref` in JSON).

### `balance`

`issue-cli balance <slug> --metric <id> --state <state> [--hypothesis ..] [--change ..] [--evidence ..] [--verdict ..] [--pass n]` writes a typed balance comment on the issue (format: [Issue File Format](../issue-format.md#balance-notes)). Metric and state are required. The state accepts `needs-pieces` and `not-this-branch`. Invalid input leaves the issue untouched.

The output names the note and the board URL. When the note replaces an earlier one (same issue, metric and change), it adds `Updates the note from <date> (was <state>)`. Without a `workbench.balance` block it warns that the Balance tab is hidden; the note is still saved. `--json` returns `{note, replaces, board_url, board_enabled}`.

`issue-cli balance --list [--metric m] [--state s] [--pass n] [--json]` prints the current notes per metric and the number of free-text notes. `issue-cli comment` prints a tip pointing at `balance` when a `balance:` comment is free text.

### `lead`

Lead agents supervise workers: one project lead per project (`lead-<project>`) and at most one sub-lead per epic (`lead-<epic-slug>`). [The lead session](../patterns/lead-session.md) covers the roles; this is the command reference.

| Subcommand | Does |
|:--|:--|
| `lead spawn [--epic <slug>] [--attach] [--agent claude\|codex]` | Starts the project lead, or the epic's sub-lead. A live lead is re-attached, never started twice. `--epic` creates the base branch (the epic's `base:`, else `base/<slug>`, from `main`) and `.worktrees/<slug>` on it, records `base:` and `lead:` on the epic, then starts the lead there. Headless unless `--attach`. `--json` prints the launch response |
| `lead whoami` | Your role (`project lead`, `sub-lead`, `worker`, `unmanaged`), scope, guarded branch, your lead and whether it is live, how to reach it, and what you may tick |
| `lead list` | `== Leads ==`: one row per lead with scope, session, live/dead, base, commits on `main` not in the base, children running/total. Then the epics with no sub-lead, and a start hint when the project lead isn't running |
| `lead message <target> "text"` | Pastes `[from <your session>] text` into the target's tmux session (load-buffer + paste-buffer + Enter). `target`: `project`, a `lead-`/`agent-` session name, an epic slug (its sub-lead) or an issue slug (its worker) |

The CLI works out who is asking from `ISSUE_VIEWER_LEAD_SCOPE`, `ISSUE_VIEWER_ISSUE_SLUG` and the tmux session/window name. It doesn't need the viewer running. Refusals name who can do it instead:

- `lead spawn` from a worker or a sub-lead (the depth is fixed at project lead → sub-lead → worker);
- `lead spawn --epic` on an issue that is not an epic (no child names it as `parent:`/`umbrella:`, and it has no `type: epic`, `lead:` or `base:`);
- `lead message` from a worker, which posts a `question:` comment instead.

`show` also prints, under the header:
- `Depends on:` / `Depended on by:` lines from `depends_on`, each slug with its status;
- `Decided by:`, the decisions the body cites, with their state;
- `Guard metrics for <system>: N off (ids), N near. See <viewer>/metrics?system=<system>`, when `workbench.metrics` lists metrics for the issue's system.

When the issue's `work/<slug>` branch is open, `show` also prints `== Shared files ==`: up to 10 files that other open branches change too, with those branches. See [Workbench Files](../workbench-files.md).

`show` and `start` print a `Lead:` line naming the issue's lead and whether it is live. On an issue linked with the legacy `umbrella:` key they also print the `set-meta … --key parent` rename hint.

Lead briefs come from the `leads:` block in `workflow.yaml` (`project:` and `epic:`, see `issue-cli process schema`), else built-in defaults, plus a fixed footer with the role, authority and handoff note.

### `approve`

`issue-cli approve <slug> --to <status> --quote "<the human's words>" [--how "<where>"]` is how a lead records a gate the human approved in chat:

1. It writes an `approval: "<quote>" (<how>, <date>) — recorded by <session>` comment. `--how` defaults to `in the lead chat`, or `relayed by lead-<project>` for a sub-lead.
2. It sets `human_approval` to the status. It never toggles an existing approval off, unlike the viewer's button.

`--to` and `--quote` are required. It is refused:

- from a worker session (the error suggests a `question:` comment to its lead);
- from a sub-lead, for an issue outside its epic and the epic's children;
- for Human Testing, always: the human approves that in the viewer.

These checks read the environment and the session name; they are guardrails for agents, not security.

### `process transitions`

Rules are rendered from the loaded workflow rather than a hardcoded list. Each
row lists the validation rules, human-approval gates, and side effects
(`set_fields`, `append_section`, `inject_prompt`) attached to the transition;
optional and global statuses are surfaced separately.

Three scopes are supported. Without arguments the default project workflow is
printed and a hint at the bottom names any per-system overlays and points at
the scoping flags. Passing `--system <name>` (alias `--workflow <name>`)
prints the rules merged with that system's overlay. Passing an issue slug
resolves the issue's `system` field and prints the scoped rules; issues with
no system explicitly say `(no system overlay; project default)` so agents do
not mistake the default output for an issue-specific one.

In projects with types of work, an issue slug also applies the issue's type
(base → type → system), and `--type <name>` scopes without an issue (combine
with `--system`). The header names the type: `== Transition Rules — type
"tweak", system "UI" (issue ui/x) ==`. Unscoped output in a typed project says
it shows the base rules and lists the types.

### Types of work

When `workflow.yaml` defines `types:` ([Types of Work](../Workflow/types.md)):

- `create --type <t>` writes `type:` and starts at the type's first status; without `--type` the issue gets `default_type`. The output names the other types and how to switch.
- `set-type <slug> <t>` works while the issue is at its type's first status; later it refuses and links the viewer, where the human changes Type. If the current status is not on the new path, the issue moves to the new type's first status.
- `show`, `start` and `transition` print `Type: <t>` (and `Path:` in `show`), and warn when `type:` names no defined type or the status is off the type's path. Bad-order errors name the type's path.
- `show --json` adds `type` and `type_path`; `transition --json` adds `type` and `type_path`.
- Projects without `types:` print exactly what they did before; `--type` there is an error pointing at the doc.

The renderer is backed by `tracker.DescribeAction` and
`tracker.ValidationSummary` so descriptions stay in sync with the same strings
shown in transition previews.

### `data`

`issue-cli data <sub> <slug>` reads and writes the per-issue structured data store (`<slug>.data.json` next to the issue's markdown file). Subcommands are `add`, `list`, `set-status`, `set-tier`, `set-comment`, `remove`. Agents must use the CLI rather than touching the JSON file directly so the on-disk shape can change without breaking them. Full reference: [Per-issue Data Store](../data-store.md).

```bash
id=$(issue-cli data add <slug> --description "finding" --tier "🔴 critical")
issue-cli data set-status  <slug> "$id" "🔥 must-fix"
issue-cli data set-tier    <slug> "$id" "🟢 nice"        # pass "" to clear
issue-cli data set-comment <slug> "$id" --text "see processor_test.go:142"
issue-cli data list <slug> --json
```

`--tier` and `set-tier` validate against the body's `<!-- data tiers=... -->` enum when present; without a tiers marker the workflow opts out of validation and any value is accepted.

`add` prints the assigned id on stdout (and a human line on stderr) so it composes in shell pipelines. `--json` on `list` emits the entries array exactly.

### `workflow init`

`issue-cli workflow init` bootstraps a fresh project directory. It writes `workflow.yaml` from one of three bundled templates and scaffolds `issues/` and `docs/` if they do not already exist.

```bash
issue-cli workflow init --template development
issue-cli workflow init --template review --force
issue-cli workflow init                          # interactive prompt
```

Templates:

| Name          | Status set                                                                  | Use case                                  |
|:--------------|:----------------------------------------------------------------------------|:------------------------------------------|
| `development` | `idea → in design → backlog → in progress → testing → human-testing → documentation → shipping → done` | Software delivery flow (mirrors this repo) |
| `review`      | `inbox → triaged → reviewing → needs-changes → approved → archived`         | Review and triage of incoming items       |
| `writing`     | `idea → outline → drafting → editing → review → published`                  | Long-form content                         |

Behaviour:

- `--template <name>` selects a template. Without it, the command shows a numbered prompt when stdin is a terminal; piped or scripted invocations must pass the flag and exit non-zero with the list of valid templates if they don't.
- `--force` overwrites an existing `workflow.yaml`. Without it, the command refuses to touch the existing file and exits non-zero.
- `issues/` and `docs/` creation is idempotent — running the command in an already-initialised project does not error and does not re-create directories.
- Templates live as plain YAML under `cmd/issue-cli/templates/workflow/*.yaml` and are embedded at build time via `//go:embed`. The list of valid `--template` names is derived from the embedded directory, so adding a new template is just dropping a new `<name>.yaml` file there and rebuilding.
- Each template starts with a `template: <name>@<release>` stamp and a `features:` record, so a new project is stamped and recorded from the start. A new template needs both; run `issue-cli workflow scan` on it to write the record. See [Template Stamp and Feature Record](../Workflow/overview.md#template-stamp-and-feature-record).

### `workflow upgrade`

`issue-cli workflow upgrade` (alias `issue-cli upgrade`) tells an agent which `workflow.yaml` features the project could adopt. It is **read-only**, always exits 0, and never writes a file.

```bash
issue-cli workflow upgrade                      # proposals and steps
issue-cli workflow upgrade --all                # every catalog feature the file lacks, declined ones flagged
issue-cli workflow upgrade --template review    # compare an unstamped file with this template (default development)
issue-cli --json workflow upgrade               # machine-readable plan
```

- **Status:** `up_to_date`, `behind`, `unstamped`, `custom` or `unknown_template`, with the current and bundled stamps.
- **Record:** the `features:` record decides what is installed when the file has one (`record_source: record`); without one, the catalog's probes do (`detection`).
- **Proposals** are numbered. Each one has the feature id, `kind` (`new`, or `update` from an older installed version), version, category, whether the bundled template ships it, and the text the agent relays: What, Why, Where, Ask (the question for the human), and a YAML Snippet. By default the list covers `propose: true` entries newer than the stamp (all of them when unstamped), for the project's template, that the file lacks.
- After the proposals come three lists: already present, declined (with the re-open command `workflow scan --forget <id>`), and detected but not in the record (run `workflow scan`). These are never proposed.
- **Steps**, printed whenever there is something to propose:
  1. Continue only if the operator asked.
  2. Propose the items one at a time.
  3. `cp workflow.yaml workflow.oldN.yaml` (the first free N).
  4. Write the picked snippets, adapted to the project.
  5. Set `template: <name>@<latest>`.
  6. `issue-cli workflow scan --installed <picked> --declined <declined>`.
  7. `issue-cli process workflow`.
- `--json` emits one object: `status`, `workflow_file`, `template`, `current`, `latest`, `record_source`, `record`, `all`, `proposals[]`, `already_present`, `declined`, `detected_unrecorded`, `backup_path`, `steps[]`, `message`.

**Upgrade hint.** `start` and `process workflow` end with one line when the default plan has at least one proposal:

```
ℹ workflow.yaml has no template: stamp (compared with the bundled development@v0.36.0): 13 upgrade proposal(s). Tell the operator; run `issue-cli workflow upgrade` only if they ask you to upgrade.
```

It is built by `workflowUpgradeHint`, which returns `""` on any error so the hint can never break the command that prints it. `custom` and up-to-date files print nothing. `doctor` can reuse the same function.

### `workflow scan`

`issue-cli workflow scan` runs every catalog probe over the project's `workflow.yaml` and writes the `features:` record.

```bash
issue-cli workflow scan --dry-run                         # report + the record it would write; writes nothing
issue-cli workflow scan                                   # write the features: record
issue-cli workflow scan --installed types-of-work,scoring # record features as installed at the catalog version
issue-cli workflow scan --declined worktree               # record features the human declined
issue-cli workflow scan --forget worktree                 # drop ids from the record (re-opens a declined feature)
issue-cli --json workflow scan --dry-run
```

- **Report:** every catalog feature grouped by category, marked ✓ detected, + recorded only, ✗ declined, or · absent. A detected line shows the evidence at its concrete path (`transitions[3].actions[1].rule = "has_test_plan"`, `+N more`). Then come the changes to the record, notes, and the block itself.
- **Merge rules:**
  - installed = detected ∪ already recorded ∪ `--installed`. A recorded version is kept; new entries and `--installed` ones get the catalog version.
  - declined = recorded ∪ `--declined`, minus anything installed.
  - A recorded feature that is no longer detected stays installed, with a note pointing at `--forget`.
  - Ids the catalog doesn't know (written by a newer binary) are kept.
- **Errors:** an unknown id, declining a feature that is present in the file, the same id in `--installed` and `--declined`/`--forget`, extra arguments, or a file that isn't a YAML mapping.
- **Writing:** scan writes only when the record changes, or when there is none yet; a second run reports "record already up to date". It edits text: it replaces the existing `features:` block, or inserts one (with a comment) after `template:`, or else before the first top-level key. It then re-parses the result and refuses to write if anything other than `features:` changed. Comments and layout elsewhere are kept. It never sets `template:`, so an unstamped file keeps its proposals.
- `--json` emits `workflow_file`, `dry_run`, `written`, `had_record`, `features[]` (`id`, `version`, `category`, `title`, `detected`, `matches`, `evidence`, `recorded`), `record`, `block`, `changes`, `notes`.

### `projects` and multi-project resolution

`issue-cli projects` lists every project parsed from `projects.yaml`. The active project (resolved via `--project` or the single-project default) is marked `(active)`; in a multi-project setup with no `--project` the historical fallback (`projects[0]`) is marked `(historical default)`. `--json` emits an array with `slug`, `name`, `issue_dir`, `active`, and `default` fields.

```bash
issue-cli projects
issue-cli projects --json
issue-cli --project cli projects   # marks "cli" as active
```

`projects` is intentionally tolerant of a missing or unreadable config file — it is the discovery surface a confused bot reaches for first, so it must succeed when nothing else does. `help` and `process` are in the same allow-list.

**Resolution order in `loadProjectOrErr`:**

1. **Explicit `--project <slug>` always wins.** A bot inside one project workdir (with its own `./issues/`) can still query a sibling project by passing `--project`. The bootstrap auto-detection on cwd yields to an explicit flag.
2. **No `--project` + a local `./issues/`** → bootstrap mode synthesizes a single project from cwd. `--project` is unused; `len(allProjects) == 1`.
3. **No `--project` + no local `./issues/`** → consult `projects.yaml`. With `>1` project this returns `errAmbiguousProject` (exit non-zero) and lists configured slugs so the bot retries with the right `--project`. Single-project setups silently use `projects[0]` — byte-identical to pre-multiproject behaviour.

**Error message contract.** When an issue isn't found in a multi-project setup, the error appends `Searched project: <slug>` and an `Available projects:` enumeration with a `Retry with: issue-cli --project <slug> ...` hint. Single-project setups keep the historical `issue not found: <slug>\n\nRun: issue-cli list` message verbatim — this is the regression guard for bots that already grep for it.

**Help output.** With `>1` project configured, `issue-cli` (no args) and `issue-cli help` print a `Configured projects:` block enumerating slugs with `(active)` / `(historical default)` markers. Single-project setups omit the block.

**Dispatched agent prompts.** When the web app dispatches a bot via `buildAgentPrompt`, every `issue-cli ` invocation in the prompt body is rewritten to `issue-cli --project <slug>` so the bot doesn't have to discover the project. In bootstrap mode (`proj == nil` or empty slug) no rewrite happens — there is nothing to inject.

### `process schema` and `process changes`

`process schema` is driven off reflection on the YAML struct tags defined
in `internal/tracker/workflow_config.go`. Every `yaml:"..."` field must
also carry a `desc:"..."` tag — the tracker tests fail otherwise, so the
schema output cannot drift from the parser. Action types and validation
rules are documented via explicit registries (`WorkflowActionTypes`,
`WorkflowValidationRules` in `internal/tracker/workflow_schema.go`) kept
next to the switch statements that handle them.

`process changes` embeds `cmd/issue-cli/CHANGELOG.md` at build time via
`//go:embed` and prints it newest-first, capped to the 20 most recent `## v`
entries. The CHANGELOG lives under `cmd/issue-cli/` (not the project root)
because Go's `//go:embed` cannot reference files outside the embedding
package's directory. When cutting a release, add a new `## vX.Y.Z — YYYY-MM-DD`
section to the top of that file; the annotated tag message should mirror the
bullet list.

## Design Considerations

When working on CLI changes:

- Output is consumed by agents, not humans — prioritize machine comprehension over aesthetics
- Document the output contract early: whether text is human-facing or agent-facing
- Consider whether `--json` or other machine-readable output is needed
- Script compatibility matters — avoid breaking existing agent prompts that parse CLI output
- After making changes, run `make install` to update the binary
