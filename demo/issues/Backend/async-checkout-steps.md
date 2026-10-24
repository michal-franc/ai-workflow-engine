---
title: "Async checkout steps"
status: "backlog"
system: "Backend"
version: "1.1"
priority: "medium"
parent: "backend/checkout-v2"
created: "2026-10-22"
---

Split checkout into resumable steps (cart, address, payment, confirmation) so a dropped connection resumes where it stopped.

## Design

Each step is a row in `checkout_steps` keyed by checkout id, written before the step's side effect runs. The client polls `GET /checkouts/{id}` and the server resumes from the newest unfinished step. Payment reuses its idempotency key, so a resumed payment step can never charge twice.

- [x] Step table and state machine agreed
- [x] Resume rules for each step written down
- [x] Failure cases: card declined, address invalid, timeout mid-payment

## Implementation

- [ ] `checkout_steps` table and migration
- [ ] Step runner with resume on read
- [ ] `GET /checkouts/{id}` returns the current step
- [ ] Web client resumes on reload

## Open questions

- Keep abandoned checkouts for 7 days or 30? Default 7 until support asks.
