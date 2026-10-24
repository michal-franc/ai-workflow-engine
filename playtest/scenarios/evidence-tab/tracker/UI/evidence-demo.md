---
title: "Evidence demo"
status: "done"
system: "UI"
priority: "medium"
created: "2026-10-01"
---

The badge shows the count and glows when it changes.

## Acceptance Criteria
- [x] AC1 The badge shows the count
- [x] AC2 The count matches the list
- [x] AC3 The badge glows when the count changes
- [x] AC4 The badge renders in under 16 ms

## Decisions

- **D-1** · 2026-10-01 · agent chat: The badge glows for one second, "one second is enough"

<!-- issue-viewer-comments
{"id": 1, "block": 0, "date": "2026-10-01T16:30:00+00:00", "text": "tests: make validate green at the change commit", "status": "open", "source": "cli"}
{"id": 2, "block": 0, "date": "2026-10-01T17:40:00+00:00", "text": "playtest: EV9 pass, owner in chat: \"Reads well at a glance.\"", "status": "open", "source": "cli"}
{"id": 3, "block": 0, "date": "2026-10-01T17:50:00+00:00", "text": "risk: the glow is tuned for the dark theme only", "status": "open", "source": "cli"}
-->
