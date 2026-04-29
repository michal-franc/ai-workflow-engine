---
title: "CLI Overview"
order: 1
---

## Scope

The CLI system covers `issue-cli`, the command-line tool agents use to interact with issues during automated workflows.

## Key Files

- `cmd/issue-cli/main.go` — CLI entry point, command dispatch, output formatting

## Commands

| Command                          | Description                              |
|:---------------------------------|:-----------------------------------------|
| `issue-cli show <slug>`          | Print full issue context                 |
| `issue-cli list`                 | List issues with filters — supports `--sort score` and emits `Score`/`ScoreBreakdown` in `--json` when scoring is enabled |
| `issue-cli start <slug>`         | Pick up issue from any status — claim + advance handoff states |
| `issue-cli transition <slug>`    | Attempt the next workflow transition     |
| `issue-cli comment <slug>`       | Add a comment to an issue                |
| `issue-cli append <slug>`        | Append content to issue body             |
| `issue-cli replace <slug>`       | Replace content of an existing section   |
| `issue-cli set-meta <slug>`      | Set or clear a frontmatter field         |
| `issue-cli process workflow`     | Print the active workflow                |
| `issue-cli process transitions`  | Print transition rules (default workflow, or scoped via `--system <name>` or `<issue-slug>`) |
| `issue-cli process schema`       | Print the `workflow.yaml` schema (fields, action types, validation rules) |
| `issue-cli process changes`      | Print the release history (last 20 versions) |
| `issue-cli report-bug "..."`     | File a bug report about issue-cli itself |
| `issue-cli retrospective <slug>` | Save a workflow retrospective            |
| `issue-cli data <sub> <slug>`    | Per-issue structured data store — see [Per-issue Data Store](../data-store.md) |
| `issue-cli workflow init`        | Bootstrap a new project: writes `workflow.yaml` from a bundled template and scaffolds `issues/`, `docs/` |

### `append`

`issue-cli append <slug> --body "..."` adds content to the issue body. Two routing modes:

- **Default** — content is appended after the existing body. Headings inside `--body` must be unique against the issue.
- **Section** — pass `--section "Name"` to append into an existing section (or create it if missing). Use `--force` to disambiguate when the same heading exists at multiple levels.

If `--body` starts with a heading that is already present in the issue (and the rest contains only deeper subheadings), the command auto-routes into that section — equivalent to passing `--section`. This means agents drafting `## Implementation\n…` style appends do not have to retry with `--section` after a duplicate-heading failure.

The duplicate-heading guard still fires when `--body` introduces a *peer* heading that collides (e.g., `--body "## New\n…\n## Existing"`); pass `--section` to disambiguate.

### `list`

`issue-cli list` filters by `--status`, `--system`, `--assignee`, `--version`. With `--json`, each entry is the full issue plus two scoring fields:

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

The renderer is backed by `tracker.DescribeAction` and
`tracker.ValidationSummary` so descriptions stay in sync with the same strings
shown in transition previews.

### `data`

`issue-cli data <sub> <slug>` reads and writes the per-issue structured data store (`<slug>.data.json` next to the issue's markdown file). Subcommands are `add`, `list`, `set-status`, `set-comment`, `remove`. Agents must use the CLI rather than touching the JSON file directly so the on-disk shape can change without breaking them. Full reference: [Per-issue Data Store](../data-store.md).

```bash
id=$(issue-cli data add <slug> --description "finding")
issue-cli data set-status  <slug> "$id" "🔥 must-fix"
issue-cli data set-comment <slug> "$id" --text "see processor_test.go:142"
issue-cli data list <slug> --json
```

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
