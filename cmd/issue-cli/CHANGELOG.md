# Changelog

Release history for the `issue-viewer` web app and the `issue-cli` CLI.
GitHub releases on `michal-franc/ai-native-project-viewer` are the source
of truth for release notes; `issue-cli process changes` fetches them live
and falls back to this embedded file when the API is unreachable. Keep
entries here mirrored with the GitHub release descriptions.

Lives at `cmd/issue-cli/CHANGELOG.md` (co-located with the CLI) because Go's
`//go:embed` cannot reference files outside the embedding package's directory.

Entries are newest-first. Each entry has the form:

    ## <version> — <YYYY-MM-DD>

    - user-visible change
    - another user-visible change

## v0.10.4 — 2026-04-30

- Internal refactor: `cmd/issue-cli/main.go` (2380 lines, ~250-line dispatch switch) split into one `cmd_<name>.go` per subcommand plus a `Command`/`Context` registry (`commands.go`, `context.go`, `helpers.go`). `main()` is now 12 lines and only logs the invocation, parses globals, and delegates to `run()`. Every subcommand owns its own `flag.FlagSet` and returns errors instead of calling `os.Exit`; the package-global `jsonOutput` is gone (`Context.JSONOutput` carries the flag), so two CLI invocations with different `--json` modes can run concurrently without racing — verified by a new `TestConcurrentJSONOutputDoesNotRace` under `go test -race`. Top-level help is generated from the registry rather than a hand-rolled blob, so adding a subcommand no longer requires editing `printHelp`. No public CLI behavior change: text and `--json` shapes are byte-for-byte identical to v0.10.3.
- Side-effect fixes embedded in the refactor: `findIssue` now returns `(*Issue, error)` (the old `fatal()`-on-miss path swallowed every "not-found" branch); `runStart` matches missing approvals via `errors.Is(err, tracker.ErrApprovalMissing)` instead of `strings.Contains` against the message; `claim --force` is parsed by `flag.FlagSet` rather than walked out of `os.Args` with a `goto`; `report-bug` opens its log file with `O_CREATE|O_EXCL` plus a counter suffix so two reports in the same second no longer truncate each other; `update --title ""` is now distinguishable from "title flag absent" (`flag.Visit`).

## v0.10.3 — 2026-04-29

- Internal refactor: `internal/tracker/workflow.go` (1812 lines) split into six cohesive files (`workflow_config.go`, `workflow_transition.go`, `workflow_merge.go`, `workflow_validate.go`, `workflow_preview.go`, `workflow_schema.go`), plus a new `heading.go` for the section/heading helpers shared with `issue.go`. No public API change, no behavior change — pure source reorganization to make subsequent workflow-area changes produce smaller, single-area diffs. `docs/API/overview.md`, `docs/CLI/overview.md`, and `docs/Workflow/overview.md` updated to reference the new layout.

## v0.10.2 — 2026-04-29

- Fixed three latent correctness bugs in `internal/tracker`. (1) Frontmatter parsing: `updateIssueFrontmatterLocked` and `SetFrontmatterField` were splitting on bare `"---"` instead of `"\n---"`, so an issue file whose YAML carried a `---` substring (a hyphen-separator inside a value) got corrupted on the next write. All four split sites now use the same `"\n---"` form as `ParseFrontmatter`. (2) `WorkflowConfig.Clone` shallow-copied `Scoring` and `Board`, leaking map and slice mutations from per-system overlays back to the base config. Both are now deep-copied. (3) `ApplyTransitionWithFields` ran the post-action `human_approval` clear after the action loop, silently overwriting any `set_fields` action that targeted `human_approval`; the clear now runs first so explicit `set_fields` values win. The `set_fields` row in `docs/Workflow/overview.md` now enumerates the supported `field` values.

## v0.10.1 — 2026-04-29

- Internal refactor: `handlers.go` (3512 lines) split into 13 cohesive files (`routes.go`, `template_funcs.go`, `helpers.go`, `tmux.go`, and `handlers_<area>.go` per responsibility). `handlers_test.go` (60K) split to mirror the new layout. No public API change, no behavior change — pure source reorganization to make subsequent handler-area changes produce smaller, single-area diffs.

## v0.10.0 — 2026-04-29

- New `issue-cli workflow init` command bootstraps a fresh project in one shot: writes `workflow.yaml` from a bundled template and scaffolds `issues/` and `docs/` if they don't exist. Three templates ship: `development` (the canonical software-delivery flow this repo uses), `review` (`inbox → … → archived` triage flow), and `writing` (`idea → … → published` long-form content flow). `--template <name>` picks one; without it, an interactive prompt appears when stdin is a terminal, otherwise the command exits non-zero with the list of valid names. `--force` overwrites an existing `workflow.yaml`. Templates live as editable YAML under `cmd/issue-cli/templates/workflow/*.yaml`, are embedded via `//go:embed`, and the list of valid `--template` names is derived from the embedded directory so adding a new template is just dropping a file and rebuilding.

## v0.9.0 — 2026-04-29

- New per-issue **structured data store**. Every issue can carry a sidecar `<slug>.data.json` of `{id, description, status, comment}` rows. Manage rows with `issue-cli data add | list | set-status | set-comment | remove` (`add` prints the assigned id on stdout for shell composition; `list --json` emits the entries array). The detail view renders the rows as an inline table at a `<!-- data statuses=... -->` marker in the body — per-issue dropdown statuses (spaces and emojis allowed inside a token), inline status select, contenteditable comment, row remove button. Designed for agent code-review findings the human triages inline. Atomic temp+rename writes; ids are monotonic and not reused after delete; missing sidecar means empty store, no error. New endpoints: `POST /issue/<slug>/data`, `POST /issue/<slug>/data/<id>/status`, `POST /issue/<slug>/data/<id>/comment`, `DELETE /issue/<slug>/data/<id>`. See `docs/data-store.md`.
- Markdown rendering now passes raw HTML through (`goldmark` `html.WithUnsafe()`), so HTML comments — including the new `<!-- data -->` marker — survive into the rendered body instead of being replaced by `<!-- raw HTML omitted -->`. Issue bodies are already trusted content, so this aligns with the existing trust model.

## v0.8.0 — 2026-04-29

- `issue-cli list --json` now emits `Score` (float) and `ScoreBreakdown` (`{Total, Components[]}`) on every entry when `workflow.yaml` has `scoring.enabled: true`. Both fields are `null` when scoring is disabled or the issue has no scoring inputs. The breakdown is the same `tracker.ComputeScore` output that drives the viewer's `⚡N` badge — CLI consumers no longer need to re-implement the scoring formula in bash against raw frontmatter.
- New `--sort score` flag on `issue-cli list` orders output by `Score` descending. When scoring is enabled and `default_sort: score_desc` is set in `workflow.yaml`, the sort is applied automatically with no flag.
- Documented that `scoring.formula.priority` keys should be lowercase: frontmatter values are normalized to lowercase before lookup. Uppercase keys still match via a case-insensitive fallback but lowercase is the canonical form.

## v0.7.3 — 2026-04-28

- Fixed `issue-cli transition` silently dropping the `append_section` payload when the issue body quoted the section heading inside a fenced code block (` ``` ` or `~~~`). Heading detection in `findHeadingMatches` / `findAllHeadings` now skips lines inside fences, so transitions like `human-testing → documentation` correctly add the `## Documentation` section even when the body documents a `## Documentation` example.
- `issue-cli check <slug> "<query>"` now skips checkboxes inside fenced code blocks. Quoting `- [ ] …` examples in the issue body no longer absorbs the next `check` and leaves the real workflow checkbox unchecked.

## v0.7.2 — 2026-04-28

- `issue-cli append <slug> --body "..."` now auto-routes into an existing section when `--body` starts with a heading already present in the issue and contains only deeper subheadings — no more retrying with `--section` after a duplicate-heading failure for the common `## Implementation\n…` pattern. The duplicate-heading error still fires when a *peer* heading collides (e.g. `## New\n…\n## Existing`); pass `--section` in that case.
- `--section` is now documented in `issue-cli help` and surfaced in the dispatch prompt's "commands you can use freely" block, so agents see it before hitting the duplicate-heading error.

## v0.7.1 — 2026-04-28

- `issue-cli transition` no longer rejects valid `target: frontmatter` answers that are already on the issue. The validator now consults the issue's existing frontmatter for required `target: frontmatter` fields, so `set-meta <key> <value>` followed by `transition` succeeds without re-supplying the value. `target: section:<Title>` fields still require an explicit answer (they record a fresh body line each time).
- New repeatable `--field key=value` flag on `issue-cli transition` for inline answers: `issue-cli transition <slug> --to "waiting-for-team-input" --field waiting="design review"`. Explicit `--field` values win over frontmatter fallback when both are set.
- `issue-cli process transitions` now renders required fields with target context: `Required frontmatter field "waiting" (set via \`--field waiting=…\` or \`set-meta\`)` instead of the prior generic `Prompts for field "waiting" (required) before commit`.

## v0.7.0 — 2026-04-28

- `issue-cli process transitions` now renders rules from the loaded workflow rather than a hardcoded ruleset. Each row lists validation rules, human-approval gates (previously only discovered by failing a transition), and side effects (`set_fields`, `append_section`, `inject_prompt`); optional and global statuses are surfaced separately. Three scopes are supported: no-arg prints the project default and lists configured per-system overlays at the bottom; `--system <name>` (alias `--workflow <name>`) prints the rules merged with that overlay; passing an issue slug resolves the issue's `system` field and labels the header accordingly (issues with no system explicitly say `(no system overlay; project default)`). Backed by new `tracker.DescribeAction` and `tracker.ValidationSummary` exports so descriptions stay in sync with transition previews.

## v0.6.0 — 2026-04-27

- New `issue-cli replace <slug> --section "<name>" --body "<content>"` command. Replaces the content of an existing section in place — finds the heading at any depth, swaps everything between it and the next heading of equal or shallower depth, and preserves the heading line. Errors if the section doesn't exist (use `append --section` to create), and requires `--force` when multiple normalized matches exist. Pairs naturally with `append --section` so evolving sections (status tables, checklist progress, summary paragraphs) can be rewritten rather than accreted.

## v0.5.0 — 2026-04-24

- Board drag-and-drop now runs the same workflow engine as `issue-cli transition`. Moving a card runs validations, executes `actions[]` (`append_section`, `inject_prompt`, `set_fields`, `require_human_approval`), and blocks the move with HTTP 409 + a toast when any rule fails. Cancelling the prompt reverts the card to its source column and leaves the file untouched.
- New `transitions[].fields[]` declarative block in `workflow.yaml`. Each field has `name`, `prompt`, `target` (`frontmatter` or `section:<Title>`), `required`, and `type` (`text` or `multiline`). Answers are captured through a modal before the transition commits. Frontmatter targets write an arbitrary scalar key; section targets append `- **<prompt>:** <answer>` under the named section.
- New wildcard source: `from: "*"` matches any source status when no exact `from:/to:` edge is defined, so a single transition can cover every column (e.g. "defer from anywhere"). Exact edges still win over wildcards.
- New `statuses[].global: true` flag: marks a status as an escape hatch where transitions out to any other status are allowed, with no linear-lifecycle constraint. The board column renders a `global` badge next to the existing `optional` badge.
- New `GET /p/<proj>/issue/<slug>/transition?to=<status>` preview endpoint returns the same `PreviewTransition` struct used by the CLI, plus the declarative `fields[]` — the board uses it to decide whether to open the prompt modal and what to render in it.
- `IssueUpdate` gains an `extra_fields` map for writing arbitrary scalar frontmatter keys; protected keys (`title`, `status`, `human_approval`, `started_at`, `done_at`, `number`, `repo`, `created`, `labels`) are always refused.

## v0.4.2 — 2026-04-24

- Agent Timeline now strips issue-cli global flags (`--json`, `--config <val>`, `--project <val>`) from clilog entries before interpreting the subcommand, so calls like `issue-cli --project demo show 42` render as `show` instead of mis-classifying `--project` as the command. Reader-side fix: existing historical logs now render correctly.

## v0.4.1 — 2026-04-24

- Agent dispatch now persists the exact briefing prompt to `.agent-logs/<session>/dispatch-prompt.txt` at dispatch time. The Agent Timeline's dispatch row replays the real prompt when the file exists and falls back to the reconstructed version (labeled `(reconstructed)`) for older logs.

## v0.4.0 — 2026-04-24

- Agent Timeline on the issue detail view. Parses `.agent-logs/<assignee>/<assignee>.clilog` into a structured list of events (start, show, process, append, check, transition, comment, retrospective) with click-to-expand detail for bodies.
- Transition events are enriched from `workflow.yaml`: validations (with their rule descriptions), `inject_prompt` text, appended section bodies, required human-approval gates, and `set_fields` actions all render inside the expanded row.
- `start` events show the canonical claim-to-work transition (typically `backlog → in progress`) with the same workflow actions the bot received.
- Every transition surfaces the target status's `prompt` field as a `status_prompt` action, so the view shows the guidance the agent reads on entering each new status.
- A synthetic `dispatch` row at the top reconstructs the base agent prompt (via `buildAgentPrompt`) using the pre-start status, approximating what the bot was briefed with at dispatch time.
- Purely server-side: works on every existing `.clilog` file without any CLI changes.

## v0.3.1 — 2026-04-24

- `issue-cli process changes` now fetches release history live from the GitHub releases API for `michal-franc/ai-native-project-viewer`. The embedded CHANGELOG.md remains as an offline fallback when the API is unreachable or rate-limited.
- Shipping workflow gains a `CHANGELOG.md updated` checkbox so the offline fallback stays in sync with the GitHub release notes.

## v0.3.0 — 2026-04-23

- Opt-in ticket scoring system driven by a `scoring` block in `workflow.yaml`: priority weights, due-date urgency (capped), staleness per day, per-label weights, and a `default_sort` option.
- New `score_boost` frontmatter field for manual bumps on individual issues.
- Board cards show a color-graded `⚡ N` badge; list view adds a score chip and a default/score toggle; detail sidebar renders the full per-component breakdown.
- `?sort=score` works on list and board; `default_sort: score_desc` controls initial order.
- No migration required — existing projects see no change unless `scoring.enabled: true` is set.

## v0.2.0 — 2026-04-23

- New `issue-cli process schema` command — prints every `workflow.yaml` field (from `yaml:"..."` + `desc:"..."` struct tags), every action type, and every validation rule. Drift guarded by tracker tests.
- New `issue-cli process changes` command (aliases: `changelog`, `versions`) — prints this changelog, newest-first, capped to the last 20 version entries.
- Both commands listed under `issue-cli process` (no topic), `issue-cli --help`, and the unknown-topic error.
- docs/CLI/overview.md and README.md updated with the new commands.

## v0.1.1 — 2026-04-23

- Shipping status now includes release-description polish and explicit semver bump guidance.
- Release-marker workflow publishes a GitHub Release when a `vX.Y.Z` tag is pushed.

## v0.1.0 — 2026-04-23

- First tagged release. Establishes the release-marker workflow and the shipping-to-release process going forward.
- Workflow capabilities available at this release:
  - `optional: true` on statuses (skippable on forward transitions, CTA in the viewer).
  - Per-transition actions: `validate`, `require_human_approval`, `append_section`, `inject_prompt`, `set_fields`.
  - Validation rules: `body_not_empty`, `has_checkboxes`, `section_has_checkboxes`, `has_assignee`, `all_checkboxes_checked`, `section_checkboxes_checked`, `has_test_plan`, `has_comment_prefix`, `approved_for` / `human_approval`.
  - Per-system workflow overlays merged over the base workflow (`systems:` block).
  - Board configuration: `board.columns` and `board.card_fields`.
