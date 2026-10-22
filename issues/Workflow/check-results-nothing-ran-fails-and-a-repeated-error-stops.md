---
title: "Check results: \"nothing ran\" fails, and a repeated error stops"
status: "idea"
system: "Workflow"
priority: "high"
type: "feature"
parent: "workflow/harness-checks-the-work-ideas-from-rigone"
created: "2026-10-22"
version: "0.55"

---

`command_succeeds` (`internal/tracker/validations/command_succeeds.go`) passes on exit code 0 and nothing else. A
test command that matched no packages, a `-run` filter that selected nothing, or a script that exits early all pass.
RigOne's rule: "a green exit code without proof that tests ran is not accepted as a green check", and "nothing ran"
is shown as its own outcome, never as a pass.

## Ideas

1. **Proof that it ran.** Optional fields on the action:
   - `expect_output: "<regex>"` — stdout or stderr must match, for example `(?m)^ok\s` for `go test`.
   - `min_matches: N` — at least N matches, so one passing package of twenty does not count.
   - A failure message that says "nothing ran" apart from "failed", with the output tail.
2. **Program not found is not a code failure.** Exit 127 or `exec: not found` reports "command not found: <name>"
   with a fix hint, not "shell check failed".
3. **A repeated error stops the retries.** When the same transition fails the same check with the same output twice
   in a row, the CLI says so and tells the agent to stop and ask the human (or lead) instead of trying again. Where
   the last failure is kept (data sidecar or `.agent-logs`) is a design question.
4. **Said vs found vs approved.** Record each check's result (rule, command, exit, matched count, output tail,
   time) in the issue's data sidecar, so the detail page can show three things apart: what the agent wrote
   (`tests:` comments), what the checks found, and what the human approved.

## Notes

- Keep it opt-in per action; existing `workflow.yaml` files keep working.
- Add `expect_output` to the bundled `development` template's test checks once it exists.
