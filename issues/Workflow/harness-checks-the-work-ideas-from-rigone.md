---
title: "Harness checks the work: ideas from RigOne"
status: "idea"
system: "Workflow"
priority: "medium"
type: "epic"
created: "2026-10-22"
source: "https://github.com/monoone-dev/rig-one-landing-page/blob/main/docs/user-guide.md"
children:
  - workflow/check-results-nothing-ran-fails-and-a-repeated-error-stops
  - workflow/lessons-from-retros-kept-only-when-the-human-keeps-them
  - cli/dispatch-preflight-check-the-run-before-an-agent-starts
  - ui/health-section-on-the-status-page
  - cli/agent-process-safety-prompt-off-the-command-line-clean-stop-env-allowlist
version: "0.58"

---
Umbrella for ideas taken from RigOne, a closed-source macOS control room for coding agents (Claude Code, Codex,
OpenCode). Its app code is not public; the ideas come from its user guide and README (`source`). We already share
its core: a lead agent, an isolated copy of the code per worker, one list of what waits on the human
(`attention.go`), playtests with screenshots, and skills.

RigOne's stance, in its own words: "RigOne runs the checks itself; it does not ask the agent whether it worked", and
"orchestration failures are product failures, not terminal noise". The children below bring that stance here.

## Children

| Child | RigOne idea | What it covers |
|:--|:--|:--|
| `workflow/check-results-nothing-ran-fails-and-a-repeated-error-stops` | Checks and second opinions | `command_succeeds` can require proof that tests ran; a repeated identical failure stops the retries; what the agent said and what the checks found are recorded apart |
| `workflow/lessons-from-retros-kept-only-when-the-human-keeps-them` | Knowledge: notes waiting for you | The retro stage proposes up to 3 lessons; they reach prompts only after the human presses Keep; Discard is remembered |
| `cli/dispatch-preflight-check-the-run-before-an-agent-starts` | Graph check before the first step | Dispatch refuses, naming the cause, when the agent binary, model, base branch, worktree or `workflow.yaml` would fail mid-run |
| `ui/health-section-on-the-status-page` | Settings → Health check | OK / Attention / Problem / Optional rows on `/status`, each problem with its fix, built on `issue-cli doctor --json` |
| `cli/agent-process-safety-prompt-off-the-command-line-clean-stop-env-allowlist` | Safety model | Prompt off argv, stop that verifies the agent's processes are gone, launch env from an allowlist, refuse two agents on one worktree |

Not taken: a second opinion from the other vendor, design review on screenshots, the Lab, triggers and ⌘K. Revisit
after these land.

## Overlap

- The health section reuses `issue-cli doctor` from `cli/easier-setup-install-config-and-agent-wiring` (#5) and
  `workflow/issue-cli-doctor-check-workflow-yaml-and-suggest-improvements`. It adds the board view and the agent
  sign-in and disk rows; it does not write a second checker.
- Dispatch preflight calls the same doctor checks, scoped to the one agent being started.

## Order

1. Check results first: it is small, in one validator, and it closes the "green exit, no tests" hole today.
2. Process safety next: the prompt-on-argv fix is a few lines in `internal/agentrun`.
3. Preflight and the health section together, after `doctor --json` exists.
4. Lessons last: it touches the retro command, the Status page and the dispatch prompt, and needs a design.
## Roadmap 2026-10-22

- Cross-epic dependency: preflight and the health section need `doctor --json` from the adoption epic (`workflow/issue-cli-doctor-check-workflow-yaml-and-suggest-improvements` tier 1). Start them after it.
- Owner decision (cli/agent-process-safety-…#D-1): the prompt moves off argv to a file or stdin. That replaces the argv launch from the dispatch-prompt fix, and delivery must stay verified.
- Gate *bugs* (existing checks measuring the wrong thing) live in `workflow/gate-correctness-checks-an-agent-cannot-satisfy-by-accident`. This epic adds new checks.

