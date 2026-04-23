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
| `issue-cli process schema`       | Print the `workflow.yaml` schema (fields, action types, validation rules) |
| `issue-cli process changes`      | Print the release history (last 20 versions) |
| `issue-cli report-bug "..."`     | File a bug report about issue-cli itself |
| `issue-cli retrospective <slug>` | Save a workflow retrospective            |

### `process schema` and `process changes`

`process schema` is driven off reflection on the YAML struct tags in
`internal/tracker/workflow.go`. Every `yaml:"..."` field must also carry a
`desc:"..."` tag — the tracker tests fail otherwise, so the schema output
cannot drift from the parser. Action types and validation rules are
documented via explicit registries (`WorkflowActionTypes`,
`WorkflowValidationRules`) kept next to the switch statements that handle
them.

`process changes` embeds `cmd/issue-cli/CHANGELOG.md` at build time via
`//go:embed` and prints it newest-first, capped to the 20 most recent `## v`
entries. The CHANGELOG lives under `cmd/issue-cli/` (not the project root)
because Go's `//go:embed` cannot reference files outside the embedding
package's directory. When cutting a release, add a new `## vX.Y.Z — YYYY-MM-DD`
section to the top of that file; the annotated tag message should mirror the
bullet list.

## Design Considerations

When working on CLI changes:

- Output is consumed by agents, not humans — prioritize machine comprehension over aesthetics
- Document the output contract early: whether text is human-facing or agent-facing
- Consider whether `--json` or other machine-readable output is needed
- Script compatibility matters — avoid breaking existing agent prompts that parse CLI output
- After making changes, run `make install` to update the binary
