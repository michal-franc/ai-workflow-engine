---
title: "Idempotency keys for payment retries"
status: "human-testing"
system: "Backend"
version: "1.0"
priority: "critical"
parent: "backend/checkout-v2"
assignee: "agent-idempotency-keys"
created: "2026-10-07"
---

A client retry after a timeout must never charge twice. Accept an `Idempotency-Key` header on payment endpoints and replay the stored response for a repeated key.

## Implementation

- [x] `Idempotency-Key` accepted on `POST /payments` and `POST /refunds`
- [x] Response stored for 24 h, keyed by account and key
- [x] Same key with a different body returns `422`
- [x] Concurrent requests with one key: the second waits for the first

## Testing

- [x] Unit tests for the key store and body fingerprint
- [x] Integration test: 50 parallel retries, one charge
- [x] `tests:` comment posted with the run

## Test Plan

### Automated

- `go test ./payments/...` covers the store, the fingerprint and the race.

### Manual

- [ ] In staging, pay with a test card, kill the connection before the response, retry with the same key: one charge in the dashboard, same receipt id twice.
