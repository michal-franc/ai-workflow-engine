---
title: "Folded brief"
status: "in design"
system: "UI"
assignee: "agent-ui-folded-brief"
created: "2026-10-01"
depends_on: "ui/parked-one"
---

**In short:** the issue page opens on a band that says what this is, where it is and what it needs from you.

| Part | Source |
|:--|:--|
| Brief | the body |

## Decisions

- **D-1** · 2026-10-01 · lead chat: Build the band first, "the band first, the rest can wait"
- **D-2** · 2026-10-01 · recorded by agent: Fold the body by section, "Fold, nothing lost (Recommended)"

## Idea

- [x] Description written
- [x] Edge cases identified

The owner rarely reads a whole issue, so the page leads with what it needs.

### Edge cases

No lead paragraph: the title stands alone.

### Not included

The Evidence tab.

## Design

- [x] Approach documented
- [x] Dependencies identified

The tab reads the same functions as the Status page.

### Approach

A band in three columns, then the body folded by section.

## Acceptance Criteria

- [ ] The band answers what, where and needs you
- [ ] Needs you shows what Status shows
- [ ] The body folds by section
- [ ] Expand all gives today's body
