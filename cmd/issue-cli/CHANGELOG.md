# Changelog

Release history for the `issue-viewer` web app and the `issue-cli` CLI.
This file is the single source of truth for release notes: `issue-cli process
changes` embeds and prints it verbatim, and tagged release annotations should
mirror these entries.

Lives at `cmd/issue-cli/CHANGELOG.md` (co-located with the CLI) because Go's
`//go:embed` cannot reference files outside the embedding package's directory.

Entries are newest-first. Each entry has the form:

    ## <version> — <YYYY-MM-DD>

    - user-visible change
    - another user-visible change

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
