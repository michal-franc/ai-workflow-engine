---
title: "Demo: linkify URLs in data-table comments"
status: "idea"
---
## Summary

This issue tests automatic link detection inside the data-table **Comment** column. URLs typed into a comment cell should render as clickable anchors that open in a new tab, even though the cell stays `contenteditable`.

Two recognised formats:

- explicit scheme — `http://...` or `https://...`
- bare host — `host.domain/path` (at least one dot, then a slash) — gets prefixed with `https://`

Trailing sentence punctuation (`.,;:!?)]`) is kept outside the link.

## Findings

<!-- data statuses=todo,checked -->

## What to look at

- Click each comment in the table — entries 1–6 should render the URL as a link and open it in a new tab.
- Entry 7 has no URL, just plain text — should render as before.
- Entry 8 has a trailing period — the period must stay outside the `<a>`.
- Entry 9 has a `(parens)` URL — closing paren stays outside.
- Edit any cell, blur, then reload — the saved value should still round-trip as plain text (no anchor markup leaks back into the JSON).
