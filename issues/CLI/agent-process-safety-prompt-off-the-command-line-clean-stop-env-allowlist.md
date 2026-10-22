---
title: "Agent process safety: prompt off the command line, clean stop, env allowlist"
status: "idea"
system: "CLI"
priority: "medium"
type: "feature"
parent: "workflow/harness-checks-the-work-ideas-from-rigone"
created: "2026-10-22"
version: "0.55"

---
RigOne's safety model, the parts that apply to us:

- prompts and secrets go through stdin, never command-line arguments;
- stop and timeout end the whole process group, then verify it is dead, and record the proof;
- child environments are rebuilt from an explicit allowlist;
- two steps that would write to overlapping places are refused before the first process starts.

Where we stand:

- **Prompt on argv.** `LaunchCommand` (`internal/agentrun/agentrun.go`) types
  `claude ... "$(cat <promptPath>)"`, so the whole prompt is in the agent's argv: visible in `ps` to every user on
  the machine, and in long prompts close to `ARG_MAX`.
- **Env via send-keys.** `Start` types `export KEY=value` into the tmux shell, so values land in shell history and
  the pane scrollback.
- **Stop.** `KillTmux` (`internal/agentrun/tmux.go`) kills the tmux session or window and does not check what
  survives. Child processes the agent started (dev servers, test runners) can outlive it.
- **Env allowlist.** Only `command_succeeds` scrubs env (`scrubbedEnv`); agents inherit the full server env.
- **Overlap.** Each worker has its own worktree, but nothing refuses a second agent on the same worktree or branch
  under another session name.

## Ideas

1. Pass the prompt on stdin, or as a file the agent reads, for each agent CLI that supports it; keep argv only as a
   fallback, and say so in the dispatch steps.
2. Put env into the session with `tmux new-session -e KEY=VALUE` (or a 0600 env file sourced once) instead of typed
   `export` lines.
3. Stop: collect the pane PIDs (`tmux list-panes -F '#{pane_pid}'`), kill their process groups, wait, check none
   remain, and record the result in the agent's log dir.
4. Launch env from an allowlist in `workflow.yaml` / `projects.yaml` (PATH, HOME, TERM, the agent's own auth vars,
   plus what the project names).
5. Refuse a dispatch whose worktree or branch is already held by another live agent session, naming that session.

## Decisions

- **D-1** · 2026-10-22 · lead chat: Dispatch moves the prompt off argv to a file or stdin; this replaces the argv launch from the dispatch-prompt fix, delivery must stay verified, "Move it to a file / stdin"

