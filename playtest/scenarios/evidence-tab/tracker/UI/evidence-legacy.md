---
title: "Evidence legacy"
status: "done"
system: "UI"
priority: "low"
---

An older issue from before proof links.

## Decisions

- **D-1** · 2026-09-01 · lead chat: Upgrade covers new features such as types of work, "yes, the upgrade covers types"

## Idea

### Open questions

- Where does the version live: a top-level `template:` field or a header?
- Should the upgrade also cover per-system overlays and `types:` paths?
- What format should changelog entries use?

### Clarifications

- **Version:** a top-level `template:` field holds the version; no header, the field lives in the file.

## Acceptance Criteria
- [x] AC1 The template carries its version
- [x] AC2 Upgrade lists missing features
- [x] AC3 Docs describe the stamp
