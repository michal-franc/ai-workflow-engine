---
title: "Timeline tab replaces the Agent Timeline"
status: "idea"
system: "UI"
priority: "high"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
mockups: "https://claude.ai/artifact/RKBJWJJAbGqEyQLUbvwEe1"
---

Owner, 2026-10-22: "the timeline is perfect it should be our second tab and it should replace our current timeline".
Mockup board B.

The detail page gets a tab bar under the header: **Issue · Timeline · Evidence**. This issue adds the bar and the
Timeline tab. Issue holds today's body and sidebar until `ui/issue-tab-brief-needs-you-bar-and-a-folded-body` lands;
Evidence shows "coming" until `ui/evidence-tab-proof-shots-and-criteria`. The "Agent Timeline" list under the body
(`templates/detail.html`, `agent_timeline.go`) is removed.

## What the tab shows, and where each part comes from

| Part | Source today |
|:--|:--|
| Where the time went: total, agent working, waiting on you, rework loops, commits | stats transitions `ts` and `approved_at`; `progress: … Waiting on` comment time from the clilog; backward transitions; git |
| Life bar cut by status | stats transitions |
| Life by status: events per status band | clilog (`decide`, `comment`, `append`, `check`), git log on `work/<slug>`, `tests:` comments, failed playtest runs |
| Waits as amber rows ("Waiting on you · 45m · design approval") | as above |
| Your approvals, with your words or "no words recorded" | `approval:` comments; stats `approved_at` |
| Repeated reads folded into one line | clilog verbs `show`, `checklist`, `context`, `--dry-run` |
| Filters: All, Your actions, Commits, Tests and CI, **CLI calls** | CLI calls is today's Agent Timeline, with the workflow actions each transition saw |
| Side: your part, your words, left open at done, CI after merge | stats `approved_at`; `approval:` comments; checkbox state; local CI runs once the merge sha is recorded |

Inside an agent band, gaps of 15 minutes or more with no CLI call show as idle (the same rule as `workerState`).
There is no heartbeat and no tokens card. Each tab is a URL (`?tab=timeline`) so links and playtests can open it.

## Acceptance criteria

- [ ] Tab bar on every issue page; Timeline opens at `?tab=timeline`
- [ ] Old Agent Timeline list removed; everything it showed is under the CLI calls filter
- [ ] Time totals match the stats sidecar for a done issue (local CI: 5h 0m, 3h 48m waiting)
- [ ] Works when the clilog or the branch is missing: the tab says what it could not read
- [ ] Playtest session for the tab
