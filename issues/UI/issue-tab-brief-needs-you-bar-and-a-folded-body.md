---
title: "Issue tab: brief, needs-you bar and a folded body"
status: "idea"
system: "UI"
priority: "medium"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
mockups: "https://claude.ai/artifact/RKBJWJJAbGqEyQLUbvwEe1"
---

The first tab answers in 10 seconds: what is this, where is it, what does it need from me. Mockup board A (without
"From the agent's retro" and "Since your last visit", both dropped) and board D for the phone.

| Part | Source today |
|:--|:--|
| Needs-you bar: the action, its command, the buttons, wait time | `ownerAttention`, `waitingOnOwner`, `playtestsToRun`, open `question:` comments (same wording as Status) |
| One-line brief and your original words | `## Idea` "In short", `## Bug` first line, `## Why` owner quote |
| Path stepper with your gates | `workflow.yaml` type path, stats transitions |
| What you're approving, since the last gate | clilog `append` / `check` / `decide` / `comment` between two transitions; git log |
| Decisions led by your quote; "took the recommendation" mark | `## Decisions` D-n quote, `(Recommended)` in the quote |
| Body folded: done gates as one line, long sections to their lead | sections, box ids, gate names from `workflow.yaml` |
| Side: agent card, acceptance criteria, relations | `workerState`, `lastCliCall`, checkboxes, `depends_on`, `parent`, `#slug#D-n` |

All parts work from data recorded today. At 390 px the side column stacks under the main one.
