---
title: "Demo: data-table status pill clipping & layout brittleness"
status: "idea"
---
## Summary

This issue exists to **visualize** the data-table rendering bug.

The table below has findings spanning short and long status labels, short and very-long descriptions, and URLs of varying length.

## Findings

<!-- data -->

## What to look at

- Status select clips long labels like `positive (no action)` to a few characters.
- Long descriptions push other columns around (no `table-layout: fixed`).
- Comment URLs do not truncate gracefully on narrow viewports.
- Compare with the metadata sidebar — could we spill into free horizontal space?
