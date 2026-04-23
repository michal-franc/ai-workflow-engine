---
title: "Review payment service refactor"
status: "in progress"
pr: "https://github.com/acme/payment-service/pull/312"
pr_author: "jsmith"
jira: "https://jira.acme.com/browse/PAY-789"
team: "Payments"
risk: "medium"
type: "Refactor"
tech_lead: "Jane Doe"
due: "2025-05-01"
participants:

  - jsmith
  - alice
  - bob
created: "2025-04-18"
---

Refactor of the payment service to consolidate charge and refund paths into a single transaction pipeline.

## Checklist

- [x] Read the PR description
- [ ] Review core transaction logic
- [ ] Check error handling paths
- [ ] Verify rollback behavior
