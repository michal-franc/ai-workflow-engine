---
title: "Approve button takes your words"
status: "idea"
system: "UI"
priority: "medium"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
---

`issue-cli approve --quote` saves the owner's words as an `approval:` comment. The viewer's approve button
(`handleApproveIssue`) records only `approved_at`, so the Timeline shows "no words recorded" for most approvals
(local CI: 2 of 3).

Add an optional text box next to the button. When filled, save it exactly as `approve --quote` does
(`approval: "<words>" (viewer, <date>)`). Empty stays as today. The Needs-you bar on the Issue tab uses the same box.
