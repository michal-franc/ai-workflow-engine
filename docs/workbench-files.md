---
title: "Workbench Files: Metrics and Knobs"
order: 8
---

# Workbench files: metrics and knobs

The **Metrics** view (`/p/<project>/metrics`) and the system-page panels show a project's guard metrics and tuning knobs. They read JSON files that the project writes, such as a probe run or a knob export. The viewer never stores values or targets, and it never parses source code: change the files in the repo, and the page reloads.

The same block also points the viewer at the project's own HTML pages: a custom Status page and extra sidebar pages, shown sandboxed. See [Project pages](#project-pages-status_page-pages).

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
