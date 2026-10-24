---
title: "Workflow"
order: 3
---

## Issue Lifecycle

Every issue follows this flow from left to right on the board:

1. **Idea** — new feature or bug lands here. Just a title and rough description is enough.
2. **In Design** — flesh out the idea: requirements, approach, edge cases.
3. **Backlog** — design is done, ready to be picked up for implementation.
4. **In Progress** — actively being worked on.
5. **Testing** — implementation done, verifying it works correctly.
6. **Documentation** — update docs to reflect the change.
7. **Done** — shipped and documented.

## Optional Statuses

A status can be marked `optional: true` in `workflow.yaml`. Optional statuses remain part of the lifecycle but can be skipped on forward transitions — useful for parking states (e.g. `waiting-for-team-input`) that only apply when a specific condition fires.

```yaml
statuses:
  - name: "in progress"
  - name: "waiting-for-team-input"
    optional: true
    description: "Parked — blocked on another team (skip if not blocked)"
  - name: "testing"
```

With this config, `in progress → testing` is valid (skips the optional status). `in progress → waiting-for-team-input` is also valid (ordinary forward step). Backward transitions into an optional status still require an explicit `transitions:` edge. See [Workflow Overview](Workflow/overview) for full semantics.

If a transition into an optional status also has `require_human_approval`, the detail view hides the approval checkbox behind a CTA button so the default (required) next step stays unambiguous. Customise the button text with `cta_label` on the transition:

```yaml
transitions:
  - from: "in progress"
    to: "waiting-for-team-input"
    cta_label: "Block on another team — park until they respond"
    actions:
      - type: "require_human_approval"
        status: "waiting-for-team-input"
```

When `cta_label` is unset, the button falls back to `Divert to <status> — <description>`.

When a CLI transition fails on a missing approval, the error includes a deep link to the corresponding approve button (`/p/<project>/issue/<slug>#approve-<status>`). See [Agent Dispatch — Approval-gate Deep Links](agent-dispatch.md#approval-gate-deep-links) for the URL contract.

## Updating Status

Edit the `status` field in the issue's frontmatter:

```yaml
status: "in progress"
```

The board view updates automatically on page refresh.

## Workflow Prompts

Workflow YAML supports two different prompt concepts:

- `statuses[].prompt`
  Baseline guidance for working in that status. This is shown when an agent starts work in the status or enters it from the previous status.
- `transitions[].actions[].type: inject_prompt`
  Additional transition-specific guidance. This is emitted only for that exact `from -> to` move.

Use `status.prompt` for persistent stage expectations, for example:

```yaml
statuses:
  - name: "in design"
    description: "Being designed and specced out"
    prompt: |
      Review the relevant docs and code before proposing changes.
      Turn requirements into explicit checklists.
      Surface assumptions, open questions, and required human input clearly.
```

Use `inject_prompt` for extra context that should only apply to one transition, for example:

```yaml
transitions:
  - from: "idea"
    to: "in design"
    actions:
      - type: inject_prompt
        prompt: |
          Focus on unresolved scope and design questions from the raw idea.
```

In practice:

- `status.prompt` defines how an agent should behave while in a status
- `inject_prompt` adds extra guidance triggered by a specific transition

For the full end-to-end agent flow from dispatch through approvals, retrospectives, and bug reporting, see [Agent Workflow Flow](agent-workflow-flow).

### Lead brief (`leads:`)

A top-level `leads:` block sets the brief the [project lead](patterns/lead-session.md) is spawned with by `issue-cli lead spawn`:

```yaml
leads:
  project: |
    You are the project lead of {{project}}, running in {{lead}}.
    Dispatch workers from the board and relay their questions to the human one at a time.
```

- Placeholders: `{{project}}` and `{{lead}}` (the lead's tmux name). `issue-cli` commands get `--project` injected.
- A missing or empty `project` uses the built-in default brief, so a workflow without `leads:` still gets a working lead.
- The CLI always appends a footer: role, what the lead may tick, and its handoff note under `sessions/`.
- An old `epic` key still loads but is ignored; `issue-cli process workflow` warns about it.
- `leads:` is project-wide: type, system and per-issue overlays keep it as it is.

## Side-Effects

Statuses support a `side_effects` field — actions that run automatically after entering a status:

- `clear_assignee` — clears the assignee field

```yaml
- name: backlog
  side_effects: [clear_assignee]
```

This is used so design agents are unassigned when an issue moves to backlog, before a different agent picks it up for implementation.

## Validation Rules

Each `transitions[].actions[]` entry of `type: validate` runs one rule against the issue. There are two flavors of rule encoding:

- **Legacy colon-string** — the rule and (optional) argument live in a single `rule:` string, e.g. `rule: "section_checkboxes_checked: Design"`. Used by the original 10 rules: `body_not_empty`, `has_checkboxes`, `section_has_checkboxes`, `has_assignee`, `all_checkboxes_checked`, `section_checkboxes_checked`, `has_test_plan`, `has_comment_prefix`, `approved_for` / `human_approval`, and by `criteria_checkboxes` and `criteria_proven`.
- **Structured action params** — the rule name in `rule:` plus companion fields on the same action (e.g. `field`, `values`, `pattern`, `section`, `min`, `max`, `command`, `ref_key`, `linked_status`, `hint`). All new validators use this form.

Both flavors can mix freely in the same workflow. New validators should use the structured form for clarity; existing legacy rules keep working unchanged.

### Frontmatter validators

```yaml
- type: validate
  rule: field_in
  field: priority
  values: [low, medium, high, critical]
  hint: "set with: issue-cli set-meta {{slug}} --key priority --value high"
```

- `field_present` — frontmatter `action.field` exists, regardless of value.
- `field_not_empty` — frontmatter `action.field` exists and is non-blank.
- `field_in` — frontmatter `action.field` value is one of `action.values`.
- `field_matches` — frontmatter `action.field` value matches the Go RE2 regex `action.pattern` (no backreferences or lookarounds).
- `has_label` — issue labels contain the name in `action.field`.
- `has_any_label` — issue has at least one label.

### Linkage validators

```yaml
# Block shipping until the parent issue is done.
- type: validate
  rule: linked_issue_in_status
  ref_key: parent
  linked_status: done
```

- `has_pr_url` — frontmatter `pr` is a github pull request URL (`https://github.com/<org>/<repo>/pull/<N>`).
- `linked_issue_in_status` — the issue referenced by `action.ref_key` (a frontmatter key whose value is another issue's slug) has the status `action.linked_status`.
- `children_closed` — every child of the issue (the issues naming it in `parent:`) is in a closing status: `values:` when set, else done, parked or obsolete. An issue with no children passes. The `epic` type uses it on in progress → done.

### Body-structure validators

```yaml
- type: validate
  rule: section_min_length
  section: Design
  min: 200
- type: validate
  rule: no_todo_markers
```

- `has_section` — body contains `## <action.section>`.
- `section_min_length` — section `## <action.section>` has at least `action.min` non-whitespace chars.
- `section_max_length` — section `## <action.section>` has at most `action.max` non-whitespace chars (a missing section passes; pair with `has_section` if presence matters).
- `no_todo_markers` — body contains no whole-word `TODO` or `FIXME` (case-sensitive).

### Checkbox validators

- `has_checkboxes` — body contains at least one `- [ ]`/`- [x]` checkbox anywhere.
- `section_has_checkboxes: <Title>` — the `## <Title>` section exists and contains at least one checkbox. **Existence only** — the boxes need not be checked here; they are typically verified at a later transition. This is how the `Acceptance Criteria` section gates `in design → backlog`: it must be present and non-empty, but its boxes are checked during testing.
- `all_checkboxes_checked` — every checkbox in the body is checked.
- `section_checkboxes_checked: <Title>` — the `## <Title>` section must exist with at least one checkbox, and **every** checkbox in it must be checked. A missing or checkbox-less section **fails** (it does not pass vacuously) — this stops an issue imported without a templated section from transitioning with none of that section's checklist content. In the normal flow each gated section is appended by the prior transition's `append_section`, so it always exists when the gate runs.

- `criteria_checkboxes: <Title>` — every list item in `## <Title>` is a checkbox, and none still holds the text the workflow's `append_section` wrote there. Used on `Acceptance Criteria` at the design gates.
- `criteria_proven[: since YYYY-MM-DD]` — every `Acceptance Criteria` box has a proof link that passes its check. Issues created before the date, and issues without criteria, pass. See [Proof Links](Workflow/proof.md).

`process transitions` annotates these in its output: `section_checkboxes_checked` rows read "(gates this transition)" and `section_has_checkboxes` rows read "(existence only — contents verified later)".

### Shell / external

`command_succeeds` runs a shell command and passes when the exit code is 0. It is **opt-in** — set `allow_shell: true` at the top of `workflow.yaml` to enable it; otherwise every `command_succeeds` action fails with a fix-it hint.

```yaml
allow_shell: true

transitions:
  - from: shipping
    to: done
    actions:
      - type: validate
        rule: command_succeeds
        command: "gh pr view {{number}} --json state -q .state | grep -q MERGED"
        timeout_seconds: 15
        hint: "land or close PR #{{number}} before marking done"
```

- The command is run via `/bin/sh -c <command>` with `text/template` substitution of `{{slug}}`, `{{number}}`, `{{repo}}`, `{{system}}` from the issue's frontmatter.
- Working directory is the project's issue root.
- Environment is scrubbed to `PATH`, `HOME`, `GH_TOKEN` only.
- Timeout: `timeout_seconds` (default 10s). On non-zero exit or timeout, captured stdout/stderr (truncated to 400 chars) is included in the failure message.

### Failure hints

Every validator returns a failure message of the form `<problem> — <hint>`, where `<hint>` is a concrete `issue-cli` command bots can run to fix the gate. Set `action.hint` on any structured validator to override the default hint with project-specific guidance (the override is templated with the same `{{slug}}/{{number}}/{{repo}}/{{system}}` vars as `command_succeeds`).

## Scoring

The `scoring` block in `workflow.yaml` turns on a score that ranks issues by urgency + importance + staleness. Scores are computed server-side on every page load from frontmatter — no cache, no background job — and surface in three places:

- Board cards: a small `⚡ N` badge in the corner, color-graded (green → yellow → orange → red)
- List view: a score chip on each row and a `Sort: score ↓` toggle at the top
- Detail view: a sidebar breakdown showing each component's contribution

The block is opt-in; omit it or set `enabled: false` to hide all scoring UI.

### Formula

```
score = priority_weight
      + min(overdue_cap, max(0, 30 - days_until_due) * urgency_weight)
      + age_days * staleness_weight
      + sum(label_weights for each label on the issue)
      + score_boost          # optional manual override
```

Missing fields contribute 0 — an issue with no `priority`, no `due`, and no `created` simply scores 0 (unless `score_boost` is set).

### Frontmatter fields that participate

| Field         | Role                                                                  |
|:--------------|:----------------------------------------------------------------------|
| `priority`    | Looked up in `scoring.formula.priority`. Frontmatter values are normalized to lowercase before lookup, so map keys should also be lowercase (`p0`, `critical`) — uppercase keys still match via a case-insensitive fallback but lowercase is the canonical form |
| `due`         | `YYYY-MM-DD` or RFC3339 date. Triggers urgency under a 30-day horizon |
| `created`     | Accumulates staleness at `staleness_weight` per day since that date    |
| `labels`      | Summed against `scoring.formula.labels`; unlisted labels add 0         |
| `score_boost` | Integer added as a separate component. Negative values work            |

### Example config

```yaml
scoring:
  enabled: true
  formula:
    priority:
      critical: 40
      high: 20
      medium: 10
      low: 0
    due_date:
      urgency_weight: 2     # points per day under the 30-day horizon
      overdue_cap: 60       # max from the due-date term (keeps ancient overdue from dominating)
    age:
      staleness_weight: 0.1
    labels:
      bug: 5
      blocker: 25
      enhancement: 0
  default_sort: score_desc   # initial sort on list + board; overrideable via ?sort=
```

### Sorting

With scoring enabled:

- `default_sort: score_desc` makes the list and board sort by score descending on first load.
- Append `?sort=score` to any list or board URL to force score ordering regardless of the default.
- Drop the query param (or use `/?sort=` cleared) to return to the original order (most-recently-modified first on list, insertion order per column on board).

Existing URL filters (`?status=`, `?system=`, `?label=`, etc.) compose with `?sort=`.

## Local CI

Agents run `make validate` on their own branch before they fast-forward main. Two branches that are green on their own can still break main together, and nobody runs the slow checks. Local CI re-checks the integration branch after every move and tells the system when it goes red. It is opt-in per project: without a `ci:` block nothing runs and nothing shows.

### Config

```yaml
ci:
  branch: main          # default: the branch checked out in the main checkout
  timeout: 20m          # for the whole run; default 10m
  nice: 10              # default 10, so CI doesn't slow the agents; 0 = off
  on_red: mark          # mark (default) or file_issue
  run:                  # in order; the first failure stops the run
    - make validate                    # a plain string = a default-tier command
    - cmd: make ux-test
      tier: slow                       # never on merge: issue-cli ci run --tier slow
      failures: '--- FAIL: (\S+)'      # optional: group 1 names a failing test
      results:                         # optional: test results the command writes
        - {kind: e2e, format: playtest, file: e2e.json}
```

- `ci:` is project-wide: system and type overlays can't change it.
- `issue-cli process schema` documents every key, and `process workflow` lints the block. It flags an empty `run`, a bad timeout, an unknown `on_red`, a `failures` regex without a group, and a block with only slow commands.
- Commands run with `sh -c` in their own session: their own process group, so a timeout kills everything they started, and no controlling terminal. The detached runner lives in a tmux window, so without that a command that starts an interactive shell (`bash -i`, `$SHELL -lic`) would stop on the window's tty until the timeout; with it the shell says "no job control" and carries on, as in any CI. They get `CI=true` and `CI_RESULTS_DIR` (see [Test results](#test-results-results)). `ISSUE_VIEWER_*`, `AGENT_NAME` and `TMUX` are removed, so issue-cli inside a test never thinks it is an agent and never reaches the real tmux server.
- The block does not need `allow_shell`: it is its own opt-in, and its commands are the project's own.
- This repo runs `make validate` on every move of main, with `make ux-test` as the slow tier. Both declare their test results.

### Test results (`results:`)

A command can name the machine-readable test results it writes, so the [health tests family](workbench-files.md#the-tests-family) can count tests and time them without running anything again:

- **Where:** each command gets `CI_RESULTS_DIR`, an absolute folder `.ci/results/<sha>[.<tier>]/`, emptied when the run starts. The command writes its files there, and the entries name them.
- **Entry:** `{kind, format, file}`.
  - `kind`: `unit` or `e2e`.
  - `format`:
    - `go-test-json` (`go test -json`)
    - `trx` (`dotnet test --logger trx`)
    - `junit` (pytest, jest and most runners)
    - `playtest` (the e2e results `ux-test -results` writes)
  - `file`: a plain file name in the folder. `process workflow` lints all three.
- **After the command,** red or green, local CI parses each file:
  - The run record (`.ci/runs/<sha>.json`) gets a per-kind summary under `results`: tests, subtests, passed, failed and skipped, wall and summed seconds, mean/p50/p90 per test, the slowest parts.
  - The log gets a line such as `results: unit 1184 tests, 0 failed, 31.1 s wall (unit.json)`.
  - Every test with its time goes to `<kind>.tests.json` next to the file.
- **Problems never change the verdict.** A missing or broken file becomes the summary's `error`, for example `not written (the command must write $CI_RESULTS_DIR/unit.json)`.
- **Retention:** results folders are pruned with their run records.

Per project, as Makefile lines (only under local CI, so a plain `make test` is unchanged):

```make
# Go: issue-cli ci gotest prints what plain go test prints and saves the stream
test:
	go test ./... -count=1 $(if $(CI_RESULTS_DIR),-json | issue-cli ci gotest --out $(CI_RESULTS_DIR)/unit.json)
# .NET
test:
	dotnet test MyTests $(if $(CI_RESULTS_DIR),--logger "trx;LogFileName=$(CI_RESULTS_DIR)/unit.trx")
```

This repo uses `go run ./cmd/issue-cli ci gotest`, so CI tests the commit's own helper. The playtest runner writes `$CI_RESULTS_DIR/e2e.json` by itself when the variable is set.

### How a run happens

1. The viewer polls each configured project's branch head every 5 s. When the head was not tested yet and no run is live, it starts `issue-cli ci run --detach --if-untested`. A commit from a fast-forward, a `--no-ff` merge, a `git pull` or a hotfix on main counts the same, because the viewer watches the ref, not the merge command.
2. The run lives in tmux session `ci-<project>`, or window `ci-<project>` in the project's shared session. The owner can watch it live, and it stays open with the result until the next run. Restarting the viewer never kills a run, and on startup the viewer catches up on any untested head. Without tmux the run is a background process logging to `.ci/runner.log`.
3. The run takes `.ci/lock`, so there is one run per repository at a time. A lock whose process died is replaced.
4. The run checks out the head in the CI worktree `.worktrees/.ci`. The worktree is created once and reused (`checkout --force` plus `clean -fd`), so ignored build output survives and builds stay incremental. `issue-cli ci reset` rebuilds it from scratch.
5. When the run ends it re-reads the branch. If merges landed meanwhile it tests the newest head, so three merges during a run give one more run, not three.

Without the viewer, `issue-cli ci watch` does the polling; `--once` checks once.

### Files (`.ci/` in the main checkout)

`.ci/` writes its own `.gitignore`, so no repo has to ignore it.

| File | What |
|:--|:--|
| `.ci/state.json` | Where the branch stands: last tested sha, `green`/`red`/`error`, last green sha, red since, failing command and tests, what landed since green, the issues among it, the log tail. The viewer reads only this file and the lock |
| `.ci/runs/<sha>.json` | One run: each command's exit code and duration, the failing command and tests, the commit range from the last green run, the merged branches and issues, the log tail. A non-default tier writes `<sha>.<tier>.json`. The newest 50 runs are kept |
| `.ci/runs/<sha>.log` | The full output of the run |
| `.ci/results/<sha>/` | The run's `CI_RESULTS_DIR`: the result files the commands wrote, and a `<kind>.tests.json` list per parsed file |
| `.ci/lock` | The run in progress: pid, sha, tier, start |

### When it goes red

- **Blame:** the run lists what landed between the last green commit and this one. That covers local branches whose tip is in the range (matched to issues by `work/<slug>`), branches named by merge commits, and commits made straight on the branch. A rewound branch counts from the merge-base, and the first run ever blames nothing.
- **Status page:** the Local CI card shows the state chip, the one-line state and a Last runs row (the last 12 runs as green/red/amber boxes, oldest first, with a dashed box for a live run; each box's tooltip has the commit, result, age, duration and what failed). While red it also shows the failing command and tests, the issues that landed since green and the log tail. Red CI is the first item in the Needs you tile (Waiting on you in `status.md`) and counts in the sidebar's Status number. `status.md` carries the same line.
- **Card markers:** each issue merged since the last green run carries a red `CI red · <command>` chip while main is red, on the workboard (done cards too) and the system pages.
- **`ci:` comments:** each of those issues gets a comment with the failing command, its tests, what else landed and the log path.
- **The lead:** CI never messages it (nothing on a timer prompts a session). The lead reads `issue-cli ci status` when the owner asks, at check-ins and before a push, and decides with the owner whether to revert, fix forward or dispatch a fix. CI never reverts anything.
- **`on_red: file_issue`:** the first red run after green also files one `bugfix` issue (`Fix red CI on <branch> at <sha>: <command>`) with the range, the merged issues and the log tail in a `ci:` comment. Later red runs comment on that same issue, and the `ci:` comments link it. When main is green again the issue gets a comment and stays open for whoever ships the fix.
- **Quiet while still red:** later red runs comment and message again only if the failing command changes. The lead hears once more when main is green again, and the chips go.
- **CI can't run** (git or the worktree failed): the state is `error`, not red. Nobody is blamed, the card says why, and the lead hears once.
- **Health snapshot:** after each default-tier run, red or green, `ci run` takes a [health snapshot](workbench-files.md#health-snapshots) at the tested commit and prints `health: snapshot <path>`. A failed snapshot prints one line and never changes the run's result.
- **Slow tiers:** `issue-cli ci run --tier slow` runs the default commands, then the slow ones, once. Its result is shown separately (`ci status --tier slow`) and never turns main red, blames issues or messages anyone.

### What agents and the lead see

- `issue-cli start` and the dispatch prompt print the one-line state, for example `Local CI: main red since def5678: make validate: TestFoo — inherited from main, not caused by your branch`. An agent can tell a failure it inherited from one it caused.
- After `issue-cli transition <slug> --to done`, the agent reads that CI tests main by itself and that it must not wait.
- The lead brief adds a line when `ci:` is set: check `issue-cli ci status` before any push, and never push a release while main is red.
- `issue-cli ci status` prints the one-liner first, then what landed and the recent runs; `--json` dumps the state and runs. See [CLI Overview](CLI/overview#ci).

## Private work and code

A project's work files tell how the work was done: issue bodies with the owner's words, session notes, retros. In the code repo, they go wherever the code goes. The opt-in `privacy:` block records the owner's choice of where they live, and nothing is decided silently: `issue-cli init` asks on a terminal, `workflow upgrade` proposes the `privacy` entry with its question and setup steps, and the Updates card lists them.

```yaml
privacy:
  work_files: tracker        # tracker | in_repo (keep them in the code repo, by choice)
  work_paths: [issues, sessions, retros, bugs, .health/snapshots]   # the default
  code: snapshots            # snapshots | public (develop in the public repo, by choice)
  public_remote: public      # code: snapshots only (default public)
  public_branch: main        # (default main)
  decided: "2026-10-24, owner: \"…their words…\""
```

Without the block the project has not decided. `process workflow` warns about an unknown value, a work path outside the project, and an empty `decided:`.

### The tracker repo (`work_files: tracker`)

The work files live in a private tracker repo whose git folder sits **outside the checkout**: `<checkout's parent>/.trackers/<slug>.git` by default, or `tracker_git_dir:` on the project in projects.yaml. Its `core.worktree` is the checkout and `status.showUntrackedFiles` is `no`, so:

- every path stays where it is: the viewer, issue-cli and the lead's handoff path work unchanged;
- the code repo ignores the work paths (`.gitignore`) and sees nothing;
- `git clean -fdx` in the checkout can't take the tracker's history.

Setting it up (the `privacy` entry's setup steps):

1. The owner answers, and the answer goes in `privacy:` with their words.
2. The owner creates a private repo, e.g. `<name>-tracker`. Agents never create repos.
3. `issue-cli tracker init --remote <url>`, with the URL the owner gives (init refuses without it), creates the git folder (or adopts an existing one, as it is), adds the remote, ignores the work paths in the code repo, installs the pre-push guard and pushes the first backup. When the code repo still tracks work files it prints the `git rm -r --cached` to run with the owner; it never runs it. Their old history stays in the code repo until a one-time clean from a mirror (`git filter-repo --path <work path> --invert-paths` on a fresh mirror clone, then a force push, with the owner present).
4. `issue-cli privacy check` shows every line green, or each fix.

**Sync.** The viewer syncs each tracker every 10 minutes (tracker_watch.go): it commits every change under the work paths (`tracker: N files`), pulls with rebase and pushes. It prompts no session. A conflict aborts the rebase, keeps the local commit and pushes nothing; the error goes to `<git folder>/issue-cli-sync.json` and Status shows **Backup failing**. `issue-cli tracker sync` does the same on demand.

**Another machine or a cloud session** clones the tracker too, with the same layout (a git folder outside the checkout, `core.worktree` the checkout), and runs `issue-cli tracker sync` before work (pull) and after (push). A session that writes an issue into the code repo instead is how a work file goes public: PR #62 on the public repo was a cloud session's.

### Private development, public releases (`code: snapshots`)

A public dev repo publishes how the work was done, not just the result: commit messages with owner quotes, D-n text and `Proves:` trailers, branch names (issue slugs), PRs and their refs, which GitHub never lets anyone rewrite. With `code: snapshots`:

- a **private dev repo** (e.g. `<name>-dev`) is `origin` and holds everything: main, branches, tags, PRs;
- the **public repo** is the remote `public` (its tags are never fetched, so they can't clash with the dev tags) and gets **one commit per release**: the release branch tip's tree, message `vX.Y.Z`, built with `git commit-tree`, so no dev commit, message, branch name or quote travels with it. Between releases it does not change; released code stays readable and `go install`-able;
- the snapshot is **signed** with the owner's `user.signingkey` (`commit-tree -S`), so GitHub shows it Verified; a key that fails to sign stops the publish, and the Code badge turns amber (**Release unsigned**) while the public tip has no signature;
- releases follow the branch flow in [Roadmap: Cutting a release](roadmap.md#cutting-a-release-candidate-soak-publish): `issue-cli release cut` (the branch `release/vX.Y` and its rc tags), `release pick` (fixes from main), `release publish` (the snapshot). `release/vX.Y` stays in the dev repo for good.

**The first switch of an existing public repo** (the `privacy` entry's setup steps):

1. The owner creates the private dev repo. Agents never create repos.
2. `issue-cli release setup --dev <url>`: the old `origin` becomes `public`, the dev repo becomes `origin` and gets every branch and every non-candidate tag, the public branch's tip is recorded as the base the first snapshot goes on, and the pre-push guard is installed. Nothing is pushed to `public`.
3. The public repo still holds the old dev history. The owner cleans it **once, from a mirror**, never in the working checkout (so no worker waits): `git clone --mirror <public url>`, `git filter-repo --path issues --path sessions … --invert-paths`. Commit messages (quotes, D-n, trailers) and branch names stay public after a filter alone, so issue-viewer went further (2026-10-24): one squashed, signed commit per old tag (`git commit-tree -S`, the tag's filtered tree, message `vX.Y.Z`, the previous one as parent), the tags moved onto them, the old branches deleted, then `git push --force` of main and the tags. Back up the untouched mirror first (a tarball and a private repo). PR refs GitHub keeps can't be removed this way. Then record the cleaned tip as the base, so the Code badge doesn't read the cleaned history as dev history: `git fetch public && git config issue-cli.publicbase $(git rev-parse public/main)`.
4. In the release workflow, skip private repos (`if: ${{ !github.event.repository.private }}`) so the dev repo builds no releases, and take the release notes from the CHANGELOG's version section (a snapshot is one commit, so commit subjects say nothing). This repo's `release-marker.yml` does both.
5. The next release is the first snapshot. If the public tip is no longer the recorded base (the mirror clean rewrote it), `git fetch public` and pass `--first` once.

**Cloud sessions and other machines** clone the dev repo (and the tracker), never the public one, and push there. A cloud session that opens a PR on the public repo publishes it: that is how PR #62 went public.

**Commit messages.** With `code: snapshots` they keep the owner's quotes and D-n text as today: they never leave the dev repo. With `code: public` the guard refuses a push whose commit message contains a recorded decision's quote (16 characters or more, matched against every issue's `## Decisions`).

### The guard

`tracker init` and `release setup` install a pre-push hook in the repository's common git folder, so every worktree shares it. An existing pre-push hook is kept as `pre-push.local` and runs first. The hook runs `issue-cli privacy pre-push`, which refuses:

- with `work_files: tracker`, a push whose commits add a work path to the code repo (`privacy guard: <sha> adds work files to the code repo (issues/…)`);
- with `code: snapshots`, any commit or tag to the public remote (by name or URL) that is not a release snapshot (`privacy guard: public gets release snapshots only, and <sha> ("…") is a dev commit`);
- with `code: public`, a commit message that quotes a decision.

`issue-cli privacy hook` reinstalls it. A clone without the hook (another machine, a cloud session) is the gap it can't close, which is why everyone works against the dev repo. For a PR opened on GitHub, `.github/workflows/privacy-guard.yml` reads the same `privacy:` block: a PR touching a work path fails (`work_files: tracker`), and with `code: snapshots` every PR on the public repo fails, pointing at the dev repo. Copy it into a project that turns the feature on.

### What Status shows

A **Work** and a **Code** badge after the word Status, their lines at the foot of the Health card (or a Privacy card without one), and markers on the projects list and the `/stats` overview. All local: `git ls-files`, the tracker's git folder and its sync file; no network.

| State | Badge | When |
|:--|:--|:--|
| Green | Work private | a tracker with a remote; the line names its repo and the last backup |
| Amber | No backup | ignored by git and backed up nowhere, or a tracker without a git folder or a remote |
| Amber | Backup failing | the last sync failed |
| Red | Work in code repo | the code repo tracks a work path; shown even before the feature is on, because it is a fact from git |
| Grey | Work public | `work_files: in_repo`, the owner's choice; never a warning |
| Grey | Not set up | no choice and no work files |

| State | Code badge | When |
|:--|:--|:--|
| Green | Code private | `code: snapshots`, a public remote, the guard installed, the public tip a snapshot; the line names the dev repo and the last published version |
| Amber | Not switched | `code: snapshots` but no public remote yet (`issue-cli release setup`) |
| Amber | Guard off | this clone has no pre-push guard (`issue-cli privacy hook`) |
| Red | Dev history public | the public branch's tip (as of the last fetch) is not a release snapshot and not the recorded base |
| Grey | Code public | `code: public`, the owner's choice |
| Grey | Not set up | no choice |

The badge's title and aria-label carry the full sentence; an amber or red line adds the fix. `issue-cli privacy check` prints the same lines (exit 1 on red) plus the guard.

## System Overlays

Each system (API, CLI, UI, Workflow) can override status prompts and add transition actions. Overlays are defined under `systems:` in `workflow.yaml` and merge with the base workflow:

```yaml
systems:
  API:
    statuses:
      - name: "in design"
        prompt: |
          Extra API-specific design guidance.
    transitions: []
```

System-specific documentation lives in `docs/<System>/` and agents should reference it during the documentation status. See:

- [API Overview](API/overview)
- [CLI Overview](CLI/overview)
- [UI Overview](UI/overview)
- [Workflow Overview](Workflow/overview)

## Keeping workflow.yaml up to date

Projects edit their `workflow.yaml` heavily, so a newer issue-cli never overwrites it. Instead:

- `workflow.yaml` carries a `template: <name>@<release>` stamp and a `features:` record (installed `<id>@<version>`, declined `<id>`). `issue-cli workflow init` writes both.
- `issue-cli workflow scan` detects which catalog features the file already has and writes the record. Use `--dry-run` to preview.
- `issue-cli workflow upgrade` lists the features the project could adopt, each with what it does, why, a snippet and a question for you. An agent proposes them one at a time and copies in only the ones you pick. Declined ones stay declined until you re-open them with `issue-cli workflow scan --forget <id>`.
- You see what there is to adopt on the viewer's **Updates** tab (`/p/<project>/updates`, and `/updates` for every project): decline a feature, re-open it, or start an agent that installs it with you. No `issue-cli` command mentions upgrades unless asked, so workers and leads are never nudged.
- With a `features:` record, the record decides what is offered: the stamp hides nothing undecided. Bump the stamp only once every proposal is installed or declined.
- `upgrade` calls a current stamp `up_to_date` only when the record decides everything: `undecided` while proposals wait on you, `stale_record` when the file has features the record misses. `process workflow` and `health lint` warn about a stale record and name `issue-cli workflow scan`.

Details: [Workflow Overview](Workflow/overview) (stamp, record, catalog, keeping it complete) and [CLI Overview](CLI/overview) (`workflow upgrade`, `workflow scan`, `workflow catalog-check`).
