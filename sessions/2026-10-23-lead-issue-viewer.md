# Lead handoff: issue-viewer (2026-10-23)

Follows `2026-10-22-lead-issue-viewer.md`; read that for the plan, the owner rules and how to resume. Lead session: `issue-viewer-7b`.

## Released

**v0.53.0 "Safe core"** (`8abe91d`, pushed 2026-10-23; release Action green, 8 assets). It contains:
- the localhost-only server with a cross-origin guard
- local CI (`ci:` in workflow.yaml; live here and in RogueTech)
- the Status page redesign and the Waiting for release card
- report-bug files real issues (`engine_project:` in ~/Work/projects.yaml)
- issue-cli fixes: slug lookup, flags, `--help`, `list --all`, `check-edit`, retitle keeps the slug
- frontmatter safety, graceful shutdown
- dispatch starts the agent as the window's own command
- issue-cli finds its config from any folder
- the README revamp

## Running now (0.54: the issue page tabs epic goes first, owner's call)

Epic `ui/issue-page-tabs-issue-timeline-evidence`, in the epic's own order, chained with `depends_on`:
1. `ui/timeline-tab-replaces-the-agent-timeline` + `cli/clilog-and-stats-record-what-the-timeline-needs`: **dispatched**. A background watch in the lead session dispatches stage 2 when both are done.
2. `ui/approve-button-takes-your-words`
3. `ui/issue-tab-brief-needs-you-bar-and-a-folded-body`
4. `workflow/issue-cli-prove-link-each-criterion-to-its-proof`, then `ui/evidence-tab-proof-shots-and-criteria`

If the lead session restarted, the watch is gone: dispatch each next stage by hand once its `depends_on` is done.

**Experiment:** branch `exp/browser-terminal` (worktree `.worktrees/exp/browser-terminal`). A subagent is building a POC: a tmux terminal in the browser (xterm.js + WebSocket + PTY, grouped tmux session per viewer, read-only by default), tested on port 8091. Never merged; the owner tries it. Filed for later: `workflow/poc-type-of-work-experiments-on-exp-branches-done-creates-the-real-issue` (0.54; a done POC with verdict *keep* creates the real issue).

## New owner rules and decisions since 2026-10-22

- **Milestones are renumbered** when an urgent feature jumps in; the owner is fine with that. Current list: 0.54 Project health (now also the issue page tabs), 0.55 Fast server, 0.56 Setup, 0.57 Gates, 0.58 One landing page, 0.59 Preflight and onboarding, 1.0.
- **Bug intake:** report-bug creates `cli-bug` issues on this board. From other machines they'll come as GitHub issues (`cli/report-bug-sends-to-github-issues-from-other-machines`, 0.54). Triage: high and medium go into the current version, low go to `future`.
- **~/Work/bugs is fully triaged:** 53 reports in `~/Work/bugs/triaged/` with an `INDEX.md`.
- **Raw agent logs** were cleaned (1.17 GB). They grow without limit; a cap or cleanup fix was offered, not filed.
- `~/.zshenv` exports `ISSUE_VIEWER_CONFIG`. It's redundant now that issue-cli finds the config itself; harmless.

## Open with the owner

- Browser terminal: file it as a spike plus the feature (0.56?) after trying the POC
- Raw log cap fix: file it?
- Decision ledger placement
- polybar commit
- `workflow.old1.yaml`
- four dirty worktrees
