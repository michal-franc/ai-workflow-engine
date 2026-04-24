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
