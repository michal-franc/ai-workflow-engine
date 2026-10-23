---
title: "Checkout v2"
status: "in progress"
system: "Backend"
version: "1.0"
priority: "critical"
type: "epic"
created: "2026-09-18"
---

Rebuild checkout around the refactored payment service: retries that never double-charge, p99 under 400 ms, and steps that can resume.

## Children

- Payment service refactor (review)
- Idempotency keys for payment retries
- p99 checkout latency regression
- Async checkout steps (1.1)
