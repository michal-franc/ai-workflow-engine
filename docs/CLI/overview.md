---
title: "CLI Overview"
order: 1
---

## Scope

The CLI system covers `issue-cli`, the command-line tool agents use to interact with issues during automated workflows.

## Key Files

- `cmd/issue-cli/main.go` — CLI entry point, command dispatch, output formatting

## Commands

| Command                          | Description                              |
|:---------------------------------|:-----------------------------------------|
| `issue-cli show <slug>`          | Print full issue context                 |
| `issue-cli start <slug>`         | Pick up issue from any status — claim + advance handoff states |
| `issue-cli transition <slug>`    | Attempt the next workflow transition     |
| `issue-cli comment <slug>`       | Add a comment to an issue                |
| `issue-cli append <slug>`        | Append content to issue body             |
| `issue-cli set-meta <slug>`      | Set or clear a frontmatter field         |
| `issue-cli process workflow`     | Print the active workflow                |
| `issue-cli process transitions`  | Print available transitions              |
| `issue-cli report-bug "..."`     | File a bug report about issue-cli itself |
| `issue-cli retrospective <slug>` | Save a workflow retrospective            |

## Design Considerations

When working on CLI changes:

- Output is consumed by agents, not humans — prioritize machine comprehension over aesthetics
- Document the output contract early: whether text is human-facing or agent-facing
- Consider whether `--json` or other machine-readable output is needed
- Script compatibility matters — avoid breaking existing agent prompts that parse CLI output
- After making changes, run `make install` to update the binary
