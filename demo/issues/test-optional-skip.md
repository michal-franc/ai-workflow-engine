---
title: "Test skipping optional waiting-for-team-input"
status: "in progress"
assignee: "demo-agent"
system: "Backend"
created: "2026-04-21"
---

Scratch issue to verify the new `optional: true` status behaviour in the demo workflow.

`waiting-for-team-input` is marked optional and now sits between `in progress` and `testing`. Two paths should both be valid:

1. Skip path — transition directly `in progress → testing` (the optional status is jumped).
2. Park path — `in progress → waiting-for-team-input → in progress → testing` (explicit transitions declared in workflow.yaml).

## Implementation

- [x] Code changes complete
- [x] Tests written or updated

## Test Plan

### Automated

- [x] Covered by internal/tracker/workflow_test.go in the main project

### Manual

- [ ] From this issue's detail page, transition directly to `testing` (should succeed, skipping `waiting-for-team-input`)
- [ ] Create a second copy and transition through `waiting-for-team-input` the long way to confirm the explicit park transitions still work
- [ ] Confirm `issue-cli process workflow --project demo` shows `waiting-for-team-input (optional)`
