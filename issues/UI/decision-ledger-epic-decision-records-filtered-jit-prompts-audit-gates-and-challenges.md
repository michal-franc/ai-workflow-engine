---
title: "Decision ledger (epic): decision records, filtered JIT prompts, audit gates and challenges"
status: "idea"
system: "UI"
priority: "high"
depends_on: "ui/workbench-decisions-index-generated-from-decisions"
type: "epic"
ideas: "https://claude.ai/artifact/1huMqWapzpffvve2nFPPnB"
version: "0.59"

---
The owner, in the lead chat:

- 2026-10-07: "we will probably want to make a way for agents to inspect decisions, for this we will need to build some system with filters etc. ok i like the idea 1 - 2 - 3 - 4 - 5, lets create issue for it and dispatch it later when we have decision work done"
- 2026-10-08: "for decisions i think we will expand on this idea https://claude.ai/artifact/1huMqWapzpffvve2nFPPnB this is artifact i like 1 2 3 4 6 (but we would need to create good filtering) 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21"

This is an **umbrella (epic)**. Split it into child issues during design, one per phase or capability, the same way the workbench umbrella was split.

**Do not dispatch** until `ui/workbench-decisions-index-generated-from-decisions` has shipped. That issue built `## Decisions` lines with `<slug>#D-n` refs, `issue-cli decide`, `issue-cli decisions`, and the active / deferred / replaced / stale states. This epic starts from that code.

## Source material

- Ideas artifact "A decision ledger for the workflow engine" (21 ideas, a flow diagram and a 3-phase rollout): https://claude.ai/artifact/1huMqWapzpffvve2nFPPnB (private). In design, save a copy to `issues/attachments/decision-ledger/ideas.html`.
- Prelint (https://prelint.com/), reviewed on 2026-10-07: a hosted PR reviewer built on a "decision ledger". Its statements carry certainty, source, and whether a human or an agent decided them. Contradictions are resolved as kept-existing / replaced / coexist. Agents query the ledger over MCP before they write code. We take its ideas, not its architecture.

## Why

Agents make product decisions while they build. Today those decisions live in `## Decisions` lines (lead-session convention), `question:` answers and chat. Agents can't look up which decisions bind their issue, nothing checks the code against them, and a direction change gets swept by hand (rust-manager: one pivot made 17–22 issues obsolete). The goal: decisions are first-class, scoped, cited records. The right ones reach the agent at the right step, conflicts block a transition until the operator resolves them, and every decision keeps a trail of who made it, who cited it and who overrode it.

## Ideas selected (numbers from the artifact)

**A · Capture**
- **1. Decision records as markdown files.** A `decisions/` folder next to `issues/`, with frontmatter: id, title, status (proposed / accepted / superseded / retired), scope (systems, types, path globs), reversibility (easy / moderate / hard), source (human / agent), decided_by, decided_in, supersedes. Rendered at `/p/<proj>/decisions`.
- **2. `issue-cli decision` family:** `add | list | show | cite | supersede`. An agent can only create `proposed` decisions with `source: agent`, and `accepted` needs a human. `decision list --for <slug>` returns the decisions in scope for an issue. JSON output follows the CLI contracts, and the command goes through `parseFlags` for telemetry.
- **3. Harvest `## Decisions` on done.** A status side-effect, `harvest_decisions`, turns each `## Decisions` line into a ledger record linked back to its issue.
- **4. Answered `question:` comments become decisions.** The operator's answer is stored verbatim as the rationale, and `decided_in` points at the comment or question.

**B · Just-in-time prompts**
- **6. A "Decisions in force" block** on `start` and `transition` (`EntryPrompts`), filtered to the issue. The owner's condition: **this needs good filtering.** See "Filtering" below.
- **7. A citation step in `in design`.** Leaving design requires a `## Decisions cited` section (`has_section` validator, with a hint to run `decision list --for`).
- **8. Business rules as acceptance criteria.** `inject_prompt` on backlog → in progress tells the agent to restate each in-scope rule as a checkbox tagged with its D-id. `section_checkboxes_checked` then enforces them.
- **9. Stop-and-ask on hard-to-reverse decisions.** If a change touches behaviour covered by a `reversibility: hard` decision, the agent posts a challenge and stops.

**C · issue-cli flow and gates**
- **10. Three validators:** `cites_decisions`, `no_open_challenges` and `decisions_ratified`.
- **11. `issue-cli decision check`:** a diff audit of `work/<slug>` against base, using the decisions whose path scope overlaps the diff. Findings become data-sidecar rows (open / upheld / overridden / superseded, blocking / advisory).
- **12. The audit as a gate** on in progress → testing: blocking rows that are still open block the transition, and advisory rows appear under `Will:`.
- **13. `issue-cli decision challenge`:** a structured `challenge:` that cites the D-id with a reason and evidence. `--wait` blocks like `transition --wait` (exit 3 on timeout).
- **14. Approval for hard-to-reverse changes.** A project-level `reversibility:` map of path globs. When the diff touches a glob, `require_human_approval` is added on the fly.
- **15. Decisions in `--dry-run`, `show` and `context`,** plus `process decisions`.

**D · Audit trail**
- **16. An append-only event log** (`decisions/.log.jsonl`) and a decisions panel on `/stats`: citations, challenge rate, override rate, time to ratify.
- **17. Backlinks both ways.** The issue sidebar lists the decisions it cited, proposed and challenged. Each decision page lists the issues, PRs and overrides that touched it, plus its supersede chain.
- **18. Drift sweep** as a `project_action` (one-click and schedulable). It files a `drift` issue for each conflict between merged work and the ledger.

**E · Operator**
- **19. A challenge inbox with three resolutions:** Uphold, Override once, Supersede. It unblocks `--wait` and nudges the agent over tmux. Builds on the workbench inbox.
- **20. Decision interview and weekly ratification.** Hard decisions are asked about straight away. Easy decisions that agents propose are collected into a weekly batch to ratify.

**F · Tests**
- **21. Table-driven unit tests tagged with decision IDs** (`// decision: D-0042`). This adds:
  - `enforce: unit_test` in the decision record
  - a `command_succeeds` gate on `TestDecision_`
  - `decision lint`
  - protection for tagged test files: agents may add rows, never remove or flip them

**Not selected:** **5** (a `rules:` list with `check:` commands in the decision record). Testable rules go through idea 21 instead.

**Carried over from the 2026-10-07 list (approved):**
- **Expiry and reconfirmation:** an optional `review_by` date. After it, the decision shows as expired until a human reconfirms it.
- **"Coexist" as a fourth resolution** next to 19's three. Two decisions both stand, and a note records why.

## Filtering (the owner's condition for idea 6)

The "in force" block is only useful if it is short and correct. Design this before building 6, 7 and 10:
- **Scope matching:** a decision matches on system, type, labels, path globs against the touched files (the branch diff once work has started, the design's named files before that), and explicit citations. Define how unscoped decisions behave. They should probably never be injected, only listed on demand.
- **Ranking and cap:** hard before moderate before easy, cited before merely in scope, newer before older. At most N lines (configurable), with `+K more: issue-cli decision list --for <slug>`.
- **One shared filter set:** the CLI, the JIT block and the Decisions tab use the same filters: `--system`, `--type`, `--label`, `--path`, `--status`, `--reversibility`, `--source human|agent`, `--decided-by`, `--since`, `--cites` / `--cited-by`, `--search <text>`, `--for <slug>` and `--json`.
- **Token cost:** the block's tokens count toward `static_tokens` in the stats sidecar, so its cost shows on `/stats`.

## Key design question: reconcile with the decisions index

The decisions index (built) keeps decisions as lines inside issues, with `issue-cli decide` and `issue-cli decisions`. Ideas 1 and 2 make `decisions/*.md` files the records. Pick one model before splitting:
- **Lead's recommendation:**
  - `decisions/*.md` become the source of truth: they are still markdown in the repo, not a second store outside it.
  - `## Decisions` lines and inbox answers remain the capture surface, and harvesting turns them into records (3, 4).
  - `decide` and `decisions` fold into the `decision` family, with the old names kept as aliases.
  - Existing `<slug>#D-n` refs map to the new IDs.
- Keep within the rule from the rust-manager field report: everything an agent writes goes through issue-cli, and there is no store outside the repo.

## Rollout (from the artifact, without idea 5)

1. **Config only:** 7, 8, 9, 3, 21 for the top rules.
2. **Ledger and gates:** 1, 2, 6 (with filtering), 10, 13, 19, 15.
3. **Audit:** 11–12, 14, 16–18, 20, plus expiry.

The bundled-template parts (citation step, prompts, validators) also ship as `issue-cli workflow upgrade` proposals (see `workflow/versioned-workflow-yaml-templates-and-an-upgrade-command-for-agents`).

## Related

- `ui/workbench-decisions-index-generated-from-decisions`: the base, which must ship first.
- `ui/workbench-redesign-inbox-system-pages-balance-board-and-decisions-index`: the parent umbrella. Its inbox hosts the challenge cards (19).
- `ui/workbench-inbox-balance-calls-stale-alerts-and-agents-sidebar`: stale, expired and proposed decisions as inbox alerts.
- `workflow/record-a-human-approval-given-in-chat`: chat decisions use the same source and decided_by format.
- `workflow/versioned-workflow-yaml-templates-and-an-upgrade-command-for-agents`: delivery of the template changes.

## Decisions

- **D-1** · 2026-10-08 · lead chat: Build the decision ledger from the ideas artifact, taking ideas 1-4 and 6-21 (not 5); idea 6 needs good filtering, "i like 1 2 3 4 6 (but we would need to create good filtering) 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21"
- **D-2** · 2026-10-21 · lead chat: Remove issue-cli ask and answer (structured Q-n questions under ## Questions). Questions stay in Claude Code / Codex chat; only the outcome is recorded, with issue-cli decide. The ledger builds on decide and the D-n lines alone, "lets remove issue-cli ask we dont need this feature lets jus rely on claude codex we jsut want decision to be recorded"
- **D-3** · 2026-10-23 · lead chat: The ledger is milestone 0.60, after onboarding and before the 1.0 freeze; its design (records model, who decided, proposed vs accepted) and decide --amend go first in 0.57, "Yeah 0.60"
- **D-4** · 2026-10-23 · lead chat: Setup and onboarding merge into one milestone, 0.56; the ledger moves from 0.60 to 0.59, still after onboarding and before the 1.0 freeze, "We should merge setup is easy with onboarding" (replaces ui/decision-ledger-epic-decision-records-filtered-jit-prompts-audit-gates-and-challenges#D-3)

## Roadmap 2026-10-22

Detached from the workbench umbrella (it is its own epic now) and scheduled **Later**. Before it starts, rewrite it without ideas that rest on removed features: #4 (`question:` answers; ask/answer were removed in v0.40) and #19 (challenge inbox; the inbox was removed). Challenges go through the lead plus `issue-cli decide --replaces`.

## Roadmap 2026-10-23

Scheduled as its own milestone, **0.59 Decision ledger** (0.60 until setup and onboarding merged into 0.56 the same day): after 0.58 and before 1.0, because it changes the CLI, `workflow.yaml` and JSON contracts that 1.0 freezes. Until then `decide` stays an experiment that captures decisions.

A first slice goes in **0.57 Gates you can trust**:
- `workflow/decision-ledger-design-one-records-model-who-decided-agents-propose-and-the-owner-accepts`: the records model, who decided, proposed vs accepted, and the rewrite and split of this epic
- `cli/decide-amend-correct-how-a-recorded-decision-was-made-with-an-audit-line`: amend a recorded decision with an audit line (RogueTech D1 retro)



<!-- issue-viewer-comments
{"id":1,"block":0,"date":"2026-10-23T19:46:37+02:00","text":"Input from RogueTech's D1 retro (relayed by lead-roguetech, 2026-10-23), child cli/decide-amend-correct-how-a-recorded-decision-was-made-with-an-audit-line: a recorded decision can't be corrected. An agent labelled the lead's picks as 'owner chat' because its question answers don't say who answered, then added D-5..D-8 to replace D-1..D-4. The ledger needs (1) amend with an audit trail and (2) a reliable who-decided (owner, lead or agent). Owner: decisions are an experiment for now; the ledger will improve this.","status":"open","source":"cli"}
-->