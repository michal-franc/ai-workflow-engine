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
