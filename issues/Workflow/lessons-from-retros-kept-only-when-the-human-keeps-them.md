---
title: "Lessons from retros, kept only when the human keeps them"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
parent: "workflow/harness-checks-the-work-ideas-from-rigone"
created: "2026-10-22"
---

Retros today are free text in `retros/` (`issue-cli retrospective`, `cmd/issue-cli/cmd_retrospective.go`). The lead
reads them and fixes the process by hand (`handlers_dispatch.go`, the retro review prompt). What an agent learned
never reaches the next agent's prompt unless someone edits `workflow.yaml` or `CLAUDE.md`.

RigOne's version: each run suggests up to three notes. A note stays out of every prompt until the human presses
**Keep**. **Discard** asks once, and that note never comes back, even if an agent learns it again. Each note shows
the rule, why it was suggested, its length, its reach (this project, all projects, one agent) and the run it came
from.

## Idea

Add it at the retro stage:

1. **Propose.** `issue-cli retrospective` takes up to three `--lesson "<rule>" --why "<reason>"` pairs (or a
   `## Lessons` section in the body). Each lands as a pending lesson with its source issue and retro file.
2. **Decide.** Pending lessons show on the Status page (and count in "waiting on the owner", `attention.go`) with
   **Keep** and **Discard**. Also `issue-cli lesson keep|discard <id>` for the lead.
3. **Use.** Kept lessons go into every dispatch prompt (worker and lead), in a short `Lessons` block. Optional
   reach: whole project, one status, or one system.
4. **Remember discards.** A discarded lesson is stored with a normalised fingerprint; a later proposal that matches
   it is dropped with a note in the retro.
5. **Budget.** Cap the total size of kept lessons in a prompt; when full, the Status page asks which to retire.

## Open questions

- Storage: one `lessons.yaml` at the project root (diffs well, committable) or one file per lesson.
- Does the lead's retro review propose lessons too, or only workers?
- Should a kept lesson suggest a `workflow.yaml` or `CLAUDE.md` change instead, when it is really a rule?
