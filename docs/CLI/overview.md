---
title: "CLI Overview"
order: 1
---

## Scope

The CLI system covers `issue-cli`, the command-line tool agents use to interact with issues during automated workflows.

## Key Files

- `cmd/issue-cli/main.go` — entry point, global-flag parsing, top-level error printing
- `cmd/issue-cli/commands.go` — registry (`registerCommand`, `lookupCommand`, `printHelp`); top-level help is auto-generated from this list
- `cmd/issue-cli/context.go` — `Command` and `Context` types, `findIssueOrErr`, `requireSlug`, `newFlagSet`, `flagWasSet`, and flag parsing: `parseFlags` / `parseFlagsArgs` / `parsePositionals`, `flagsFirst`, `usageError`
- `cmd/issue-cli/helpers.go` — shared helpers (`loadProjectOrErr` → `resolveBootstrap` / `resolveFromConfig`, `formatProjectList`, `parseFieldFlags`, `normalizeEscapedText`, `writeJSON`, `printCheckboxes`, etc.)
- `cmd/issue-cli/cmd_<name>.go` — one file per subcommand. Each file declares a `*Command`, registers it in `init()`, and owns its own `flag.FlagSet`. Group commands (`data`, `workflow`, `process`) dispatch internally to per-subcommand handlers in the same file.
- `cmd/issue-cli/workflow_init.go` — testable `doWorkflowInit` core (the `workflow init` subcommand wraps it from `cmd_workflow.go`)
- `cmd/issue-cli/cmd_upgrade.go` / `cmd_scan.go` / `cmd_catalog_check.go` — the `workflow upgrade` / `upgrade`, `workflow scan` and `workflow catalog-check` commands. The catalog, structural probes, `features:` record writer, `BuildPlan` and `CheckBranch` are `internal/workflowupgrade/`, shared with the viewer's Updates tab; the embedded templates and catalog are `cmd/issue-cli/templates` (package `templates`)
- `cmd/issue-cli/workflow_upgrade.go` — `projectWorkflowFile` / `workflowFileLabel`: which workflow file the project runs on
- `cmd/issue-cli/cmd_health.go` — the `health` command (print the latest snapshot and the scores, take one, backfill past days, the snapshot after `ci run`); `cmd_health_review.go` is `health review` (brief, record, file, dismiss, show, `--spawn`); snapshots, families, the score and the opinion are `internal/health/` (plus `internal/timeline` and `internal/milestone`, imported for their families)
- `cmd/issue-cli/cmd_ci_gotest.go` — `ci gotest`, the go test -json filter for `ci.run` results
- `cmd/issue-cli/cmd_ci.go` / `ci_report.go` — the `ci` command (detached runs in tmux) and what a run reports (`ci:` comments, `on_red: file_issue`; it never messages a session); the runner itself is `internal/ci/`

### Adding a new subcommand

1. Create `cmd/issue-cli/cmd_<name>.go`.
2. Declare a `var <name>Command = &Command{Name, ShortHelp, LongHelp, Run}` and call `registerCommand(<name>Command)` in `init()`.
3. Build the `Run` function as `func(ctx *Context, args []string) error`. Construct a `flag.FlagSet` via `newFlagSet("<name>", ctx)` and parse `args` (everything after the subcommand name) with one of these, never `fs.Parse` directly. Call it before doing any work. Return errors instead of calling `os.Exit`.
   - `parseFlags(ctx, fs, args)`: the command takes no positionals beyond what it already pulled off (e.g. the slug from `requireSlug`). A leftover positional is a usage error.
   - `parseFlagsArgs(ctx, fs, args)`: the command reads positionals from `fs.Args()` (text, ids, a status).
   - `parsePositionals(ctx, name, args, max)`: the command has no flags at all; it returns the positionals and refuses more than `max`.

   All three accept flags anywhere in `args` (see [Flags, `--help` and exit codes](#flags---help-and-exit-codes)), record the flag names for [usage telemetry](telemetry.md), and let help and the never-used report introspect the command's flags. `TestEveryCommandRejectsUnknownFlags` and `TestEveryCommandHelpNeedsNoProject` run every registered command and subcommand, so a new command that skips them fails the build.
   - Group commands set `Subcommands` (and `SubAliases`) on the `Command`. Flags parsed by hand go in `ExtraFlags`.
4. Reach the project via `ctx.Project`, write to `ctx.Stdout` / `ctx.Stderr`, and consult `ctx.JSONOutput` for output mode. The package-global `jsonOutput` no longer exists.
5. The new command appears in `issue-cli help` automatically.

## Commands

| Command                          | Description                              |
|:---------------------------------|:-----------------------------------------|
| `issue-cli show <slug>`          | Print full issue context                 |
| `issue-cli create`               | Create an issue (`--title`, `--system`, `--status` before backlog, `--priority`, `--type`). It writes `created: "<today>"`, the date the `criteria_proven` gate reads; so do `report-bug`, CI's red issue and the viewer's New issue form |
| `issue-cli list`                 | List issues with filters; `--all` drops the default project-version filter. Supports `--sort score` and emits `Score`/`ScoreBreakdown` in `--json` when scoring is enabled |
| `issue-cli start <slug>`         | Pick up issue from any status — claim + auto-advance handoff states (announced with a banner) |
| `issue-cli transition <slug>`    | Attempt the next workflow transition. `--dry-run` lists every unmet requirement |
| `issue-cli comment <slug>`       | Add a comment to an issue                |
| `issue-cli decide <slug>`        | Record a decision as a `D-n` line under `## Decisions`; `--replaces <slug>#D-n` lists the open issues still citing the old one |
| `issue-cli prove <slug> [<AC-id>]` | Link an acceptance criterion to its proof (`--test`, `--step`, `--commit`, `--value`, `--shot`, `--clear`); without an id, list every criterion with its links and checks. See [`prove`](#prove) |
| `issue-cli check-script <slug>`  | Run the issue's guided check scripts (`scripts/checks/<name>.sh`) from its worktree: the owner's run from a terminal, or `--yes` (machine checks only, never ticks); `--list`, `--new <name>`, `--lib`. See [`check-script`](#check-script) |
| `issue-cli playtest prune <slug>` | Tidy a worktree's `.playtest/runs` to what the Evidence tab needs; `--all` for every issue's worktree, `--dry-run` to report the MB first. See [`playtest prune`](#playtest-prune) |
| `issue-cli balance <slug>`       | Add a typed balance note (`--metric`, `--state`, `--hypothesis`, `--change`, `--evidence`, `--verdict`, `--pass`); `balance --list` prints the current notes |
| `issue-cli decisions`            | List the project's decisions (active, deferred, replaced, stale) with `--system`, `--state`, `--issue`, `--json` |
| `issue-cli lead <sub>`           | The project lead: `spawn`, `whoami`, `list`, `message <target> "text"`. See [`lead`](#lead) |
| `issue-cli dispatch <slug> --quote "…"` | Start (or re-attach to) the issue's worker exactly like ▶ on the board; needs the human's words (`--quote`) and leaves a `dispatch:` comment; refuses, exit 3, while the issue only waits on your approval. `--how`, `--agent`, `--force`, `--reprompt`. See [`dispatch` and `status`](#dispatch-and-status) |
| `issue-cli status`               | The project's stand-up from the running viewer (its `status.md`; `--json` for `status.json`). See [`dispatch` and `status`](#dispatch-and-status) |
| `issue-cli ci <sub>`             | Local CI on the integration branch: `status`, `run`, `reset`, `watch`, `gotest`. See [`ci`](#ci) |
| `issue-cli tracker <sub>`        | The private tracker repo for the work files: `init [--remote <url>]`, `sync [--quiet]`. See [`tracker` and `privacy`](#tracker-and-privacy) |
| `issue-cli release <sub>`        | A release on its branch: `setup --dev <url>`, `cut <version>`, `pick <slug\|sha>...`, `publish <version> [--first]`. See [`release`](#release) |
| `issue-cli privacy <sub>`        | The privacy choice's checks: `check` (exit 1 on red), `hook` (install the pre-push guard), `pre-push` (what the hook runs). See [`tracker` and `privacy`](#tracker-and-privacy) |
| `issue-cli health [snapshot\|backfill\|lint\|review]` | Project health: print the latest snapshot and both scores, take one, compute past days, or the agent review. See [`health`](#health) |
| `issue-cli approve <slug>`       | Record a human approval with the human's words (leads only). See [`approve`](#approve) |
| `issue-cli check <slug> <id>...` | Tick checkboxes by id (several at once), whole section (`--section X --all`), text, or `--section` + `--index` |
| `issue-cli check-edit <slug> <id> --text "…"` | Change one box's text; it keeps its tick and id. See [`check-edit`](#check-edit) |
| `issue-cli checklist <slug>`     | List checkboxes grouped by section, each with its id (`D3`, `AC2`) |
| `issue-cli append <slug>`        | Append content to issue body             |
| `issue-cli replace <slug>`       | Replace content of an existing section, or one line of it (`--line N`, `--match RE`). See [`replace`](#replace) |
| `issue-cli update <slug>`        | Change the title (the slug is kept; `--rename` moves it) and/or replace the body. See [`update`](#update) |
| `issue-cli set-meta <slug>`      | Set or clear a frontmatter field, replacing its whole old value (list items and block lines included); refuses a write whose YAML would not parse (refuses `type` in projects with types — use `set-type`; `created` only on an issue that has none, `YYYY-MM-DD`, never changed or cleared; checks every slug in `depends_on`, and writes two or more as a YAML list) |
| `issue-cli set-type <slug> <t>`  | Change an issue's type of work; agents only while it is at its type's first status. See [Types of Work](../Workflow/types.md) |
| `issue-cli process workflow`     | Print the workflow file in use (`Workflow file:`), the active workflow, the `== Types ==` block and `workflow.yaml` lint warnings; `--type <t>` prints one type's lifecycle |
| `issue-cli process transitions`  | Print transition rules (default workflow, or scoped via `--system <name>`, `--type <t>` or `<issue-slug>`) |
| `issue-cli process schema`       | Print the `workflow.yaml` schema (fields, action types, validation rules) |
| `issue-cli process changes`      | Print the release history (last 20 versions) |
| `issue-cli process playtest [web\|game]` | Print the shared playtest-session contract, or one of the two worked examples (web app with chromedp, game engine). The guides are embedded; `docs/patterns/playtest/` holds the same files |
| `issue-cli report-bug "..."`     | Report a bug in issue-cli or the engine: it becomes a `cli-bug` issue on the engine board (see [`report-bug`](#report-bug)) |
| `issue-cli retrospective <slug>` | Save a workflow retrospective            |
| `issue-cli data <sub> <slug>`    | Per-issue structured data store — see [Per-issue Data Store](../data-store.md) |
| `issue-cli workflow init`        | Bootstrap a new project: writes `workflow.yaml` from a bundled template and scaffolds `issues/`, `docs/` |
| `issue-cli workflow upgrade`     | Read-only: propose catalog features `workflow.yaml` lacks, item by item, plus the upgrade steps. Alias `issue-cli upgrade`. `--all` lists the whole catalog |
| `issue-cli workflow scan`        | Detect which catalog features `workflow.yaml` has and write its `features:` record. `--dry-run`, `--installed`, `--declined`, `--forget` |
| `issue-cli workflow catalog-check` | issue-cli developers: fail when a work branch changes a catalog feature's code or a bundled template without a version bump (`make validate` runs it). `--base main` |
| `issue-cli projects`             | List configured projects (slug, name, issue dir). `--json` for scripting |
| `issue-cli telemetry report`     | Local usage report: calls per command, never-used commands/flags/topics, errors, unknown tokens, retry sequences. See [CLI Usage Telemetry](telemetry.md) |
| `issue-cli telemetry path`       | Print the active telemetry file and whether recording is enabled |

### Slug arguments

Every command that takes `<slug>` resolves it the same way (`findIssueOrErr` in `context.go`):

1. **Exact slug**, case-insensitive; a trailing `.md` or a path under the issue directory is accepted.
2. **Unique suffix** on `/<arg>`, so `sample` finds `cli/sample`.
3. **Unique substring**, so `first-partial` finds `cli/slug-lookup-silently-picks-the-first-partial-match`. The command then prints `note: resolved "<arg>" to <slug> (partial match)` on stderr before it acts.

When a step matches two or more issues, the command fails with exit status 1 and writes nothing:

```
Error: ambiguous slug "sim": 4 issues match:
  sim/a
  sim/b
  …

Retry with the full slug.
```

The list stops at 10 slugs and ends with `… and N more (issue-cli list)`. A system name is almost always ambiguous, so `issue-cli retrospective <System>` fails instead of filing the retro under some issue of that system.

### Flags, `--help` and exit codes

Every command parses its arguments the same way:

- **Flags go anywhere.** `issue-cli list done --status done` and `issue-cli comment <slug> "text" --text more` parse every flag, wherever it is. Before 0.53 Go's flag parsing stopped at the first positional, so a flag after one was silently dropped: `list done --all` listed everything with exit 0. `--` ends the flags; everything after it is a positional, even `--text`.
- **A flag is a word shaped like one:** `-name`, `--name` or `--name=value`. Text that merely starts with a dash (`"- [ ] item"`, `"--force is risky"`, `-5`) stays a positional.
- **Mistakes exit 2.** An unknown flag, a bad flag value, an unexpected positional, or a flag where the slug belongs fails with **exit 2** and a short message on stderr:

  ```
  Error: unknown flag --al for 'list'
  Valid flags: --all --assignee --category --sort --status --system --type --version

  Run: issue-cli list --help
  ```

  Other failures keep exit 1. A removed flag (`--wait`) also exits 2.
- **The slug comes first.** `issue-cli comment --text hi <slug>` fails with `comment takes <slug> first, then flags: …` instead of looking up an issue called `--text`.
- **`--help` anywhere.** `-h`, `-help` or `--help` in a command's arguments prints that command's help: its description, examples, and a **Flags** section read from the code (one per subcommand for `data`, `ci`, `lead`, `telemetry`, `workflow`). It exits 0 and needs no project, and it is never read as a slug or as text. `issue-cli help <command>` prints the same thing.
- **The global `--json`** also works after the command (`issue-cli list --all --json`). `--project`, `--config` and `--debug` must come before the command; after it they get a hint saying so.
- **`--debug`** (global) prints `debug: config: <path> (<why>)` on stderr, and the lookup's checked list when no config was found.

### `start`

`issue-cli start <slug>` is the single entry point for picking up an issue. It claims the issue (sets the assignee if unset), prints the checklist, status guidance, and next-transition contract, and is idempotent on re-runs.

From a **handoff status** (`backlog`, `human-testing`) it also *auto-advances* to the next work status when the matching approval is present — `backlog → in progress`, `human-testing → documentation`. The target is the next non-optional status in the workflow, not a hardcoded value; only the handoff set is fixed. Because crossing into implementation from a re-claim is surprising, the advance is **never silent**: it is announced with a prominent `⚠ AUTO-ADVANCED  <from> → <to>` banner that also notes the consumed approval. If the approval is missing, `start` fails without mutating assignee or status. From any non-handoff status, `start` only claims and reports `Status unchanged`.

When the approval is missing, the agent stops and ends its turn; the approval message from the viewer resumes it (see [At a human-approval gate](#at-a-human-approval-gate)). The advance is recorded in the stats sidecar like any other transition.

### `append`

`issue-cli append <slug> --body "..."` adds content to the issue body. Two routing modes:

- **Default** — content is appended after the existing body. Headings inside `--body` must be unique against the issue.
- **Section** — pass `--section "Name"` to append into an existing section (or create it if missing). Use `--force` to disambiguate when the same heading exists at multiple levels. When the body's first line is a heading with that name (`--section Design --body "## Design\n- [ ] x"`), it is dropped, so the heading is not written twice. `--section` matches a heading at any level (`--section Automated` finds `### Automated`); a transition's `append_section` matches only `## Name` (see [Workflow](../Workflow/overview.md#gate-sections-append_section)).

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

Flags may come before or after the ids or text.

While the `criteria_proven` gate covers the issue, acceptance criteria can't be ticked here: they are ticked by
their proof (`issue-cli prove`, see [Proof Links](../Workflow/proof.md)), and `check` refuses them with a pointer
to `prove <slug> --list`.

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
- Don't write the id into the box text. If a box's text already starts with its own id (`- [ ] TP4: run the suite`), the id is printed once (`TP4 [ ] run the suite`), not twice. The file keeps the text as written; `check-edit` drops a leading copy of the id when it rewrites a box.

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

### `check-edit`

`issue-cli check-edit <slug> <id> --text "…"` replaces the text of one checkbox. The id is the short or long form `check` takes (`D3`, `"Design#3"`). The box keeps its tick state, indentation and position, so its id doesn't change:

```
✓ Edited D3 [Design #3]
  was: Overlap reviewed
  now: Overlap reviewed: similar features listed
file: …
```

An unknown id lists the issue's ids. The text must be one line.

### `replace`

`issue-cli replace <slug> --section S --body "…"` replaces the body of a section. To change **one line** of it instead:

```bash
issue-cli replace <slug> --section "Design" --line 3 --text "- Risk: none"
issue-cli replace <slug> --section "Design" --match "^- Risk:" --text "- Risk: none"
```

- `--line N` counts from the line after the heading, blank lines included (1-based).
- `--match RE` (Go regexp) must match exactly one line of the section.
- When the line can't be chosen (out of range, no match, several matches), nothing is written and the error prints the section with line numbers, so you can retry with `--line`.
- `--line` and `--match` can't be combined. The replacement is a single line. A heading that appears twice is an error.

### `update`

`issue-cli update <slug> --title "…"` changes an issue's title and **keeps its slug**. An issue's slug comes from its title, so before 0.53 a retitle moved the issue to a new slug, and links, `parent:`/`depends_on:` references and agents holding the old slug broke. Now the old slug is written to a `slug:` frontmatter field (see [Issue File Format](../issue-format.md)), which wins over the title:

```
✓ Retitled: cli/old-name (slug kept: pinned with slug: in frontmatter)
  To let the slug follow the title: issue-cli update cli/old-name --title "New name" --rename
```

`--rename` opts into the old behaviour. It removes the pin, renames the file to the new slug, and lists the issues whose `parent:` or `depends_on:` still name the old slug. It doesn't rewrite them. If a file with the new name already exists, nothing is written. The title editor on the detail page keeps the slug the same way, and never renames.

`--body "…"` replaces the whole body (frontmatter and comments are kept).

### `list`

`issue-cli list` filters by `--status`, `--system`, `--assignee`, `--version` and, in projects with types of work, `--type` (untyped issues count as `default_type`; rows then show a type column).

When the project sets a `version:`, `list` shows only that version by default, and the footer says how many issues that hid: `12 issues (version 0.53; 40 more in other versions: issue-cli list --all)`. **`--all`** drops that default and lists every issue in every status and version. Filters you pass still apply (`list --all --status done`).

With `--json`, each entry is the full issue plus two scoring fields:

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
- After a move to the last status (`done`), `transition` prints `== Done ==` and the local CI line, never a `== Next ==` or the guidance of a side status like `parked`. `--json` carries `"done": true`.
- A move to `done` also archives the issue's playtest runs (see [`playtest prune`](#playtest-prune)) and says so in one line: `✓ playtest: archived 2 of 9 runs, deleted 70; the latest of issue-tab and issue-tab-phone (3.8 MB) moved to .playtest/runs/, 110 MB deleted` (N of the issue's own M runs archived, K runs deleted in all). Archiving none of a non-zero M prints `⚠ playtest: archived 0 of M runs, …` instead: the issue's Evidence tab would have no playtest. Nothing is printed when the worktree has no runs; a problem prints `playtest: <error>` and never fails the transition.
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

### `prove`

`issue-cli prove <slug>` lists every acceptance criterion with its proof links, where each came from (a `Proves:`
commit trailer, a playtest step's `proves:`, or `prove`), each link's check result, and `proven` / `UNPROVEN`. It
ticks the criteria that are proven, and ends with `Evidence (with the playtest shots): <viewer url>?tab=evidence`, the
issue page's [Evidence tab](../UI/overview.md#the-evidence-tab), which shows the same report next to the shots.
`--json` returns the same report, plus `tests_at`, `last_change` and `last_change_sha` (the newest `tests:` comment and
the last commit that changed code; docs-only commits don't count).

`issue-cli prove <slug> <AC-id> --test <Name> | --step <id> | --commit <sha> | --value "…" | --shot <path>` adds
links (repeatable) to the data sidecar; `--clear` removes that criterion's prove links. Most links need no call:
agents declare them where they write the proof. Sources, checks, refusals and the `criteria_proven` gate:
[Proof Links](../Workflow/proof.md).

### `balance`

`issue-cli balance <slug> --metric <id> --state <state> [--hypothesis ..] [--change ..] [--evidence ..] [--verdict ..] [--pass n]` writes a typed balance comment on the issue (format: [Issue File Format](../issue-format.md#balance-notes)). Metric and state are required. The state accepts `needs-pieces` and `not-this-branch`. Invalid input leaves the issue untouched.

The output names the note and the board URL. When the note replaces an earlier one (same issue, metric and change), it adds `Updates the note from <date> (was <state>)`. Without a `workbench.balance` block it warns that the Balance tab is hidden; the note is still saved. `--json` returns `{note, replaces, board_url, board_enabled}`.

`issue-cli balance --list [--metric m] [--state s] [--pass n] [--json]` prints the current notes per metric and the number of free-text notes. `issue-cli comment` prints a tip pointing at `balance` when a `balance:` comment is free text.

### `lead`

The project lead supervises workers: one lead per project (`lead-<project>`), and every worker reports to it. [The lead session](../patterns/lead-session.md) covers the roles; this is the command reference.

| Subcommand | Does |
|:--|:--|
| `lead spawn [--headless] [--agent claude\|codex]` | Starts the project lead in the project checkout. A live lead is re-attached, never started twice. What opens follows `lead_terminal:` (unset: headless, printing the attach commands and, inside tmux, a `tip:` naming `lead_terminal`). `--headless` opens nothing; the old `--attach` is accepted and does nothing. `--json` prints the launch response. `--epic` fails with one line: there is one lead per project |
| `lead whoami` | Your role (`project lead`, `worker`, `unmanaged`), guarded branch, your lead and whether it is live, how to reach it, and what you may tick |
| `lead list` | `== Lead ==`: the project lead, live or not. `== Epics ==`: each open epic with its children running/total. A start hint when the lead isn't running. `--json` gives `{lead, epics}` |
| `lead message <target> "text"` | Pastes `[from <your session>] text` into the target's tmux session (load-buffer + paste-buffer + Enter), Codex sessions only. A Claude session (read from the pane's start command, `agentrun.SessionAgent`) is never pasted into: it exits 1 and prints the `SendMessage({to: "<session>", …})` call to make. `target`: `project`, a `lead-`/`agent-` session name, or an issue slug (its worker) |

The CLI works out who is asking from `ISSUE_VIEWER_LEAD_SCOPE`, `ISSUE_VIEWER_ISSUE_SLUG` and the tmux session/window name. It doesn't need the viewer running. Refusals name who can do it instead:

- `lead spawn` from a worker;
- `lead message` from a worker, which posts a `question:` comment instead;
- `dispatch` and `health review --spawn` from a worker, or without `--quote`;
- `start`, `claim` and `transition` from a worker on any slug but its own: "you work on <own-slug> only; ask your lead", and nothing changes. `start` and `claim` default the assignee to the worker's session name (`agent-<slug>`, `/` as `-`), its `SendMessage` address.

`show` also prints, under the header:
- `Depends on:` / `Depended on by:` lines from `depends_on`, each slug with its status;
- `Decided by:`, the decisions the body cites, with their state;
- `Guard metrics for <system>: N off (ids), N near. See <viewer>/metrics?system=<system>`, when `workbench.metrics` lists metrics for the issue's system.

When the issue's `work/<slug>` branch is open, `show` also prints `== Shared files ==`: up to 10 files that other open branches change too, with those branches. See [Workbench Files](../workbench-files.md).

`show` and `start` print a `Lead:` line naming the issue's lead and whether it is live. On an issue linked with the legacy `umbrella:` key they also print the `set-meta … --key parent` rename hint.

The lead's brief comes from `leads.project` in `workflow.yaml` (see `issue-cli process schema`), else a built-in default, plus a fixed footer with the role, authority and handoff note. An old `leads.epic` is ignored, and `process workflow` warns about it.

### `dispatch` and `status`

These two commands do what leads used to do with `curl` against the viewer. One `issue-cli` permission covers them, and nobody types a host.

**`issue-cli dispatch <slug> --quote "<the human's words>" [--how "…"] [--agent claude|codex] [--force] [--reprompt]`** starts the issue's worker.
- **Same as ▶.** It is the same launch: `agentrun.DispatchSpec` builds the prompt (`tracker.BuildDispatchPrompt`), session (`agent-<slug>`), worktree and reprompt hint for both. The CLI then starts it through tmux itself, so the viewer doesn't need to be running. The worktree is resolved against the project checkout (`workdir`, else the issues folder's parent), never the caller's cwd.
- **Output.** It prints `✓ Dispatched agent-<slug> for <slug>` (or `✓ Already live: …` when the worker runs: re-attached, prompt not re-sent), then the launch steps, then `Inside tmux:` (switch-client) and `From a shell:` (attach).
- **Gate refusal.** When everything the issue's next transition needs is done except your approval (`tracker.WaitingOnGate`, the same rule as the Status page's "waiting on you" gates), it starts nothing. It prints the gate, the approve link and the `--force` command, and exits 3. A gate whose work isn't finished doesn't block, and a live worker is always re-attached.
- **`--reprompt`** pastes the current briefing into the live worker (`agentrun.Reprompt`, the Re-send prompt button). With no live worker it creates nothing, names `issue-cli dispatch <slug>`, and exits 1. It takes neither `--agent` nor `--force`.
- **Workers can't dispatch.** The refusal is the same as `lead spawn`'s, and it points at a `question:` comment. A shell under a `.worktrees/` folder counts as a worker.
- **`--json`** prints the object `POST …/dispatch` returns: `{status, prompt, session, attach_cmd, switch_cmd, gate, steps}`, with `status` one of `dispatched`, `reattached`, `reprompted`, `waiting-on-gate`, `no-session`, `error`.
- **`--quote`** is required (exit 2 without it, nothing starts): a human starts the work. A start or re-prompt leaves `dispatch: "<words>" (<how>, <date>)` on the issue, plus ` — recorded by <session>` when an agent ran it; `--how` defaults to `lead chat` for the lead, `owner's shell` otherwise. A re-attach starts nothing and leaves no comment. One slug per call.
- **Exit codes:** 0 started, re-attached or re-prompted; 1 the launch failed, or `--reprompt` found no worker; 2 usage (no `--quote`, a bad `--agent` value); 3 waiting on a gate.

**`issue-cli status [--md|--json]`** prints `GET <viewer>/p/<project>/status.md` exactly as served, or with `--json`, `/status.json` (its `as_of` is each request's time). The viewer builds the Status model, so it must be running.
- When it doesn't answer, or answers with an error, the command exits 1 with `viewer not reachable at <url>: …`, how to start it, and the `viewer_url` hint.
- A slug by mistake (`status <slug>`) is a usage error pointing at `issue-cli show <slug>`.

**The viewer's address** for `status` and every link issue-cli prints (approval hints, `show`'s Timeline, Balance, Decisions, Metrics) comes from `viewerBaseURL` → `viewerstate.BaseURL`, in this order:
1. `viewer_url:` at the top of `projects.yaml`
2. the URL in `viewer.json`, only when that viewer serves the same config file
3. `http://localhost:8080`

`ISSUE_VIEWER_URL` is gone: neither the viewer nor issue-cli reads or sets it. When `viewer_url` is unset, the approval hint ends with `(set viewer_url in projects.yaml if your viewer is on a different host/port)`.

### `ci`

Local CI runs the `ci:` commands of `workflow.yaml` on the integration branch after every move. [Workflow: Local CI](../workflow.md#local-ci) covers the config and what it reports; this is the command reference. Without a `ci:` block, `ci status` prints `ci: not configured — add a ci: block to workflow.yaml (see docs/workflow.md, "Local CI")` and exits 0 (`--json`: `{"configured": false}`), and `ci run` fails with the same line.

| Subcommand | Does |
|:--|:--|
| `ci status [--tier <t>]` | The first line is the one-liner (contract below). While red it then lists `Landed since green <sha>:`, the bugfix issue (`Fix tracked in:`) and the log path, then `Recent runs:` (the last 5: sha, state, age, duration, tier, what failed). `--tier slow` adds the tier's newest result. `--json`: `{configured, branch, line, state, runs, running?}` |
| `ci run [--tier <t>] [--detach] [--if-untested]` | Runs in the foreground and streams the output. It re-tests the head, then keeps going while new commits land, testing only the newest. Exits 0 when green, 1 when red or CI can't run. A live run makes it print `already running at <sha> (pid N); it tests the newest head when done` and exit 0. `--detach` starts the same command in tmux `ci-<project>` (or a background process without tmux) and returns. `--if-untested` skips a head already tested; the watchers pass it. `--json` writes the runs to stdout and the live output to stderr |
| `ci reset` | Removes `.worktrees/.ci`, ignored build output included; the next run creates it again (a cold build). Refuses while a run is live |
| `ci watch [--interval 5s] [--once]` | Polls the branch head and starts a detached run when it is untested. For projects without the viewer running |
| `ci gotest [--out <file>]` | A filter for a `ci.run` command: reads `go test -json` on stdin, prints what plain `go test` prints (failing tests' output, the ok/FAIL line per package), saves the raw stream to `--out` (default `$CI_RESULTS_DIR/unit.json`) and exits 1 when a test or package failed, so the pipe keeps go test's verdict. Needs no project. No events on stdin is exit 1 with a hint |

The one-liner, also printed by `start` and the dispatch prompt (with a `Local CI: ` prefix):

```
main green at abc1234 (12m ago)
main red since def5678: make validate: TestA, TestB, TestC (+4)
main: CI can't run: <reason> — issue-cli ci reset
main: no CI run yet
… · running at 1234567 (3m)          # appended while a run is live
```

The test names come from the command's `failures:` regex. While red, `start` and the dispatch prompt add `— inherited from main, not caused by your branch`. `transition --to done` ends with `Local CI tests <branch> at <sha> by itself: don't wait for it, you are done…` when `ci:` is set.

### `tracker` and `privacy`

The owner's choice lives in `privacy:` in workflow.yaml; [Workflow: Private work and code](../workflow.md#private-work-and-code) covers the model. Both commands need it: without `work_files: tracker`, `tracker` refuses with the snippet to record (`privacy.work_files is not tracker in workflow.yaml: the owner has not chosen a private tracker`).

| Subcommand | Does |
|:--|:--|
| `tracker init --remote <url>` | `<url>` is the private repo the owner created (agents never create repos); it can be left out only when an adopted tracker already has an origin, else init refuses (exit 2) asking for it. Creates the tracker git folder outside the checkout, or adopts an existing one whose work tree is this checkout (another checkout's is refused), adds `--remote` as origin, ignores the work paths in `.gitignore`, installs the pre-push guard, and runs a first sync. Work paths the code repo still tracks are printed with the `git rm -r --cached` to run; init never runs it |
| `tracker sync [--quiet]` | Commits every change under the work paths (`tracker: N files`), pulls with rebase, pushes. Prints what it did (`--quiet`: errors only). A conflict keeps the local commit, pushes nothing and exits 1. The result goes to the tracker's `issue-cli-sync.json`, which Status reads |
| `privacy check` | One line per badge plus the guard: `work   green  Work private: Work files private: <repo>, last backup 12m ago`, each amber or red line followed by `fix: …`. Exit 1 when a line is red. `--json`: `{project, lines: [{kind, level, label, sentence, fix}], red}` |
| `privacy hook` | Installs the pre-push guard in the common git folder (`core.hooksPath` when set). A hook that isn't ours is kept as `pre-push.local` and still runs |
| `privacy pre-push <remote> <url>` | What the hook runs, with git's ref lines on stdin. Refuses (exit 1) a push whose commits add a work path (`work_files: tracker`), a dev commit or tag to the public remote (`code: snapshots`), or a commit message quoting a decision (`code: public`) |

### `release`

The branch flow of [Roadmap: Cutting a release](../roadmap.md#cutting-a-release-candidate-soak-publish); [Workflow: Private work and code](../workflow.md#private-work-and-code) covers `code: snapshots`. `cut` and `publish` read the running viewer's Waiting for release card (`/p/<project>/release.json`) and refuse unless it says the same: they print each unmet check (`✗ Local CI green on main's head: main red since abc1234`). Without the viewer they say how to start it.

| Subcommand | Does |
|:--|:--|
| `release setup --dev <url>` | Once, with `code: snapshots`: the old `origin` becomes the public remote (`remote.public.tagOpt --no-tags`), `<url>` becomes `origin` and gets every branch and non-candidate tag, the public tip is recorded as the base, the pre-push guard is installed. Prints the mirror clean and the release-workflow change; pushes nothing to public |
| `release cut <version>` | Creates `release/vX.Y` from main (the CI branch) with its worktree `.worktrees/release-vX.Y` when missing, and tags the next `<version>-rc.N` on its tip. Refuses a second candidate on the same tip. With `code: snapshots` the branch goes to origin. Prints the soak steps |
| `release pick [--release <version>] <slug\|sha>...` | Cherry-picks (`-x`) onto the release branch, in its worktree, the commits of `work/<slug>` the branch lacks (by patch, `git cherry`), or a commit. A conflict aborts the whole pick. Without `--release` it uses the only release worktree |
| `release publish <version> [--first]` | `code: snapshots` only, from the release worktree (elsewhere it names it), on a soaked candidate. Refuses a dirty worktree, a version already tagged, work paths in the tree, or a public tip that is not a snapshot (unless it is the recorded base, or `--first`). Then: the changelog heading on the branch, one commit `<version>` on the public branch, the dev tag, both pushed to origin, main's released changelog lines moved under the version |

### `health`

Project health snapshots: dated JSON files in `.health/snapshots/` of the main checkout. The viewer takes one a day and `ci run` takes one after each run. [Workbench files: Health snapshots](../workbench-files.md#health-snapshots) covers the file, the config and every metric.

| Subcommand | Does |
|:--|:--|
| `health [--family <name>]` | Prints the latest snapshot. First the header: `Health snapshot <date> · <branch> at <sha> · <trigger> · <ms> ms` and the file path. Then `Scores:`: the derived health with the share of the weight it is based on (or `not enough data`), the agent opinion with its age (or `no review yet`), and each component's measured value, sub-score and weight (`no data: <why>` when empty). Then the codebase: files and lines per kind with 7d and 30d growth (`n/a` without a base), source lines, test ratio and binary files; the top 10 areas and languages by lines; the big files. `Families:` gives each family `ok, N metrics` (plus ` · <hint>` when the family set one) or `error: <why>`. `Next:` names what is missing, with the command or doc: CI without `results:`, no local CI, no review or one over 7 days old, recommendations waiting for the owner. It ends with `Charts: <viewer>/p/<slug>/stats · scores and the review: <viewer>/p/<slug>/health`, and with fewer than two snapshots, `Trends need history (N snapshot so far): issue-cli health backfill`. `--family tests` (shape, score, opinion, codebase, ...) lists that family's rows by id instead; an unknown one names the families. Without a snapshot it prints `No health snapshot yet: run issue-cli health snapshot (the viewer takes one daily)` and exits 0. `--json` prints the snapshot file (`{"snapshot": null, "hint": ...}` without one) |
| `health review [--spawn [--attach] [--agent claude\|codex]]` | The agent opinion ([Workbench files: The agent opinion](../workbench-files.md#the-agent-opinion)). Writes the evidence pack and prints the review brief, for the agent running it. `--spawn` starts a review agent with the brief in tmux `review-<project>`, headless unless `--attach`, in the main checkout. `--schema` prints the JSON schema of the review file. `--json`: `{brief, evidence}` |
| `health review record <file> [--reviewer <name>]` | Validates the agent's review and stores it in `.health/opinions/`, then takes a snapshot (trigger `review`). Refused with `<file>: N problem(s), nothing recorded:` and one line per problem with its fix (score outside 0–100, assessment length, an unknown kind or effort, a metric id not in the snapshot, a path not at the main head, a repeated title). Unknown JSON fields are refused too. Prints `✓ Recorded the review: score N, M recommendation(s)` and that nothing becomes an issue by itself. `--reviewer` defaults to `$AGENT_NAME` |
| `health review show` | The newest review: score, age, reviewer, assessment, and each recommendation with its kind, effort, state (`filed → <slug>`), evidence and expected effect |
| `health review file <R#> --quote "<owner's words>" [--how "lead chat"]` | Creates an `idea` issue from a proposed recommendation, records the owner's decision on it and marks the recommendation filed. Without `--quote`: `needs the owner's approval: ask them, then pass --quote "<their words>"`. A filed or dismissed one, or an unknown id (the error lists the ids), is refused |
| `health review dismiss <R#> --quote "<owner's words>" [--reason "..."]` | Marks a proposed recommendation dismissed. Needs `--quote` like `file` |
| `health lint [--check a,b] [--list] [--all]` | Checks workflow.yaml and the board (`internal/lint`) and prints each warning as `<check> <where>: <message>` with `  fix: <command or edit>` under it, then `N warnings; the health score's lint component counts them`. Checks: `undated` (no `created:`; the fix is a `set-meta … --key created --value <earliest recorded date>`), `workflow` (the `process workflow` warnings: types:, workbench:, ci:, health:), `schema` (keys the parser doesn't know, with `did you mean`; no `statuses:` or `transitions:`; deprecated keys: `actions:`, `statuses[].template`/`validation`/`side_effects`), `template` (the `template:` stamp behind the bundled template, unknown, malformed or missing), `placeholders` (a `{{name}}` its field never fills, `{{ name }}` with spaces where plain replacement fills it, any `{{…}}` in an untemplated field such as a status prompt) and `sections` (a checkbox gate on a section no `append_section` or status template writes, or writes without a box; a `has_test_plan` template without exact `### Automated`/`### Manual`). Every finding is a warning: exit 0, 2 on bad usage or an unknown `--check`. Text shows 20 warnings per check (`--all` for every one). `--list` names the checks. `--json`: `{project, checks, findings: [{check, where, message, fix}], warnings}`. The [lint family](../workbench-files.md#the-lint-family) records the count, and /health lists the warnings live |
| `health snapshot` | Takes a snapshot now (trigger `manual`), prints `✓ Snapshot written: <path>`, then the same summary. Fails with `health snapshots are off for this project (health.off: true in workflow.yaml)` or `<dir>: not in a git repository`. `--json` prints the snapshot |
| `health backfill [--days 90]` | Computes one snapshot (trigger `backfill`) for each of the last N days that has none, as it stood at 23:59 that day: the codebase at the commit the branch had then, the workflow over the issues done by then. Prints one line per day (the path, `skipped: has a snapshot`, or `(before the first commit: no codebase)`), then `✓ Backfilled W of N days (S skipped, F failed) · charts: <url>`. The milestone family is left out (it knows only the present). `--days` outside 1–3650 is a usage error (exit 2). `--json` prints `{written, skipped, failed, days: [{day, path, skipped, error, no_commit}], charts}` |

### `playtest prune`

`make ux-test` runs every session, so every worktree collects every session's runs (`.playtest/runs/<session>/<time>-<mode>/`). The issue page's Evidence tab needs only the latest auto and judge run of each of the issue's own sessions (`issue:` in `results.yaml`). A game's runner writes modes `ux-test` and `playtest`; they count as auto and judge (the Timeline and Evidence tabs read them the same way).

```
issue-cli playtest prune <slug> [--dry-run]   # one issue's worktree
issue-cli playtest prune --all [--dry-run]    # every worktree that belongs to an issue
```

- **Done issue:** of each own session and mode, keep the newest run with steps (a header-only run, left when a runner stopped, doesn't count) and every run newer than it; move them to the project root's `.playtest/runs/` (a run already there stays put), delete the rest and the runner's cached binaries (`runs/.bin`, about 35 MB a worktree). An own run goes only when a newer run of its session and mode is kept. Also kept: the run a kept `-cached` run takes its shots from (its steps' `cached:`), an own run of a mode other than auto or judge, and a run it can't classify (a `results.yaml` that isn't YAML and names no other issue, or files with no `results.yaml`). Removing the worktree afterwards keeps the evidence. `transition --to done` does this by itself.
- **Open issue:** delete only the runs of other issues' sessions and empty aborted runs (no `results.yaml`, no files).
- Each done issue gets a line `<slug>: archived N of M runs, deleted K`, with a ⚠ when none of its own runs was archived.
- `--all` skips hidden folders (`.worktrees/.ci`) and names the worktrees with no issue it left alone.
- The report gives the MB deleted, moved and kept, and what is left (`After: 71 MB. Run without --dry-run to apply.`).
- The viewer's daily health pass later shrinks the shots of issues done 30+ days ago (`health.shrink_shots_after_days`, see [Workbench Files](../workbench-files.md)).

### `check-script`

Runs an issue's [guided check scripts](../patterns/guided-check-scripts.md): `scripts/checks/<name>.sh` files whose first lines carry `# issue: <slug>`, found in the issue's worktree (or the project folder when the workflow has no worktrees).

```
issue-cli check-script <slug>               # the owner's run (stdin a terminal): questions, then the tick offer
issue-cli check-script <slug> <name>        # one script
issue-cli check-script <slug> --yes         # machine run: machine checks only, nothing ticked (agents, before handover)
issue-cli check-script <slug> --list        # name, path, whether the owner has run it; exit 1 when there are none (--json)
issue-cli check-script <slug> --new <name>  # write a starting script: one step per unticked Test Plan ### Manual box
issue-cli check-script --lib                # print the helper library (needs no project)
```

- The script runs under bash in its checkout with `CHECKS_LIB` (the library, written to a cache file named after its content), `CHECKS_SLUG`, `CHECKS_PROJECT`, `CHECKS_ROOT`, `CHECKS_NAME`, `CHECKS_ISSUE_CLI` (this binary: the tick uses it) and `ISSUE_VIEWER_CONFIG` (the config this call resolved). A machine run is also what you get when stdin isn't a terminal.
- Exit codes: 0 when every script passed, 1 when one failed (`check script failed: <names>`) or none was found (the message says how to start one), 130 when the owner stopped with `q` or Ctrl-C.
- With worktrees on and the issue's worktree missing, it says which worktree and the `git worktree add` line.
- `show`, `start` and `transition` print a **Guided checks** block at testing and human-testing when the workflow uses the feature (a testing or human-testing prompt names `check-script`): the scripts and who runs what, or, with open Manual boxes and no script, the `--new` line.

### `approve`

`issue-cli approve <slug> --to <status> --quote "<the human's words>" [--how "<where>"]` is how a lead records a gate the human approved in chat:

1. It writes an `approval: "<quote>" (<how>, <date>) → <status> — recorded by <session>` comment (`tracker.ApprovalComment`). `--how` defaults to `in the lead chat`. The viewer's words box writes the same comment with `viewer` as how, and the Timeline pairs these comments with their approvals.
2. It sets `human_approval` to the status. It never toggles an existing approval off, unlike the viewer's button.

`--to` and `--quote` are required. It is refused:

- from a worker session (the error suggests a `question:` comment to its lead);
- for Human Testing, always: the human approves that in the viewer.

These checks read the environment and the session name; they are guardrails for agents, not security.

### `report-bug`

`issue-cli report-bug "<what went wrong>"` is for bugs in issue-cli or the workflow engine, from any project. A project's own bugs go on its own board with `issue-cli create`.

**Where it goes.** The engine project in the same `projects.yaml` (found by the [config lookup](#config-lookup), so report-bug works from any folder once the viewer has run with `-config`): `engine_project: <slug>` at the top, else the project whose `repo:` is `michal-franc/ai-workflow-engine` (or the old `michal-franc/ai-native-project-viewer`). The reporter needs no setup, and `--project` is optional.

**What it files.** An issue in the engine project at the bugfix type's first status (`idea`), system `CLI`, label `cli-bug`, with:

- the first sentence as the title (cut near 80 characters)
- a `## Report` section: the full text, plus the reporter's project and issue slug, agent, issue-cli version and OS, and the command. The command is `--cmd "<command line>"` if given, otherwise the agent's previous issue-cli call from its clilog (if it's under 10 minutes old), with free text shown as the clilog's `‹text …›` placeholders
- `reported_from: <project>:<issue slug>` and `reported_by: <agent>`

The `## Repro` section is left for triage, so the bugfix gate still needs a human repro.

**From a release candidate.** When the reporting build is a candidate (`v0.54.0-rc.2`, or a build past one), the bug belongs to the release being soaked. A new issue gets that milestone's `version:` (the label the board already uses for v0.54.0, e.g. `0.54`). A `report:` comment sets it on an issue that has no version, and leaves any other version alone. The output then says `Added to milestone 0.54 (found in release candidate v0.54.0-rc.2): the release waits for its fix`, or that the issue keeps its version. `--json` adds `milestone` and `found_in`. The engine's Status page lists these reports against the candidate ([Roadmap: Cutting a release](../roadmap.md#cutting-a-release-candidate-soak-publish)).

**Dedupe.** If an open `cli-bug` issue has the same title after normalizing (lowercase, punctuation dropped, spaces collapsed), the report becomes a `report:` comment on it instead. If the only match is closed (done or parked), a new issue `<title> (again)` is filed, linking the old one. A lock makes two agents reporting at once produce one issue.

**No engine project, or its issues directory is missing.** The report is saved in `${XDG_DATA_HOME:-~/.local/share}/issue-cli/bug-reports/` (per user) and the output says `saved locally, not sent` and why. With no config found at all, the why is the config lookup's error: every place checked and the fix. The next report-bug that finds the engine sends the queue, oldest first, and so does `issue-cli report-bug --send`. Until then, the Status page shows *N issue-cli bug reports not sent* under Needs you.

**Old reports.** `issue-cli report-bug --import ~/Work/bugs/*.json` files the old JSON reports with the same dedupe. It finds the reporter's project from the `issue_slug`, and moves each filed file into `imported/` next to it, which the Retros page skips.

**Output.** Agent-facing; it always names the issue or the queue file:

```
✓ Reported as new cli-bug issue cli/<slug> on the issue-viewer board
✓ Already reported: added your report to cli-bug issue cli/<slug> on the issue-viewer board (3 reports)
saved locally, not sent: ~/.local/share/issue-cli/bug-reports/<file>.json
✓ Sent 2 queued report(s): cli/<a>, cli/<b>
```

`--json` prints `{"result": "created|commented|queued", "project", "slug", "file", "reports", "queued", "reason", "imported": [...], "sent_from_queue": [...], "still_queued"}`.

The `Sender` interface in `internal/clibug` is where a second transport (GitHub issues, for machines without the board) plugs in.

### `process transitions`

**Workflow file line.** `process workflow`, `process transitions` and `show` print `Workflow file: <absolute path>` under their header (`built-in default` when the project has no workflow file), so an agent can see which workflow it is being walked through. `show --json` carries it as `workflow_file`.

**Timeline and Evidence lines.** `show` prints `Timeline: <viewer url>/p/<project>/issue/<slug>?tab=timeline` and `Evidence: …?tab=evidence` after the lead and dependency lines: the issue page's Timeline tab (where its time went, every CLI call) and Evidence tab (is it proven: shots, criteria → proof), links a lead or agent can hand the owner. Not in `--json`.

**Brief line.** `show` prints `Brief: <text>` next: the sentence the issue page's Issue tab leads with (`tracker.Brief`, see [Issue File Format](../issue-format.md#the-brief)), so an agent sees what the owner will read first. With no brief it prints `Brief: none.` and how to write one (a lead paragraph or `**In short:**`).

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

- **Status:** with the current and bundled stamps:
  - `up_to_date`: the stamp is the bundled one and the record decides every feature.
  - `undecided`: the stamp is current, but proposals wait on the owner. The message says how many and that `workflow scan --installed/--declined` records each pick.
  - `stale_record`: the stamp is current, but the file has features its `features:` record doesn't list (the "detected but not in the record" line). `issue-cli workflow scan` records them.
  - `behind`, `unstamped`, `custom` or `unknown_template`.
- **Record:** the `features:` record decides what is installed when the file has one (`record_source: record`); without one, the catalog's probes do (`detection`).
- **Proposals** are numbered. Each one has the feature id, `kind` (`new`, or `update` from an older installed version), version, category, whether the bundled template ships it, and the text the agent relays: What, Why, Where, Ask (the question for the human), and a YAML Snippet. By default the list covers `propose: true` entries for the project's template that the file lacks and nobody declined. With a `features:` record the record decides and the stamp hides nothing; without one, only entries newer than the stamp are listed (all of them when unstamped). An entry with `requires:` waits until the project has those features; `--all` lists it tagged `needs <id>`.
- After the proposals come three lists: already present, declined (with the re-open command `workflow scan --forget <id>`), and detected but not in the record (run `workflow scan`). These are never proposed.
- **Steps**, printed whenever there is something to propose:
  1. Continue only if the operator asked.
  2. Propose the items one at a time.
  3. `cp workflow.yaml workflow.oldN.yaml` (the first free N).
  4. Write the picked snippets, adapted to the project.
  5. Only once every proposal is installed or declined, set `template: <name>@<latest>`; while any is undecided, leave the stamp as it is.
  6. `issue-cli workflow scan --installed <picked> --declined <declined>`.
  7. `issue-cli process workflow`.
- `--json` emits one object: `status`, `workflow_file`, `template`, `current`, `latest`, `record_source`, `record`, `all`, `proposals[]`, `already_present`, `declined`, `detected_unrecorded`, `undecided` (the ids proposed without `--all`), `backup_path`, `steps[]`, `message`.

**No hints.** No other command mentions upgrades: `start` and `process workflow` used to end with an upgrade hint, which reached every worker and confused them. The operator sees the same plan on the viewer's Updates tab (`/p/<project>/updates`). A stale record is different: it is a fact about the file, not an upgrade. `process workflow` and `health lint` (check `template`) warn "features: record is stale: <ids> detected in the file but not recorded" and name `issue-cli workflow scan`, whatever the stamp. Undecided proposals stay the owner's, and neither command mentions them.

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

- **Report:** every catalog feature grouped by category (each heading once: the catalog keeps a category's entries together, and `TestCatalogKeepsEachCategoryTogether` fails otherwise), marked ✓ detected, + recorded only, ✗ declined, or · absent. A detected line shows the evidence at its concrete path (`transitions[3].actions[1].rule = "has_test_plan"`, `+N more`). Then come the changes to the record, notes, and the block itself.
- **Merge rules:**
  - installed = detected ∪ already recorded ∪ `--installed`. A recorded version is kept; new entries and `--installed` ones get the catalog version.
  - declined = recorded ∪ `--declined`, minus anything installed.
  - A recorded feature that is no longer detected stays installed, with a note pointing at `--forget`.
  - Ids the catalog doesn't know (written by a newer binary) are kept.
- **Errors:** an unknown id, declining a feature that is present in the file, the same id in `--installed` and `--declined`/`--forget`, extra arguments, or a file that isn't a YAML mapping.
- **Writing:** scan writes only when the record changes, or when there is none yet; a second run reports "record already up to date". It edits text: it replaces the existing `features:` block, or inserts one (with a comment) after `template:`, or else before the first top-level key. It then re-parses the result and refuses to write if anything other than `features:` changed. Comments and layout elsewhere are kept. It never sets `template:`, so an unstamped file keeps its proposals.
- `--json` emits `workflow_file`, `dry_run`, `written`, `had_record`, `features[]` (`id`, `version`, `category`, `title`, `detected`, `matches`, `evidence`, `recorded`), `record`, `block`, `changes`, `notes`.

### `workflow catalog-check`

For issue-cli developers, in the issue-viewer repository; `make validate` runs it after the tests.

```bash
issue-cli workflow catalog-check               # compare the work branch with main
issue-cli workflow catalog-check --base main   # the branch it merges into
issue-cli --json workflow catalog-check
```

- **What it compares:** the files changed since `git merge-base <base> HEAD` (commits, the working tree, untracked files) against each catalog entry's `sources:` (a glob, or `file.go#Symbol`: that declaration's code, doc comments ignored) and against `cmd/issue-cli/templates/workflow/*.yaml`. The catalog, templates and changelog are read from the working tree, so it works with any binary.
- **Passes** when each touched entry's `version:` is newer than the last release (the newest `## vX.Y.Z` in `cmd/issue-cli/CHANGELOG.md`), or a commit on the branch carries `Catalog: n/a <id>[,<id>] <why>`; and each touched template's stamp is newer than the last release.
- **Fails** with, per entry: the changed files, the entry's version, the next release, and both fixes (set `version:` to the next release and update its what/snippet, or add the trailer). A template gets the stamp to set and a reminder to re-run `workflow scan` on it.
- **Skips** (exit 0) on main and when there is no merge-base. Outside the issue-viewer repository it fails.

### `projects` and multi-project resolution

`issue-cli projects` lists every project parsed from `projects.yaml`. Its first line after the heading is `config: <path> (<why>)`, the config the lookup chose. The active project (resolved via `--project` or the single-project default) is marked `(active)`; in a multi-project setup with no `--project` the historical fallback (`projects[0]`) is marked `(historical default)`. `--json` emits an array with `slug`, `name`, `issue_dir`, `viewer_url` (the address issue-cli uses for that project, see [`dispatch` and `status`](#dispatch-and-status); the issue-approve mod reads it), `active`, and `default` fields.

```bash
issue-cli projects
issue-cli projects --json
issue-cli --project cli projects   # marks "cli" as active
```

`projects` is intentionally tolerant of a missing or unreadable config file — it is the discovery surface a confused bot reaches for first, so it must succeed when nothing else does. `help` and `process` are in the same allow-list.

#### Config lookup

Which `projects.yaml` issue-cli reads (`findConfig`, `cmd/issue-cli/config_lookup.go`):

1. `--config <path>`
2. `$ISSUE_VIEWER_CONFIG`
3. `./projects.yaml`, **only if it exists**
4. the config named in `~/.local/state/ai-workflow-engine/viewer.json` (`$XDG_STATE_HOME`), if that config file still exists. `issue-viewer -config` writes the file at start: `config` (absolute), `url`, `pid`, `started` (package `internal/viewerstate`)
5. `~/.config/ai-workflow-engine/projects.yaml` (`$XDG_CONFIG_HOME`), if it exists

Steps 1 and 2 are used even when the file is missing: the user named that file, so loading it fails loud instead of falling through to another config. When nothing is found, a command that needs a project fails with:

```
no projects config found. Checked, in order:
  1. --config: not passed
  2. $ISSUE_VIEWER_CONFIG: not set
  3. ./projects.yaml: not found
  4. ~/.local/state/ai-workflow-engine/viewer.json: not found (issue-viewer --config writes it at start)
  5. ~/.config/ai-workflow-engine/projects.yaml: not found

Fix: export ISSUE_VIEWER_CONFIG=/path/to/projects.yaml, or start the viewer (issue-viewer --config /path/to/projects.yaml)
```

(The real message shows the absolute paths.) Bootstrap mode (a cwd with `./issues/`) still picks the project; the lookup only decides which config it matches cwd against. Tests in `cmd/issue-cli` run with their own `XDG_STATE_HOME` and `XDG_CONFIG_HOME` and no `$ISSUE_VIEWER_CONFIG` (`main_testmain_test.go`), so they never read this machine's config.

**Resolution order in `loadProjectOrErr`:**

1. **Explicit `--project <slug>` always wins.** A bot inside one project workdir (with its own `./issues/`) can still query a sibling project by passing `--project`. The bootstrap auto-detection on cwd yields to an explicit flag.
2. **No `--project` + a local `./issues/`** → bootstrap mode synthesizes a single project from cwd. `--project` is unused; `len(allProjects) == 1`.
   The workflow file comes from, in order: `workflow:` in `./project.yaml`, then `./workflow.yaml`, then the one `projects.yaml` entry whose `workdir` is cwd or whose `issues` is `cwd/issues` (paths compared absolute, symlinks resolved). `version` comes from `./project.yaml` or that same entry. An entry for any other directory contributes nothing. When no local file sets the workflow and several entries match cwd, it returns `errAmbiguousProject` and lists only the matching slugs. With no match and no local file, the project runs on the built-in default workflow.
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
package's directory. Shipped changes collect under `## Unreleased` at the
top. To cut a release (once per milestone, see docs/roadmap.md), rename that
heading to `## vX.Y.Z — YYYY-MM-DD` and push main to origin; the release-marker
Action tags, writes the GitHub Release and builds the binaries. A push with
`## Unreleased` on top releases nothing.

## Design Considerations

When working on CLI changes:

- Output is consumed by agents, not humans — prioritize machine comprehension over aesthetics
- Document the output contract early: whether text is human-facing or agent-facing
- Consider whether `--json` or other machine-readable output is needed
- Script compatibility matters — avoid breaking existing agent prompts that parse CLI output
- After making changes, run `make install` to update the binary
