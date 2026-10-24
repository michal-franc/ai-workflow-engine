---
title: "Playtest sessions"
order: 20
---

# Playtest sessions

A playtest session is an issue's manual checks, written once as a YAML file
and run two ways:

- **ux-test**: a machine runs every step's checks with no human, the same
  way every time. Agents run it before human-testing.
- **playtest**: the owner sees only the steps a machine can't judge (look and
  feel, whether something reads well), on the real UI, in an overlay. Each
  step has Pass, Fail or Skip and notes.

A **report** then ticks the issue's boxes that passed and posts each failure
and opinion as a comment.

Each project builds its own playtest system, because a web app, a game and a
CLI are driven differently. What projects share is the shape below. Two
worked examples show how to build one:

- `issue-cli process playtest web`: a web app driven by headless Chrome
  (issue-viewer's own board).
- `issue-cli process playtest game`: a game engine, after RogueTech.X's
  in-game playtest.

Checks that aren't on a screen (CLI commands, API routes, tmux, scripts) are
not playtest sessions: they come as a guided check script,
`scripts/checks/<name>.sh`, run with `issue-cli check-script <slug>` (start
one with `--new <name>`; the helpers: `issue-cli check-script --lib`). See
docs/patterns/guided-check-scripts.md.

## The shape every project shares

### A session file

```yaml
session: board-basics          # name; usually the file name
issue: ui/some-issue-slug      # the issue whose checkboxes the steps tick
title: One line for reports and the overlay
scenario: basic                # the known starting state every step uses
seed: 4127                     # optional: anything random is seeded
session_checks: {}             # run-wide checks: layout, console/log errors, sizes

steps:
  - id: TP3                    # the issue's checkbox id (issue-cli checklist)
    title: One line
    from: TP2                  # optional: replay TP2's setup and act first
    setup: []                  # actions that bring the step to its state (both modes)
    act: []                    # the actions under test: ux-test does them, the owner does them in a playtest
    checks: []                 # machine checks, evaluated after act
    targets: []                # ids the overlay highlights: id or {id, note}
    do: What the owner does    # a step with expect: is a judgement step
    expect: What the owner should see
    why: Why it matters
    proves: [AC2]              # optional: the acceptance criteria this step proves
```

The rules:

- A step's `id` is a checkbox id from the issue (`issue-cli checklist <slug>`
  shows them, e.g. `TP3`). That is how a result finds its box.
- **A step with `expect:` is a judgement step.** Only judgement steps reach
  the owner. A step without one is machine-only.
- `proves:` lists the acceptance criteria the step shows working (ids from
  the issue's `## Acceptance Criteria`). Copy it into the step's result, so
  `issue-cli prove` finds the link with no extra call; the step's latest
  verdict is the link's check. Reject ids that aren't the issue's criteria.
- Every step starts fresh from its scenario, so steps never depend on the
  order they ran in, and any step can be replayed alone.
- The actions and checks are each project's own vocabulary. Name them after
  what your app has (a web app clicks targets; a game also has probes and
  rigs).
- Check shape as well as content. A screen whose styles failed to load
  still has all its text, so give the vocabulary a way to say where things
  are and that styling applied (the web runner's `beside`, `below`, `size`
  and `styles_applied`; a game's `primary`).

### A run's results.yaml

Keep every run in a gitignored folder,
`.playtest/runs/<session>/<time>-<mode>/`, holding `results.yaml`, logs and
screenshots:

```yaml
session: board-basics
issue: ui/some-issue-slug
mode: auto                     # auto (ux-test) or judge (playtest)
build: 3f2a1c9+ab12cd34        # the commit, plus a hash of local changes
started: 2026-10-21T12:54:53+02:00
steps:
  - id: TP3
    title: …
    judgement_step: true
    status: pass               # pass | fail | skip | pending
    proves: [AC2]              # copied from the session step, when it has one
    duration_ms: 4210          # the step's time at every size, setup included (REPEAT ignores it)
    viewports:                 # or resolutions: one entry per size, each with its checks, layout, errors, screenshot
      - viewport: 1440x900
        status: pass
        screenshot: shots/TP3-1440x900.png   # relative to the run folder
    judge:                     # judge runs only
      status: fail
      note: the hover card hides the drop target
      screenshot: shots/TP3-fail.png
      checks: []
      opinions: [{ target: board.search, note: too small }]
reduced: "2026-11-22"          # set when the shots were shrunk (below)
```

- **Paths:** `screenshot:` is relative to the run folder, so a run that moves still finds its shots. A reader resolves
  a shot as `shots/<file name>` next to `results.yaml`, which also covers older runs with absolute paths.
- **Own sessions only:** an auto run keeps the shots of the run's own issue's sessions. The own issue is `-issue`, else
  the `work/<slug>` branch the repo is on. Another issue's session keeps its `results.yaml` and the shots of failing
  sizes, nothing else, and ux-test prints "shots kept for <issue> only" once. Judge runs keep every shot. With no own
  issue (on main) every shot is kept.
- **At done:** `issue-cli transition <slug> --to done` keeps the latest auto and judge run of each of the issue's own
  sessions (a game's `ux-test` / `playtest` count as auto / judge), moves them to the project root's `.playtest/runs/`
  and deletes the rest of the worktree's runs and the runner's cached binaries (`runs/.bin`), so removing the worktree
  keeps the evidence. An own run is deleted only when a newer run of its session and mode is kept; the run a kept
  `-cached` run takes its shots from, an own run of another mode and a run it can't classify are kept too. It prints
  `archived N of M runs, deleted K` (a ⚠ when none of the issue's M runs was archived).
  `issue-cli playtest prune <slug> | --all [--dry-run]` does the same for worktrees that already exist (an open issue
  only loses other issues' runs).
- **Reduced:** the viewer's daily health pass shrinks the shots of issues done `health.shrink_shots_after_days` ago
  (default 30, `0` off), in `shots/` or beside `results.yaml`, to JPEGs at most 720 px wide and three screens tall,
  and sets `reduced:`. Every check and verdict stays.

#### What the viewer reads

The issue page's Evidence and Timeline tabs, and the proof links, read every `results.yaml` whose `issue:` is the
issue, in the project checkout's and the issue worktree's `.playtest/runs/`. They read two schemas, converted to one
when the file is loaded:

| | This repo's runner (above) | A game's runner (RogueTech.X) |
|:--|:--|:--|
| `started:` | RFC 3339 with an offset | also local time with no offset (`2026-10-24T11:04:20`) |
| `mode:` | `auto` / `judge` | `ux-test` / `playtest` (read as `auto` / `judge`) |
| Judgement step | `judgement_step: true` | `judgement: true` |
| Your verdict | `judge.status` | `verdict:` on the step |
| Screen sizes and shots | `viewports[].viewport`, `.screenshot` | `screenshots: [MV3@1920x1080.png]`: one view per `@WxH` in a name or `layout[].resolution` |
| Checks | `viewports[].checks[]`: `check`, `pass`, `actual` | `checks[]` on the step: `kind` + `subject`, `passed`, `reason` (on every view) |
| Layout | `viewports[].layout[]`: the broken rules | `layout[]`: every rule per `resolution`, `passed`, `failures[]` |
| Shot file | `shots/<name>` in the run folder | `<name>` beside `results.yaml` |

Also read: `id`, `title`, `status`, `proves`, `session`, `issue`, `build`, `reduced`. A run whose `started:` or YAML
can't be read is listed at the top of both tabs, not dropped silently.

- **Shots:** each step a criterion points to (`proves:` or a link) gets its shot from the newest run that has the shot
  file, so a run that saved none (a game's `-cached` replay) is passed over. When no step proves a criterion, the
  Shots card shows every step of each session's latest ux-test run instead. Add `proves: [AC…]` to the session steps
  so shots line up with criteria.

### The e2e results file (for local CI)

When `CI_RESULTS_DIR` is set (a local CI run, see [Local CI: Test results](../../workflow.md#test-results-results)), ux-test also writes `$CI_RESULTS_DIR/e2e.json`, or the path given with `-results`. It covers every session it ran:

```json
{"started": "2026-10-23T18:00:00Z", "finished": "2026-10-23T18:03:10Z",
 "sessions": [{"session": "board-basics", "issue": "ui/some-issue-slug", "duration_ms": 60000,
               "steps": [{"id": "TP3", "status": "pass", "duration_ms": 20000}]}]}
```

A `ci.run` entry `{kind: e2e, format: playtest, file: e2e.json}` feeds it to the health tests family. Another project's runner (RogueTech's GPU ux-test) can write the same shape. A step is one test, a session one part, and the issue slug's first segment the area.

### Four commands

| Command | What it does |
|:--|:--|
| check | Validates every session with no app running: schema, action and check names, targets that exist, scenarios that exist, step ids that are checkboxes in the issue. Part of the project's test target. |
| ux-test | Runs the machine checks. Exits non-zero on any failure. `REPEAT=2` runs twice and fails on any difference. |
| playtest | The owner's judgement steps, in an overlay on the real UI: highlighted targets, DO / EXPECT / WHY, live checks, Pass / Fail / Skip / Replay, opinions on elements. |
| report | Prints what the last runs mean for the issue. With `APPLY=1` it ticks the boxes that passed with `issue-cli check <slug> <ids…>`, and posts each failure and opinion with `issue-cli comment <slug> --text "playtest: …"`. |

The report ticks a box when:

- **Machine step:** its ux-test passed.
- **Judgement step:** the owner passed it, and its machine checks (if any)
  passed too.

The report posts each failure and each opinion as a comment, only once.

### Wiring it into the workflow

Add these to the project's `workflow.yaml`. The bundled `development`
template has opt-in wording to adapt.

- **In progress, Test Plan:**
  - "Playtest session covers the Manual steps (step id = checkbox id) and
    `make playtest-check` passes (or N/A with reason)".
  - New UI elements a step points at get a test id.
- **Testing:** "`make ux-test ISSUE=<slug> REPEAT=2` passes, run once before
  handoff (or N/A with reason)".
- **Human-testing prompt:** the owner runs `make playtest ISSUE=<slug>`, then
  `make playtest-report ISSUE=<slug> APPLY=1`.

## Building one for your project

1. **Label what steps point at.** Give every element a stable id
   (`page.area.element`), listed in one file. Add a test that fails when the
   UI and that list drift apart.
2. **Make scenarios.** A scenario is a named starting state that a step can
   rebuild from nothing: fixtures, an isolated config, temp state, and a
   fixed seed and clock where it matters. The runner owns the isolation
   (separate HOME, config, sockets and ports), so a session can't get it
   wrong.
3. **Write check** first. It is cheap, and it catches most mistakes.
4. **Write ux-test.** For each step, start a fresh scenario, run `from`, then
   `setup`, then `act`, then the checks. Wait for conditions with a budget
   and never sleep blindly. Take a screenshot per step and size. Dump the
   available targets into a failing step's log.
5. **Write the overlay.** Show it only for judgement steps. Inject it from
   outside, so the product ships without it. Implement the checks once and
   use them both in ux-test and live in the overlay.
6. **Write report** on top of `issue-cli check` and `issue-cli comment`.
7. **Add make targets** (`playtest-check`, `ux-test`, `playtest`,
   `playtest-report`) and wire the workflow as above.
