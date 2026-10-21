# Lead session handoff: issue-viewer (AI Workflow Engine)

Saved 2026-10-13 before a shutdown. The session ran 2026-10-07 → 2026-10-13.

- **Lead session (Claude Code):** https://claude.ai/code/session_01RDZNeiuqLVyv1J5wNF7ZsM. Resume it with `claude --resume`, or open the link.
- **Lead working dir:** `~/Work/issue-viewer`, config `~/Work/projects.yaml`, project `issue-viewer`.
- **Server:** `issue-viewer -config ~/Work/projects.yaml`, run from `~/Work`, port 8080.

## After reboot: bring things back

1. **Server.** `make install` has already run with #54 merged, so the binary is new. Start it with `cd ~/Work && issue-viewer -config ~/Work/projects.yaml`. The new binary passes the prompt as an argument, so dispatched prompts are no longer lost. Until this restart, every dispatch needed the lead to deliver the briefing by hand (write it to a file, then tell the agent to read it).
2. **Agents.** Re-dispatch from the board (▶ Claude) on the issues below. Dispatch reuses each worktree. Brief each one with the "Resume" line in its row.
3. **Lead check-ins.** Workers no longer wait or poll at gates (wait issue, D-2). They post what they need and end their turn, and the lead has to notice. Consider scheduled check-ins as described in `docs/patterns/lead-session.md`.

## Owner rules set this session

- **Record every owner decision** as a `D-n` line under `## Decisions` with `issue-cli decide <slug> --how "lead chat" --text … --quote "<owner's words>"`. The owner: "recording decision is super important". `decide` is on the unmerged UX branch. Until it merges, use `.worktrees/ui/workbench-redesign-…/.agent-logs/decisions-sandbox/bin/issue-cli` with `env -u ISSUE_VIEWER_CONFIG`. Gate ticks stay as `approval:` comments.
- **The lead may approve gates.** Owner: "you can go ahead with approvals". Ticking Human Testing still needs the owner, or their explicit "tick it".
- **No inbox screen; the lead relays approvals and questions** (workbench umbrella D-8, D-9).
- **No waiting at gates.** Workers hand off to the lead and end their turn (wait issue D-2).
- **Sandboxes must never touch the real tmux server.** Use `tmux -L <name>`, or a private `TMUX_TMPDIR` with `TMUX` unset. On 2026-10-08 at 21:39:46, the dispatch sandbox's `clean` killed the real server and every agent with it.
- **On `main`, `human_approval` holds one value.** Approve one gate at a time (backlog, then in progress).
- **Retitling an issue changes its slug**, because the slug comes from the title. Rename the file and fix references after a retitle.

## issue-viewer agents (tmux session `issue-viewer`)

| Issue | Status | Branch / worktree | State at shutdown | Resume |
|---|---|---|---|---|
| `ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index` (umbrella), plus child 1 `ui/workbench-decisions-index-generated-from-decisions` and child 2 `ui/workbench-balance-board-from-typed-balance-comments` | all three **human-testing** | `work/ui/workbench-redesign-…`, 12 commits ahead of main, clean. Head is `0a7bcb5` "drop the inbox; lead relays answers" | The inbox has been removed. Decisions index and balance board are built. Child 3 (`ui/workbench-read-only-metric-tuning-and-dependency-views`) is still at idea; the agent had researched rust-manager `Targets.cs`, `make balance` and `Sim/Tuning.cs`. Its retro notes that two test plans went stale when the inbox was removed. | Owner tests the decisions and balance sandboxes (`.agent-logs/decisions-sandbox` :8097, `balance-sandbox` :8094; `inbox-sandbox` is obsolete). Then one PR for the branch. Redispatch on child 3. |
| `workflow/versioned-workflow-yaml-templates-and-an-upgrade-command-for-agents` (phase 1 of the feature store) | human-testing; **Human Testing ticked, documentation approved** | `work/workflow/versioned-…`, 3 commits ahead, clean. Head is `b780095` | Scope extended: per-feature installed/declined record, copy model, a catalog of 34 features with structural probes, `workflow scan` (dry-run report on copies of all 9 projects is posted as a comment), and `upgrade --all`. `approval-wait-prompts` became a hand-off-to-lead feature. The owner said "approved documentation, carry on with the docs". | Redispatch: `start` → documentation → write docs → shipping → PR. **Version clash:** it stamps `v0.34.0`, as does PR #53. Whichever merges second bumps. |
| `cli/wait-re-check-every-2-min-3-checks-then-stop-and-let-the-nudge-resume-the-agent` | in design; **backlog approved** | `work/cli/wait-re-check-…`, no commits yet | Redesigned for D-2: remove the `--wait`/`--timeout 9m` guidance from everything agents read, and keep the flag for scripts. The owner said "approved for backlog, run start". | Redispatch: `start` → in progress (lead approves). The title is outdated; retitle at ship time (and fix the slug). |
| `api/dispatch-prompt-lost-…-add-a-get-prompt-endpoint` | shipping | PR #54 **merged** | Done except for removing the worktree. | Owner: `git worktree remove .worktrees/api/dispatch-prompt-lost-…`, then approve `done`. |
| `workflow/types-of-work-…` | shipping | merged earlier | Needs closing. | Approve `done`. |

**Open PR:** #53 "feat: detect a terminal + init writes projects.yaml (v0.34.0)", mergeable and waiting for owner review.

## Parked (idea, not dispatched)

- `ui/decision-ledger-epic-decision-records-filtered-jit-prompts-audit-gates-and-challenges`. Built on Prelint (prelint.com) and the ideas artifact https://claude.ai/artifact/1huMqWapzpffvve2nFPPnB. The owner picked ideas 1–4 and 6–21 (D-1). Waits for the decisions index to ship. Idea 19's "challenge inbox" becomes the lead relaying challenges.
- `workflow/feature-store-epic-installable-workflow-features-catalog-and-project-scan`. Copy model, rescan the catalog, scan projects (D-1). Phase 1 is the upgrade issue.
- `cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use`. Five children, order: setup + `doctor` → agent-led onboarding → first run → one binary.
- Workbench children 4, 5 and 7: sidebar/system pages, board markers, retire old views. Child 6 (inbox extras) is **won't do** (D-1, `closed:` field). The workflow has no won't-do status; `workflow/close-an-issue-as-delivered-by-another-issue` would cover that.

## Known bugs found this session (not yet filed)

- Worktree sparse-checkout `!issues/` also hides `demo/issues/`, so `TestDemoConfig` fails in every dispatched worktree.
- The viewer's tmux calls and the inbox sandbox use the real tmux server. Tests spawn zsh panes that write into the real `~/.zsh_history`.
- `issue-cli comment --help` posts "--help" as a comment instead of printing help.
- Five stale `in design` issues from May/June and two duplicate umbrella-feature issues need triage.

## Other projects' agents running at shutdown (not led from this session)

| tmux window | State |
|---|---|
| `roguetech:agent-ui-command-staff-ui-redesign-battle-hud-and-roster-screens` | human-testing. Waiting for the owner's in-game check, docs approval, and a pick between bright and dim damage numbers. Listed 7 unconfirmed bugs (weapon quality prices, trader Marta never unlocks, galaxy-map launch timers, mechanic discount not applied, facility effects display-only, salaries skipped silently, paint resets to Woodland). |
| `wipe-manager:agent-map-the-siege-goes-upward-the-camp-at-the-bottom-the-keep-at-the-top` | The owner asked it to "save this agent work" (queued). Its background "Rebuild lanes-3 to the flank layout" sub-agent was stopped. Caravan balance ideas are in `sim/timed-side-quests-on-the-lanes-map-…`. |
| `wipe-manager:agent-tooling-the-tuners-workbench-tools-to-watch-stage-measure-and-tune-the-ai-and-the-balance` | Idle after a Football Manager comparison (hidden numbers, visible levers). Nothing pending. |
| `wipe-manager:agent-tooling-hundreds-of-ai-scenarios-templates-fuzzing-mirror-and-handicap-record-from-a-match` | Posted 6 numbered questions. The owner replied "i marked them in the docket, read the verdicts" (queued). |
