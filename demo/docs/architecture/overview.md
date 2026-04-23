---
title: "Architecture Overview"
order: 1
---

## Stack

| Layer     | Technology         |
|:----------|:-------------------|
| Server    | Go + `net/http`    |
| Templates | Go `html/template` |
| Styling   | CSS custom props   |
| Data      | Markdown + YAML    |
| Storage   | Filesystem         |

## Data Flow

```
Markdown files → LoadIssues() → Handlers → Templates → HTML
                                    ↑
                              POST updates → UpdateIssueFrontmatter() → Write .md
```

## Project Structure

Issues are organized by system in subdirectories:

```
issues/
  Backend/
    1.md
    2.md
  Frontend/
    3.md
  Infrastructure/
    6.md
```

Each file contains YAML frontmatter (metadata) and a markdown body (description).
