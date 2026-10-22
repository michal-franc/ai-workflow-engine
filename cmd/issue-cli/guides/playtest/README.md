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
```

The rules:

- A step's `id` is a checkbox id from the issue (`issue-cli checklist <slug>`
  shows them, e.g. `TP3`). That is how a result finds its box.
- **A step with `expect:` is a judgement step.** Only judgement steps reach
  the owner. A step without one is machine-only.
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
    viewports: []              # or resolutions: one entry per size, each with its checks, layout, errors, screenshot
    judge:                     # judge runs only
      status: fail
      note: the hover card hides the drop target
      screenshot: …
      checks: []
      opinions: [{ target: board.search, note: too small }]
```

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
