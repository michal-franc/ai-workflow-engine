---
title: "One name, one binary"
status: "idea"
system: "CLI"
priority: "medium"
type: "feature"
umbrella: "cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use"
created: "2026-10-06"
ideas: "https://claude.ai/artifact/EQ1ttCchKDkjnC8M4Mi3o1"
---

The product is AI Workflow Engine, the board is `issue-viewer`, the agent CLI is `issue-cli`, and the Go module
keeps the `issue-viewer` name. Newcomers learn three names and install two binaries, and the docs have to explain
which is which. Idea #20 from the adoption artifact.

## Idea

Ship one binary with subcommands, for example:

```
awe board [-config projects.yaml] [-port 8080]   # today: issue-viewer
awe start <slug>                                 # today: issue-cli start <slug>
awe transition <slug> --to …                     # today: issue-cli transition …
```

- Keep `issue-viewer` and `issue-cli` working as aliases (symlinks or a tiny shim that checks `argv[0]`), so
  existing `workflow.yaml` prompts, agent briefings, `.agent-logs` and muscle memory keep working. Agent prompts in
  other projects (Raid League's 710-line workflow) call `issue-cli` by name, so the alias has to stay indefinitely.
- One release archive with one binary; `install.sh` creates the aliases.
- `awe help` lists the board and the CLI commands together.

## Questions for design

- The name. `awe` is short and matches the product, but it should be checked for clashes (Homebrew, apt, common
  shell aliases). Alternatives: `aiwf`, `wfe`.
- Is a module rename (`go.mod`) part of this, or a separate cleanup?
- Does the board process stay one long-running command (`awe board`), or should `awe` start it in the background
  when a command needs it?
- Telemetry (`docs/CLI/telemetry.md`) logs command names. How do aliases show up there?

## Order

Do this last in the umbrella, before 1.0, so the commands the other children add (`doctor`, `quickstart`, `import`,
`approve`) are named once under the new binary.
