---
title: "Deterministic scheduling: issue-cli next decides what runs, the lead relays"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
version: "0.57"
parent: "workflow/harness-checks-the-work-ideas-from-lights-on"
source: "https://github.com/gojko/lights-on-software-factory"

---
Lights-On's line: the platform orchestrates, agents don't. Its generator decides fan-out, order, gates and resume
from the plan, deterministically, and the README credits that with about 80% lower token spend. Here the lead, an LLM
agent, decides which issue runs next by reading the board.

## Idea

1. **`depends_on` is respected by dispatch.** Dispatch and `issue-cli next` refuse or skip an issue whose
   `depends_on` issues are not done, and say which one blocks it (RogueTech #40).
2. **`issue-cli next` is the schedule.** It orders ready issues by version, priority/score and dependencies, and
   reports the order and why. The lead dispatches the top of it instead of choosing by inference.
3. **Parallel by rule.** `next --parallel N` returns up to N ready issues that touch no shared hot files
   (`depends_on` plus the Metrics view's hot-file data).
4. **The lead keeps the human parts:** relaying questions, approvals, judgement calls, and overriding the order with
   a recorded decision.

## Notes

- Covers RogueTech #40 "dispatch respects `depends_on`" from the 0.57 roadmap row.
- Measure it: lead tokens per dispatched issue before and after (Stats).
- Source: ORCHESTRATOR.md and README.md in gojko/lights-on-software-factory.
