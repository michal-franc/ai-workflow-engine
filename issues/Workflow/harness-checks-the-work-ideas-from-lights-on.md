---
title: "Harness checks the work: ideas from Lights-On"
status: "idea"
system: "Workflow"
priority: "medium"
type: "epic"
children:
  - workflow/check-step-that-tries-to-refute-each-acceptance-criterion
  - workflow/boxes-the-engine-ticks-from-evidence-not-the-agent
  - workflow/command-guard-hook-pack-per-status-rules-from-workflow-yaml
  - workflow/repeated-owner-feedback-becomes-a-rule-not-a-prompt-line
  - workflow/deterministic-scheduling-issue-cli-next-decides-what-runs-the-lead-relays
  - workflow/real-token-counts-per-agent-run-from-transcripts
version: "0.57"
source: "https://github.com/gojko/lights-on-software-factory"
comparison: "https://claude.ai/artifact/F1VUVpegV86UikpShHVZ5C"

---
Umbrella for ideas taken from Lights-On (gojko/lights-on-software-factory, `source`), a boilerplate for running a
"lights-on software factory" with Claude Code: humans supervise, deterministic checks replace "markdown prayers", and
a deterministic orchestrator schedules work instead of LLM inference. Read at commit 46eea67 (2026-10-29): README,
WORKFLOW, ORCHESTRATOR, REGULATOR, LINTER-HOOK, the three agent definitions and `/execute`. Comparison page:
`comparison`.

We share the thesis but sit in a different layer. Lights-On works inside the vendor harness (a `PreToolUse` command
regulator, lint on every edit, worker / gate / check subagents, a generated Workflow-tool script for one plan run).
We are the workflow harness above it: an issue's whole lifecycle, approvals in a board, any agent. Its two strengths
we lack are checking that claims are true, and taking orchestration out of inference. The children below bring
those here.

## Children

| Child | Lights-On idea | What it covers |
|:--|:--|:--|
| `workflow/check-step-that-tries-to-refute-each-acceptance-criterion` | `workflow-check` agent | A fresh agent answers each criterion `held` / `notHeld` / `notProvable` with file:line evidence; `notHeld` blocks `testing → human-testing` |
| `workflow/boxes-the-engine-ticks-from-evidence-not-the-agent` | "No agent writes the record" | Boxes that name their evidence (CI green, playtest passed, `tests:` comment) are ticked by the engine; `check` refuses them |
| `workflow/command-guard-hook-pack-per-status-rules-from-workflow-yaml` | Bash-command regulator | Opt-in `claude-mods/` hook: `guard:` rules per status in `workflow.yaml`, deny reasons that are fix commands |
| `workflow/repeated-owner-feedback-becomes-a-rule-not-a-prompt-line` | "Write a rule each time you repeat feedback" | The lead turns repeated owner corrections into a validator, lint or guard rule before a prompt line |
| `workflow/deterministic-scheduling-issue-cli-next-decides-what-runs-the-lead-relays` | Platform orchestrates, agents don't | Dispatch respects `depends_on` (RogueTech #40); `issue-cli next` is the schedule; the lead relays |
| `workflow/real-token-counts-per-agent-run-from-transcripts` | `measure-run` | Real tokens and tool calls from transcripts next to the `len/4` estimate on Stats |

Not taken: the Docker container (the vendor sandbox's job), the generated Workflow-tool script and file-level plan
steps (our unit is the issue), and lint-on-edit for a specific stack (a project's own hook).

## Overlap

- The refute check builds on `workflow/issue-cli-prove-link-each-criterion-to-its-proof`: `prove` says where the
  proof is; the check tries to break it.
- Evidence ticks sit beside `workflow/check-results-nothing-ran-fails-and-a-repeated-error-stops` (RigOne epic):
  that one records what the checks found; this one lets a found result own a box.
- The guard pack is not `cli/agent-process-safety-prompt-off-the-command-line-clean-stop-env-allowlist`; that one is
  the launcher.
- Feedback-to-rule extends `workflow/lessons-from-retros-kept-only-when-the-human-keeps-them` and the lead check-ins.
- Scheduling covers RogueTech #40 from the 0.57 roadmap row.

## Order

1. Evidence ticks and scheduling first: small, deterministic, and they close "the agent says so" and "the lead
   guesses" today.
2. The refute check after `prove` exists.
3. The guard pack next: one hook plus a `guard:` block.
4. Feedback-to-rule and real token counts last: they need lessons and the clilog/Stats work.
