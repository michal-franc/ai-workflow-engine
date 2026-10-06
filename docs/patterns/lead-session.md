---
title: "The Lead Session"
order: 1
---

A way to run several agents on one project without the human talking to each of them. It is a convention built from `CLAUDE.md`, `workflow.yaml` prompts and frontmatter; the tool doesn't enforce it. Raid League, a game built this way, is the worked example at the end.

## The roles

| Role | Who | Does |
|---|---|---|
| **The human** | You | Decides. Raises ideas, answers questions, approves gates, playtests. |
| **The lead** | One Claude session you start in the project root | Runs the project with you: grooms ideas, dispatches workers from the board, checks on them, relays their questions, reviews designs and diffs, ticks the gates you approved in its chat, fixes the process from the retros. |
| **Workers** | One agent per issue, dispatched from the board | Own one issue in its own tmux window and worktree. Walk it step by step, leave a trail of comments, stop at every human gate. Never tick a gate. |

You talk to the lead; the lead talks to the workers. The board stays the source of truth for status and approvals.

![Who does what: the human, the lead session, the board, worker agents, issue-cli, workflow.yaml and the issue files, and where finished work lands](../images/lead-and-workers.png)

## Setting it up

### 1. Tell each session which mode it is in

Put a short section at the top of the project's `CLAUDE.md`. A session is a **worker** when it was dispatched: `ISSUE_VIEWER_URL` is set, it runs in a tmux session or window named `agent-<slug>`, its working directory is under `.worktrees/`, or its prompt names an issue slug. Otherwise it is the **lead**.

### 2. Write the worker rules

In a doc your `CLAUDE.md` points workers to (Raid League keeps an agents guide under its `docs/`):

- Own your issue only. New ideas become `idea` issues (`issue-cli create`), referenced with `#slug`.
- Stop at human gates. Ask once in chat, then block with `issue-cli transition <slug> --to <status> --wait --timeout 9m`. A "go" in your chat is not an approval.
- Leave a trail: tick boxes as they're done, add `progress:` comments at milestones, and the comments your gates require (`tests:`, `docs:`, …).
- Commit on your own branch as you go; only shipping merges.
- Write a retrospective when you stop (`issue-cli retrospective`).

### 3. Give the lead its channels

The lead reads everything without interrupting anyone:

| Channel | Command | Shows |
|---|---|---|
| Issue | `issue-cli context <slug>` | Status, checklists, comments |
| Branch | `git worktree list`, `git log --oneline main..<branch>` | Code written so far, overlaps between branches |
| Logs | `.agent-logs/<session>/` | Every `issue-cli` call, the full transcript |
| Screen | `tmux capture-pane -p -t <session> \| tail -40` | What the agent is doing or waiting on |

Messaging a worker: `tmux send-keys -t <session> -l "$MSG"`, then `tmux send-keys -t <session> Enter`. Message only to coordinate (an overlap, a decision recorded elsewhere) or when you ask it to.

### 4. Check-ins and stand-ups

While workers run, the lead checks in on a schedule (Raid League uses :07, :27 and :47 past the hour) and reports only what changed: questions waiting, a gate reached, red tests, an idle agent, two branches touching the same files. On request it gives a **stand-up**: one line per dispatched issue with status, last progress and blockers, then the approvals waiting for you.

### 5. Relaying questions

Workers that need a decision post numbered `question:` comments with options and a recommendation, say they're waiting, and stop. The lead brings them to you **one at a time**, with the worker's recommendation and its own. It records your answer under `## Decisions` in the issue, in your words, then messages the worker.

### 6. Letting the lead tick a gate

Approvals are a `human_approval` field in the issue's frontmatter, set by the approve endpoint the board's checkbox calls. If you approve a gate in the lead's chat, the lead can tick it for you:

```bash
# 1. your words on the issue first
issue-cli comment <slug> --text 'approval: "<your words>" (in the lead chat, <date>)'
# 2. then the tick; the endpoint toggles, so check human_approval first
curl -X POST -d '{"status":"<to>"}' localhost:8080/p/<project>/issue/<slug>/approve
```

Decide in `CLAUDE.md` when the lead may do this. Workers never do.

### 7. Before dispatching

- Commit process changes (`CLAUDE.md`, `workflow.yaml`, the worker rules) first: worktrees branch from the checkout's `HEAD`, so workers read whatever is committed.
- Don't dispatch onto a pending gate. If `issue-cli checklist <slug>` shows the issue only waits for your approval, ask for it instead; a worker would claim the issue and stop.
- After a restart (tmux and the board gone, worktrees kept): start the board, check each branch is committed, re-dispatch the workers that were running (the board reuses their worktrees) and brief each one on where it was.

## Umbrella features

A large feature built in many pieces runs as an **umbrella issue** with **child issues**. Children branch from a staging branch and ship into it, never into `main`; the staging branch reaches `main` only when you decide.

![Child branches start from the experiment's base branch and merge back into it; the base branch goes to main only when the human decides](../images/umbrella-branches.png)

- Mark each child with two frontmatter fields: `umbrella: <umbrella slug>` and `base: <staging branch>`.
- Say in each status prompt of `workflow.yaml` how a child differs: build on the base branch and merge the base (never `main`) to pick up a sibling's work; ship into the base branch.
- The board creates worktrees from `HEAD`, so the lead creates a child's worktree from the base branch before dispatching, and the board reuses it:

  ```bash
  git worktree add --no-checkout -b work/<slug> .worktrees/<slug> <base>
  git -C .worktrees/<slug> sparse-checkout set --no-cone '/*' '!issues/'
  git -C .worktrees/<slug> checkout HEAD
  ```

- The lead merges `main` into the base branch now and then so it doesn't drift.
- The umbrella issue runs the workflow too: its build boxes tick when every child has shipped into the base branch, and its final steps take the base branch to `main`.

Parent links and roll-up progress for umbrella issues are on the roadmap; until then this is all convention.

## Worked example: Raid League

Raid League is a football-manager-style game about a fantasy siege tourney, built in Godot and C# by one person and Claude agents since September 2026. It runs this pattern on one board project:

- **235 issues**, a **710-line `workflow.yaml`** with ten statuses and 13 system overlays, and three board buttons (*Balance report*, *Speed sweep*, *Idea sweep*).
- **Four human gates**: discussion → design, design → backlog, starting work, and playtest → documentation. The lead may tick discussion → design once every question was answered by the human and recorded, and any gate the human approves in its chat, always after an `approval:` comment.
- **Workers** run as windows in one shared tmux session for the project, each in a worktree under `.worktrees/` with `issues/` left out.
- **Retros** from every worker are swept by the lead, which fixes the process and files tool issues; swept files move to `retros/archive/`.
- **An umbrella** (a new game mode) stages around 18 child issues on its own experiment branch, which reaches `main` only when the human decides.

![Raid League's ten statuses, each with the check that guards it; four of them need a human](../images/lifecycle.png)

One issue there, replayed from its logs, went from discussion to pushed in 3 h 50 m with four approvals and a few chat answers:

![A worker's issue-cli calls and the human's words from discussion to git push, beside the issue as the human saw it](../images/issue-replay.png)
