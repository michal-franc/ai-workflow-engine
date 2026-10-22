---
title: "Roadmap"
order: 90
---

# Roadmap

Set 2026-10-22 at v0.50.0, after a cleanup that triaged every open issue against the code. The epics on the board are the source of truth. This page explains their order.

## Goals

1. **A safe, correct core.** Nothing on the network and no other web page can drive the server. Writes never corrupt an issue, and pages stay fast on a large project.
2. **Others can adopt it.** Install, then `init`, then a board that reaches its first approval in minutes. "Claude, set this up" works.
3. **The harness checks the work.** Every gate measures what it says, and checks run without trusting the agent.
4. **A lean surface.** One lead per project and one landing page. Nothing is left over from removed features (sub-leads, inbox, designer, ask/answer, `--wait`). CI runs on every push.

## Epics

| Epic | Goal | Size | Notes |
|:--|:--|:--|:--|
| `api/server-hardening-safe-correct-and-fast-core` | 1 | 10 children, mostly S | Starts with localhost bind + cross-origin rejection (critical) and the frontmatter `$` / newline fix |
| `cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use` | 2 | 5 children + 5 CLI papercuts | `doctor` tier 1 is the dependency several other epics wait on |
| `workflow/gate-correctness-checks-an-agent-cannot-satisfy-by-accident` | 3 | 11 small bugs | Fills gaps between bigger work; includes the bootstrap-mode wrong-workflow bug |
| `workflow/harness-checks-the-work-ideas-from-rigone` | 3 | 5 children | Preflight and the health section wait on `doctor --json` |
| `ui/lean-surface-one-lead-one-landing-page-no-leftovers` | 4 | 5 children | /status becomes `/`; Classic board and Graph are retired; worktree prune |
| `workflow/code-health-ci-consolidated-helpers-modern-go` | 4 | 8 children | Background work: CI first, then one refactor at a time |

## Phases

| Phase | Work | Exit |
|:--|:--|:--|
| 1 | Localhost bind + origin check, frontmatter fix, bootstrap-mode bug, sub-lead removal. Finish one-terminal tmux mode | No network exposure, no frontmatter corruption, issue-cli always loads the right workflow |
| 2 | Adoption setup slices (salvage `origin/claude/project-adoption-ideas-gtw1bo`) and `doctor` tier 1, in parallel with the hardening cache/perf work, graceful shutdown and CI | `init` gives a working board; /status under 500 ms on a large project; CI green on push |
| 3 | RigOne check-results and process safety (prompt off argv), gate-correctness bugs, retiring the legacy views, worktree prune | Gates trustworthy; /status is the landing page |
| 4 | Dispatch preflight and the Status health section, agent-led onboarding, first run, one binary | Candidate for 1.0 |

Small gate-correctness and code-health children can be picked up in any phase when an agent is free.

## Later

- `ui/decision-ledger-epic-decision-records-filtered-jit-prompts-audit-gates-and-challenges`: rewrite without questions and the inbox before starting
- `workflow/feature-store-epic-installable-workflow-features-catalog-and-project-scan`, phase 2
- `workflow/types-of-work-slice-2-per-type-stats-init-template-with-types-an-epic-type`
- `workflow/lessons-from-retros-kept-only-when-the-human-keeps-them`, `workflow/workflow-benchmarks-measure-whether-retros-and-workflow-changes-actually-improve-agent-work`
- `add-move-file-action-to-workflow-transitions` (3.md), `ui/show-docs-from-an-issues-worktree-branch-in-the-viewer`
- Jobs and bots (`enable-executing-jobs-bots-skills-on-the-project`, `ui/ability-to-define-jobs-to-run-with-agent-skills-prompts-that-we-can-run-from-the-web`), the event hub, the capture-bug button: each needs a rewrite into something concrete

## Decisions behind this plan (2026-10-22, lead chat)

- The cleanup ran in full: delivered, obsolete and duplicate ideas were parked with a reason (`Delivered:`, `Won't do`, `Merged into`), because the workflow has no idea → done edge (`workflow/close-an-issue-as-delivered-by-another-issue`).
- Worktrees of merged branches may be pruned (`cli/prune-worktrees-of-merged-branches#D-1`).
- The dispatch prompt moves off argv to a file or stdin (`cli/agent-process-safety-prompt-off-the-command-line-clean-stop-env-allowlist#D-1`).
- /status becomes the landing page, and Classic board and Graph are retired (`ui/workbench-retire-old-list-landing-and-legacy-views#D-1`).
