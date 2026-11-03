---
title: "Check step that tries to refute each acceptance criterion"
status: "idea"
system: "Workflow"
priority: "high"
type: "feature"
version: "0.57"
parent: "workflow/harness-checks-the-work-ideas-from-lights-on"
source: "https://github.com/gojko/lights-on-software-factory"
depends_on: "workflow/issue-cli-prove-link-each-criterion-to-its-proof"

---
Today the agent ticks the Acceptance Criteria boxes itself, and the gates check the ticks, not the work. Lights-On
ends every run with a `workflow-check` agent whose only job is to try to refute what each step claimed. Its rules, in
its own words: "Look for the case that makes it false, not for agreement", and "a guessed `held` is worse than a gap
you show".

## Idea

1. **A check at `testing → human-testing`.** A fresh agent (not the worker, no shared context) gets the issue's
   Acceptance Criteria, the branch diff and the `prove` links, and answers each criterion:
   - `held`: evidence found and it could not refute it;
   - `notHeld`: the tree disagrees with the claim;
   - `notProvable`: no evidence, or the only evidence is the claim. "Not sure" is `notProvable`.
2. **Evidence is a file, a line and a name.** "`<source>:<line>` writes it and `<test>:<line>` asserts it." A test
   file that exists is not a test that asserts the claim. Ask "what goes red if this becomes false?"; if nothing,
   it is `notProvable`.
3. **Recorded by the engine.** The answers land in the data sidecar as structured records, not as a comment the
   worker writes. The Evidence tab shows them beside the `prove` links.
4. **Gate.** `notHeld` blocks the transition and names the criterion. `notProvable` passes only with a human
   approval that sees the list.

## Notes

- Builds on `workflow/issue-cli-prove-link-each-criterion-to-its-proof`: `prove` says where the proof is, this
  check tries to break it.
- The checker writes and repairs nothing. Read-only tools only.
- Open: how the check agent is started (a dispatch of its own, or a step the lead runs) and what it costs per issue.
- Source: `workspace/.claude/agents/workflow-check.md` in gojko/lights-on-software-factory.
