---
title: "Decision ledger design: one records model, who decided, agents propose and the owner accepts"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
version: "0.57"
created: "2026-10-23"
parent: "ui/decision-ledger-epic-decision-records-filtered-jit-prompts-audit-gates-and-challenges"
---

## Why

The decision ledger (epic, 0.59) can't start until two things are settled, and both affect whether today's gates can be trusted:

- **One records model.** Decisions live as `## Decisions` lines inside issues (`decide`, `decisions`, the Decisions tab), while the epic's ideas 1–2 propose `decisions/*.md` records. Pick one before any ledger code is written.
- **Who decided.** `--how` is free text an agent fills in. RogueTech's D1 agent recorded four of its lead's picks as "owner chat", because answers to an agent's question prompt don't say who answered (see `cli/decide-amend-correct-how-a-recorded-decision-was-made-with-an-audit-line`). A gate or a JIT prompt that cites a decision is only as good as knowing whether the owner, a lead or an agent made it.

## Idea

A design pass, no ledger features:

1. **Records model:** the lead's recommendation in the epic (`decisions/*.md` as the source of truth, `## Decisions` lines as the capture surface harvested into records, `decide`/`decisions` folded into a `decision` family with aliases, `<slug>#D-n` refs mapped) or keep lines. Decide, with the migration of existing D-n lines.
2. **Provenance:** a structured `decided_by` (owner / lead:<name> / agent:<name>) instead of free-text `--how`; how each is established (the viewer and owner-run commands for owner; the lead session; agents only ever `proposed`).
3. **Agents propose, the owner accepts:** an agent's decision is `proposed` until the owner or lead accepts it; what a proposed decision may and may not gate.
4. **Amend:** where the amend issue's audit line lives in the chosen model.
5. **Rewrite the epic** without ideas resting on removed features (#4 `question:` answers, #19 challenge inbox) and split it into 0.59 issues.

## Acceptance criteria

- [ ] Records model chosen and recorded as a decision, with the migration path for existing `## Decisions` lines and `<slug>#D-n` refs
- [ ] Provenance fields and who can set each value specified
- [ ] Proposed vs accepted states specified, including what a proposed decision can gate
- [ ] The ledger epic rewritten (no #4/#19 as written) and split into child issues for 0.59
