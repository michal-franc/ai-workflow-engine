---
title: "decide --amend: correct how a recorded decision was made, with an audit line"
status: "idea"
system: "CLI"
priority: "low"
type: "tweak"
version: "0.57"
created: "2026-10-23"
parent: "ui/decision-ledger-epic-decision-records-filtered-jit-prompts-audit-gates-and-challenges"

---
## Why

From RogueTech's D1 retro (relayed by `lead-roguetech`, 2026-10-23). D1's agent recorded four of the lead's picks as `--how "owner chat"`. Its AskUserQuestion answers don't say who answered, so it guessed. `decide` can't correct a recorded decision. The only fix was D-5..D-8 replacing D-1..D-4, so the wrong labels stay visible and the Decisions section reads as eight decisions where there were four.

## Idea

1. **`issue-cli decide --amend D-n [--how …] [--quote …]`** rewrites that decision's how/quote in place and appends an audit line under it, e.g. `amended 2026-10-23 by lead-roguetech: how "owner chat" → "lead chat"`. The decision text itself stays (a changed decision is a new D-n). It refuses an unknown D-n and amends nothing without `--how` or `--quote`.
2. **Agent guidance:** the dispatch prompt / `decide` help says answers to an agent's question prompt may come from the lead, not the owner. If the answer doesn't say who gave it, ask before recording `--how`.

## Acceptance criteria

- [ ] `decide --amend D-n --how "lead chat"` changes that decision's how, keeps its text and quote, and adds a dated audit line naming who amended it
- [ ] `--quote` amends the quote the same way; unknown D-n and no fields are usage errors (exit 2)
- [ ] The viewer's decisions index and the issue page show the amended values and the audit line
- [ ] The dispatch prompt (bundled template and issue-viewer's workflow.yaml) tells agents that question answers may come from the lead and to ask who answered when unclear

## Decisions

- **D-1** · 2026-10-23 · lead chat: Not a standalone fix: amending a decision and recording who answered are inputs to the decision ledger epic and get built there; today's decide stays an experiment, "Decisions are a experiment for now we capture them but decision ledger will improve this solution"

