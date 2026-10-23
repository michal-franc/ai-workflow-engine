---
title: "Lead check-ins: the lead proposes features, type changes and fixes to the owner, from retros and health"
status: "idea"
system: "Workflow"
priority: "high"
type: "feature"
version: "0.56"
parent: "cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use"

---
## Why

Owner, 2026-10-23: "the lead should be periodically asking you as owner to add something, fix or improve, or, based on retros, activate features and suggest changes and improvements to issue types etc."

Today the lead only reacts. Improvements to the workflow itself (new feature-catalog entries, type changes, gates that keep failing, lessons from retros) wait until the owner happens to ask. The engine already has the inputs:
- retros and the review tools
- `workflow upgrade` / the feature catalog
- health scores and recommendations
- decisions, gate-failure and rework stats
- the CI red rate

## What to build

A **lead check-in**: a short, periodic owner conversation the lead starts itself.

- **When:**
  - weekly (configurable, `leads.checkin_every`)
  - after a milestone release
  - when retros or health cross a threshold (e.g. 5 unreviewed retros naming the same friction, a gate failing on most issues, the health score dropping)
  - the viewer nudges the lead like the handoff-note nudge, and the Status page shows "Check-in due"
- **What the lead prepares** (`issue-cli lead checkin` drafts it), at most about 5 items, each with evidence and a one-click answer:
  - **Activate a feature:** from `workflow upgrade` / the feature catalog, e.g. "turn on local CI here", "add the poc type", "add playtest sessions".
  - **Change issue types or gates:** e.g. "tweaks keep failing the design gate; drop it for tweaks", "bugfix needs a repro step".
  - **Lessons from retros:** proposed rules, the same Keep/Discard as `workflow/lessons-from-retros-kept-only-when-the-human-keeps-them`.
  - **Health recommendations:** from the agent-opinion score (remove slow redundant tests, add e2e for a flow, simplify a file).
  - **Open questions:** anything the owner should add, fix or improve next; stale ideas to park.
- **Answers are recorded** with `issue-cli decide`. Accepted items become issues or workflow.yaml changes, applied through the engine with the diff shown. Declined items are remembered, so they aren't asked again without new evidence.
- **Every project:** issue-viewer, RogueTech, gpu-runner.

## Acceptance Criteria

- [ ] `issue-cli lead checkin` drafts up to ~5 evidence-backed proposals from retros, the feature catalog, health and gate stats
- [ ] The lead is nudged on schedule, after releases and on thresholds; the Status page shows when a check-in is due
- [ ] Accepted proposals become issues or applied workflow changes (diff shown); declined ones are remembered; all answers recorded as decisions

## Decisions

- **D-1** · 2026-10-23 · lead chat: Onboarding milestone gains: first lead spawn gives a tour and interviews the owner to configure features and types; leads run periodic owner check-ins proposing features, type changes and fixes from retros and health, "when spawn lead IT SHOULD GIVE YOU A TOUR AND ASK QUESTION WHAT KIND OF PROJECTS YOU WILL BE RUNNING ... lead should be periodically asking you as owner to add something fix improve or based on retro activate features and suggest changes improvement to issue types etc - lets add it to the onboarding milestone"

