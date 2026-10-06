---
title: "issue-cli doctor: check workflow.yaml and suggest improvements"
status: "idea"
system: "Workflow"
priority: "high"
type: "feature"
umbrella: "cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use"
created: "2026-10-06"
ideas: "https://claude.ai/artifact/EQ1ttCchKDkjnC8M4Mi3o1"
---

`workflow.yaml` is the product's main interface, and today nothing checks it. Mistakes show up later, in the middle
of an agent's run, as a blocked transition the agent can't fix. Nothing tells the author that a workflow which works
could still be better: lighter for agents, faster for the human, or with fewer steps that fail.

This issue makes `issue-cli doctor` the workflow's reviewer. It runs in three tiers: **errors** (this won't work),
**warnings** (this works, but agents will stumble), and **suggestions** (based on how the workflow is actually being
used). The roadmap item "a command that validates `workflow.yaml`" is the first tier. The setup child
(`cli/easier-setup-install-config-and-agent-wiring`, idea #5) keeps the environment checks: tmux, git, the agent
CLIs, `projects.yaml` and the port. Both run under the same `doctor` command.

## Today

I wrote this `workflow.yaml` with five deliberate mistakes:

```yaml
statuses: [todo, doing, done, orphan]          # (as name: entries); nothing leads to "orphan" on purpose
transitions:
  - from: todo
    to: in-progress                            # no such status
    actions:
      - { type: validate, rule: section_checkbox_checked, section: Plan }   # misspelled rule
  - from: doing
    to: done
    actions:
      - { type: validate, rule: "section_checkboxes_checked: Testing" }     # nothing appends "Testing"
      - { type: validate, rule: command_succeeds, command: "make test" }    # no allow_shell: true
```

`issue-cli process workflow` printed the lifecycle `todo → doing → done → orphan` with **no warnings**, and
`issue-cli create` happily created an issue. `WorkflowConfig.Lint()`
(`internal/tracker/workflow_types.go:194`) only checks `types:`. Its section-gap check (an edge validates a section
nothing appends) is exactly what the base workflow needs too.

## Tier 1: errors (exit 1)

The workflow will misbehave. Each error carries a `file:line` and a fix.

- A transition `from`/`to`, a `require_human_approval` status, a `board.columns` entry or an overlay status that
  isn't a defined status. Offer "did you mean" by edit distance.
- An unknown action `type` or validation `rule` (checked against the `process schema` catalogue), or a rule that's
  missing its parameter (`section`, `field`, `values`, `pattern`, `ref_key`…).
- `command_succeeds` without top-level `allow_shell: true`.
- A section that is validated (`section_checkboxes_checked`, `section_has_checkboxes`, `has_section`,
  `section_min_length`) but never appended anywhere upstream on the path, and isn't a section people write by hand.
  This is the base-workflow version of the type lint.
- A status no transition (or linear fallback) can reach, or a status that isn't terminal but has no way out.
- The existing type and overlay lint, which already runs under `process workflow`.

## Tier 2: warnings (exit 0, listed)

The workflow works, but agents or humans will trip over it.

- A gate (`require_human_approval`) whose status prompt doesn't tell the agent to ask the human and wait. The
  bundled template's wording ("tell the human in chat… then run `issue-cli transition <slug> --to … --wait`") can
  be the suggested fix.
- A `validate` rule with no `hint:`, where the default failure message doesn't name a command that fixes it.
- A prompt that mentions an `issue-cli` subcommand or flag that doesn't exist, or a status name that isn't
  defined. Prompts go stale after renames.
- Deprecated keys (`actions:` → `issue_actions:`, `template`, `validation`, `side_effects` → transition actions),
  with the replacement YAML.
- An `append_section` with checkboxes that nothing later checks, so the boxes are decoration.
- The same long prompt text copied into several overlays. Suggest moving it to the base or to an `inject_prompt`.
- Prompts above a size budget (default: the 90th percentile of `static_tokens` from the stats sidecars, or a fixed
  number when there are no stats), since every token is read on every transition.

## Tier 3: suggestions from real use (`doctor --suggest`)

The project already records what happened locally. Doctor can read it and point at the steps that cost the most.
It reads local files only and sends nothing anywhere.

| Evidence | Where it lives | Suggestion |
|:--|:--|:--|
| Transitions that fail most often, by edge and error class | telemetry (`.agent-logs/telemetry.jsonl`: `cmd`, `issue`, `exit`, `err_class`) | "`testing → human-testing` failed 14 times in 30 days, mostly on `has_comment_prefix: tests:`. Add a `hint:` with the exact comment command, or append a checklist item for it on the edge into `testing`." |
| Gates the human waits longest on | stats sidecars (`wait_started_at` → `approved_at`) | "Agents waited a median 2 h 10 m for `in progress` approval. Approve from the inbox, or merge this gate with the design gate before it." |
| Gates that are always approved within a minute | stats sidecars | "`documentation → shipping` was approved in under 60 s on 22 of 22 issues. Consider removing the gate or making it a type-specific step." |
| Statuses issues pass through in seconds | stats sidecars (consecutive `ts`) | "Issues spend a median 40 s in `documentation`. Mark it `optional: true`, or leave it off the path of a `tweak` type." |
| Heaviest prompts | stats sidecars (`static_tokens`) | "The `in design` prompt for system UI is 1,900 tokens, 3× the median. The Mockups steps could move into the appended section's body." |
| Recurring complaints | `retros/` | "7 retrospectives mention `human-testing` or the sandbox. See the list." |

Suggestions are never applied automatically. Each one names the evidence (counts, date range, example issues), so
the human can decide.

## Output

```
$ issue-cli doctor
workflow.yaml
  ✗ E101  workflow.yaml:8   transition to "in-progress": no such status (did you mean "in progress"?)
  ✗ E104  workflow.yaml:11  unknown rule "section_checkbox_checked" (did you mean "section_checkboxes_checked"?)
  ✗ E110  workflow.yaml:17  command_succeeds needs allow_shell: true at the top level
  ⚠ W203  workflow.yaml:30  gate "in progress": the "backlog" prompt doesn't tell the agent to ask you and wait
  ⚠ W207  workflow.yaml:1   "actions:" is deprecated; rename to "issue_actions:"
2 errors, 2 warnings · run `issue-cli doctor --suggest` for usage-based suggestions · `issue-cli doctor explain E104`
```

- Stable IDs (`E1xx`, `W2xx`, `S3xx`), each with a docs entry and `doctor explain <id>`.
- `--json` for agents and the board. `--fix` applies only mechanical, meaning-preserving fixes (deprecated key
  renames), and shows the diff first.
- `issue-cli process workflow` keeps showing errors and warnings in its `== Warnings ==` block. `transition` and
  `start` print one line when the workflow has errors (`workflow.yaml has 2 errors — issue-cli doctor`), so agents
  find it through a CLI hint rather than prompt text.
- The board's workflow designer shows the same findings inline (the types doc notes it doesn't show lint yet).
  The Stats tab links the tier-3 suggestions.

## Who uses it

- **People writing a workflow**: run it after every edit, or let the designer show it.
- **The agent-led onboarding child**: `doctor --json` is its "did setup work?" check.
- **A lead session or a `project_actions` button**: "Workflow review" runs `doctor --suggest --json` and files one
  issue per suggestion it agrees with. That closes the loop the Retros page starts.

## Slices

1. Tier 1 and the CLI output with IDs, `--json` and the hint line in `transition`/`start`. Generalise `Lint()` to
   the base workflow.
2. Tier 2, `--fix` for deprecated keys, and the designer integration.
3. Tier 3 `--suggest` from telemetry, stats and retros, plus the board button.

## Acceptance criteria (draft)

- [ ] The five-mistake workflow above produces one error per mistake, each with a line and a fix (or the design
      says why one isn't flagged; for example, the linear fallback may make "orphan" reachable)
- [ ] This repo's `workflow.yaml`, the three bundled templates and the demo project's workflow pass with zero errors
- [ ] Every finding has a stable ID and a `doctor explain` entry
- [ ] `--json` output is documented in docs/CLI/overview.md
- [ ] `--suggest` reports only from local files, says which files and which date range it read, and degrades to
      "no usage data yet" on a fresh project
- [ ] Telemetry opt-out (`telemetry: false`) is respected: `--suggest` skips that source and says so

## Questions for design

- Thresholds for tier 3 (what counts as "always approved quickly", minimum sample size). Make them flags or
  `doctor:` keys in `workflow.yaml`?
- Should errors block `issue-viewer` from loading the project, or only show a banner? Today a failed load silently
  falls back to the default workflow (docs/Workflow/types.md), which is worse than either.
- Does `--suggest` need git history (`git log -p workflow.yaml`) to avoid suggesting a change the human just
  reverted?
