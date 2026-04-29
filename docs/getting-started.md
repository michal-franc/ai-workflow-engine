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

## Bootstrapping a New Project

In an empty directory, generate a `workflow.yaml` and the standard `issues/` + `docs/` layout in one shot:

```bash
issue-cli workflow init --template development
```

Pick `development` for software delivery, `review` for triage queues, or `writing` for long-form content. Pass `--force` to overwrite an existing `workflow.yaml`. Run without `--template` in a terminal for an interactive picker. See [CLI Overview → workflow init](CLI/overview.md#workflow-init) for the full reference.

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
