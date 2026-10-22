---
title: "Signed webhook payloads"
status: "testing"
system: "Backend"
version: "1.0"
priority: "high"
parent: "backend/api-hardening"
assignee: "agent-signed-webhooks"
created: "2026-10-09"
---

Sign every outgoing webhook with HMAC-SHA256 so receivers can verify it came from us.

## Implementation

- [x] `X-Signature` header with timestamp and HMAC-SHA256 of the body
- [x] Per-endpoint secrets, rotatable without downtime
- [x] Reject replays older than five minutes on the verifier helper
