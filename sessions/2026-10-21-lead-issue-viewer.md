# Lead handoff: issue-viewer (AI Workflow Engine)

Saved 2026-10-21, evening. The session ran 2026-10-18 → 2026-10-21 and survived one crash (2026-10-20, resumed from the 10-13 note's state).

- **Lead session:** the Claude in `~/Work/issue-viewer` (addressable as `issue-viewer-7b`). It does not run in a `lead-issue-viewer` tmux window, so `issue-cli` and the agents canvas show the project lead as "not running"; workers were told to message `issue-viewer-7b`.
- **Server:** tmux window `issue-viewer:viewer-server`, `issue-viewer -config projects.yaml` from `~/Work`, port 8080. Restart it after every major merge: `git merge --ff-only origin/main`, `make install`, then `tmux respawn-window -k -t '=issue-viewer:=viewer-server' -c ~/Work 'issue-viewer -config projects.yaml; exec zsh'`.
- **Version on main:** v0.44.0 (74e5c31).

## Next

1. Owner playtests the **status page** (`ui/workbench-project-status-page-replaces-waiting-on-you`, human-testing) once its agent has merged v0.44 and reports green; then approve documentation.
2. The three workbench children in progress come back at human-testing for playtests: **board markers + agent footer** (new `/workboard`; also removes `--wait`), **system pages**, **metric/tuning/dependency views**.
3. After those ship: decide on **retire old views** (`ui/workbench-retire-old-list-landing-and-legacy-views`, idea) once the new views are proven in daily use; then close the workbench umbrella.
4. **Adoption epic** (`cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use`, 5 children): the next big piece, run by the project lead (no sub-leads).
5. **Decision ledger epic**: the rest of the owner's D-1 ideas, built on `issue-cli decide` only (D-2).
6. Triage the 7 stale `in design` issues from May/June (close, park or redispatch), and close `umbrella-issue-grouping-multiple-issues` as covered by the canvas.
7. File the known bugs: `issue-cli comment --help` and `set-meta --help` take `--help` as a slug; the `lead spawn --epic` worktree-reuse bug is filed.

## Owner rules set this session

- **One lead per project, no sub-leads** (workbench umbrella D-13). Epic children branch from main and ship to main.
- **No pull requests.** Shipping merges `origin/main`, runs `make validate`, then `git push origin HEAD:main`, without asking, after the owner's playtest. The release-marker Action tags on push to main.
- **`done` needs no approval;** agents close their own issues. Worktrees are never cleaned up.
- **Optional `in design → in progress`** edge skips backlog.
- **Questions stay in chat** (Claude Code / Codex, SendMessage to the lead); `issue-cli ask/answer` were removed (v0.40.0). Record decisions with `issue-cli decide <slug> --text … --how "lead chat" --quote "<owner's words>"`.
- **Playtests** are how the owner tests web UI (`make playtest` in the worktree, ISSUE inferred from the branch), then `make playtest-report APPLY=1`.
- **Types of work** (feature, tweak, bugfix) and a global **parked** status are in workflow.yaml; shell gate and scoring were declined.
- **Restart the server** after major merges (memory note).

## Shipped this session

v0.34 playtest sessions · v0.35 issues canvas · v0.36 versioned workflow templates + upgrade · v0.37 lead engineer agents · v0.38 workbench decisions + balance · v0.39 project-scoped tmux agents, parked + types · v0.39.1/.2 no sub-lead buttons, hierarchy-only agents canvas · v0.40 ask/answer removed · v0.41 sidebar + new theme · v0.42 workflow designer removed · v0.43 approve buttons in the agent's session (`issue-approve` mod, user scope) · v0.44 workflow-flow page removed.

## Outside the repo

- **polybar** `~/.config/polybar/agents.py`: agents grouped by project with a crown for the lead and icons for working / permission / question / playtest / gate / stuck / idle; todo, activity and katas hidden from the bar.
- **issue-approve** mod installed at user scope from `~/Work/issue-viewer/claude-mods` (marketplace `ai-workflow-engine`). Running sessions need `/reload-plugins` once.
- **klaudiush** is gone; commit normally with the Co-Authored-By trailer.
