---
title: "Adoption: make AI Workflow Engine easy to set up, start and use"
status: "idea"
system: "CLI"
priority: "high"
type: "epic"
created: "2026-10-06"
ideas: "https://claude.ai/artifact/EQ1ttCchKDkjnC8M4Mi3o1"
children:
  - cli/easier-setup-install-config-and-agent-wiring
  - ui/easier-first-run-reach-the-first-approval-in-minutes
  - cli/one-name-one-binary
  - cli/agent-led-onboarding-claude-sets-the-project-up-and-explains-it
  - workflow/issue-cli-doctor-check-workflow-yaml-and-suggest-improvements
---

Umbrella for the adoption work. The core idea of the engine works: markdown issues, one `workflow.yaml`, agents
that can't skip a step, and a human who ticks the gates. What holds adoption back is everything before the first
tick. A new user has to:

1. Install with `curl … | bash`.
2. Install tmux, git and the claude or codex CLI themselves.
3. Run `issue-cli init --template development`.
4. Create an issue.
5. Write `projects.yaml` by hand, and know to set `terminal: "none"`.
6. Run `issue-viewer -config projects.yaml`.
7. Walk the 9-status template before reaching a first gate.

The goal is three steps and no hand-written files: install, `init`, open the board on a tour issue that reaches a
gate in minutes. A second goal is that someone can say to Claude "look at this repo, set up the workflow system and
tell me how to use it", and it works.

The 20 ideas behind this split, with code references, are in the `ideas` artifact (private; ask the owner for
access).

## Children

| Child | Ideas | What it covers |
|:--|:--|:--|
| `cli/easier-setup-install-config-and-agent-wiring` | #1–#7 | `make demo` fix, `init` writes `projects.yaml`, zero-flag `issue-viewer`, terminal auto-detect, `issue-cli doctor`, more install channels, agent hints on `init` |
| `ui/easier-first-run-reach-the-first-approval-in-minutes` | #8–#14 | `quickstart`, empty-board states, hero GIF, tour issue, hosted demo, `lite` template, import |
| `cli/one-name-one-binary` | #20 | One binary with subcommands; `issue-viewer` and `issue-cli` kept as aliases |
| `cli/agent-led-onboarding-claude-sets-the-project-up-and-explains-it` | new | The "hey Claude, set this up" path: agent-facing docs, a non-interactive setup contract, a Claude Code plugin, and an eval that proves it |
| `workflow/issue-cli-doctor-check-workflow-yaml-and-suggest-improvements` | #5 (workflow part) | `doctor` checks `workflow.yaml` (errors, warnings) and suggests improvements from telemetry, stats and retros |

Not split out yet: the daily-use ideas (#15 approval inbox and notifications, #16 approve from a phone, #17
`issue-cli approve` with an audit trail, #18 "what the agent will see" in the designer, #19 watch an agent in the
browser). #15 and #17 overlap the roadmap's inbox and "recording an approval you gave in chat". Split them out once
the children above are designed.

## Order

1. Setup first, starting with the `make demo` bug: it breaks the README's "just looking?" path today. Doctor's
   tier 1 (workflow errors) ships alongside, since setup and onboarding both lean on `doctor --json`.
2. Agent-led onboarding next. It depends on `doctor` and the non-interactive `init` from the setup child, and it is
   the fastest route to new users, since most of them already run Claude Code.
3. First run after that. The tour issue and the `lite` template are what the agent-led path will set up.
4. One binary last, before 1.0, while renames are still cheap. Do it after the others land so their new commands
   are named once.

## Open questions

- Should children stage on a base branch (`umbrella/adoption`) or ship into `main` one at a time? They touch
  different files and each one is useful alone, so `main` seems fine. If so, leave the `base` field unset.
- This repo's `workflow.yaml` has no `types:` block yet, so `type:` on these issues is a plain custom field. Add
  `types:` (at least `feature` and `epic`) so the umbrella gets the epic path, or keep it as a convention?
