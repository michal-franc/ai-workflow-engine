# Sub-lead handoff: workbench redesign phase 2 (2026-10-21)

> **Superseded, 2026-10-21.** The owner decided there are no sub-leads for now, one lead per project (umbrella D-13). This sub-lead stood down. The project lead dispatches the phase-2 children from main and ships them to main, and it owns the leftover `base/` branch. Don't resume from this note. Retro: `retros/20261021-192020-ui-workbench-redesign-inbox-system-pages-balance-board-and-decisions-index.md`.

Sub-lead `lead-ui-workbench-redesign-inbox-system-pages-balance-board-and-decisions-index` of epic
`ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index` (status shipping; phase 1 is in main as v0.38.0).
It reports to the project lead (Claude session `issue-viewer-7b`), and the owner talks to the sub-lead through that lead (D-3).

## What is live

- Base branch `base/ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index`, created by hand from main (29bfb02) on 2026-10-21. `lead spawn` had reused the phase-1 worktree, which was still on the shipped `work/` branch, and never created the base. `make validate` is green on the base. Main commits not in base: 0.
- Bug filed: `workflow/lead-spawn-epic-reuses-an-existing-worktree-without-switching-it-to-the-base-branch-and-reports-the-branch-as-created`.
- No workers running yet.

## Order (from the project lead, D-10 to D-12)

1. `ui/workbench-sidebar-navigation-and-a-new-theme-across-all-views`: foundation. **Waiting for the owner to dispatch it from the board** (requested from the project lead).
2. `ui/workbench-project-status-page-replaces-waiting-on-you` (D-11)
3. `ui/workbench-board-card-markers-and-agent-footer`
4. `ui/workbench-sidebar-and-system-pages`: system pages only
5. `ui/workbench-read-only-metric-tuning-and-dependency-views`: its assignee is stale (from 10-08)
6. `ui/workbench-retire-old-list-landing-and-legacy-views`: last; decide each view separately with the owner

Rules:
- Each child designs first, with refreshed mockups on real data (UI overlay rules), then stops for the owner's approval, relayed by the project lead.
- Don't run 2–6 in parallel until child 1's design has settled the frame and theme.
- Children ship into the base, never main. Run `make validate` after every merge. Merge main into the base when `lead list` shows drift.
- The base goes to main only when the owner says so.

## Resume

`issue-cli --config ~/Work/projects.yaml --project issue-viewer lead list`: check base drift and the running children. Then `issue-cli … show <child>` for each child in the order above.
