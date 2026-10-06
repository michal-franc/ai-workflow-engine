---
title: "Agent-led onboarding: Claude sets the project up and explains it"
status: "idea"
system: "CLI"
priority: "high"
type: "feature"
umbrella: "cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use"
created: "2026-10-06"
---

Most people who would use this tool already run Claude Code or Codex. The shortest route to adoption is to let
them say one sentence to the agent they already have:

> Look at this repo, set up the workflow system and tell me how to use it.

and get a working board, a workflow that fits their repo, and a short explanation built from *their* workflow.
Today an agent can't do this reliably. Nothing in the repo is written for an agent that is setting the tool up for
someone. `CLAUDE.md` here is a contributor guide (project structure, file layout), so an agent asked to "set this
up" starts reading Go handlers. `init` writes only part of the config. Nothing tells the agent which choices belong
to the human. "How to use it" is left for the agent to invent from the README.

## The two entry points

1. **The user's own repo** (most common). They name the tool:
   "Set up https://github.com/michal-franc/ai-workflow-engine in this repo and tell me how to use it."
   The agent needs one stable URL it can fetch that holds the whole procedure.
2. **A clone of this repo.** "Look at this repo, set up the workflow system…" The agent reads `CLAUDE.md` first,
   so `CLAUDE.md` has to send a *user* to the setup procedure and a *contributor* to the code map.

Both lead to the same runbook, and once the plugin is installed (below), to the same skill.

## Proposal

### 1. An agent runbook: `docs/agents/setup.md`

Written for an agent, not a person. Fetchable raw from GitHub, and linked from the top of `README.md`,
`CLAUDE.md`, `AGENTS.md` and an `llms.txt` at the repo root. It contains:

- **What to check first**: `issue-cli version`, `issue-cli doctor --json` (setup child #5). If the binaries are
  missing, the install command and how to confirm `~/.local/bin` is on `PATH`.
- **The only three questions to ask the human**, each with a default, so the agent doesn't interrogate them or
  silently decide for them:
  1. How much process? `lite` (one gate before merge; default for a first try) or `development` (design, build,
     test and ship gates).
  2. Who does the work? Agents dispatched from the board (needs tmux), or this session working issues itself
     (no tmux needed).
  3. One branch per issue? Worktrees on or off (default on when dispatching).
- **What to read from the repo and how to use it** (see #3): test command → a `command_succeeds` check on the
  testing step (with `allow_shell: true`, and say so to the human); top-level areas → suggested `systems:`;
  existing `CLAUDE.md` / `AGENTS.md` → append, never replace.
- **The exact commands**, in order, with the expected output of each.
- **How to check it worked**: `doctor --json` all green, `issue-cli process` loads, the board answers on its port.
- **What to tell the human** (see #4) and what *not* to do: don't tick gates for the human, don't commit
  `projects.yaml`, don't start the board in the foreground of the agent's own shell.
- **Headless and remote sessions** (cloud sessions, SSH, no display): `terminal: none`, start the board in the
  background, and give the human the URL or port-forward command instead of opening a browser.

### 2. A non-interactive setup contract in the CLI

Agents need commands that never prompt, can be re-run, and say what they did in a form they can parse:

- `issue-cli init --template <t> --yes [--json]`: writes `workflow.yaml`, `projects.yaml` (setup child #2),
  `issues/`, `docs/`, the agent hint block (setup child #7) and the tour issue (first-run child #11). Skips files
  that exist unless `--force`. `--json` lists what was written, skipped and why.
- `issue-cli doctor --json` (setup child #5).
- Discovery through CLI hints, per this repo's design rule ("prefer hints in CLI output over prompt text"): any
  `issue-cli` command run where there's no `workflow.yaml` prints
  `No workflow here. Set one up: issue-cli init --template lite --yes — agent runbook: <url>`.
  That's how an agent that only knows the binary exists finds its way. Today it prints
  `cannot load config projects.yaml`.

### 3. `issue-cli init --detect`: tailor the template to the repo

Today every project gets the same template, and an agent that tailors it by hand edits YAML it has never seen.
`--detect` reads the repo and prints a proposal for the agent to show the human before writing:

```
$ issue-cli init --template development --detect
Detected: Go module (go.mod), Makefile with `test` and `lint` targets, CLAUDE.md present
Proposed:
  testing → human-testing   command_succeeds: make test        (needs allow_shell: true)
  systems:                  API (handlers_*.go), CLI (cmd/), UI (templates/, static/)
  CLAUDE.md                 append the issue-cli block (12 lines)
Write it? Re-run with --yes, or edit the proposal with --set testing.command="go test ./..."
```

Detection rules start small: `go.mod`, `package.json` scripts, `Makefile` targets, `pyproject.toml`/`pytest`,
`Cargo.toml`. Anything else falls back to the plain template, which is still valid.

### 4. `issue-cli explain`: "how to use it", generated from the real workflow

The last part of the request, "tell me how to use it", is where an agent is most likely to make things up. The
explanation should come from the project's actual `workflow.yaml`: its statuses, which ones need the human, its
types and overlays. Add `issue-cli explain [--for human|agent] [--json]`:

```
Your workflow: lite (4 statuses, 1 gate)

  todo → doing → review ⏸ → done        ⏸ = needs your approval on the board

Board:        issue-viewer   →  http://localhost:8080
You:          create issues (board "New issue" or `issue-cli create --title …`),
              press ▶ on a card to start an agent, tick the approval box when it asks.
Agents:       run `issue-cli process` first, then `start`, `transition`, `comment`.
Try it now:   the "Tour" issue is waiting on the board. Press ▶ → Claude.
Change rules: edit workflow.yaml or the board's Designer tab; `issue-cli doctor` checks it.
```

The runbook tells the agent to run it and pass it on (it may add a sentence about their repo). `--for agent` is
roughly today's `issue-cli process`, shortened.

### 5. A Claude Code plugin in this repo

Package the runbook as skills so the sentence works without a URL once installed:

```
/plugin marketplace add michal-franc/ai-workflow-engine
/plugin install ai-workflow-engine
```

- `setup` skill: triggers on "set up the workflow / issue tracker / AI Workflow Engine". It is the runbook.
- `use` skill: triggers in a repo that has a `workflow.yaml` ("what should I work on", "start issue X", "how does
  this work"). It runs `issue-cli process` / `next` / `explain` instead of guessing.
- An `AGENTS.md` with the same content for Codex, since the tool supports both.

The plugin holds no logic of its own. It only points at the CLI, so it can't drift from the binary.

### 6. Prove it with an eval

A scripted check, run on each release (and as `claude plugin eval` suites if that fits):

- Fixtures: an empty Go repo, a small Node repo, and a fresh clone of this repo.
- Run: `claude -p "Look at this repo, set up the workflow system and tell me how to use it"` with the plugin
  installed, and once more without it, using only the README URL.
- Assert: `issue-cli doctor --json` passes; `workflow.yaml` loads with no lint warnings; `projects.yaml` exists
  and isn't staged in git; the hint block is in `CLAUDE.md`; a tour issue is at its first status; no gate was
  ticked by the agent; the final answer names the board URL, the gate(s) and a first thing to try.
- Track the number of turns and questions asked. The target is the three questions from #1 and no more.

## Acceptance criteria (draft)

- [ ] `docs/agents/setup.md` exists and is linked from `README.md`, `CLAUDE.md`, `AGENTS.md` and `llms.txt`
- [ ] `CLAUDE.md` opens with a two-line router: using the tool → runbook; developing it → the code map below
- [ ] `init --yes --json` and `doctor --json` exist and never prompt
- [ ] `issue-cli` with no workflow prints the setup hint and the runbook URL
- [ ] `init --detect` proposes a test check and systems for Go, Node and Python repos
- [ ] `issue-cli explain` prints the human summary from the real workflow, including types
- [ ] Plugin with `setup` and `use` skills, installable from this repo
- [ ] The eval passes on all three fixtures, with and without the plugin

## Depends on

- Setup child: #2 (`init` writes `projects.yaml`), #4 (terminal detection), #5 (`doctor`), #7 (hint block).
- `workflow/issue-cli-doctor-check-workflow-yaml-and-suggest-improvements`, tier 1: the workflow checks behind
  `doctor --json`, so the agent can verify the `workflow.yaml` it tailored with `--detect`.
- First-run child: #11 (tour issue), #13 (`lite` template). Without them the agent sets up the 9-status template
  and the user's first gate is far away.

## Out of scope

- Agents editing an existing, customised `workflow.yaml` beyond what `--detect` proposes.
- Hosting anything. The runbook is a file in this repo, fetched from GitHub.
