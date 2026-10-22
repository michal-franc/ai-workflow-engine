# Lead handoff: issue-viewer (AI Workflow Engine)

Saved 2026-10-22. This session ran the roadmap cleanup, the new release and ship flow, v0.51.0, v0.52.0 and most of 0.53.

- **Lead session:** the Claude in `~/Work/issue-viewer`, addressable as `issue-viewer-7b`. It doesn't run in a `lead-issue-viewer` tmux window, so `lead list` shows the project lead as "not running". Workers reach it via SendMessage anyway.
- **Server:** tmux window `issue-viewer:viewer-server`, `issue-viewer -config projects.yaml` from `~/Work`. Since the 0.53 localhost fix it listens only on `127.0.0.1:8080` and answers 403 to cross-origin and `Origin: null` POSTs. Restart after merges: `make install`, then `tmux respawn-window -k -t '=issue-viewer:=viewer-server' -c ~/Work 'issue-viewer -config projects.yaml; exec zsh'`.
- **Released:** v0.52.0 (project status pages, plus the sub-lead removal and bootstrap fix). Local main is at `14b8f6c`, **16 commits ahead of origin, not pushed**, with CI green.

## Plan

[docs/roadmap.md](../docs/roadmap.md) is the plan. Each version is one milestone with one focus, set as `version:` on the issues:

- 0.51 One terminal (released)
- 0.52 Project status pages (released)
- **0.53 Safe core** (in progress)
- 0.54 Project health
- 0.55 Fast server
- 0.56 Setup in minutes
- 0.57 Gates you can trust
- 0.58 One landing page
- 0.59 Preflight and onboarding
- 1.0

The epics:
- server hardening
- adoption
- gate correctness
- RigOne harness checks
- lean surface
- code health
- project health

Later: the decision ledger (unscheduled; the owner hasn't picked a slot) and feature store phase 2.

## Running now (0.53)

| Issue | Status |
|:--|:--|
| `api/frontmatter-writes-expand-dollar-references-and-break-on-newlines` | human-testing (owner playtest) |
| `ui/status-page-shows-what-waits-to-be-released` | in progress ("Ready to cut" also requires green CI) |
| `api/add-graceful-shutdown-and-http-server-timeouts` | in progress (depends_on the localhost fix, done) |

Done in 0.53:
- bootstrap-mode workflow fix
- sub-lead removal
- local CI
- canvas folding and epic filter
- localhost bind and cross-origin guard
- issue-approve mod 0.2.1 (Playtest button at testing for tweaks)

When the last three are done and CI is green, cut **v0.53.0**: rename `## Unreleased` in `cmd/issue-cli/CHANGELOG.md`, commit, `git push origin main` with the owner's go-ahead, then check the release Action ran.

## Owner rules set this session (recorded with `issue-cli decide`)

- **Shipping is a local fast-forward of main, with no pushes by agents.** The lead pushes main to origin only when the owner asks, usually to cut a release. Agents' `git push` was denied by auto-mode anyway: the global `autoMode.environment` only trusts the Toadie repo.
- **Releases are cut per milestone, not per push.** Agents write under `## Unreleased`. The release-marker Action releases only when the top CHANGELOG heading is a version.
- **Urgent features ship as their own version.** Milestones get renumbered when that happens (owner: "milestones look good").
- **Worktrees of merged branches may be pruned.**
- **The dispatch prompt moves off argv** to a file or stdin (process-safety issue).
- **/status becomes the landing page**, and Classic board and Graph are retired (0.58).
- **Local CI** runs on local main after every merge, via the opt-in `ci:` in workflow.yaml. It's live here (`make validate`, about 33 s; ux-test is the slow tier) and in RogueTech (`make check`, `on_red: file_issue`). Both were green at their first runs.
- **Project health is the engine's job.** It measures; agents don't self-report time.

## Cross-project

The RogueTech lead (`roguetech-x-25`) handed over six agent-readiness items. They're filed in 0.54–0.57 and linked from RogueTech's `data/agent-ready-codebase`. RogueTech's local CI has been on since its commit `e664ae36`.

## Open with the owner

- Where the decision ledger goes: 0.60/0.61 before 1.0, earlier, or Later.
- Commit the polybar changes (`~/.config/polybar/agents.py`, project name opens the board; `config`) to the home dotfiles repo?
- `origin` still points at the old repo name (`ai-native-project-viewer`); GitHub redirects to `ai-workflow-engine`.
- `workflow.old1.yaml` is untracked at the repo root; it's the owner's call whether to keep it.
- Four worktrees with uncommitted changes were left alone: `ui/add-issue-from-ui-via-button` (210 files), `issue-cli-check-by-index…`, `api/edit-in-nvim-should-include-frontmatter`, `~/Work/issue-viewer-readme`.
- Five old `bugs/*.json` reports are untriaged.

## Resume

1. Read this note and `docs/roadmap.md`.
2. Check the running agents with `issue-cli --config ~/Work/projects.yaml --project issue-viewer list --status "in progress"`, plus human-testing and in design. Then check `ci status`.
3. Always pass `--project issue-viewer`.
4. Dispatch with `curl -X POST -H 'Content-Type: application/json' -d '{"agent":"claude"}' http://localhost:8080/p/issue-viewer/issue/<slug>/dispatch`. A request with no Origin header passes the cross-origin guard.
