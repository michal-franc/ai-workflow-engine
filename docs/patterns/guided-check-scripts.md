---
title: "Guided check scripts"
order: 21
---

# Guided check scripts

Some manual checks can't be [playtest sessions](playtest/README.md): CLI
commands, API routes, tmux and dispatch behaviour, scripts, anything run from a
shell. Without a guided check script, the agent hands the owner a list of
commands to copy, paste and judge. With one, the agent commits
`scripts/checks/<name>.sh` on the issue branch and the owner runs **one
command**:

```
issue-cli check-script <slug>
```

(or presses **▶ Run checks** on the issue page, the Status page or the
issue-approve band). The script walks the owner through each check, one step
at a time. Each step:

- prints its id and the boxes it proves, **what** it checks, **why**, what to
  **look at** and what to **expect**
- runs its commands, showing each command, its output and its exit code
- checks what a machine can (✓/✗) and asks the owner about the rest (y/n)
- waits for Enter before the next step; `q` stops, and everything the script
  started is cleaned up, also on Ctrl-C

The script ends with one line per step and the boxes that passed. In the
owner's run from a terminal it offers to tick those boxes on the issue and to
comment the failures as `checks: …`. The agent reads those comments as test
results, the same way it reads `playtest:` comments.

It is the `guided-check-scripts` workflow feature: `issue-cli workflow
upgrade` proposes it to development-template projects, and `issue-cli init`'s
development template has it.

## Adopting it in a project

For a project lead. Five steps, about ten minutes:

1. **Use an issue-cli that has it** (v0.54.0 or later, or a build of main):
   `issue-cli check-script --lib | head -3` prints the library's header. If
   it says unknown command, update issue-cli first (`make install` in
   issue-viewer).
2. **See the proposal:** the project's Updates tab in the viewer, or
   `issue-cli --project <p> workflow upgrade`. Both list
   `guided-check-scripts` with what it adds, why, where, and a snippet.
3. **Copy the snippet into the project's `workflow.yaml`**, adapted to its
   status names. If the project has no `human-testing` or `shipping` status,
   put those lines in the status where the owner checks or the agent merges.
   The snippet has these parts:
   - a line in the `testing` prompt (agents write the script with `--new`,
     pass `--yes`, never tick the owner's boxes)
   - a line in the `human-testing` prompt (the owner runs
     `issue-cli check-script <slug>`)
   - a line in the `shipping` prompt (remove the script before the merge)
   - a Test Plan `### Manual` box on the transition that starts the work
   - a Shipping box
4. **Record it:** `issue-cli --project <p> workflow scan`, which writes
   `guided-check-scripts@v0.54.0` into the `features:` record, then
   `issue-cli --project <p> process workflow` to see the workflow still loads.
   Run records go to
   `.agent-logs/checks/`, so make sure `.agent-logs/` is in `.gitignore`.
5. **Tell the agents.** New transitions append the new boxes, and running
   agents pick up the prompt lines at their next status. Issues already past
   testing keep their old Test Plan.

That is all a project needs: the library ships with issue-cli, and nothing
else is copied in. Projects with heavier setups (a game, GPU slots, a
server) write their own helpers inside each script, as RogueTech's
`writable-debug-api.sh` does for its game. ▶ Run checks on the board works
for every project the viewer serves. The issue-approve band's Run checks
needs the mod set up for that project, which is a separate follow-up.

## Playtest session or script?

| The check is about | Use |
| --- | --- |
| what a page shows, how it reads, a click in the browser or the game | a playtest session |
| a command's output, an HTTP route, tmux windows, files written, a process started | a guided check script |

An issue can have both. Each Manual box belongs to one of them (a session
step id, a script step's `--box`), so the owner checks nothing twice. The
issue page asks for one thing at a time: the playtest, then the checks, then
the approval. The Status page lists the issue under Playtest and under Checks,
and the board card and the issue-approve band show both.

## Writing one

```
issue-cli check-script <slug> --new <name>   # starts scripts/checks/<name>.sh, one step per unticked Manual box
issue-cli check-script --lib                 # the helper library and its API
issue-cli check-script <slug> --yes          # machine run: pass it before handing over
```

A script is short: a header, the library, then the steps.

```bash
#!/usr/bin/env bash
# issue: cli/issue-cli-dispatch-issue-cli-status      ← check-script finds the script by this line
# shellcheck source=/dev/null disable=SC2034,SC2154
source "${CHECKS_LIB:?run me with: issue-cli check-script <slug>}"
checks_intro "issue-cli status prints the viewer's stand-up. About 2 minutes."

step MV1 "status prints the stand-up" --box HT2 \
  --what "issue-cli status against a sandbox viewer" \
  --why "agents read the board through it instead of raw HTTP" \
  --look "the list of agents" --expect "the worker is listed with its issue"
run issue-cli status
expect '[ $code = 0 ]' "status answered"
ask "Does the stand-up list the worker?"

summary --all HT1
```

The library is built into issue-cli and is never copied into the project.
`check-script` hands it to the script as `$CHECKS_LIB`, together with
`CHECKS_SLUG`, `CHECKS_PROJECT`, `CHECKS_ROOT` (the checkout it runs in; the
library cds there) and `CHECKS_ISSUE_CLI` (the binary that ticks).
`issue-cli check-script --lib` prints the library: its header is the full
API reference.

### Steps and boxes

- `step <id> <title> --box <ids>` names the checkboxes the step proves. A
  box passes only when **every** step naming it passed. Without `--box` the
  box is the step id; `checks_default_box TP2` sets one for every step that
  doesn't name its own.
- The same step id twice adds a second part to one step. It passes only if
  both parts did.
- `summary --all HT1` ticks HT1 only when every step passed, for an issue
  with one box for the whole check (several: `--all HT1,TP3`).
  `summary --then "Approve documentation when you're happy."` is the owner's
  closing line when nothing failed.
- A step with no machine check and no question passes.
- Inside a step: `run <cmd…>` (sets `$out` and `$code`), `expect '<cond>'
  "<meaning>"`, `eventually '<cond>' "<meaning>" [seconds]` (retried, for
  things that happen a moment later), `ask "<question>"`, `skip "<why>"`,
  `show <image>`. `step_active` is true while the step runs (not skipped);
  `step_end` closes it early.

### Machine run and owner run

| | owner run | machine run |
| --- | --- | --- |
| when | stdin is a terminal | `--yes`, or stdin is not a terminal (agents, CI, `yes \| …`) |
| `ask` | asks y/n | marks the step "needs your eyes" |
| `--touches-real` steps | run after an explicit y | skipped |
| Enter between steps | yes | no |
| ticks and comments | offered at the end | never |
| run record | `.agent-logs/checks/<name>/<time>-owner.txt` | `…-machine.txt` |

Only the owner ticks the owner's Manual boxes. An agent passes the machine
run and hands over. The viewer offers ▶ Run checks until an owner run record
exists.

### Safety helpers

These come from the first scripts' mistakes:

- `sandbox_dir [var]`: a temp folder outside any `.worktrees/`, removed on
  exit. Scratch projects inside `.worktrees/` make issue-cli take the caller
  for a worker and refuse dispatch and lead spawn.
- `sandbox_project <name> [template]`: a throwaway project (git repo,
  `issue-cli init`, its own projects.yaml). `XDG_STATE_HOME` and
  `ISSUE_VIEWER_CONFIG` point into it. `sic …` runs issue-cli against it;
  `SANDBOX_ISSUE_CLI` picks the binary, e.g. a build of the branch.
- `private_tmux [--clean]` and `ptmux …`: a tmux server of the script's own
  (a short `TMUX_TMPDIR`, `$CHECKS_TMUX_DIR`, and a marker file). On exit
  `kill-server` runs on that server only. A tmux server and each new session
  take the environment of the client that starts them, and panes inherit it:
  with `--clean` the server and every `ptmux` call run in `clean_shell`'s
  environment, so an agent started in a pane finds the stand-in, not the real
  `claude`.
- `stand_in claude [body]`, `clean_shell <cmd…>` and `expect_stand_in_ran
  claude`: run commands with no profile or rc and a recording stand-in first
  on PATH, then check the stand-in ran. Never use `$SHELL -lic` in a check
  that starts agents: a login shell rereads the profile, and the real
  `claude` gets in front of the stand-in. `stand_in --agent claude` stays
  alive like an agent and appends what it is sent to `claude.input`
  (`expect_stand_in_got claude "<text>"`), so a re-sent prompt never lands in
  a shell.
- `background <var> [clean_shell] <cmd…>` (a sandbox viewer, a server: its
  pid in `$var`, killed on exit), `free_port [var]`, `wait_for_url <url>`.
- `sandbox_project` also takes `--workflow <file>` (a copy instead of `init`)
  and `--name <display name>`; its config is headless (`terminal: "none"`),
  and more settings can be appended to `$SANDBOX_CONFIG`. `sic` goes through
  `clean_shell` once a stand-in exists. `sandbox_git_identity` gives sandbox
  git commands an author and no global config (no signing, hooks or aliases).
- `step … --touches-real "<what it creates>"`: for anything in the owner's
  real environment, such as a window in their tmux session. It lists what it
  creates and runs only after an explicit y. Register its cleanup with
  `on_exit` before creating anything. `step` returns 1 when such a step is
  skipped, so wrap its commands: `if step …; then …; fi`.
- `on_exit <cmd…>`: cleanup that runs on exit, `q`, Ctrl-C or a kill, last
  registered first.

The sandbox variables are exported for the rest of the script. Ticking the
real issue at the end runs with the environment the script started with.

### Gotchas

- `run` runs its command in a subshell to capture the output. Call helpers
  that change the script's state (`sandbox_dir`, `sandbox_project`,
  `private_tmux`, `on_exit`, `cd`, `export`) directly, never through `run`.
- shellcheck can't see that `sandbox_dir build` assigns `build`, and sees
  `$out`/`$code` used only inside `expect` strings: declare sandbox variables
  first (`build=`) and put `# shellcheck source=/dev/null
  disable=SC2034,SC2154` above the source line (`--new` writes it).
- A viewer writes `viewer.json` under `XDG_STATE_HOME`: start sandbox viewers
  after `sandbox_project`, which moves it into the sandbox, or the owner's
  real `viewer.json` is overwritten.
- Read anything you need from the owner's real environment (their tmux
  session, `$TMUX`) before `private_tmux`, and only in an owner run.
- Don't `set -e`: a failing check is recorded against its step, not a reason
  to stop the script.

## Where scripts live

Scripts stay **on the issue branch**. They are committed there for the owner
to run from the worktree, and a last commit removes them before the merge, so
main never collects them. The Shipping section has a box for it. The library
is versioned with issue-cli, so a branch's script runs with the library of
the issue-cli that runs it.

## How it is wired

- `issue-cli check-script <slug> --list` lists the issue's scripts (exit 1
  when there are none). `show`, `start` and `transition` print a **Guided
  checks** block at testing and human-testing when the workflow uses the
  feature.
- The viewer: when an issue at human-testing has a script with no owner run,
  the Needs-you band shows **▶ Run checks** and the command, the Status page
  lists it under Checks, and its card carries a "run checks" marker.
  ▶ Run checks starts the owner's run in a tmux window named
  `check-script-<slug>` in the issue's worktree: a window in the project's
  shared session, or its own session, opened like an agent (`terminal:`). A
  second press brings back the running window.
- The issue-approve mod probes `check-script <slug> --list` at start and
  after each turn, and its ▶ Run checks asks the viewer to do the same.

## Example

RogueTech's `scripts/checks/writable-debug-api.sh` (the first one) drives a
game on an invisible screen: it starts the game, sends debug-API actions,
saves screenshots for the owner to judge, and stops the game and its GPU
slot on exit. issue-viewer's first three (issue-cli prove, dispatch and
status, the lead tab) are kept as library examples under
`internal/checks/testdata/ported/`.
