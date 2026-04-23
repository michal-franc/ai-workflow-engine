---
title: "CTA demo — optional approval hidden behind button"
status: "in progress"
system: "Frontend"
priority: "medium"
assignee: "demo-agent"
created: "2026-04-22"
---

Demo ticket for eyeballing the CTA-for-optional-approval feature.

Open this issue in the viewer. The sidebar "Human Approval" section should render a single CTA button:

  `Divert to waiting-for-team-input — Parked — blocked on another team (optional, skip if not blocked)`

No inline pending approval checkbox — the CTA is the default state because the target status (`waiting-for-team-input`) is `optional: true` in `demo/workflow.yaml`.

Click the CTA to reveal the approval checkbox and a Cancel link. Click the checkbox to persist the approval; reload the page and the widget should stay revealed with the check visible.
