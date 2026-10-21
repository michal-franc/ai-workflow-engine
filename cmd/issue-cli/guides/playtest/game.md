---
title: "Playtest: a game"
order: 22
---

# Playtest example: a game

This is how RogueTech.X (a Godot game) playtests its screens. Its full
reference is the playtest-sessions page under docs/workflows in that repo,
with code in its Game/Playtest, UI/Testing and tools/PlaytestTool folders. Copy the parts
your game needs. The shared shape is in `issue-cli process playtest`.

## What is different from a web app

- **The game is the runner.** There is no external browser to drive. The
  game takes `--ux-test` or `--playtest` flags plus the session. It reads
  its own controls, moves a real mouse over them, and evaluates checks in
  process.
- **Time is frames, not seconds.** It runs with `--fixed-fps 60`, and every
  wait is a frame budget (`wait_for: { target: x, frames: 300 }`). Running
  out of budget fails the step.
- **Randomness is seeded.** `seed:` seeds campaign generation and each
  battle's random streams, so the same build and session give the same
  result. `REPEAT=2` diffs two runs, including a fingerprint of every unit's
  state, to find where runs split.
- **State is probed, not scraped.** Checks read game state through named
  probes (`credits`, `squad.pilots.0.hp`, `battle.units`) as well as UI
  targets.
- **Rigs fix outcomes.** Dev-only actions such as `rig: { auto_resolve: win }`
  and `rig: { damage_hostile: Head }` make combat results scripted. Every
  use is logged, and they only work under the playtest flags.

## Parts

| Part | In RogueTech.X |
|:--|:--|
| Labels | `TestId.Set(control, TestIds.PostBattle.Accept)`. Ids are listed in `UI/Testing/TestIds.cs` (`#` an index, `*` a segment). Non-controls (map tokens, worlds) implement `ITestTarget` and register themselves. A data table labels its headers and rows from a prefix. |
| Debug API | `GET /api/ui/targets` (id, kind, rect, visible, enabled, text, primary, selected) and `GET /api/state/<probe>`. They show what a session can point at. |
| Scenarios | Named starting states (`galaxy-map`, `battle`, `mechlab`, …) the game boots into from a flag. Each step runs in a fresh process with a throwaway save (`--save-path`). The run fails if the real save changes. |
| ux-test | `make ux-test ISSUE=<slug> [REPEAT=2]`. Runs one process per step under xvfb, at each resolution in `session_checks.resolutions`. Layout rules (`no_clipping`, `no_overlap`, `inside_viewport`, `no_zero_width_text`) run on every labelled control, plus `log: no_errors`. It takes a screenshot per step and resolution. |
| Overlay | `make playtest`. A dev-only cyan layer in the game: a dock (steps, DO / EXPECT / WHY, live checks, a WATCH table of probe values against a snapshot), pulsing highlights with id chips and edge arrows, and hover cards. Alt+P, Alt+F (note plus screenshot), Alt+S and Alt+R, plus opinions on controls. |
| Report | `make playtest-report ISSUE=<slug> APPLY=1` ticks passed Manual Verification boxes and comments failures and opinions through issue-cli. |
| check | `make playtest-check`, part of `make test`. Validates the schema, actions, checks, probes and target ids, with no game running. |

## Actions and checks worth copying

- **Actions:**
  - `wait_for` (a target, or a flow state)
  - `click` and `select` (a real mouse event; refused when disabled, covered
    or off screen)
  - `key`, `wait` (frames), `open` (a screen)
  - `set` (writable probes), `snapshot`, `rig`, an inline `check`
  - map actions that name a role instead of coordinates (`reachable`,
    `hostile`, `ground`)
- **Checks:**
  - `exists` / `absent`, `visible` / `hidden`, `enabled` / `disabled`
  - `primary` (styled as the main action), `selected`
  - `text_equals` / `text_contains`, `no_text` (no visible text matches a
    regex), `count`
  - `state` (a probe equals a value, or every item satisfies a condition)
  - `unchanged` / `changed` against a snapshot, `matches` (two probes with
    the same shape)
  - `never_shown` (a target never appeared during act)
- **A guard against brittle numbers:** an exact `equals:` on a probe that
  depends on combat rolls is an error without `seed:` and a warning with it.
  Compare against a snapshot or fix the outcome with a rig instead.

## Workflow wiring it uses

- **Design:** UI that needs checking gets labels. The Manual Verification
  steps are written as a session with step id = checkbox id, and
  `make playtest-check` must pass.
- **While building:** run `make ux-test ISSUE=<slug>` single runs.
- **Before human-testing:** run `make ux-test ISSUE=<slug> REPEAT=2` once.
- **Human-testing:** the owner runs `make playtest ISSUE=<slug>`, then
  `make playtest-report ISSUE=<slug> APPLY=1`.
- **Bugfixes:** a playtest session reaches the bug's state and checks it, so
  the repro is a session too.
