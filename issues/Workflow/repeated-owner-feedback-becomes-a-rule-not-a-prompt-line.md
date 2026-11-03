---
title: "Repeated owner feedback becomes a rule, not a prompt line"
status: "idea"
system: "Workflow"
priority: "low"
type: "feature"
version: "0.57"
parent: "workflow/harness-checks-the-work-ideas-from-lights-on"
source: "https://github.com/gojko/lights-on-software-factory"

---
Lights-On's habit: "Ask Claude to write more custom rules each time you find yourself providing repetitive
design/code feedback." The feedback becomes a deterministic check (a lint rule or a regulator rule), not another line
of prompt text that can be lost in the middle.

## Idea

1. **Notice repeats.** When the owner gives the same correction twice (approval notes, `decide` records, retro
   lessons, needs-revision comments), the lead flags it.
2. **Propose a rule, not a prompt line.** The lead proposes the cheapest deterministic form, in order: a validator
   in `workflow.yaml` (with a `hint:`), a project lint rule, a guard rule, and only last a prompt line.
3. **The owner keeps or discards it,** like a lesson.

## Notes

- Extends `workflow/lessons-from-retros-kept-only-when-the-human-keeps-them` (a kept lesson can be promoted to a
  rule) and `workflow/lead-check-ins-the-lead-proposes-features-type-changes-and-fixes-to-the-owner-from-retros-and-health`.
- Small once those exist: mostly lead brief text plus a "promote to rule" action.
- Source: README.md in gojko/lights-on-software-factory.
