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
| `issue-cli start <slug>`         | Pick up issue from any status — claim + auto-advance handoff states (announced with a banner). `--wait` blocks until the handoff approval exists |
| `issue-cli transition <slug>`    | Attempt the next workflow transition. `--dry-run` lists every unmet requirement; `--wait` blocks until the human approval exists |
| `issue-cli comment <slug>`       | Add a comment to an issue                |
| `issue-cli check <slug> <id>...` | Tick checkboxes by id (several at once), whole section (`--section X --all`), text, or `--section` + `--index` |
| `issue-cli checklist <slug>`     | List checkboxes grouped by section, each with its id (`D3`, `AC2`) |
| `issue-cli append <slug>`        | Append content to issue body             |
| `issue-cli replace <slug>`       | Replace content of an existing section   |
| `issue-cli set-meta <slug>`      | Set or clear a frontmatter field (refuses `type` in projects with types — use `set-type`) |
| `issue-cli set-type <slug> <t>`  | Change an issue's type of work; agents only while it is at its type's first status. See [Types of Work](../Workflow/types.md) |
| `issue-cli process workflow`     | Print the active workflow, the `== Types ==` block and `workflow.yaml` lint warnings; `--type <t>` prints one type's lifecycle |
| `issue-cli process transitions`  | Print transition rules (default workflow, or scoped via `--system <name>`, `--type <t>` or `<issue-slug>`) |
| `issue-cli process schema`       | Print the `workflow.yaml` schema (fields, action types, validation rules) |
| `issue-cli process changes`      | Print the release history (last 20 versions) |
| `issue-cli report-bug "..."`     | File a bug report about issue-cli itself |
| `issue-cli retrospective <slug>` | Save a workflow retrospective            |
| `issue-cli data <sub> <slug>`    | Per-issue structured data store — see [Per-issue Data Store](../data-store.md) |
| `issue-cli workflow init`        | Bootstrap a new project: writes `workflow.yaml` from a bundled template and a one-project `projects.yaml`, and scaffolds `issues/`, `docs/` |
| `issue-cli projects`             | List configured projects (slug, name, issue dir). `--json` for scripting |
| `issue-cli telemetry report`     | Local usage report: calls per command, never-used commands/flags/topics, errors, unknown tokens, retry sequences. See [CLI Usage Telemetry](telemetry.md) |
| `issue-cli telemetry path`       | Print the active telemetry file and whether recording is enabled |

### `start`

`issue-cli start <slug>` is the single entry point for picking up an issue. It claims the issue (sets the assignee if unset), prints the checklist, status guidance, and next-transition contract, and is idempotent on re-runs.

From a **handoff status** (`backlog`, `human-testing`) it also *auto-advances* to the next work status when the matching approval is present — `backlog → in progress`, `human-testing → documentation`. The target is the next non-optional status in the workflow, not a hardcoded value; only the handoff set is fixed. Because crossing into implementation from a re-claim is surprising, the advance is **never silent**: it is announced with a prominent `⚠ AUTO-ADVANCED  <from> → <to>` banner that also notes the consumed approval. If the approval is missing, `start` fails without mutating assignee or status. From any non-handoff status, `start` only claims and reports `Status unchanged`.

`issue-cli start <slug> --wait [--timeout <dur>]` blocks on a handoff status until the approval exists, then advances as above. It works like `transition --wait` (see [Blocking on a human approval](#blocking-on-a-human-approval---wait)), including exit code 3 on timeout. The advance is recorded in the stats sidecar like any other transition.

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
      → meanwhile block on it: issue-cli transition cli/sample --to "backlog" --wait --timeout 9m
Nothing was changed.
```

Kinds are `order` (not a legal next step), `field` (a required `--field` answer is missing), `validator`, and `approval`. In text output a `→` fix line is left out when the validator's message already contains that command. JSON always carries it in `fix[]`. When nothing is unmet, the output is `✓ Ready: <from> → <to>` plus the `Will:` side-effects. It exits 0 when ready and 1 otherwise. `--json` returns `{dry_run, ready, from, to, slug, problems[{kind, requirement, message, fix[]}], side_effects}`.

It is built on `WorkflowConfig.PreviewTransitionAll`. That is the engine behind the viewer's transition preview, without the stop at the first failure, so the CLI and the viewer agree on what is missing.

#### Blocking on a human approval (`--wait`)

`issue-cli transition <slug> --to "<status>" --wait [--timeout <dur>] [--interval <dur>]` replaces the "stop, ask, retry later" loop at approval gates:

1. It checks every requirement first. If any requirement other than the approval is unmet, it prints them (like `--dry-run`) and exits 1 without waiting, because a human click will not fix them.
2. If only the approval is missing, it prints one line to stderr with the approve link and blocks. There is no heartbeat output.
3. It re-checks the issue file every `--interval` (default `2s`). When the approval appears, it transitions and prints the normal output plus `✓ Waited: <duration> (approved <time>)`.
4. If the issue changes during the wait so that a machine check fails (for example a box is unticked) or the status moves, it exits 1 with that error instead of transitioning.

| Exit code | Meaning |
|:--|:--|
| 0 | Transitioned (or, with `--dry-run`, would succeed) |
| 1 | Error or unmet requirement; nothing changed |
| 3 | `--wait` timed out; nothing changed. Re-run the same command to keep waiting |

Without `--timeout` the wait is unlimited. Agents that run issue-cli from a Bash tool with a foreground cap (Claude Code: ~10 minutes) should pass `--timeout 9m` and re-run on exit 3, or run the command in the background. A re-run keeps the original wait start time, so a gate wait spread over several calls is measured end to end. Exit 3 does not trigger the repeated-failure retry hint.

**Don't pipe `--wait`.** `issue-cli … --wait | head` (or `| tail`) reports the pipe's exit status, which hides exit 3. Read the `Still waiting for human approval …` line, or run the command unpiped.

The CLI points agents at `--wait` itself, so the guidance does not depend on any project's `workflow.yaml`:

- The `== Next ==` block from `transition` and `start` prints the `--wait` form when the next step needs a human approval. That is `start <slug> --wait --timeout 9m` from a handoff status, otherwise `transition … --wait --timeout 9m`. A one-line note follows it. It is never suggested for `done`. `--json` carries this as `next_command` / `next_command_note`.
- A missing-approval error from `transition` or `start` ends with the exact `--wait` command to run instead of retrying.
- When the same command fails 3+ times in a row on a missing approval, the retry hint points at that `--wait` command instead of the generic "try a different approach".

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

`issue-cli workflow init` bootstraps a fresh project directory. It writes `workflow.yaml` from one of three bundled templates, writes a one-project `projects.yaml` for the board, and scaffolds `issues/` and `docs/` if they do not already exist. It ends by printing the next two commands (`issue-cli create`, `issue-viewer -config projects.yaml`).

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
- `projects.yaml` is written only when the directory has none, and is never overwritten, even with `--force` (the output says `· Kept the existing projects.yaml`). The project is named after the directory, with the same slug the CLI uses when it runs from a folder with `./issues`. Paths are relative (`./issues`, `./docs`, `./workflow.yaml`, `workdir: "."`), and `terminal` is left unset so the board detects one (see [Agent Dispatch](../agent-dispatch.md#terminal-configuration)).
- Templates live as plain YAML under `cmd/issue-cli/templates/workflow/*.yaml` and are embedded at build time via `//go:embed`. The list of valid `--template` names is derived from the embedded directory, so adding a new template is just dropping a new `<name>.yaml` file there and rebuilding.

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
