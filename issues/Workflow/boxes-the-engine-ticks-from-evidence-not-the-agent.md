---
title: "Boxes the engine ticks from evidence, not the agent"
status: "idea"
system: "Workflow"
priority: "high"
type: "feature"
version: "0.57"
parent: "workflow/harness-checks-the-work-ideas-from-lights-on"
source: "https://github.com/gojko/lights-on-software-factory"

---
Lights-On's progress record has one rule: "No agent writes the record." Each agent answers a schema, and a tool
derives every line of the record from those answers and from the gates. Here, the agent ticks every box, including
boxes whose truth the engine could see for itself.

## Idea

1. **Engine-owned boxes.** A box in `workflow.yaml` can name the evidence that ticks it:
   ```yaml
   - type: append_section
     title: Testing
     body: |
       - [ ] Local CI green on the branch {evidence: ci_green}
       - [ ] Playtest session passed twice {evidence: playtest_passed}
       - [ ] Test results logged {evidence: comment_prefix "tests:"}
   ```
2. **`issue-cli check` refuses them.** Ticking one by hand fails and says which evidence ticks it and how to produce
   it (`issue-cli ci run`, `make ux-test ISSUE=<slug> REPEAT=2`).
3. **The engine ticks them** on `start`, `show` and `transition` when the evidence exists, and unticks one whose
   evidence went stale (a newer commit after the green run).
4. **Visible as engine-ticked** on the detail page, apart from boxes the agent ticked.

## Notes

- Overlaps `workflow/check-results-nothing-ran-fails-and-a-repeated-error-stops` ("said vs found vs approved"):
  that issue records what the checks found; this one lets a found result own a box.
- Keep plain boxes as they are. Only boxes that name evidence change.
- Source: ORCHESTRATOR.md "Progress and resume" in gojko/lights-on-software-factory.
