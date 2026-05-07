---
title: "Demo: data-table tier as a structured second axis"
status: "idea"
---
## Summary

This issue exists to **visualize** the new tier (second-axis) field on data entries. Status (`💬 commented`, `✅ addressed`, …) tracks review state; tier (`🔴 critical`, `🟢 nice`) tracks importance — orthogonal axes.

Compare with the older "[CRITICAL] / [NICE]" prefix-in-description workaround visible on the sibling demo issue: there, the tier was baked into the description string and could not be filtered or sorted.

## Findings

<!-- data statuses=💬 commented,🤝 aligned,✅ addressed,✨ positive (no action) tiers=🔴 critical,🟢 nice -->

## What to look at

- The toolbar above the table has **two** filter dropdowns now — *All statuses* and *All tiers* — and they compose with AND.
- The Tier column is its own dropdown. Changing it is independent of the Status column.
- The empty option (`—`) lets you clear a tier without removing the row.
- Workflows that don't declare `tiers=` on their data marker render the table exactly as before — see `demo-data-table-status-pill-clipping-layout-brittleness.md`.
