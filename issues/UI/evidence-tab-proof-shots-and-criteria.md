---
title: "Evidence tab: proof shots and criteria"
status: "idea"
system: "UI"
priority: "medium"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
mockups: "https://claude.ai/artifact/RKBJWJJAbGqEyQLUbvwEe1"
depends_on:
  - workflow/issue-cli-prove-link-each-criterion-to-its-proof
---

The third tab answers "is it proven?". Mockup board C. The tab label carries the count ("8 / 8 proven", amber when
short).

| Part | Source |
|:--|:--|
| Screenshot hero and gallery, each tagged with what it proves | body images, `results.yaml` screenshots, `prove --by shot:` |
| Verdict: shipped, merge, suite count, time, wait | status, stats, merge sha, `tests:` comment |
| Criteria → proof matrix, unproven rows amber | `prove` records; text match as a fallback |
| Change summary: commits by area, files, +/− | git on `work/<slug>` or the recorded merge sha |
| Playtest verdict per step, yours and the machine's | `results.yaml` per step; `approval:` / `playtest:` comments |
| Stale text in the body | `Open …` / `pending` lines answered by a D-n |
| Watch | `risk:` comments |

Decisions, the path with checkpoints, and the folded body move to the Issue and Timeline tabs. Images and the
playtest verdict work from today's data and can ship before `prove`.
