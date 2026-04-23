---
title: "Board Configuration"
order: 5
---

## Overview

The kanban board layout is controlled by the `board` section in `workflow.yaml`. Both which columns appear and which fields show on each card are configurable.

## Columns

`board.columns` sets which statuses appear as swimlane columns and in what order. Only listed statuses are shown:

```yaml
board:
  columns:
    - backlog
    - in progress
    - testing
    - done
```

If omitted, all statuses defined in `statuses:` are shown in their declared order.

Issues whose status is not in the column list are hidden from the board (they still appear in the list view).

## Card Fields

`board.card_fields` sets which issue fields appear on each card. The order in the list determines the display order:

```yaml
board:
  card_fields:
    - system
    - labels
    - priority
    - assignee
```

Supported field names:

| Field      | Renders as                          |
|:-----------|:------------------------------------|
| `system`   | System tag text                     |
| `labels`   | Individual label badges             |
| `priority` | Priority text                       |
| `assignee` | Assignee name                       |
| `version`  | Version string                      |
| `number`   | Issue number prefixed with `#`      |

Any other name falls through to custom frontmatter fields. If an issue has a matching frontmatter key (e.g. `waiting: infra-team`), its value renders as a card pill; list values render as individual badges. Empty values are skipped. This lets custom workflows surface blocker fields like `waiting`, `team`, `pr_author`, or `due` directly on the board without code changes.

If omitted, defaults to `system` and `labels`.

## Full Example

```yaml
board:
  columns:
    - idea
    - in design
    - backlog
    - in progress
    - testing
    - human-testing
    - documentation
    - done
  card_fields:
    - system
    - labels
    - priority
    - assignee
```
