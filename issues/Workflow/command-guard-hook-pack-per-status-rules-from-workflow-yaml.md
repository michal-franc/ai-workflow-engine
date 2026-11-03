---
title: "Command guard hook pack: per-status rules from workflow.yaml"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
version: "0.57"
parent: "workflow/harness-checks-the-work-ideas-from-lights-on"
source: "https://github.com/gojko/lights-on-software-factory"

---
The engine checks work at transitions and leaves single commands to the vendor's sandbox (`docs/why.md`). Some of
our own rules are still only prompt text: agents never push, no code edits while an issue is in design, no `--wait`.
Lights-On enforces rules like these with a regulator: a Claude Code `PreToolUse` hook that parses every Bash command
(unwrapping `bash -c`, `timeout`, `env` …), applies ordered rules per agent role, and denies with a reason that says
what to do instead.

## Idea

An opt-in hook pack in `claude-mods/`, driven by the issue the agent works on:

1. **Rules come from `workflow.yaml`.** A `guard:` block per status, for example:
   ```yaml
   guard:
     always:
       - deny: "git push"
         instead: "Agents never push. Ship merges into local main; the lead pushes."
     "in design":
       - deny_edit: "**/*.go"
         instead: "Design writes no code. Put it in ## Design, then ask for approval."
   ```
2. **The hook asks `issue-cli`** for the session's issue and status (from the dispatch env), so the rules follow
   the workflow, not a hard-coded list.
3. **Deny reasons are fix commands,** like validator hints.
4. **Every deny is logged** in the clilog, so Stats can show which rules fire.

## Notes

- Opt-in, so the engine stays vendor-neutral. Codex gets the same rules later if it has a hook point.
- Not the launcher work in `cli/agent-process-safety-prompt-off-the-command-line-clean-stop-env-allowlist`; that
  one is about how the agent process starts and stops.
- Source: REGULATOR.md in gojko/lights-on-software-factory.
