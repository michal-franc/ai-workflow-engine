---
title: "Issue File Format"
order: 2
---

## Frontmatter Fields

Every issue is a markdown file with YAML frontmatter.

| Field      | Required | Description                                        |
|:-----------|:---------|:---------------------------------------------------|
| `title`    | Yes      | Issue title                                        |
| `status`   | No       | Workflow stage (see below)                         |
| `system`   | No       | Categorization tag, also used as subdirectory name |
| `version`  | No       | Version string, filterable on the board            |
| `labels`   | No       | List of label strings                              |
| `priority` | No       | `low`, `medium`, `high`, or `critical`             |
| `created`  | No       | Date string for sorting (newest first)             |
| `number`   | No       | GitHub issue number (links to GitHub with `repo`)  |
| `repo`     | No       | GitHub repo in `owner/repo` format                 |

## Status Values

- `idea` — raw idea, needs exploration
- `in design` — being designed and specced out
- `backlog` — ready to work on
- `in progress` — actively being implemented
- `testing` — under verification
- `documentation` — being documented
- `done` — completed

## Example

```markdown
---
title: "Add dark mode toggle"
status: "idea"
system: "UI"
priority: "low"
labels:
  - enhancement
  - ux
---

Allow users to switch between dark and light themes.

## Requirements

- [ ] Toggle button in header
- [ ] Persist preference in localStorage
- [ ] Respect system preference by default
```

## Custom Fields

Any frontmatter key not listed above is preserved as a custom field and displayed in the detail view sidebar. URL values render as clickable links, list values render as bullet lists:

```yaml
pr: "https://github.com/org/repo/pull/456"   # renders as a link
pr_author: "jsmith"                            # renders as text
participants:                                  # renders as a list
  - alice
  - bob
```

Set or clear a custom field with `issue-cli set-meta`:

```bash
issue-cli set-meta <slug> --key waiting --value "blocked on design review"
issue-cli set-meta <slug> --key waiting --clear
```

Workflow-managed fields (`title`, `status`, `human_approval`, `approved_for`, `started_at`, `done_at`, `number`, `repo`, `created`, `labels`) are refused — use the dedicated commands (`transition`, `update --title`, etc.) for those.

## File Organization

Issues can live flat in the issues directory or in subdirectories by system:

```
issues/
  UI/
    add-dark-mode.md
  API/
    rate-limiting.md
  fix-typo.md
```
