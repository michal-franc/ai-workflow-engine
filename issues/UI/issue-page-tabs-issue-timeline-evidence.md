---
title: "Issue page tabs: Issue, Timeline, Evidence"
status: "idea"
system: "UI"
priority: "high"
type: "epic"
version: "0.54"
created: "2026-10-22"
mockups: "https://claude.ai/artifact/RKBJWJJAbGqEyQLUbvwEe1"
analysis: "https://claude.ai/artifact/PcYZzYRactTqGtC9xLLugw"
children:
  - ui/timeline-tab-replaces-the-agent-timeline
  - cli/clilog-and-stats-record-what-the-timeline-needs
  - ui/approve-button-takes-your-words
  - ui/issue-tab-brief-needs-you-bar-and-a-folded-body
  - workflow/issue-cli-prove-link-each-criterion-to-its-proof
  - ui/evidence-tab-proof-shots-and-criteria
---

The owner opens an issue when an agent asks for an approval or a playtest, and rarely reads it. Today the page is
the raw markdown plus a sidebar, with the Agent Timeline (issue-cli calls only) under the body. Owner, 2026-10-22:
"i am not reading issues that often so we need to figure out how to make them useful again".

The page becomes three tabs:

1. **Issue** — the Needs-you bar, a one-line brief, what you're approving since the last gate, decisions in your
   words, and the body folded. Mockup board A, minus "From the agent's retro" and "Since your last visit" (dropped).
2. **Timeline** — where the time went (agent working vs waiting on you, per status), the issue's life by status with
   commits, decisions, comments, tests and CI inline. It replaces today's Agent Timeline. Mockup board B.
3. **Evidence** — screenshots tagged with what they prove, the verdict, criteria → proof, change summary, playtest
   verdict, stale body text, what to watch. Mockup board C.

The phone view (board D) is the Issue tab at 390 px.

## What the data analysis found

`analysis` checks every element of the three tabs against the code at v0.52.0. 24 of 33 elements work from data
recorded today: the clilog (every issue-cli call with full args), telemetry (exit code, error class), the stats
sidecar, comments, decisions, git and playtest `results.yaml`. The rest:

| Bucket | Elements | Child |
|:--|:--|:--|
| issue-cli records more, no new ask | gate refusals, CI after merge | `cli/clilog-and-stats-record-what-the-timeline-needs` |
| Viewer change | your words at an approval | `ui/approve-button-takes-your-words` |
| New agent ask | criterion → proof, proof shots, image tags, watch list | `workflow/issue-cli-prove-link-each-criterion-to-its-proof` |

## Children

| Child | Size | What it covers |
|:--|:--|:--|
| `ui/timeline-tab-replaces-the-agent-timeline` | M | The tab bar, and the Timeline tab from today's data, with a "CLI calls" filter so nothing in the old list is lost |
| `cli/clilog-and-stats-record-what-the-timeline-needs` | S | Exit code in the clilog, `waiting_since`, branch head and merge sha, comment timestamps, `dispatched_at` |
| `ui/approve-button-takes-your-words` | S | Optional words on the viewer's approve button, saved like `issue-cli approve --quote` |
| `ui/issue-tab-brief-needs-you-bar-and-a-folded-body` | M | Needs-you bar, brief, what you're approving, decisions, folded body, agent card, relations; phone layout |
| `workflow/issue-cli-prove-link-each-criterion-to-its-proof` | M | `issue-cli prove`, criteria as checkboxes with ids, proof shots, `risk:` comments; prompt changes |
| `ui/evidence-tab-proof-shots-and-criteria` | M | The Evidence tab on top of `prove` and `results.yaml` |

## Overlap with 0.54 Project health

The roadmap's 0.54 milestone lists "workflow timeline (working, waiting on owner, idle)" under
`workflow/project-health-trends-that-tell-the-owner-when-the-project-gets-slower`. The Timeline tab is the per-issue
view of the same data. Build the recording once (`cli/clilog-and-stats-…`) and let Project health aggregate it.

Dropped on 2026-10-22: an agent heartbeat (the raw log is being removed; the agent-vs-you split comes from gates and
approvals, and gaps of 15 min or more with no CLI call show as idle) and tokens by step.

0.54's rule is "agents don't self-report". Everything here follows it except
`workflow/issue-cli-prove-link-each-criterion-to-its-proof`, which asks agents to link proof. It may fit 0.57
(Gates you can trust) better; the owner decides.

## Order

1. Timeline tab from today's data, and the clilog/stats recording. They ship together; the old Agent Timeline goes.
2. Approve button words.
3. Issue tab.
4. `prove`, then the Evidence tab.
