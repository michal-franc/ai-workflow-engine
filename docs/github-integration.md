---
title: "GitHub Integration"
order: 7
---

## Issue Reference

Issues can link back to a GitHub issue via `number` and `repo` frontmatter fields:

```yaml
number: 42
repo: "owner/repo"
```

When both are set, the detail view sidebar shows a clickable link to `https://github.com/owner/repo/issues/42`.

## Auto-Close on Done

When an issue is marked `done` in the web UI and has both `number` and `repo` set, the server automatically closes the corresponding GitHub issue and posts a comment with the implementation details from the issue body.

## Syncing from GitHub Projects

The sync script downloads all items from a GitHub Project and writes them as markdown files:

```bash
./sync-issues.sh <owner> <project-number> [output-dir]
```

Example:

```bash
./sync-issues.sh my-username 4 ./issues
```

This pulls all items, organizes them by system field into subdirectories, and writes markdown files with YAML frontmatter. The output directory is cleaned before writing.
