---
title: "Workbench Files: Metrics and Knobs"
order: 8
---

# Workbench files: metrics and knobs

The **Metrics** view (`/p/<project>/metrics`) and the system-page panels show a project's guard metrics and tuning knobs. They read JSON files that the project writes, such as a probe run or a knob export. The viewer never stores values or targets, and it never parses source code: change the files in the repo, and the page reloads.

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
  1. its epic's `base:`;
  2. otherwise, the open branch its head descends from (it was forked from that branch);
  3. otherwise, main (`origin/HEAD`, then `main`, then `master`).
- **Hot files:** a file changed on two or more open branches. "Hot" means three or more.
- **Shared changes:** a change is marked shared, and hidden by default, when it is not real overlap. That is either:
  - a branch carrying another branch's commits (forked from it): the commits count once, for the branch nearer the fork point;
  - the change of a branch that another branch touching the file is stacked on (an epic's base).
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
