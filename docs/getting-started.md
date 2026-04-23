---
title: "Getting Started"
order: 1
---

## Installation

```bash
go build
```

## Running

```bash
./issue-viewer -dir ./issues -docs ./docs -port 8080
```

Point `-dir` at any directory containing markdown issue files (supports subdirectories organized by system). Point `-docs` at a directory of documentation markdown files.

## Creating Your First Issue

Create a markdown file in `issues/` (or a subdirectory like `issues/UI/`):

```markdown
---
title: "My first issue"
status: "Idea"
system: "UI"
---

Description of the issue.
```

Open `http://localhost:8080` to see it in the list view, `/board` for the kanban board, or `/graph` for the workflow status graph.
