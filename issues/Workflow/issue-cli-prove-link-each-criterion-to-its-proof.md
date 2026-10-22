---
title: "issue-cli prove: link each criterion to its proof"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
---

Nothing records which test, playtest step, screenshot or commit proves which acceptance criterion. Agents write
`tests:` comments as free text, criteria are often plain bullets (the Eldritch boss ticked only the template
placeholder), and proof screenshots sit unlinked in the worktree's `screenshots/`. The Evidence tab's centre,
criterion → proof, has nothing to read.

This is the one part of the epic that asks agents for something new.

## Idea

1. `issue-cli prove <slug> <criterion-id> --by test:<name> | step:<session step id> | shot:<path> | commit:<sha>
   [--value "690 vs 380"]`, repeatable, stored in the data sidecar. `--by shot:` copies the file into the issue's
   files so the viewer can serve it.
2. A validation at the design gate: acceptance criteria are checkboxes with ids.
3. The testing status prompt asks for one `prove` per criterion before `testing → human-testing`; a validation
   checks every criterion has at least one proof.
4. The shipping prompt asks for `risk:` comments for anything to watch after the ship.

## Open questions

- Data sidecar or a generated `## Evidence` section in the body?
- 0.54's rule is "agents don't self-report". This is self-reported proof; can the viewer verify part of it (the test
  exists and passed in the last `make validate`, the step passed in `results.yaml`)? If not, does this move to 0.57?
