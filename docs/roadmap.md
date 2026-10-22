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
| `api/server-hardening-safe-correct-and-fast-core` | 1 | 10 children, mostly S (0.52–0.53) | Starts with localhost bind + cross-origin rejection (critical) and the frontmatter `$` / newline fix |
| `cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use` | 2 | 5 children + 5 CLI papercuts | `doctor` tier 1 is the dependency several other epics wait on |
| `workflow/gate-correctness-checks-an-agent-cannot-satisfy-by-accident` | 3 | 11 small bugs | Fills gaps between bigger work; includes the bootstrap-mode wrong-workflow bug |
| `workflow/harness-checks-the-work-ideas-from-rigone` | 3 | 5 children | Preflight and the health section wait on `doctor --json` |
| `ui/lean-surface-one-lead-one-landing-page-no-leftovers` | 4 | 5 children | /status becomes `/`; Classic board and Graph are retired; worktree prune |
| `workflow/code-health-ci-consolidated-helpers-modern-go` | 4 | 8 children | Background work: CI first, then one refactor at a time |

## Versions

Each version is one milestone with one focus. Issues carry it as `version:` (the issues canvas shows a column per version; `issue-cli next --version 0.52`). A version is released once, when its last issue ships. See [Releases](#releases).

| Version | Focus | Issues | Released when |
|:--|:--|:--|:--|
| **0.51** One terminal | Run the lead and every worker from one tmux, no extra terminal windows | one-terminal tmux mode (in flight; released on its own as soon as it ships) | The owner's sandbox walk passes |
| **0.52** Safe core | Nothing outside can drive the server; issue-cli always loads the right workflow; one-lead model only | localhost bind + origin check, frontmatter `$`/newline fix, bootstrap-mode wrong workflow, remove sub-lead leftovers, CI runs `make validate`, graceful shutdown | No network exposure, CI green on every push |
| **0.53** Fast, robust server | Big projects stay fast; failures are visible, not lost in stderr | LoadIssues cache, slow /status and /workboard, data/delete locks, writeJSON helpers, GitHub + timeline error paths, server failures on /status, GitHub token path in config, consolidated frontmatter helpers, sentinel `%w` sweep, repo hygiene | /status under 500 ms on roguetech |
| **0.54** Setup in minutes | A new user goes from install to a working board without hand-editing config | easier setup (salvage `origin/claude/project-adoption-ideas-gtw1bo`), `doctor` tier 1, `--help` never needs a project, `create --body/--meta`, `--body-file` everywhere, issue-cli inside a worktree, uncheck, repair commands, worktree prune | `install → init → board` works on a clean machine |
| **0.55** Gates you can trust | Every gate measures what it says; checks run without trusting the agent | the gate-correctness children, RigOne check-results, process safety (prompt off argv), guidance dedupe (22) | No gate passes on stale comments/boxes; "nothing ran" fails |
| **0.56** One landing page | One shape for the product | /status at `/`, Classic board + Graph retired, Go 1.22 routing, report-bug creates issues, modernized tracker | No legacy views or removed-feature leftovers |
| **0.57** Preflight, health, onboarding | Problems show up before an agent starts, and Claude can set a project up | dispatch preflight, Status health section, agent-led onboarding, first run in minutes | A fresh project reaches its first approval in minutes |
| **1.0** Stable | One binary, pinned contracts | one name one binary, pinned CLI JSON output; closes the code-health epic | CLI, `workflow.yaml` and JSON contracts frozen for 1.x |

Small issues from a later version can be picked up early when an agent is free. Their release then simply comes sooner.

## Releases

Until 2026-10-22 every push to main cut a release (v0.34 → v0.50 in two days). Now:

- Agents add their change under `## Unreleased` at the top of `cmd/issue-cli/CHANGELOG.md` and never bump the version.
- Agents never push. Shipping merges local `main` into the branch, runs `make validate`, and fast-forwards the main checkout (`git merge --ff-only`). Local main is the integration branch; origin/main moves only when the lead pushes.
- The lead pushes main to origin when the owner asks. The release-marker Action releases nothing while `## Unreleased` is the top heading.
- When a version's last issue ships, the lead renames `## Unreleased` to `## v0.N.0 — YYYY-MM-DD`, adds a one-line focus summary, and pushes (with the owner's go-ahead). The Action tags it, writes the GitHub Release and builds the binaries.
- An urgent fix can be released alone as a patch (`v0.N.1`) the same way.

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
- Releases are cut per version (milestone), not on every push to main (owner, lead chat: "we also need to change our ci to not build new release on each change").
- Agents merge into local main instead of pushing to origin; the lead pushes when asked (owner: "so shipping should change to push to local merge … then you as a lead will be asked sometimes to push to master").
