---
title: "Dispatch preflight: check the run before an agent starts"
status: "idea"
system: "CLI"
priority: "medium"
type: "feature"
parent: "workflow/harness-checks-the-work-ideas-from-rigone"
created: "2026-10-22"
---

Today dispatch (`internal/agentrun.Start`) creates the worktree and tmux session and types the launch command. If
the agent binary is missing, the model name is not offered, the epic's base branch is gone or `workflow.yaml` has
an error, the agent finds out mid-run, after spending tokens, as a blocked transition or a dead terminal.

RigOne checks the whole graph before the first step: every step's model, effort and permissions against what the
agent app reports. An unsupported choice stops the run before any agent spends time, and names the step.

## Idea

Before `CreateTmux`, run a preflight and refuse with a named reason (shown in the dispatch steps on the board and in
`issue-cli` output):

- the agent binary (`claude` / `codex`) is on `PATH` and answers `--version`;
- the enforced model (`proj.AgentModel`) passes `validModelName` and, where the CLI can list models, is offered;
- the worktree's base branch exists (epic `base`, else main) and the worktree path is free or already ours;
- `workflow.yaml` loads and `Lint()` has no errors;
- the issue's current status has a prompt for its type (a type whose `path:` skips the status would strand the agent).

Reuse the `issue-cli doctor` checks (`cli/easier-setup-install-config-and-agent-wiring` #5) scoped to one agent.
Add `--skip-preflight` for the rare case it is wrong, and record that it was skipped.
