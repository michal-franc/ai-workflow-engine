---
title: "Workbench Files: Metrics and Knobs"
order: 8
---

# Workbench files: metrics and knobs

The **Metrics** view (`/p/<project>/metrics`) and the system-page panels show a project's guard metrics and tuning knobs. They read JSON files that the project writes, such as a probe run or a knob export. The viewer never stores values or targets, and it never parses source code: change the files in the repo, and the page reloads.

The same block also points the viewer at the project's own HTML pages: a custom Status page and extra sidebar pages, shown sandboxed. See [Project pages](#project-pages-status_page-pages).

`workbench.release_candidates: true` puts a release candidate before each release. It changes no files and only affects the Status page's Waiting for release card ([UI](UI/overview.md)) and the lead brief. The flow is in [Roadmap: Cutting a release](roadmap.md#cutting-a-release-candidate-soak-publish).

The engine also writes files of its own here: [Health snapshots](#health-snapshots), the project's size and workflow measured each day into `.health/snapshots/`.

A project points at the files in the optional `workbench:` block of its `workflow.yaml`. Every path is relative to the project root. Validation rejects absolute paths and paths that leave the root, and the reader refuses symlinks that leave it and files over 2 MB.

```yaml
workbench:
  metrics:
    probe_results: docs/balance/snapshot.json   # glob allowed: the newest run.date is the master run
    targets: docs/balance/targets.json          # optional: bands kept apart from the results
  tuning:
    - build/workbench/knobs.json                # knob export files
  systems:
    Sim:
      metrics: [full-holds, stage-share, open-*]   # ids, globs allowed
      knobs: [OpeningPrep, Role*]                  # names, globs allowed
      families:
        Play calls:
          umbrella: ai/play-calls
          metrics: [dominant-plays]                # a family's list wins over its system's
```

- **Metrics entry:** with `metrics` or `tuning` set, the sidebar shows **Metrics** after Balance, with a red "N off" when any master metric is off.
- **Branches only:** a git project with neither key still gets the entry, labelled **Branches** and showing only the Branches tab.

## Metric run (`metrics.probe_results`)

```json
{
  "run": {"date": "2026-09-29", "commit": "208225f", "n": 200, "command": "make balance", "dirty": false},
  "metrics": [
    {"id": "stuck", "name": "Stuck fighters in the run", "group": "Headline",
     "band": {"min": null, "max": 0, "unit": "count", "max_exclusive": false},
     "value": 10, "n": null, "verdict": "fail", "probe": "Pacing", "source": "et-core §12"}
  ]
}
```

- **Required fields:** only `metrics[].id` and `metrics[].value`.
  - Without `name`, the id is shown.
  - Without `band`, the Target shows "—" and a banner names the file and the missing fields.
  - A top-level `date`, `commit` and `n` stand in for a missing `run` header, so rust-manager's current `snapshot.json` reads as it is.
- **`value`:** a number, or a string that is shown as written (`"n/a"`).
- **`verdict`:** `pass`/`ok`, `near`, `fail`/`off` or `notmeasured`, in any case. When it is missing, the viewer works it out from the band, as ok or off. Only the file can say "near", because the noise margin is the project's own maths.
- **`band.unit`:** controls formatting only.
  - `pct` and `share` show as `7%`;
  - `count` shows `0.28`;
  - `seconds` shows `132 s`;
  - `ms` shows `268 ms`;
  - `points` shows `+2`.
- **`group`:** splits the table into sections, such as Headline and Slice.
- **The master run:** when the glob matches several files, the newest `run.date` is the master run (ties go to the newest file). Older matches are ignored, not merged.
- **Run age:** the Master run card counts the commits since `run.commit` that touch the knob files' folders, or every commit when there is no knob export ("191 commits to Sim/ since this run").

## Metric targets (`metrics.targets`, optional)

```json
{"metrics": [{"id": "stuck", "name": "Stuck fighters in the run", "group": "Headline", "band": {"max": 0, "unit": "count"}, "source": "et-core §12"}]}
```

- **Merging:** the targets file wins for `name`, `group`, `band` and `source`, and the run supplies `value`, `n` and `verdict`.
- **A target with no run row** is listed as "not measured".

## Branch values

Metrics are matched by `id` alone, with no fuzzy matching, so a rename shows as one metric removed and another added. For each open branch (see [Branches](#branches)), the viewer looks for the branch's own run at the same relative path:

1. **In the branch's worktree.** The file may be uncommitted and newer than anything committed.
2. **Committed on the branch** (`git show <branch>:<path>`), for a branch without a worktree. Its cell is marked "committed".

A branch value is shown only when it differs from master's. Otherwise the cell shows "—".

| Case | What the Branch cell shows |
|:--|:--|
| The run's `run.commit` is older than the branch head | "run N commits behind"; "run from another branch" when the commit is not on the branch |
| The branch's file has a different `band` for the id (and no targets file sets one) | "band ≥ 10% on branch"; the branch verdict is checked against that band |
| An id only in the branch run | its own row, Master "—", and a "branch only" chip |
| A master id missing from a branch run that has other rows | "not measured on <branch>" |

## Knob export (`tuning`)

```json
{"knobs": [{"name": "OpeningPrep", "value": "20", "comment": "stage 1: the defenders' set-up before the clock starts", "file": "Sim/Tuning.cs", "line": 11}]}
```

- **`value`:** printed as given; a number or a boolean is turned into text.
- **`file` and `line`:** where the knob lives in the repo. A knob without them shows no history.
- **History:** one `git log -p` per knob file on the main checkout. A commit counts as a change when its changed lines assign the knob's name (`Name = value` or `Name: value`), giving `30 → 20 · Play calls balance pass 2`. Up to 10 changes are kept.
- **Branch edits:** each open branch's diff against its merge-base, read the same way. These give the Knobs tab's "also edited on N branches" chip.

Generate the export from the code (a probe test, reflection, a script), never by hand, so it matches what the code really does.

## Branches

The Branches tab and the detail page's Shared files row use git only; no file is involved.

- **Open branches:** every git worktree except the main checkout, plus unmerged `work/*` branches whose issue exists and is not done or parked. A branch maps to its issue by name: `work/<slug>`.
- **The base a branch is compared with:**
  1. the open branch its head descends from (it was forked from that branch);
  2. otherwise, main (`origin/HEAD`, then `main`, then `master`).
- **Hot files:** a file changed on two or more open branches. "Hot" means three or more.
- **Shared changes:** a change is marked shared, and hidden by default, when it is not real overlap. That is either:
  - a branch carrying another branch's commits (forked from it): the commits count once, for the branch nearer the fork point;
  - the change of a branch that another branch touching the file is stacked on.
- **Cost:** git calls time out after 5 s, so a slow repo shows "git timed out" rather than hanging the page. Calls keyed by commit sha are cached for the life of the server.

## Dependencies

- **`depends_on`** in an issue's frontmatter lists the issues it waits on. See [Issue File Format](issue-format.md#frontmatter-fields).
- **Decided by** lists the decisions an issue's body cites (`slug#D-n`), with their state from the [decisions index](issue-format.md#decisions).
- **Where they show:** both appear on the detail page, on the Branches tab and in `issue-cli show`.

## Example: rust-manager

rust-manager writes `docs/balance/snapshot.json` with `make balance` (Tests/Balance/Report.cs). The file has ids, values, n and verdicts, but no bands or names, so the view shows its 39 metrics with the "no band" banner. To fill the view:

- **Bands and names:** `make balance` adds `name`, `group` and `band` from `Tests/Balance/Targets.cs`, plus a `run` header.
- **Branch values:** `make balance-check` writes its partial snapshot into the worktree at the same path.
- **Knobs:** a `make knobs` export of `Sim/Tuning.cs` fills the Knobs tab.

## Health snapshots

A project health snapshot is a dated JSON file. Its families record:

- how big the project is and what shape it is in
- how its tests run
- how its workflow is going
- what the newest agent review thought

Every snapshot also gets the derived health score. Snapshots build up into a history read by:

- the Health view (`/p/<project>/health`)
- the Stats page (`/p/<project>/stats`) and `/stats`, which compares the projects
- later, the "getting worse" alerts

`issue-cli health` prints the latest one.

- **Where:** `.health/snapshots/<YYYY-MM-DD>T<HHMM>.json` (local time) in the main checkout. `.health/` writes its own `.gitignore`, so the snapshots stay on this machine and no repo has to ignore them.
- **When:**
  - **Daily:** the viewer looks every 10 minutes and takes one for each project (once per repository) that has none from today.
  - **After CI:** `issue-cli ci run` takes one after each default-tier run, red or green, at the commit it tested. A failed snapshot prints `health: no snapshot: <why>` and never changes the CI result.
  - **By hand:** `issue-cli health snapshot`.
  - **After a review:** `issue-cli health review record` takes one (trigger `review`), so the new opinion shows at once.
  - **The past:** `issue-cli health backfill [--days 90]` computes one snapshot for each past day that has none, as it stood at 23:59 that day: the codebase at the commit the branch had then, the workflow over the issues done by then. Days before the branch's first commit get the workflow rows only; the milestone family only knows the present and is left out. A new project gets its trend lines on day one this way.
- **Who:** every project in a git repository. `health.off: true` turns snapshots off, and a project outside git is skipped.
- **Kept:** every snapshot from the last 35 days, and after that the last snapshot of each day. A snapshot in the same minute replaces the earlier one.

### File

```json
{
  "run": {"date": "2026-10-23T14:05", "project": "issue-viewer", "branch": "main", "commit": "c55d815", "trigger": "daily", "duration_ms": 412},
  "families": {"codebase": {"ms": 380, "metrics": 142}, "tests": {"ms": 4, "metrics": 9, "warnings": ["health.e2e_sessions (default playtest/sessions/*.yaml) matched no file, so no e2e session rows"]}, "workflow": {"ms": 20000, "metrics": 0, "error": "timed out after 20s"}},
  "metrics": [
    {"id": "codebase.lines{kind=code}", "metric": "codebase.lines", "labels": {"kind": "code"}, "value": 61082, "name": "Lines in text files", "group": "codebase", "band": {"unit": "count"}}
  ]
}
```

- **`run.trigger`:** `daily`, `ci`, `manual`, `backfill` or `review`.
- **`families`:** one entry per registered family, with its time, row count and `hint` (see [Adding a family](#adding-a-family)). A family that fails, panics or runs past its timeout gets `error`, and its rows are missing. The other families are still written. `warnings` lists what a family could not measure as configured, such as a glob that matched no file; its rows are still written. `issue-cli health` prints each one under the family's line (`⚠ …`), and the Health view lists them under Derived health.
- **Rows:**
  - `metric` and `labels` are the series key.
  - `id` is `metric{k=v,...}` with the labels sorted, or the bare name when there are no labels.
  - `group` is the family. `name` is the metric's one-line description.
  - `band.unit` is `count`, `seconds`, `share` (0..1) or `bytes`.
  - Rows keep the Metrics view's row shape (`id`, `value`, `name`, `group`, `band`).
- **Names:** `<family>.<snake_case path>`, with snake_case label keys and `all` for a rollup row. Names are stable: a rename breaks every chart drawn from them.

### Config (`health:` in workflow.yaml, all optional)

```yaml
health:
  big_file_lines: 1000                  # a big file has more lines than this
  test_globs: ["**/*_test.go", "**/*Tests.cs"]   # replaces the defaults
  exclude: ["vendor/**", "**/*.min.js"] # left out of the codebase counts
  timeout: 20s                          # per family
  off: false                            # true: no snapshots
  limits:                               # the Stats page gauges' thresholds
    waiting_share: 0.25                 # share of issue time waiting on you
    big_files: 10                       # files over big_file_lines
  e2e_sessions: ["playtest/sessions/*.yaml"]   # the tests family's playtest sessions (default)
  long_func_lines: 80                   # a long function has more lines than this
  shrink_shots_after_days: 30           # shrink playtest shots of issues done this long ago (0: never)
  score:                                # override a derived-score component
    unit_wait: {good: 60s, bad: 240s, weight: 20}
    e2e_coverage: {weight: 0}           # 0 turns a component off
```

- **Globs:** `**` matches any number of folders. A glob without a `/` matches the file name in any folder, as in `.gitignore`.
- **Limits:** a key left out takes its default (above); `0` turns that limit off, and a negative value fails validation. The "getting worse" alerts are meant to share this block.
- **Default test globs:** `**/*_test.go`, `**/*Tests.cs`, `**/*Test.cs`, `**/*.test.js`, `**/*.test.ts`, `**/test_*.py` and `**/*_test.py`.
- **Score bounds:** `good` and `bad` take a number, a duration (`60s`, `10m`: seconds) or a percentage (`5%`: 0.05). See [Scores](#scores).
- **RogueTech:** sets `e2e_sessions: ["Data/Scenarios/*.yaml"]`.
- **Shrinking shots:** once a day, after the daily snapshot, the viewer shrinks the playtest shots in the project root's `.playtest/runs/` whose issue has been done `shrink_shots_after_days` days (default 30; `0` turns it off, a negative value fails validation). Each PNG, in the run's `shots/` or beside its `results.yaml` (a game's runner saves them there), becomes a JPEG at most 720 px wide and three screens tall, cut from the top, and the run's `results.yaml` points at the new names (`screenshot:`, or a game's `screenshots:` list) and gets `reduced: <date>`; every check and verdict stays. It logs one line per project when it changed something (`health: <project>: shrank shots of 4 issues done before 2026-09-23: 15.2 MB → 1.9 MB`). Runs reach the root at done, see [`playtest prune`](CLI/overview.md#playtest-prune).

### The codebase family

It reads git objects at the head of the main branch: one `git ls-tree`, then one `git cat-file --batch`. It never reads the working tree, so uncommitted changes in the main checkout don't count. The branch is `ci.branch`, else the branch checked out in the main checkout. On RogueTech (3,300 files, 160 MB) it takes under a second.

- **Left out:** `exclude` matches, the project's issues dir when it is inside the repo, `.worktrees/` and `.health/`. Symlinks and submodules are skipped too.
- **Binary files:** a file with a NUL byte in its first 8 KB, or a file over 2 MB, is counted in `codebase.binary_files` and nowhere else.
- **Kinds:**
  - `code` is a source language (Go, C#, JS/TS, Python, Rust, shell, CSS, HTML, templates and so on).
  - `test` is a code file that matches `test_globs`. When no code file matches, the family warns "health.test_globs (…) matched no code file", naming the configured globs or "default" and the defaults.
  - `docs` is Markdown, txt, rst and adoc.
  - `data` is JSON, YAML, TOML, XML, CSV and SVG.
  - `other` is any other text file.
- **Languages:** the extension decides the language. Unknown extensions all count as `other`, so the `lang` labels stay a short, stable set.

| Metric | Labels | Value |
|:--|:--|:--|
| `codebase.files`, `codebase.lines` | `kind`: code, test, docs, data, other (all five, 0 when empty) | text files and lines by kind |
| `codebase.files`, `codebase.lines` | `area`: the top-level folder, `.` for files at the root | by area |
| `codebase.files`, `codebase.lines` | `lang`: go, csharp, js, markdown, yaml, ... , other | by language |
| `codebase.source_lines` | none | code + test lines |
| `codebase.test_ratio` | none | test lines / code lines (`share`) |
| `codebase.binary_files` | none | binary files and files over 2 MB |
| `codebase.big_files` | none | files over `big_file_lines` |
| `codebase.big_file.lines` | `path` | the 20 biggest of them, by lines |
| `codebase.growth.files`, `codebase.growth.lines` | `kind`, `window`: 7d or 30d | the change over the window |
| `codebase.growth.base_age_days` | `window` | how old the comparison point actually is, in days |

**Growth** compares the kind totals with the newest snapshot taken at or before 7 (or 30) days ago. When no such snapshot exists, as on a new install, the family measures the tree where the branch stood then:

1. **The branch's reflog**, which is exact.
2. **Commit dates**, when the reflog doesn't go back that far: the newest commit made at or before that date.

A repository younger than the window has no growth rows for that window.

### The workflow family

Where the time goes, from `internal/timeline`: every issue done in the last 30 days goes through the same `Load` and `Split` as its Timeline tab (`?tab=timeline`), so a project number and the tab never disagree. An issue counts when its last transition to `done` in the stats sidecar falls in the window. The time rules (agent, waiting on you, idle, rework, the 15 min gap) are in [Workflow Stats](workflow-stats.md), "Timeline tab". The Stats page shows the same numbers, as charts and tables, from the same aggregate (cached per project).

- **Left out:** issues not done or done earlier, and done issues with no recorded transition to `done` (counted in `workflow.unread`).
- **No CLI calls:** an issue without clilog records counts for `cycle` and `status.total` only, and in `workflow.issues_unsplit`.
- **Labels:** `type` is the issue's `type:` (`none` without one) or `all`. `status` is a workflow status. `transition` is `from→to`. Percentiles are nearest-rank over the issues in the row.
- **Gate failures:** refused `transition` calls need the `exit` and `err_class` that clilog records carry from v0.54 on. `dry_run=true` is the agent checking with `--dry-run` before it asks, as the guidance tells it to; `false` is a real attempt.

| Metric | Labels | Value |
|:--|:--|:--|
| `workflow.issues` | `type` | issues done in the window |
| `workflow.issues_unsplit` | `type` | of those, issues without CLI calls |
| `workflow.unread` | none | done issues with no recorded transition to done |
| `workflow.cycle.p50`, `.p90` | `type` | life from the first dispatch (else the agent's first call) to done, `seconds` |
| `workflow.status.total.p50`, `.p90` | `status`, `type` | time in the status, `seconds` |
| `workflow.status.working.p50`, `.p90` | `status`, `type` | agent working in the status, rework included |
| `workflow.status.waiting.p50`, `.p90` | `status`, `type` | waiting on the owner in the status |
| `workflow.status.idle.p50`, `.p90` | `status`, `type` | idle in the status |
| `workflow.approvals` | `transition` | approvals recorded at the transition |
| `workflow.approval_wait.p50`, `.p90` | `transition` | from the agent's last call in the status to the approval, `seconds` |
| `workflow.rework.loops` | `type` | rework loops: backward transitions and failed playtests from testing on |
| `workflow.rework.backward` | `type` | backward transitions |
| `workflow.rework.share` | `type` | agent time inside rework loops / agent working time (`share`) |
| `workflow.dispatches.p50`, `.p90` | `type` | agent dispatches per issue (recorded from v0.54 on) |
| `workflow.working.p50`, `.p90` | `type` | agent working time per issue, over the issues with CLI calls, `seconds` |
| `workflow.time.working`, `.waiting`, `.idle` | `type` | agent working, waiting on the owner and idle time summed over the issues with CLI calls, `seconds` |
| `workflow.time.share` | `kind`: waiting | waiting on the owner / (working + waiting + idle) over all types (`share`); only when there is split time |
| `workflow.gate_failures` | `transition`, `err_class`, `dry_run` | refused transition calls |

On RogueTech (813 issues, 65 in the window) the family takes about half a second.

### The milestone family

The current milestone, from `internal/milestone` (the same `Pick` the Status page's release card uses): the lowest version after the newest release (the changelog's first version heading, else the newest git tag) that still has issues. Optional statuses such as parked are left out of the total.

| Metric | Labels | Value |
|:--|:--|:--|
| `milestone.done` | `version` | its issues in the final status |
| `milestone.total` | `version` | its issues, side statuses left out |

No rows when no version has issues. It knows only the present, so `issue-cli health backfill` leaves it out.

### The tests family

It runs no tests. It reads two sources:

- **Local CI:** the newest run at or before the snapshot that wrote results for each kind, from `ci.run[].results` ([Local CI: Test results](workflow.md#test-results-results)).
- **Sessions:** the playtest session files at the commit (`health.e2e_sessions`).

Without results there are no rows for that kind, not zeros. `issue-cli health` then says how to add them. When `e2e_sessions` (configured or the default `playtest/sessions/*.yaml`) matches no file, the family warns "health.e2e_sessions (…) matched no file, so no e2e session rows" instead of writing nothing silently.

| Metric | Labels | Value |
|:--|:--|:--|
| `tests.count` | `kind`: unit, e2e; `result`: all, pass, fail, skip | top-level tests in that run (e2e: playtest steps) |
| `tests.subtests` | `kind` | subtests (Go `t.Run`) |
| `tests.wall_seconds` | `kind` | the suite's own wall time (first to last event, or the file's start and finish), not the command's |
| `tests.sum_seconds` | `kind` | per-test time summed; more than the wall when tests run in parallel |
| `tests.per_test_ms` | `kind`, `stat`: mean, p50, p90 | per-test runtime, ms |
| `tests.run_age_days` | `kind` | how old the CI run is |
| `tests.part.seconds` | `kind`, `part` | the 10 slowest parts (Go package, test class, playtest session): the wall clock's critical path |
| `tests.slow` | `kind` | tests over 1 s (unit) or steps over 30 s (e2e) |
| `tests.count_by_area` | `kind`, `area` | tests per area: the package's top folder (Go), the namespace segment after the test assembly's root (TRX), the class's first segment (JUnit) |
| `tests.slowest.seconds` | `kind`, `test` | the 10 slowest tests (`<package>.<Test>` for Go) |
| `tests.e2e.sessions`, `tests.e2e.steps` | `system` (and `all`) | playtest sessions and steps in the tree, by their issue's system |
| `tests.e2e.coverage` | none | issues done in 90 days whose system has a session / issues done with a system (`share`) |
| `tests.e2e.uncovered` | `system` | issues done in 90 days in a system without a session |

The rows below come with `workflow/health-check-speed-…` (the playtest telemetry). Until then the score leaves out the components that read them:

- `tests.e2e.estimated_seconds`
- `tests.e2e.flaky_share{window=14d}`

A project whose test classes share one namespace (RogueTech) gets a coarse area split.

### The shape family

It reads the git objects at the snapshot's commit, in the same single pass the codebase family uses. It also reads one `git log --numstat` over the 30 days before the snapshot. Its source files are code files, tests included; function length counts code only.

| Metric | Labels | Value |
|:--|:--|:--|
| `shape.funcs` | `lang` | functions read |
| `shape.func_lines` | `stat`: p50, p90, max | function length in lines, signature to closing brace |
| `shape.long_funcs` | none | functions over `long_func_lines` |
| `shape.long_funcs.share` | none | of all functions (`share`) |
| `shape.long_func.lines` | `path`, `func` | the 20 longest (`Type.Method` for Go methods) |
| `shape.unparsed_files` | `lang` | code files whose functions could not be read: a Go syntax error, or a language without a reader (Ruby, Lua, F#, ...) |
| `shape.big_lines.share` | none | source lines in files over `big_file_lines` / source lines |
| `shape.churn.lines`, `shape.churn.commits` | `path`, `window`: 30d | the 20 most-churned source files: lines added + removed, commits |
| `shape.churn.big_share` | `window`: 30d | the share of that churn in big files |
| `shape.hotspot` | `path` | big files changed in 5+ commits in 30 days: lines × commits |

How functions are read:

- **Go** with `go/parser`, which is exact.
- **Brace languages** (C#, JS/TS, Java, Kotlin, C/C++, Rust, Swift, PHP, shell, shaders) with a scanner. It skips comments, strings and char literals and counts a block whose header has a name and a parameter list, without a control keyword, an assignment or a lambda. Constructors and local functions count; lambdas and initializers don't.
- **Python and GDScript** by indentation after `def` / `func`.
- **Languages without functions** (CSS, HTML, SQL, templates) are not measured.

On RogueTech the family takes about a second and finds 5,600 C# methods.

### The storage family

How much disk the project takes, by area: one walk of the main checkout, sizes from `lstat` (symlinks are not followed), within the family timeout. It only knows the present, so `issue-cli health backfill` leaves it out and the Stats page's **Disk use** card draws its line from the second daily snapshot on. The card lists the newest snapshot's areas with a bar each.

| Metric | Labels | Value |
|:--|:--|:--|
| `storage.total_mb` | none | the checkout, everything included (MB, 2^20 bytes) |
| `storage.git_mb` | none | `.git/` |
| `storage.worktrees_mb` | none | `.worktrees/`, without the worktrees' `.playtest/` |
| `storage.worktrees` | none | worktrees under `.worktrees/` (count) |
| `storage.playtest_mb` | none | playtest runs and shots: `.playtest/` in the checkout and in every worktree |
| `storage.agent_logs_mb` | none | `.agent-logs/` (clilogs, dispatch prompts) |
| `storage.attachments_mb` | none | the issues dir's `attachments/` (walked on its own when it is outside the checkout) |

On this repo (45 worktrees, 3.2 GB) the walk takes about a quarter of a second.

### The lint family

`issue-cli health lint`'s checks over workflow.yaml and the board (`internal/lint`), counted. Every finding is a warning: lint lowers the score, it never fails a run.

- **The checks:**
  - `undated`: issues without `created:`.
  - `workflow`: the warnings `process workflow` prints.
  - `schema`: unknown keys, missing `statuses:`/`transitions:`, deprecated keys.
  - `template`: the `template:` stamp against the bundled template.
  - `placeholders`: `{{name}}` a field never fills.
  - `sections`: gates on sections no template writes the way the gate needs.

  [CLI: health](CLI/overview.md#health) has each rule.
- **When it runs:** in every snapshot: the one `issue-cli ci run` takes after each run, and the viewer's daily one. It reads the present files, so backfill leaves it out. The Health view also runs lint live: its **Lint** section lists every warning with its fix (50, then the command for the rest). The Status page's Health card shows the snapshot's count (`3 lint warnings`), linking to it.

| Metric | Labels | Value |
|:--|:--|:--|
| `lint.warnings` | none | every warning (the `lint` score component) |
| `lint.findings` | `check` | warnings per check, for checks that found any |

### Scores

After the families ran, every snapshot gets the derived health score. It isn't a family: `Derive` reads the snapshot's rows, and the snapshot from 30 days before for the trend components. Each component maps one measured value linearly from `good` (100) to `bad` (0), clamped. It rewards fast feedback and lean code (D-4): more tests are never a reward, only their cost counts.

| Component | Measured value | good → bad | Weight |
|:--|:--|:--|--:|
| `unit_wait` | `tests.wall_seconds{kind=unit}` | 60 s → 240 s | 20 |
| `unit_cost_trend` | `tests.per_test_ms{kind=unit,stat=mean}` change vs 30 days ago | +10% → +100% | 10 |
| `e2e_wait` | e2e wall (CI run under 14 days old, else `tests.e2e.estimated_seconds`) | 10 min → 40 min | 10 |
| `e2e_coverage` | `tests.e2e.coverage` | 90% → 50% | 10 |
| `flaky` | `tests.e2e.flaky_share{window=14d}` | 0% → 5% | 10 |
| `big_files` | `shape.big_lines.share` | 5% → 30% | 15 |
| `long_funcs` | `shape.long_funcs.share` | 2% → 10% | 10 |
| `test_balance` | change in `codebase.test_ratio` vs 30 days ago (without that snapshot: from the 30-day growth rows); a rise counts as 0 | −0.02 → −0.15 | 10 |
| `hotspots` | `shape.churn.big_share{window=30d}` | 10% → 50% | 5 |
| `lint` | `lint.warnings` (issue-cli health lint) | 0 → 20 | 10 |

- **Missing data:** a component without data is left out and the weights are renormalised over the rest.
- **The total** is written only when at least half the weight has data. Below that the Health view says what is missing.
- **No data at all:** no score rows, not a zero.

| Metric | Labels | Value |
|:--|:--|:--|
| `score.derived` | none | the derived health, 0–100 |
| `score.coverage` | none | weight with data / total weight (`share`) |
| `score.component` | `component` | each sub-score, 0–100 |
| `score.component.value` | `component` | the measured value behind it |
| `score.component.weight` | `component` | its weight; present for every enabled component, so one without a sub-score has no data |

On 23 Oct 2026 issue-viewer scored 86–87 from 6 of 9 components. The `lint` component (24 Oct) makes ten.

### The agent opinion

`issue-cli health review` (or Run review on the Health view) has an agent review the project:

1. It writes an evidence pack, `.health/reviews/<ts>/evidence.md`. The pack holds:
   - the score's components
   - tests per kind, with the slowest parts and tests and tests per area next to lines per area
   - e2e coverage per system
   - the biggest files, hotspots and longest functions
   - lines from the 10 newest retros that mention tests, speed or size
   - the previous review and what its cited metrics are now

   Every number carries its row id.
2. The prompt (`internal/health/review_prompt.md`) asks for:
   - a 0–100 score
   - a 60–200 word assessment
   - 1–6 recommendations, each with a kind, evidence (a row id or a repo path), an effort and the expected effect

   More tests are not a goal.
3. The agent records it with `issue-cli health review record <file>`. That refuses anything whose evidence points at a metric not in the snapshot or a path not at the main head, and lists each problem with its fix.
4. The review is stored as `.health/opinions/<YYYY-MM-DD>T<HHMM>.json`: score, assessment, recommendations, reviewer, the snapshot read and the derived score then. Each recommendation's state is `proposed`, `filed` (with the issue slug) or `dismissed` (with the reason).
5. Only the owner turns a recommendation into work:
   - **Viewer:** File as issue on the Health view; the click is the decision.
   - **CLI:** `issue-cli health review file R2 --quote "<their words>"`.

   Either one creates an `idea` issue labelled `health`, with the evidence and the decision. Its type is `feature` for `add_e2e` and `tweak` for the rest, and its system comes from a `system=` label in the evidence. Dismiss works the same way.

The `opinion` family puts the newest review into every snapshot, so the line holds as a step between reviews. In backfill it returns `ErrNoPast`.

| Metric | Labels | Value |
|:--|:--|:--|
| `opinion.score` | none | the newest review's score |
| `opinion.age_days` | none | its age |
| `opinion.recommendations` | `kind`, `state` | its recommendations |

A review over 7 days old is flagged on the Health view, on the Status card and in `/status.md`. Nothing runs by itself (D-3).

### Adding a family

A family is a Go function registered from `init()` in a package both binaries import (the server and `issue-cli`), so every snapshot includes it, whoever takes it:

```go
func init() { health.Register("workflow", workflowFamily) }

func workflowFamily(ctx context.Context, env health.Env) ([]health.Metric, error) {
	// env: Root (main checkout), Project, Workflow, Config, Branch, Commit, Now, Backfill
	return []health.Metric{{Name: "workflow.status.working.p50", Labels: map[string]string{"status": "all", "type": "all"}, Value: 840, Unit: health.UnitSeconds, Help: "Median agent working time per status"}}, nil
}
```

- **Pointer:** `health.Describe("workflow", "tables: /stats")` (also from `init()`) stores a one-line pointer with the family's result (`families.<name>.hint`). `issue-cli health` prints it after the family's `ok, N metrics`, so agents find where to read more.
- **Errors:** return an error only when the whole family fails. Skip and count the bad parts instead.
- **Warnings:** `health.Warn(ctx, format, args...)` records something the family could not measure as configured (`families.<name>.warnings`), so a setting that matches nothing is said rather than counted as zero.
- **Backfill:** with `env.Backfill` set, `Now` is a past day and `Commit` where the branch stood then (`""` before the first commit). A family that can only measure the present returns `health.ErrNoPast`, and the day's snapshot leaves it out instead of recording an error.
- **Context:** honour `ctx`, because it is cancelled at the family timeout.
- **Docs:** document each name and its labels in a subsection here.

## Project pages (`status_page`, `pages`)

A project can show its own HTML pages in the viewer: a richer status update, a balance report, an ideas page. Projects (and their agents) write them as plain files in the repo. Both keys are optional, and without them nothing changes.

```yaml
workbench:
  status_page: docs/status/index.html   # /status shows this page by default
  pages:                                # extra pages, listed under Status in the sidebar
    - title: "Balance report"
      path: docs/reports/balance.html
```

- **`status_page`:** `/status` shows the page inside the normal viewer frame (sidebar and header). Two tabs sit at the top: **Project status** (the default) and **Built-in** (`/status?view=builtin`), the generated Status page. The built-in page is always one click away, and `/status.md` stays built-in.
- **`pages`:** each entry is an indented row under Status in the sidebar, at `/p/<project>/pages/<n>` (`n` is its place in the list, from 0), in the same frame without the tabs.
- **Paths:** relative to the project root, inside it, ending in `.html` or `.htm`. Each `pages` entry needs a `title`. `issue-cli process workflow` prints a bad key under `== Warnings (workflow.yaml) ==`. A configured file that is missing shows a card in place of the page, naming the key and the fix. On `/status` the card also links to the built-in page.
- **The header** shows the page's path, how long ago it changed and an **Open raw** link.

### Sandbox

A page is arbitrary HTML and its scripts run, but it is kept away from the viewer:

- The frame embeds it as `<iframe sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation">` with **no `allow-same-origin`**. The page gets an opaque origin (`null`), so it can't read the viewer's cookies or storage, or read viewer API responses.
- Links: a link with `target="_top"` may replace the whole tab when clicked (e.g. to an issue). A script can't navigate the viewer on its own. `target="_blank"` opens a new tab.
- The viewer refuses every non-GET request whose `Origin` is `null` (403), so a page can't approve, comment or dispatch.
- `GET /p/<project>/page-raw/<path>` serves the page and its assets. It serves only files under the folder of a configured page (for `docs/status/index.html`, everything under `docs/status/`), with these extensions: html, htm, css, js, json, svg, png, jpg, jpeg, gif, webp, woff2. Responses carry the right `Content-Type`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-cache` and `Content-Security-Policy: sandbox …; frame-ancestors 'self'`, so opening the raw URL directly is sandboxed too.
- Anything else gets a 404: other folders, `..`, symlinks that leave the root, and directories (there is no listing).
- The page's own `fetch("data.json")` comes from origin `null`, and the response allows it (`Access-Control-Allow-Origin: null`).
- Give pages a folder of their own. A page at the project root makes the whole root its folder.

### Refresh

`/hash` returns a `pages` value besides `hash` when the project has pages. It changes when a file under the page folders is added, removed or rewritten (at most 500 files are counted). The frame polls every 2 s and reloads only the iframe when `pages` changes, and the header then reads "changed just now · reloaded". Other views ignore `pages`, so a rewritten report doesn't reload the board.

### Live status (postMessage)

The frame sends the page the built-in Status model, so a page can show live counts without any network access:

```js
{ type: "issue-viewer.status", project: "Atlas", status: { … } }
```

It is sent when the iframe loads, again whenever the issues change (`/hash`'s `hash`), and whenever the page asks with `parent.postMessage({type: "issue-viewer.status.request"}, "*")`. The frame ignores every other message. `status` has this shape (also at `GET /p/<project>/status.json`):

```json
{
  "project": "Atlas",
  "as_of": "2026-10-22T13:39:00Z",
  "open": 3,
  "all": { "done": 1, "total": 4 },
  "versions": [{ "label": "0.9", "done": 9, "total": 12 }],
  "epics": [{ "slug": "ui/atlas-redesign", "title": "Atlas redesign", "status": "in progress", "done": 0, "total": 2 }],
  "active": [{ "slug": "ui/search", "title": "Search as you type", "status": "human-testing", "agent": "stopped" }],
  "waiting": [{ "slug": "ui/route-planner-panel", "title": "Route planner panel", "kind": "gate", "to": "backlog" }],
  "shipped": [{ "version": "v1.4.0", "date": "2026-10-20", "text": "Route planner shows travel time per leg." }]
}
```

`kind` is `gate`, `playtest` or `question`, and `to` is a gate's target status. `text` is the release summary as plain text (markdown stripped, clipped at 140 characters). Lists are empty, never missing.

### Minimal page

`docs/status/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Status</title><link rel="stylesheet" href="style.css"></head>
<body>
  <h1>This week</h1>
  <p>Open issues: <strong id="open">…</strong></p>
  <p><a href="/p/atlas/workboard" target="_top">Open the board</a></p>
  <script>
    window.addEventListener('message', function (e) {
      if (e.data && e.data.type === 'issue-viewer.status') {
        document.getElementById('open').textContent = e.data.status.open;
      }
    });
    if (window.parent !== window) parent.postMessage({type: 'issue-viewer.status.request'}, '*');
  </script>
</body>
</html>
```

The `project-pages` playtest scenario (`playtest/scenarios/project-pages/`) is a fuller example with a sibling stylesheet and script.
