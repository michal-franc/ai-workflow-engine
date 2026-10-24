# Changelog

Release history for the `issue-viewer` web app and the `issue-cli` CLI.
GitHub releases on `michal-franc/ai-workflow-engine` are the source
of truth for release notes; `issue-cli process changes` fetches them live
and falls back to this embedded file when the API is unreachable. Keep
entries here mirrored with the GitHub release descriptions.

Lives at `cmd/issue-cli/CHANGELOG.md` (co-located with the CLI) because Go's
`//go:embed` cannot reference files outside the embedding package's directory.

Entries are newest-first. Each entry has the form:

    ## <version> — <YYYY-MM-DD>

    - user-visible change
    - another user-visible change

Shipped changes collect under `## Unreleased` at the top. A release is cut
per milestone (the `version:` on issues, see docs/roadmap.md): the lead
renames `## Unreleased` to `## vX.Y.Z — YYYY-MM-DD` and pushes, and the
release-marker Action tags and builds it. Pushes with `## Unreleased` on top
release nothing.

## v0.54.1 — 2026-10-24

- **The release Action writes its notes again (fix).** v0.54.0's run failed after tagging (exit 141): the step piped `git show` of the CHANGELOG into an `awk` that stops at the next heading, so `git show` died of SIGPIPE under `pipefail` and no release or binaries were made. It now reads the file first.

## v0.54.0 — 2026-10-24

- **The privacy badges know when a repo is private.** The viewer's health watcher asks GitHub, at most once an hour per repo and never on a page view, whether the code repo (origin) is publicly visible, and caches the answer in its git folder. Work files tracked in a private repo now read as a green **Private repo** (info, not the red "Work in code repo"), and so does Code when privacy isn't set up; the Health line adds what to do before making the repo public (tracker init, release setup, a one-time history clean). Not on GitHub, or not asked yet: the badges are as before.
- **An epic's page shows its children, and the Epics card shows where an epic continues.** On an epic, the first tab is named **Epic** with its closed/total children (amber, green when all are closed) and lists the children by version, open ones first. In a version's panel of the Status Epics card, an epic with children still open in other versions says so (**+8 → 0.55**).
- **Epics get their own short path.** A `type: epic` issue goes idea → in progress → done (or parked) instead of the full feature path: the lead moves it to in progress when its first child starts, and it closes on your approval once every child is done or parked. New validator `children_closed` (optional `values:` for the closing statuses) checks the children; it is what the Epics card's **close?** shows.
- **Status shows which open issues are epics and which epics you can close.** In Waiting for release, an open epic carries an **epic** chip; the milestone card says how many epics are still open (they count as open issues); in the Epics card an epic whose children are all done, in every version, shows **close?** (else its own status), the Version select and the Versions card list only versions not released yet (the card's count sums those).
- **Releases on the public repo are signed.** `issue-cli release publish` signs the snapshot commit with your `user.signingkey` (GPG or SSH), so GitHub shows it Verified; a key that fails to sign stops the publish before anything goes public. `privacy check` and the Status Code badge turn amber (**Release unsigned**) while the public tip has no signature, with the fix. The publish line says whether it signed.
- **The feature catalog says what is so (fix).** An audit of RogueTech's installed features found the engine saying things that weren't true:
  - **Wording:** `system-pages` is now "System page details". Every issue `system:` has a page anyway, and `workbench.systems` only adds summaries, design docs and families. `metrics-and-knobs` and `status-changelog` likewise say what shows without their key (Branches in Metrics; CHANGELOG.md, else git tags, on Status).
  - **`workflow upgrade`** no longer calls a current stamp `up_to_date` while the record isn't. It reports `undecided` (proposals wait on you) or `stale_record` (the file has features the record doesn't list; `workflow scan` records them), with `undecided` in `--json`. `process workflow`, `health lint` and the Updates tab ("not recorded") flag a stale record whatever the stamp; undecided proposals still show only on Updates and in `upgrade`.
  - **Status page:** it no longer suggests `workbench.changelog` when the record declines `status-changelog`.
  - **Health:** a glob that matches no file (`e2e_sessions`, default included, or `test_globs`) is now a family warning, printed by `issue-cli health` and listed on the Health view, instead of a silent zero.
  - **`workflow scan`** prints each category heading once (proof-links moved into gates).
- **Private work files and private development.** `privacy:` in workflow.yaml records the owner's choice of where issues, sessions, retros, bugs and health snapshots live; `issue-cli init` asks on a terminal and `workflow upgrade` proposes the new `privacy` entry. With `work_files: tracker` they live in a private tracker repo whose git folder sits outside the checkout (docs/workflow.md, Private work and code).
  - `issue-cli tracker init --remote <url>` creates or adopts it, ignores the work paths and pushes the first backup; `issue-cli tracker sync` commits, pulls and pushes. The viewer syncs every 10 minutes, prompting no session.
  - A pre-push guard refuses a push that adds work files to the code repo; `issue-cli privacy check` shows the state and the fixes.
  - **Status** shows a Work badge after the title (green private, amber no backup or failing, red work in the code repo, grey public by choice) and its line on the Health card; the projects list and the `/stats` overview show the marker.
  - **Private development, public releases.** With `code: snapshots` the dev history stays in a private dev repo (origin) and the public repo gets ONE commit per release, the release branch's tree with the message `vX.Y.Z`. `issue-cli release setup --dev <url>` switches an existing repo; the pre-push guard refuses dev commits and tags to the public remote. With `code: public` it refuses commit messages that quote a decision.
  - **Releases are cut on a branch** (docs/roadmap.md, Cutting a release): `issue-cli release cut v0.N.0` creates `release/v0.N` and its worktree and tags the rc there; fixes land on main and `release pick <slug>` cherry-picks them; `release publish v0.N.0` publishes from the branch. Waiting for release watches the branch: picks after rc.K need the next one, and **Every done fix is on release/v0.N** lists what to pick.
  - The Release Marker Action skips private repos and takes the release notes from the CHANGELOG's version section. A new Privacy Guard Action fails a PR that touches work files, and with `code: snapshots` any PR on the public repo.
  - Status gains the **Code** badge next to Work: green private, amber not switched or guard off, red dev history on the public branch, grey public by choice.
  - Catalog entries can carry `setup:` steps, each the owner's or the agent's. The Updates card lists them, `workflow upgrade` prints them, and the install agent walks them, leaving the owner's steps to the owner.
- **Ticking a box on the issue page saves that box (fix).** A click used to save "the nth `[ ]`" in the raw markdown, which also counted `[ ]` in inline code, code blocks and prose, so on an issue that quotes the checkbox syntax every box below the quote ticked or unticked the box above it, gate boxes included. Each rendered box now carries its line in the body, a click posts `{line, checked, base_body_hash}` to the new `POST /issue/<slug>/checkbox`, and the server flips only that task-list box: 409 when the file changed since the page loaded or the box is already in that state, 400 for a line that holds no box. Check boxes you clicked on issues that quote `[ ]` above their boxes.
- **Done keeps a game project's playtest runs (fix, critical).** At → done (and in `issue-cli playtest prune`) the archive kept only runs of mode `auto` or `judge`, so a game project's `ux-test` / `playtest` runs were all deleted with the worktree and its Evidence tab went empty for good. One mode reading (`ux-test` → auto, `playtest` → judge) now serves the archive and the Timeline/Evidence tabs. An own run goes only when a newer run of its session and mode is kept; the run a kept `-cached` run takes its shots from, an own run of another mode and a run it can't classify are kept. The line now says `archived N of M runs, deleted K`, with a ⚠ when none of the issue's runs was archived. The 30-day shrink also reduces a game's shots beside `results.yaml`. Runs already lost are not recovered.
- **A game's playtest runs show on the Evidence and Timeline tabs.** RogueTech's runner writes its own results.yaml (`started:` with no timezone, `ux-test` / `playtest` modes, `screenshots:` named `<step>@<W>x<H>.png` beside results.yaml, `kind`/`subject`/`passed` checks, `verdict:`). The viewer dropped every such run on its date, so the tabs said no playtest had run. It now reads both schemas when a run is loaded, and lists a run it still can't read at the top of both tabs instead of skipping it. A step's shot comes from the newest run that has the file (a `-cached` replay is passed over). When no step proves a criterion, the Shots card shows each session's latest ux-test shots. docs/patterns/playtest/README.md lists the fields of both schemas the viewer reads.
- **Workers don't message their lead unless you say so.** A worker's briefing no longer invites it to message the lead or other agents. It messages only when you, in its own chat, give it permission or ask it to. Questions go to you in its chat or as `question:` comments, and progress stays on the issue trail. The lead's briefing matches: it doesn't ask workers to report in. Running workers keep their old briefing until re-briefed (`issue-cli dispatch <slug> --reprompt`).
- **A lead writes its handoff note before it exits (issue-approve mod 0.6.0).** In a project lead's session (`ISSUE_VIEWER_LEAD_SCOPE` set, no issue slug), the first `/exit` doesn't exit: the mod says so and starts a turn asking the lead to write its handoff note (what is live, what waits on whom, decisions, `## Next`) and back up the private tracker. The next `/exit` leaves. Workers and other sessions are untouched. Ctrl+C, Ctrl+D or a killed window can't be caught; the mod records the missed handoff, and the project's next lead session gets a line in its first prompt to write it before other work. Running sessions need `/reload-plugins`.
- **Every issue gets a `created:` date.** `issue-cli create`, `report-bug`, CI's red issue and the viewer's New issue form write `created: "<today>"`: the date the `criteria_proven` gate reads, which counted an undated issue as new. A one-off backfill dated the 1,442 undated issues on this machine's 13 projects, each by the earliest date recorded for it (stats sidecar, comments, decisions, first git commit, file mtime as the fallback). `set-meta <slug> --key created --value YYYY-MM-DD` dates an issue that has none and never changes an existing date. The Evidence tab's label now follows the gate's rule exactly.
- **`issue-cli health lint` checks workflow.yaml and the board.** Six checks, every finding a warning with its fix:
  - `schema`: keys the parser silently drops, with "did you mean"; deprecated keys.
  - `template`: a stamp behind the bundled template.
  - `placeholders`: `{{name}}` placeholders a field never fills.
  - `sections`: checkbox and test-plan gates on sections no template writes.
  - `undated`: issues without `created:`.
  - `workflow`: the warnings `process workflow` printed.

  The new `lint` health family counts them, a `lint` component (weight 10, 0 → 20 warnings) joins the derived score, the Health view lists them live in a Lint section, and the Status page's Health card shows the count.
- Fix: `set-meta` on a new field no longer leaves a blank line before the closing `---`.

- **Releases go through a release candidate first.** With `workbench.release_candidates: true` (issue-viewer sets it), a ready milestone is cut as a local `v0.N.0-rc.1` tag, installed and run on this machine's projects. It is published only on the owner's ok on that soak (docs/roadmap.md, Cutting a release).
  - **Waiting for release** shows the candidate in silver under its title (`v0.54.0-rc.1 · release candidate · soaking 2d`), with its details folded to one summary line: when it was cut, and the commits merged after it. Each of those needs the next rc, bugfixes included (the `soaked` check). It never calls the release ready by itself: it says `rc.K soaking` and to publish only on the owner's ok. On the engine project it also shows the viewer and issue-cli builds running (flagged when they aren't the candidate), which projects ran the candidate (their usage logs), and the open report-bug issues filed from it.
  - **Version stamp:** `make build` / `make install` stamp both binaries with `git describe`, so `issue-cli version` stops saying `dev`. A candidate build says it is one and how to report a problem.
  - **report-bug from a candidate** puts the bug in the release's milestone (`Added to milestone 0.54 …`): a bug found in the soak waits in this release, not the next.
  - Usage-log events carry the build as `ver`. Tags with a pre-release suffix no longer count as a release when there is no changelog. The lead brief says never to publish without the owner's ok on a soaked candidate.
- **A human starts the work; agents then run on their own.** A disclaimer in Agent Dispatch (docs/agent-dispatch.md, "A human starts the work"): no scheduler, timer or auto-dispatch, and unattended runs belong on `claude -p` or the Agent SDK with your own API key, not a Claude subscription.
  - **`issue-cli dispatch` needs `--quote "<the human's words>"`**, from the lead and from your own shell. Without it nothing starts (exit 2). Each start or re-prompt leaves `dispatch: "<words>" (<how>, <date>)` on the issue. One slug per call. The board's ▶ is unchanged. `issue-cli health review --spawn` needs `--quote` the same way.
  - **The lead recalls decisions, never invents them.** Every lead brief now says: answer workers only from recorded decisions and cite them; ask the human when none covers it and tell the worker to wait; dispatch only the issues the human names in the lead's chat; tick gates only with the human's quote.
  - **`issue-cli lead message` never pastes into a Claude session.** It pastes only into Codex sessions; for a Claude session it prints the `SendMessage` call to make.
  - **Local CI no longer pastes `[from ci]` into the lead.** Red and green still reach the Status page, `status.md`, the `ci-red` markers and the `ci:` comments; the lead reads `issue-cli ci status` when you ask.
  - **A worker stays on its issue.** In a worker session, `start`, `claim` and `transition` on another slug fail ("you work on <own-slug> only; ask your lead"), and the worker prompt says to stop when the issue is done or at a gate, never picking the next one. `start` and `claim` default the assignee to the worker's session name (`agent-<slug>`), its `SendMessage` address.
- **`make ux-test` is green on main again.** The frontmatter-escapes and local-ci sessions predated the retitle that keeps the slug and the Needs you tile; they now follow the pinned slug and open the tile before checking it. Run another issue's session from a worktree with `make ux-test ISSUE= S=<session>`.
- **The issue page's Evidence tab says whether an issue is proven.** It replaces the "coming" card, and its label carries the count on every tab: green "8 / 8 proven", amber when short, grey "13 ticked · 0 linked" for an issue the proof gate doesn't cover. Issues with no `created:` are dated by their first transition, so older done issues read grey.
  - **Shots:** the latest playtest run's screenshot of each step a criterion links to, tagged with what it proves. The widest screen shows first, with chips for the others; a failed screen shows its failing check. Shots are shown in place from the run folders; nothing is copied into the issue.
  - **Criteria → proof:** the same report as `issue-cli prove --list` and the gate, with each link's source, its check and the fix for a failing one. Unproven rows are amber, and stray trailers are listed.
  - **Verdict, Change, Playtest per step, Watch, Stale text:** the merge, the `tests:` comment against the last code commit, and time and waits as on the Timeline. Then commits and files by area with lines in tests; each step's machine result next to your verdict (judge run or `playtest:` / `approval:` words); `risk:` comments; and Open questions / pending / TBD lines left in the body, each with the D-n or Clarifications bullet that likely answered it.
  - `issue-cli show` and `issue-cli prove --list` print the Evidence link.
- **Docs-only commits no longer make test links stale.** A commit whose files are all under `docs/`, Markdown or a CHANGELOG doesn't count as a change for the `tests:` check, so the documentation commit no longer turns every test link stale on a done issue (`prove --list`, the `criteria_proven` gate, the Evidence tab). `prove` also looks every test name up in one `git grep`, so a long list is fast.
- **Playtest shots stop piling up.** `.playtest/runs` held 1.2 GB across this repo's worktrees.
  - `make ux-test` keeps screenshots only for the run's own issue's sessions (from `-issue`, else the `work/<slug>` branch), plus failing screens and judge runs; `screenshot:` paths are relative to the run folder.
  - At → done, issue-cli keeps the latest auto and judge run of each of the issue's own sessions, moves them to `<root>/.playtest/runs/` (removing the worktree no longer loses the evidence) and deletes the rest. It prints one `playtest:` line and never fails the transition.
  - **`issue-cli playtest prune [<slug> | --all] [--dry-run]`** applies the same rules to existing worktrees (here: 1,240 MB → 46 MB).
  - The viewer's daily health pass shrinks the shots of issues done 30 or more days ago to small JPEGs (`health.shrink_shots_after_days` in workflow.yaml, 0 turns it off); the Evidence tab marks them "reduced".
- **Disk use on the Stats page.** A new `storage` health family records how many MB the project takes (worktrees, playtest shots, `.git`, `.agent-logs`, attachments, total), shown as a Disk use card with a bar per area and the total's trend.
- **▶ Playtest opens a tmux window you can see, never a hidden child of the agent (issue-approve mod 0.5.0).** The mod used to run `make playtest` under the agent's own process; in RogueTech that started Godot instances the owner could neither see nor stop. Now ▶ Playtest runs the project's `scripts/playtest-window <slug>` when the checkout has one (RogueTech), else opens `make playtest ISSUE=<slug>` in a new detached window `playtest-<slug tail>` of the agent's tmux session, in its worktree; the window waits for Enter when the playtest ends. It returns at once and the toast names the window. ✓ Report shows only where the project has a `playtest-report` target; outside tmux the toast says to run it in a terminal. Running sessions need `/reload-plugins`.
- **The feature catalog caught up, and it can't fall behind again.** `issue-cli workflow upgrade` offered RogueTech and gpu-runner nothing newer than proof-links; every feature a project turns on in `workflow.yaml` since v0.36 was missing.
  - **Ten new catalog entries:** `local-ci`, `ci-test-results` (offered once the project has local CI: new `requires:`), `health-tuning`, `status-changelog`, `system-pages`, `project-pages`, `metrics-and-knobs`, `balance-board` (catalog only), `lead-prompt` and `local-merge-shipping` (opt-in: the bundled development template keeps push). A new `workbench` category. The review and writing templates are stamped v0.54.0.
  - **The record decides, not the stamp:** with a `features:` record, every feature neither installed nor declined is proposed, so bumping `template:` no longer hides an undecided one (gpu-runner at v0.54.0 lost proof-links). The upgrade steps bump the stamp only once nothing is undecided. A side effect: older opt-ins nobody declined (worktree, parked statuses…) are offered too.
  - **Kept complete by `make validate`:** a test fails on a `workflow.yaml` key (top level, or under `ci`, `health`, `leads`, `workbench`), validator rule or action type no catalog entry covers. Every entry names its code in `sources:` (globs, or `file.go#Symbol`), and **`issue-cli workflow catalog-check`** fails on a branch that changes that code or a bundled template without bumping the entry's `version:` past the last release; the message names the entry, the files and the version to set, and a `Catalog: n/a <id> <why>` commit trailer passes it when nothing a project adopts changed. issue-viewer's Design and Documentation checklists ask about the catalog too.
  - **The Updates tab** (`/p/<project>/updates`, sidebar count in amber) shows you what there is to adopt, newest first: each feature's what, why, where, its question and snippet. **Decline** and **Re-open** write the `features:` record; **Install with an agent** starts an agent that asks you the question, adapts the snippet, records it and shows the diff. `/updates` compares every project, and the project list shows each one's count.
  - **No more upgrade hints:** `start` and `process workflow` stopped printing one; it reached every worker and confused them. Upgrades show only on the Updates tab and in an explicit `workflow upgrade`.
  - The catalog, scan and upgrade plan moved to `internal/workflowupgrade` so the viewer shares them. See docs/Workflow/overview.md (Feature catalog) and docs/CLI/overview.md (`workflow catalog-check`). (workflow/feature-catalog-catches-up-ci-health-project-pages-status-page-leads-and-local-merge-shipping-in-workflow-upgrade)
- **Fix: local CI no longer hangs on a command that starts an interactive shell.** The detached runner lives in a tmux window, and each CI command ran in a background process group on that window's tty, so an interactive shell (`bash -i`, `$SHELL -lic`, as the guided check scripts' tests use) stopped on SIGTTOU/SIGTTIN until the timeout; main went red at eb88b60 this way. CI commands now run in their own session with no controlling terminal; a timeout still kills everything they started. (workflow/local-ci-hangs-when-a-command-starts-an-interactive-shell-ci-runs-commands-in-a-background-process-group-on-the-tmux-tty)

- **Guided check scripts: manual checks that run from a shell walk the owner through, one command.** For checks a playtest session can't drive (CLI commands, API routes, tmux, scripts), the agent commits `scripts/checks/<name>.sh` on the issue branch and the owner runs it. Each step says what it checks, why, what to look at and what to expect, runs the commands and shows their output, checks what a machine can and asks the owner the rest (y/n, Enter between steps, `q` stops, everything started is cleaned up).
  - **`issue-cli check-script <slug>`** runs the issue's scripts from its worktree. The owner's run from a terminal offers to tick the boxes that passed (a box passes only when every step naming it with `--box` passed) and to comment the failures as `checks:`. `--yes` (or stdin not a terminal) is the machine run agents pass before handover: machine checks only, nothing ticked. `--new <name>` writes a starting script with one step per unticked Manual box; `--list`; `--lib` prints the helper library.
  - **The helper library is built into issue-cli** and never copied into projects: steps and boxes, `on_exit` cleanup, `sandbox_project` (outside `.worktrees/`), `private_tmux --clean`, `stand_in` + `clean_shell` (no profile, so the real `claude` never gets in front of the stand-in), `step --touches-real` for anything in the owner's real session, `eventually`, `background`.
  - **▶ Run checks:** an issue at human-testing whose script has no owner run shows it on the issue page's Needs-you band, the Status page (Checks), its board card ("run checks") and the issue-approve band (mod 0.4.0). It opens tmux window `check-script-<slug>` in the worktree (`POST …/issue/<slug>/check-script`).
  - **Workflow feature `guided-check-scripts`:** `workflow upgrade` proposes it to development-template projects; the bundled development template (v0.54.0) and this repo's workflow.yaml have it: testing, human-testing and shipping prompts, a Test Plan Manual box, a Shipping box. `show`, `start` and `transition` print a Guided checks block at testing and human-testing.
  - **Scripts stay on the issue branch:** a last commit removes them before the merge. The three scripts already on main (prove, dispatch-status-cli, lead-tab) are kept as library examples under `internal/checks/testdata/ported/`.
  - See docs/patterns/guided-check-scripts.md. (workflow/guided-check-scripts-as-a-workflow-feature-scriptschecksname-sh-walk-the-owner-through-manual-checks)
- **`issue-cli dispatch` and `issue-cli status`: leads drive the viewer without curl.**
  - **`issue-cli dispatch <slug>`** starts the issue's worker exactly like ▶ on the board, from the same spec (`agentrun.DispatchSpec`), through tmux. The viewer needn't be running. It prints how to reach the session. `--agent codex`; `--reprompt` re-sends a live worker's briefing. Exit 0 started or re-attached, 1 failed, 2 usage, 3 waiting on a gate.
  - **Gate refusal, on ▶ too.** Dispatch refuses an issue that only waits on your approval. The CLI exits 3 with the approve link and `--force`. ▶ shows "Waiting on your approval for <status>" with Approve and Dispatch anyway (`POST …/dispatch` answers 409; `{"force": true}` overrides).
  - **`issue-cli status [--json]`** prints the viewer's `status.md` (or `status.json`).
  - **`viewer_url:`** at the top of `projects.yaml` sets the address every link and call uses. Without it: the running viewer's (`viewer.json`), then `http://localhost:8080`.
  - **Removed.** `ISSUE_VIEWER_URL` is no longer read or set, and the viewer no longer rebuilds URLs from the request's `Host`.
  - **Allowlist.** `viewer_url`'s host is added to the Host allowlist from `allowed_hosts:` (#61), so links to it work.
  - **Elsewhere.** `issue-cli projects --json` carries `viewer_url`, and the issue-approve mod (0.3.0) reads its board address from it. The built-in lead brief names `issue-cli dispatch` / `issue-cli status`.
  - See docs/CLI/overview.md (`dispatch` and `status`) and docs/agent-dispatch.md. (cli/issue-cli-dispatch-issue-cli-status-let-agents-use-the-viewer-without-raw-http-calls)
- **Leads get their own terminal setting, so you can see the lead and keep workers headless.** `lead_terminal:` (top level or per project) decides what opens when the project lead starts or is opened: `issue-cli lead spawn`, **Spawn lead**, and **Open** on the agents canvas. Workers keep `terminal:`.
  - **`tmux-window`:** the lead becomes a background tab, `view-lead-<project>`, in your own tmux session (`lead_window_session:`, optionally at `lead_window: <n>`). The tab is a nested client pinned on the lead: spawning again adds nothing, selecting a worker window doesn't move it, and closing it leaves the lead running. A taken slot or a missing session gives a warning step and the attach commands.
  - **Behaviour change:** with `lead_terminal` unset, leads are headless, including the **Spawn lead** button, which used to follow `terminal:`. `lead spawn` follows `lead_terminal` and `--headless` opts out; `--attach` is still accepted and does nothing.
  - **Placeholders:** `terminal:` and `lead_terminal:` commands also get `{{project}}`, `{{name}}`, `{{role}}` (`lead`, `worker`, `editor`) and `{{window}}` (the agent's exact tmux target). The recipe is in docs/agent-dispatch.md ("Leads as tabs in your own session").
- **Project health gets two scores, a review and its own view.**
  - **Health view (`/p/<project>/health`),** right below Status in the sidebar with the score beside it:
    - the **derived health** (0–100, from nine weighted parts: unit and e2e wall time, cost per test, e2e coverage, flaky steps, big files, long functions, tests keeping up with code, hotspots) with each part's value and why a part has no data
    - the **agent opinion** with its assessment and recommendations
    - both scores over time
    - unit and e2e **test count vs runtime** as two aligned panels (count blue, runtime orange)
  - **Status:** a Health card in the milestone row (Current milestone · Health · Local CI). From 1200px every Status row sits on the top tiles' grid; between 901 and 1199px the side column stays 320px. `status.md` gets a `## Health` section.
  - **`issue-cli health review`:**
    - It writes an evidence pack and prints the brief for the agent running it; `--spawn` starts a review agent in tmux, and so does Run review on the Health view.
    - `record` refuses a review whose evidence points at a metric or file that doesn't exist.
    - `file R2 --quote "..."` (or File as issue) turns a recommendation into an idea issue; only the owner's words or click do. `dismiss` and `show` too.
  - **Test results from local CI:** a `ci.run` command can declare `results: [{kind: unit|e2e, format: go-test-json|trx|junit|playtest, file}]` that it writes into `$CI_RESULTS_DIR`. Local CI parses them into the run record and they never change the verdict. `issue-cli ci gotest` keeps `go test`'s plain output while saving `-json`; `ux-test` writes `e2e.json` and records step times.
  - **New snapshot rows:**
    - `tests.*`: counts, wall and per-test time, slowest tests and parts, tests per area, playtest sessions per system, e2e coverage
    - `shape.*`: function length, long functions, big-file share, churn, hotspots
    - `score.*` and `opinion.*`
  - **New config:** `health.score`, `health.long_func_lines`, `health.e2e_sessions`.
  - **`issue-cli health`** shows both scores first and `Next:` hints for what is missing; `--family <name>` lists one family's rows.

- **`issue-cli prove`: each acceptance criterion linked to its proof, checked by the engine.**
  - **Links where the proof is written:** `Proves: AC2 TestName` or `Proves: AC2` trailers on the work branch's own commits (never commits merged in from main), and `proves: [AC2]` on a playtest step (the runner copies it into `results.yaml`; `make playtest-check` rejects ids that aren't criteria). `issue-cli prove <slug> <AC-id> --test/--step/--commit/--value/--shot` is the fallback and the only way to record a value. Those links go in the data sidecar under `proofs`; `--clear` removes them.
  - **Checks on every read:** a test must be a whole word in a test file on the branch, with a `tests:` comment newer than the last non-merge commit (else stale / not found / no report). A step's latest verdict must be pass. A commit must be reachable. A value is "reported". `--shot` is refused when the project has a playtest runner or the criterion isn't `(visual)`. The test check doesn't verify the trailer's commit touched the test.
  - **`issue-cli prove <slug>`** (`--list`, `--json`) shows each criterion with its links, their sources and checks, proven or UNPROVEN, and the ways to prove the rest. It ticks proven criteria.
  - **Gates:** `criteria_checkboxes: Acceptance Criteria` (every criterion a checkbox, no template text left) at the design gates; `criteria_proven: since YYYY-MM-DD` before human-testing. Issues created before the date, and issues without criteria, are exempt. While the gate covers an issue, `issue-cli check` refuses to tick a criterion by hand, and `checklist` prints how many are proven.
  - **Rollout:** this repo from 2026-10-24. The `proof-links` feature (in the development template, stamp v0.54.0) is offered by `workflow upgrade` with today's date. It also adds an `in progress` line on declaring proof and a `shipping` line asking for `risk:` comments.
  - **Fix:** a transition now gives validators the issue's full slug (system folder included); hints such as "add a tests: comment" named `count-badge` instead of `demo/count-badge`.
  - See docs/Workflow/proof.md. (workflow/issue-cli-prove-link-each-criterion-to-its-proof)
- **Licence: PolyForm Noncommercial 1.0.0.** Free for noncommercial use; commercial use is reserved to the author.
- **A stats dashboard per project, and one page that compares them all.**
  - **`/p/<project>/stats`** opens on:
    - gauges (share of time waiting on you against `health.limits.waiting_share`, the current milestone, big files)
    - trend lines from the health snapshots (cycle time by type, waiting on you, agent time per issue, rework and gate failures, lines of code)
    - where the time goes, by status and by type, system, epic or version
    - issues done per week
  - **Options:** range (7 / 30 / 90 days / all) and group-by are links, remembered per project. Every bar and point opens the issues behind it, linked to their Timeline tab, and every chart has a table view. Today's tables stay below, folded.
  - **`/stats`** compares every project from its snapshots, with sparklines.
  - **Charts:** SVG drawn on the server in the theme's colours: no chart library, no inline script.
  - **Not here yet:** CI, context-cost and check-speed charts say "No data yet" until their health families ship.
- **`issue-cli health backfill [--days 90]` fills in the trend lines' past.** It computes a snapshot for each past day without one, as it stood that evening: the codebase at that day's commit, the workflow over the issues done by then. `issue-cli health` now names the stats page, and suggests the backfill while there is under two days of history.
- **New snapshot rows:**
  - `workflow.working.p50|p90`, `workflow.time.working|waiting|idle` and `workflow.time.share{kind=waiting}`
  - a `milestone` family (`milestone.done|total{version}`)
  - `health.limits` in workflow.yaml (`waiting_share`, `big_files`)
- **The viewer answers only to loopback names (DNS rebinding closed).** A request whose `Host` is not `localhost`, `127.0.0.1`, `[::1]` or `*.localhost` gets `421 Misdirected Request` on every route, reads included. Before, a page on another site could rebind its own name to 127.0.0.1 and then read boards and dispatch agents, because it looked same-origin. To reach the viewer by another name, list it under the new `allowed_hosts:` key in `projects.yaml`. With a non-loopback `-host`, the bind address (every interface address for `0.0.0.0`) is accepted too, and the startup warning lists what is. See docs/API/overview.md, Design Considerations. (api/host-allowlist-reject-requests-whose-host-isnt-loopback-or-configured-dns-rebinding)
- **The Issue tab leads with what the issue needs from you.** The issue page opens on a band: what this is (the brief and your words), where it is (the path with time per status and your gates) and Needs you. Needs you lists the same items as Status: approve buttons with your words, an answer box for a question, or the playtest command. When nothing waits, it says who has the issue. Under it: what you're approving since the last gate, mockups, decisions with "took the recommendation" marks, and the body folded by section (Show, Hide, Expand all for today's body). The side column has the agent, acceptance criteria and relations above the fields. At phone width it stacks. `issue-cli show` prints `Brief: …`, the sentence the tab leads with.
- **/stats shows where the project's time goes, and snapshots carry it as the `workflow` family.**
  - **What it counts:** every issue done in the last 30 days goes through the Timeline tab's own code, now in `internal/timeline`. It is split into agent working, waiting on you and idle per status and type of work, each as p50 / p90.
  - **The tables:** cycle time per type, approval waits per gated transition, rework (loops and issues sent back) and dispatches. Refused `transition` calls are listed by error class, with dry runs apart.
  - **Gaps:** issues without CLI calls count for status time only and are named. `?days=N` looks further back.
  - **Snapshots:** the same numbers are in every health snapshot as `workflow.*` series (`workflow.status.waiting.p90{status=human-testing,type=all}` and so on). `issue-cli health` points at /stats for the tables.
  - **Timeline tab:** each status band now shows its agent time.
  - See docs/workflow-stats.md#project-aggregate and docs/workbench-files.md, "The workflow family". (workflow/health-workflow-timeline-where-agents-and-owners-spend-time)
- **Project health snapshots: codebase size, big files and growth.**
  - **What:** a dated JSON file per project in `.health/snapshots/` (gitignored), one row per metric. The codebase family reads git objects at the main head: files and lines by kind (code, test, docs, data, other), area and language; the test ratio; the 20 biggest files over `health.big_file_lines` (default 1000); and growth against 7 and 30 days ago.
  - **When:** the viewer takes one a day per repository, `issue-cli ci run` takes one after each run, and `issue-cli health snapshot` takes one by hand. `issue-cli health` prints the latest (`--json` for the file).
  - **Families:** other metric families register with `health.Register` and land in the same file. A family that fails is recorded and the others are still written.
  - **Config:** an optional `health:` block in workflow.yaml (`big_file_lines`, `test_globs`, `exclude`, `timeout`, `off`). See docs/workbench-files.md, "Health snapshots".
- **Approve buttons take your words.** The issue page's Human Approval section and the agents panel's Approve gate have an optional "Your words" box. Filled, the approval also saves an `approval:` comment and the agent's nudge quotes your words in place of a canned line; empty, approving works as before. `approval:` comments now name the status (`approval: "<words>" (<how>, <date>) → <status>`), from the viewer and `issue-cli approve` alike, and the Timeline shows each approval's words instead of "no words recorded".
- **The issue page has tabs, and the Timeline tab replaces the Agent Timeline.**
  - **The tabs:** every issue page has Issue · Timeline · Evidence under the title, each a link (`?tab=timeline`, `?tab=evidence`). Issue is the page as before, minus the Agent Timeline list. Evidence says "coming".
  - **Where the time went:** from dispatch to done, split into agent working, waiting on you and idle (agent + waiting + idle = total), with rework and commits beside them.
    - Waiting on you is a gap between agent calls that ends with your approval, or a gap of 15 min or more in a status only you can move on.
    - Idle is any other gap of 15 min or more with no CLI call and no commit.
    - Rework is agent time after a backward transition, or after a failed playtest run from testing on, until it passes.
  - **Life by status:** decisions, sections, ticks, comments, commits (a merge of main is one line), playtest runs, approvals, waits and CI inline. Filters: All, Your actions, Commits, Tests and CI, and CLI calls, which is the old list with the workflow actions each transition saw.
  - **Side column:** your part, your words, what was left open at done, and CI after the merge.
  - **Sources:** it reads the stats sidecar, the clilogs (worktree and project), the work branch, playtest runs and local CI. When one is missing, it says what it could not read. On local CI's issue: 5h 0m, 3h 48m waiting on you.
  - **`issue-cli show`** prints `Timeline: <url>?tab=timeline`.
  - See docs/workflow-stats.md#timeline-tab. (ui/timeline-tab-replaces-the-agent-timeline)
- **The clilog records each call's result, keeps no free text, and lives in the project.** Every `issue-cli` call used to be logged before it ran, with its full arguments, to `$TMPDIR/issue-cli-logs/actions.jsonl` (lost on reboot) and to per-agent logs that dispatch kept inside the worktree. Now one record is written when the call ends, with `exit`, `err_class` and `dur_ms`, to `<workdir>/.agent-logs/clilog.jsonl`, `$ISSUE_CLI_LOG` and the assignee's clilog, all in the project checkout. Comment, body, quote, title and value text is replaced by `‹text n=… h=…›`; slugs, box ids and `--to`/`--section` stay. The Agent Timeline takes comment text from the issue and marks failed calls ("failed: approval missing"); the agents canvas idles from the end of the last call. `go run ./tools/clilog-scrub -root <checkout>` merges old worktree logs into the project and strips their text. See docs/CLI/telemetry.md#the-clilog. (cli/clilog-and-stats-record-what-the-timeline-needs)
- **The stats sidecar records branch heads, the merge and dispatches; comments get a time.** Each transition row stores `branch_head` (the `work/<slug>` head), the move to `done` also `merge_sha`, so commits and "CI after the merge" survive a deleted branch. Viewer dispatches append `dispatches: [{at, session, agent}]`, and dispatched agents' prompts and logs stay in the project checkout, not the worktree. New comments store an RFC3339 `date` (pages still show the day). "Waiting on you" per gate is derived from `approved_at` and the agent's last call (`tracker.OwnerWaits`), with nothing new recorded. See docs/workflow-stats.md. (cli/clilog-and-stats-record-what-the-timeline-needs)
- **Frontmatter writes no longer leave orphan lines, and never write YAML that doesn't parse.** `set-meta` on a key holding a block list (`depends_on:` with `  - …` items) replaced only the key's line and left the items below it, and `--clear` left them under the key above. Either way the file stopped parsing, and the viewer and the CLI skipped the issue with "did not find expected key". `update --labels` had the same bug. Setting or clearing a key now replaces its whole value (list items, `|`/`>` block scalars, nested maps) up to the next top-level key. `set-meta` writes two or more `depends_on` slugs as a YAML list. Labels are written as quoted items in place. Every frontmatter write (`set-meta`, `update`, claim, approve, transitions, the detail page, `create`, the web create form, the GitHub import) now parses the result first. If it would not parse, nothing is written and the error names the keys: `not written: frontmatter would not parse after setting depends_on: …`. The GitHub import quotes label names, so a label like `priority: high` no longer breaks the imported file. (cli/set-meta-on-a-list-valued-key-leaves-orphan-list-items-and-corrupts-the-frontmatter)

## v0.53.0 — 2026-10-23

Focus: safe core. The viewer listens on localhost only and refuses cross-origin writes; local CI re-tests main after every merge; a redesigned Status page that scales to hundreds of issues; report-bug files real issues; and a batch of issue-cli fixes from agents' bug reports.

- **Typing during a dispatch no longer breaks the agent's start command.** Dispatch used to select the new agent window (or open its terminal) and then type `export …`, `cd …` and the start command into its shell, so whatever you were typing in the attached tmux client landed in front of it (`aclaude: command not found`, "Prompt not delivered"). The agent now starts as the window's own command, created detached in one tmux call with its environment (`-e`) and worktree (`-c`), in your login shell (`$SHELL -lic`); when it exits, or is stopped with Ctrl-C, the window keeps a shell. Dispatch never selects the new window, so the lead's window keeps focus. The pane `rawlog` (`pipe-pane`), the dispatch response's `log_file` field and `agent-log-viewer.py`, its only reader, are gone. Needs tmux 3.2+. (api/dispatch-keystrokes-typed-during-a-dispatch-corrupt-the-agent-start-command)
- **issue-cli finds the config from any folder.** It used to look only at `--config`, `$ISSUE_VIEWER_CONFIG`, then `projects.yaml` relative to the current folder, so `--project <slug>` and `report-bug` failed in a shell the viewer did not start (`why: no projects.yaml at projects.yaml`). `issue-viewer -config` now records its config in `~/.local/state/ai-workflow-engine/viewer.json` at start, and the lookup is: `--config`, `$ISSUE_VIEWER_CONFIG`, `./projects.yaml` (only if it exists), the running viewer's config, `~/.config/ai-workflow-engine/projects.yaml`. `issue-cli projects` prints the chosen config and why; the new global `--debug` prints it on stderr. When nothing is found, the error (and report-bug's why) lists every place checked and the fix. See docs/CLI/overview.md#config-lookup.
- **README revamp with real screenshots.** The README leads with the Status page, and every screenshot is a real capture of the sample project: Status, Board, issues canvas, issue page, agents canvas, a project page, and Local CI beside Waiting for release. "How it works" now covers the lead and its workers, plus shipping by local merge, local CI and a release per milestone. The umbrella / base-branch section is gone, `lead-and-workers.png` is redrawn without a base branch, and a short "What's new" points here. The sample project (`make demo`) gains five epics across versions 0.9–1.2, shipped 0.9 issues, a `CHANGELOG.md` and a "Weekly report" project page. Its optional side statuses now come after `done`, so in-progress cards no longer show a false "approve waiting-for-team-input".
- **Gate checklists are always added on a transition.** A transition's `append_section` used to skip when the issue already had a heading with that text at any level, so a `### Testing` inside `## Design`, or a `## Testing` written earlier, left the gate with no boxes ("section Testing has no checkboxes"). Now only an exact `## Testing` counts, and an existing one gets just the boxes it lacks (ticks kept). `transition`, `start` and `--dry-run` name the section created or topped up. A status `template:` no longer skips because its heading text appears somewhere in the body. `issue-cli append --section X` with a body starting `## X` no longer writes the heading twice. (workflow/idea-in-design-append-section-skips-when-same-name-section-already-exists, cli/append-section-duplicates-the-heading-when-the-body-starts-with-it)
- **The detail page no longer overwrites newer changes.** Ticking a checkbox or saving the editor on a page opened before an agent or the CLI changed the issue used to replace the body with the page's old copy, losing sections a transition had just appended. The save is now refused with a toast ("This issue changed since the page loaded"); a refused click is undone and the page reloads.
- **Ambiguous slugs fail instead of picking an issue.** A slug argument that is not an exact match used to resolve to the first issue whose slug contained it, with no warning, so `retrospective Sim` or `check foo` could change an unrelated issue. Now an exact slug wins, then a unique `/<arg>` suffix, then a unique substring. When several issues match, every slug-taking command exits 1 with `ambiguous slug "<arg>": N issues match:` and the candidates (up to 10), and writes nothing. A unique substring match still works and prints `note: resolved "<arg>" to <slug> (partial match)` on stderr. See docs/CLI/overview.md#slug-arguments.
- **issue-cli: flags work anywhere, mistakes exit 2, `--help` on every command.**
  - A flag after a positional used to be dropped silently: `list done --all` listed everything with exit 0. Flags now parse wherever they are (`--` ends them).
  - An unknown flag, a bad value, an extra argument, or a flag where the slug belongs fails with **exit 2**: one line naming the flag, the valid flags, and `Run: issue-cli <cmd> --help`. Other errors keep exit 1. Scripts that expected 1 on a bad flag (including the removed `--wait`) get 2.
  - `<cmd> --help` (or `-h`) prints that command's help with a Flags section. It needs no project and is never read as a slug or text: `comment --help` and `decide --help` used to fail.
  - The global `--json` also works after the command (`projects --json`, `list --all --json`).
- **`issue-cli check-edit <slug> <id> --text "…"`** changes one box's text and keeps its tick and id. **`replace --section S --line N`** or **`--match RE`** changes one line. When the line can't be chosen, the error lists the section with line numbers.
- **A retitle keeps the slug.** `issue-cli update --title` and the title editor on the detail page now write the old slug to a `slug:` field, so links and `parent:`/`depends_on:` references stay valid. `update --title … --rename` lets the slug follow the title: it renames the file and lists the issues still referencing the old slug.
- **`issue-cli list --all`** lists every status and version. Without it, when the project sets a version, the footer says how many issues the version filter hid.
- A box whose text starts with its own id (`- [ ] TP4: …`) no longer shows the id twice in `checklist`, `show`, `check` and gate errors.
- **Status page at scale.** The Built-in Status page is rebuilt for projects with hundreds of issues. Needs you, Working on and What's next become three tiles at the top, each with a count and one line; a click opens the full card over the page, and a click outside, focus leaving or Escape folds it back. Below, on one shared grid: Current milestone (one bar split by status, open issues by area) beside Local CI; Epics beside Waiting for release; Recently shipped beside Versions and Recently released. Side cards are exactly as tall as the card beside them; one that doesn't fit is cut off with "Show all", which opens it over the page. Epics group by area, with Active / Not started / All tabs, a Version select (the current milestone by default; a version counts only its children) and a text filter. Every list stops at a cap with "+K more" that opens in place, and folds, tabs and filters are remembered per project. `status.md` follows the page: what needs you and what is running first, then counts and groups, capped lists with the `issue-cli list` command for the rest. Release summaries render their bold and code instead of showing raw `**`; `status.json`'s `text` is plain. Playtest scenarios' `generate:` accepts `created: "-3d"`.
- **`transition --to done` no longer prints the parked guidance.** After `done` the only status left was `parked`, so the output showed "This issue is parked. Don't work on it…" and suggested `--to "parked"` as the next step, which an agent could read as its issue having been parked. A move to the last status now prints `== Done ==` (and the local CI line), and `--json` sets `"done": true` with no `next_status`. Side statuses (optional and `global`, or reached only by a `from: "*"` edge) are never suggested as the next step by `transition` or `start`.
- **Worktree excludes stay anchored.** A test now creates a worktree with real git and checks that `worktree_sparse_exclude: ["issues/"]` hides only the root `issues/` and keeps nested folders such as `demo/issues/`. The fix from v0.43 can no longer regress unnoticed. docs/agent-dispatch.md says exclude paths are relative to the repo root.
- **`report-bug` files real issues.** `issue-cli report-bug "<text>"` used to write JSON into `bugs/` that nothing read. It now files a `cli-bug` bugfix issue (status `idea`, system `CLI`) on the engine project's board, from any project, without `--project`. The engine project is `engine_project: <slug>` at the top of projects.yaml, else the project whose repo is `michal-franc/ai-workflow-engine` or the old `ai-native-project-viewer`. The issue has the first sentence as its title, a `## Report` with the full text, the reporter's project, issue, agent, version and last command (`--cmd` overrides it), and `reported_from` / `reported_by` fields. A report with the same title as an open cli-bug adds a `report:` comment instead; one that matches only a closed issue files `<title> (again)`. The output names the issue; `--json` is supported.
  - Without an engine project, or when its board is missing, the report is saved in `~/.local/share/issue-cli/bug-reports/` ("saved locally, not sent"). The next report-bug that finds the engine sends the queue, and so does `report-bug --send`. The Status page shows the unsent count under Needs you, and it adds one to the sidebar's Status number.
  - `report-bug --import ~/Work/bugs/*.json` files old reports the same way and moves them to `bugs/imported/`, which the Retros page skips.
  - The dispatch prompt says report-bug is for bugs in issue-cli or the engine; a project's own bugs go on its board with `issue-cli create`.
- **Dispatch prompt names the issue in its retrospective command.** The Workflow review block told agents to run `issue-cli retrospective <System>`, which fuzzy-matched some other issue of that system and filed the retro there. It now names the issue's slug. The prompt template uses named placeholders (`{{slug}}`, `{{system}}`, …) instead of about 20 positional `%s`, and a test checks that every placeholder is filled.
- **Viewer shuts down gracefully and has HTTP timeouts.** Ctrl-C or SIGTERM now waits up to 10s for in-flight requests and background work (a GitHub close, the CI watcher) before exiting. A second Ctrl-C exits immediately. An edit-in-nvim session still open at shutdown is closed and leaves an `edit-in-nvim aborted: server shut down` comment on the issue. The server sets read-header, read, write and idle timeouts (5s/30s/60s/120s), so a slow client can no longer hold a connection open forever. Startup errors (bad `-dir`, port in use) exit with status 1 and a single `issue-viewer: …` line. The project list logs a project that fails to load, with its slug.
- **Status: Waiting for release.** A new card on the Status page (and a block in `status.md`) shows what is merged and not released. It lists the `## Unreleased` changelog entries and the commits not pushed to the upstream. It also shows the current milestone (the first `version:` after the newest release) with its open issues and three checks: every issue done, local CI green on the head, and entries under `## Unreleased`. When all three pass, it shows "Ready to cut vX.Y.0" with the rename and push steps. Missing data hides its row. The lead's brief points at the block. Playtest scenarios gain `upstream:` (main tracks a branch).
- **Frontmatter values save exactly.** `set-meta`, `update --title` and the detail page edits no longer drop `$1` or `${x}` from a value. A title with `"` or `\`, or any value with a newline, no longer breaks the issue file on that write or the next one.
- **Viewer listens on localhost only, and refuses cross-origin writes.** The server used to listen on every interface, and any web page you opened could POST to it: dispatch agents, spawn the lead, open nvim, approve or delete issues. It now binds `127.0.0.1` by default; `-host 0.0.0.0` (or an IP) opts into the network and prints a warning at startup. Writes (anything but GET/HEAD) that a browser sends from another origin get a 403. The UI, curl and `issue-cli` are unaffected. If you reach the viewer from another machine, add `-host` to your start command.
- **Local CI.** An opt-in `ci:` block in `workflow.yaml` makes the viewer re-test the integration branch after every move (merge, pull, hotfix), so a break between parallel merges is caught and reported. Details: docs/workflow.md, "Local CI".
  - Runs live in tmux `ci-<project>` and survive a viewer restart; the viewer catches up on an untested head when it starts. Merges that land during a run give one more run, of the newest head. The worktree `.worktrees/.ci` is kept between runs, so builds stay incremental. Results go to `.ci/` in the main checkout.
  - When main goes red: a Local CI card on the Status page (state, Last runs boxes, failing command and tests, issues merged since green, log tail), an entry under Waiting on you, `status.md`, a `ci-red` chip and a `ci:` comment on each merged issue, and a `[from ci]` message to a live lead. `on_red: file_issue` also files one bugfix issue per red streak.
  - `issue-cli ci status|run|reset|watch` (`--json`). `ci run --tier slow` runs commands marked `tier: slow`, which never run on merge. `ci watch` works without the viewer.
  - Agents see the one-line state in `start` and the dispatch prompt, with red marked as inherited, and `transition --to done` tells them not to wait for CI. The lead brief says not to push while main is red.
  - This repo runs `make validate` on every move of main, with `make ux-test` as the slow tier.
- **`issue-approve` mod 0.2.1:** the Playtest button also shows at `testing` behind an owner gate, so a tweak (whose path has no human-testing) can be playtested from the agent's session. Update with `claude plugin update issue-approve@ai-workflow-engine`, then `/reload-plugins` in running sessions.
- Canvas: lanes start folded, one compact row each with a count per status for every version. Click a lane head (or Expand all) to see its cards; open lanes stay in the URL (`?open=`). A new Epic select (`?epic=`) shows one epic and its sub-epics, unfolded. System rows fold the same way.

## v0.52.0 — 2026-10-22

Focus: project status pages. Two Safe core (0.53) fixes that landed early ship with it: one lead per project, and the bootstrap workflow lookup.

- **One lead per project: sub-leads are gone.** Every worker reports to the project lead, epic children included, and epics get no base branch of their own: children branch from `main` and ship to `main` like any issue.
  - `issue-cli lead spawn --epic` fails with one line, and the `lead/spawn` endpoint answers `422` to a body naming an epic.
  - `lead list` shows the project lead and the open epics with their children running (`--json` is now `{lead, epics}`); it no longer suggests spawning anything per epic.
  - `approve`, `lead whoami` and `lead message` lose their per-epic branches. A leftover `lead-<epic>` session or `ISSUE_VIEWER_LEAD_SCOPE=<epic>` is no lead.
  - Dispatch prompts name the project lead and the issue's epic, with no `## Base branch` section.
  - The agents canvas lists a leftover `lead-<epic>` session with the other sessions; `agents.json` epics keep only the issue and children counts. The detail page's Lead section always names the project lead. The issues canvas no longer shows an epic's base branch.
  - Old `base:` and `lead:` fields on epics still load as plain custom fields; nothing writes them. An old `leads.epic` brief in `workflow.yaml` still loads, is ignored, and `issue-cli process workflow` warns about it.
- **issue-cli loads the right workflow without `--project`.** Run from a project root with `./issues/`, issue-cli used to take the workflow of the *last* project in `projects.yaml`. From `~/Work/issue-viewer`, that was Demo's. It now uses `./project.yaml`, then `./workflow.yaml`, then the configured project whose `workdir` or `issues` is that directory. If several match, it fails with the `--project` hint.
- `process workflow`, `process transitions` and `show` print `Workflow file: <path>`, and `show --json` adds `workflow_file`.
- **Project status pages.** Two new optional `workbench:` keys in `workflow.yaml` let a project show its own HTML in the viewer. See "Project pages" in `docs/workbench-files.md`.
  - `status_page: docs/status/index.html` makes `/status` show the project's page by default, with **Project status** / **Built-in** tabs. `/status?view=builtin` and `/status.md` stay built-in.
  - `pages:` (each with a `title` and `path`) adds indented rows under Status in the sidebar, at `/pages/<n>`.
  - Pages run in an iframe sandboxed without `allow-same-origin`. `/page-raw/` serves only files under a configured page's folder, with CSP `sandbox` and `nosniff`. Any non-GET request with `Origin: null` gets a 403, so a page can't approve or dispatch.
  - An open page reloads when its files change. It gets live counts from the viewer through `postMessage({type: "issue-viewer.status", project, status})`, with the same model at `/status.json`.
  - `process workflow` now warns about bad `workbench:` paths, and `help schema` lists `workbench.pages[]`.
- Playtest sessions: `post` takes `headers:`, and a new `write` action replaces a file under the scenario's project. REPEAT runs no longer differ on a minute-only timestamp such as `status.md`'s.

## v0.51.0 — 2026-10-22

- **One terminal, a session per project.** Two new top-level keys in `projects.yaml`, `terminal:` and `tmux_session_per_project: true`, set machine-wide defaults. Every project without its own value inherits them.
  - With `terminal: none` and a session per project, each project's lead and workers are windows of one tmux session, and nothing ever opens a terminal.
  - Switching session (`prefix s`) switches lead.
  - A new "One terminal, a session per project" recipe in `docs/agent-dispatch.md` covers this, with an optional status bar listing every project session.
- With `terminal: none`, a dispatch or re-dispatch no longer selects the worker's window in a shared session, so the lead keeps the focus. "Open in tmux" on the agents canvas still selects it.
- **`switch_cmd`** (`tmux switch-client -t …`) now comes alongside `attach_cmd` in dispatch, lead-spawn and open responses and in agents.json, because a plain `tmux attach` refuses to run inside tmux.
  - `issue-cli lead spawn`, the agents canvas panel and the nvim edit hints print both, as `Inside tmux:` and `From a shell:`.
  - The canvas button becomes **Copy switch** for `terminal: none` projects.

## v0.50.0 — 2026-10-22

- **Playtest layout checks:** ux-test now notices a page whose styles didn't apply. In v0.48.0 the Status and Metrics pages rendered unstyled on desktop and every session still passed.
  - **`styles_applied` layout rule:** every class on a visible test id element must be matched by a CSS rule that is active at the viewport (top level, a matching `@media`, a holding `@supports`). A failure reads `class "status-epic" has no CSS rule active at 1440x900`. Classes only JS reads take a `js-` prefix. It is on in every session.
  - **Shape checks:** `beside: [a, b]` (same row, b to the right), `below: [a, b]`, `css: { target, property, equals | in | not }` and `size: { target, min_width?, max_width?, min_height?, max_height? }`. They work in ux-test, `wait_for` and the overlay; a failure shows both boxes or the measured value.
  - The status, metrics, system, workboard, canvas, agents and sidebar sessions now assert their main layout. A test replays the v0.48.0 stylesheet and expects the status page to fail.
- Classes the new rule found unstyled are fixed: the "Not this branch" balance chip and worker nodes get their own style; unused marker classes are removed.

## v0.49.0 — 2026-10-22

- New **Board** in the sidebar: `/workboard`, a read-only kanban whose cards carry coordination state. The old board moves to More as **Classic board** and keeps drag-and-drop.
  - Each card shows its time in status (amber after a day) and its epic's progress.
  - **Marker chips:** amber for waiting on you (a ready gate, a playtest due, an open `question:`); red for `blocked on <slug>` and needs revision; grey for parked.
  - **Agent footer:** working, idle, waiting on you, or stopped.
  - **Grouping and filters:** group by status, system or epic; filter by version, system and "Waiting on you".
  - **A card legend** in the sidebar.
- **Readable at hundreds of issues.**
  - Idea, parked and done fold to count tiles that link to the list.
  - The board opens on the cards that need attention. **Show all** adds the quiet ones as one-line rows, and each column links its hidden ones (+N quiet).
  - A column longer than 12 cards links to the list.
- **New frontmatter:** `blocked_on: <slug or list>` and `needs_revision: "<why>"`, plus the `needs-revision` label, show as markers on the board and on system-page rows. They don't gate transitions yet.
- **Agents canvas:**
  - A live agent with no issue-cli call for 15 minutes is **idle**.
  - agents.json gains `last_call` and `last_cmd`.
  - `/agents?agent=<session>` selects an agent.
- **Playtest runner:**
  - Scenarios take `generate:` to write hundreds of issues without a fixture per issue.
  - `make playtest` over several sessions now moves on to the next session after the last verdict, opening a fresh tab. It used to stop with `__ptSend` already in use.
- **Breaking:** `--wait`, `--timeout` and `--interval` are removed from `issue-cli transition` and `issue-cli start`. At a human-approval gate an agent tells the human what to approve, then stops and ends its turn; the viewer's approve button nudges its tmux session to resume. Passing one of the flags fails with `--wait was removed in v0.49.0: stop and end your turn; the approval nudge resumes you`. Exit code 3 and the `wait_timeout` telemetry class are gone, `transition --dry-run` suggests stopping instead of a `--wait` command, and `transition --json` no longer has a `wait` object.
- The stats sidecar no longer writes `pending_wait` or per-row `wait_started_at`. Older sidecars that hold them still load; the fields are dropped on the next save.

## v0.48.1 — 2026-10-22

- Fix: the Status and Metrics pages lost their layout on desktop. A merge dropped the closing `}` of the system pages' `@media (max-width: 900px)` block in `static/style.css`, so every later rule only applied below 900px. A new test fails on unbalanced braces in `style.css`.

## v0.48.0 — 2026-10-22

- **System pages** (`/p/<project>/systems`, `/systems/<System>`, `/systems/<System>/<Family>`). They are read-only, and every project whose issues set `system:` gets them, with or without config.
  - **Index:** one card per system, most open first: summary, design doc, open / done counts, a status bar, what waits on you, and family chips. A "No system" line counts the rest.
  - **System page:**
    - the header: summary, counts, family or umbrella progress, "N waiting on you", and links to Board, List and Decisions filtered by system;
    - the issues: furthest along first, with a status bar;
    - families, then the epics the system's issues belong to (with whole-epic counts when an epic spans systems);
    - design-doc sections marked **current**, **stale** (the section cites a replaced decision) or **changed / new / removed on branch** (edited on an open branch since it left main; old branches behind main mark nothing);
    - the latest decisions, open balance notes, and the guard-metrics and knobs panels from the Metrics view.
  - **Family page:** the same page, scoped to an umbrella's children inside the system.
  - A system without config shows a hint naming `workbench.systems.<name>`. An unknown name returns a 404 that lists the systems.
- **`workbench.systems.<name>.families.<family>`**: `umbrella` (required), `summary`, `design_doc`, `metrics`, `knobs`. `issue-cli process schema` lists them.
- **Sidebar:** **Systems** comes after Status whenever an issue has a system. On the systems pages, its panel lists the systems with their families indented under them.
- **Balance board** takes `?system=`. **Doc pages** give headings ids, so a section can be linked.
- issue-viewer's own `workflow.yaml` declares its UI, CLI, Workflow and API systems and the UI › Workbench family.

## v0.47.0 — 2026-10-22

- **Metrics view** (`/p/<project>/metrics`). It is read-only over JSON files the project writes, documented in `docs/workbench-files.md`.
  - **Guard metrics** (`workbench.metrics.probe_results`, optional `targets`): band, master value, verdict and each open branch's value. Metrics are matched by id. A branch value comes from the branch's worktree file, or from the copy committed on a branch with no worktree. A branch value notes when its run is behind the branch, when the branch changes the band, when a metric exists only on the branch, and when the branch run doesn't measure it.
  - **Knobs** (`workbench.tuning`, now a list of knob export files): each knob's value, its comment, its history on main (`30 → 20 · Play calls balance pass 2`) and the open branches that change it.
  - **Branches** (any git project): open worktrees and `work/*` branches, each compared with its epic's base or the branch it was forked from, plus the files several branches change.
  - In the sidebar, the view comes after Balance and shows "N off" in red. A git project without metric or knob files gets it as **Branches**.
- **`workbench.systems`** (and their families) take `metrics:` and `knobs:` lists. A system page can embed the new `system-guard-metrics` and `system-knobs` panels.
- **Balance board:** metric groups show their band and master value.
- **`depends_on` frontmatter:** `issue-cli set-meta <slug> --key depends_on --value "a/b, c/d"` sets it, and refuses unknown slugs, suggesting close ones. The detail page shows Depends on, Depended on by, Decided by (cited decisions with their state) and Shared files.
- **`issue-cli show`** prints the same links, a "Guard metrics for <system>" line and `== Shared files ==` for the issue's open branch.
- **Playtest scenarios** take `commits:` and `branches:` (worktrees, uncommitted files). Scenario commits have fixed dates, so `REPEAT=2` runs match.

## v0.46.0 — 2026-10-22

- The `issue-approve` Claude Code mod (0.2.0) adds **▶ Playtest** beside the approve buttons when the issue waits on the owner's playtest and the worktree has a `make playtest` target: it runs the playtest in the agent's worktree, then **✓ Report** runs `make playtest-report APPLY=1`. Running sessions pick it up with `/reload-plugins`.

## v0.45.0 — 2026-10-21

- **Status page** (`/p/<project>/status`), the first sidebar entry. It shows where the project stands without asking the lead: overall, per-version and per-epic progress; the issues in flight with their agent and time in status (amber at 7 days, red at 14); the project lead's `## Next` list, the backlog by priority and parked issues with their reason; what waits on you; and the newest 3 releases plus issues done in the last 7 days.
- "Waiting on you" is one rule for every view: gates where your approval is the only thing missing, human-testing issues with a playtest session and no judge run, and issues whose newest comment is a `question:`. The sidebar's Status entry shows the count on every page.
- **Copy as markdown** and `/p/<project>/status.md` give the same summary as text, a stand-up to paste.
- `workflow.yaml` gains `workbench.changelog` (default `CHANGELOG.md`) for the releases the Status page lists; without one it falls back to git tags. issue-viewer points it at `cmd/issue-cli/CHANGELOG.md`.
- The project lead's brief asks for a `## Next` section in every handoff note and names `status.md` as the stand-up.
- Playtest scenarios take `root:`, a folder copied into the project root (handoff notes, a changelog, playtest sessions).

## v0.44.0 — 2026-10-21

- **Breaking:** the read-only `/workflow-flow` page ("Agent Execution Flow") is removed. It was hand-written, not built from `workflow.yaml`, out of date, and unlinked since the designer went. [docs/agent-workflow-flow.md](../../docs/agent-workflow-flow.md) is the current description of the agent flow.

## v0.43.0 — 2026-10-21

- Approve from the agent's terminal: the `issue-approve` Claude Code mod (`claude-mods/issue-approve/`, installed once at user scope from that folder as a local marketplace) shows the issue's approvals above a dispatched agent's prompt as buttons that do what the detail page's checkboxes do. New `GET /p/<project>/issue/<slug>/approvals.json` (the same list as the Human Approval box) and `GET /project-for?cwd=<path>`; dispatch exports `ISSUE_VIEWER_PROJECT`.
- Fix: dispatched worktrees no longer hide `demo/issues/`. Sparse-checkout excludes are anchored at the repo root (`!/issues/`), so `TestDemoConfig` passes in new worktrees.

## v0.42.0 — 2026-10-21

- **Breaking:** the viewer's workflow designer is removed: the **Workflow** entry in the sidebar's More group, the `/workflow-designer` page, and its `/workflow-designer/data`, `/workflow-designer/preview` and save (`POST /workflow-designer`) endpoints. Edit `workflow.yaml` as text; `issue-cli workflow upgrade` pulls in template changes and `issue-cli process schema` prints the format. The read-only `/workflow-flow` page stays, with no sidebar entry.

## v0.41.0 — 2026-10-21

- The viewer's top bar is replaced by a left sidebar on every project page. It shows the project name and version, New issue, the main views (Board, List, Balance when configured, Decisions, Docs), and a **More** group (Graph, Canvas, Agents, Retros, Workflow, Stats, GitHub) that opens on its own views and remembers whether you left it open. Below those come the running-agents count, Spawn lead with the project's actions, and the theme swatches. On the Board the sidebar also lists systems with open counts; each one filters the board.
- New default dark theme from the workbench mockups: condensed bold page titles, monospace slugs, rounded cards and chips in four tones. Dracula and light are restyled to match. The fonts (Archivo, JetBrains Mono) ship with the viewer, so it makes no external font requests.
- Below 900px the sidebar stacks above the page, and no page scrolls sideways.
- For views and playtests: views are listed once, in `navViews` (`nav.go`). Every project template renders `{{template "sidebar" (sidebar .Prefix "<key>")}}`. The new test ids are `nav.sidebar`, `nav.more`, `nav.create`, `nav.panel.system.*` and `nav.theme.*`. `header.bar`, `header.create` and `actions.bar` are gone, and `nav.tab.*` is kept.

## v0.40.0 — 2026-10-21

- **Breaking:** `issue-cli ask` and `issue-cli answer` are removed, with the `## Questions` / `Q-n` format, the open-questions block in `show` and `checklist`, and question-wait stats. Questions stay in chat (Claude Code or Codex; workers can message their lead with SendMessage). Only the outcome is recorded, with `issue-cli decide`. Existing "answered in lead chat (Q-n)" decision lines still parse and still show on the Decisions tab.
- The dispatch prompt tells workers to record a human's decision with `issue-cli decide <slug> --text … --quote …`. The project lead's brief records relayed answers the same way and no longer suggests sub-leads (one lead per project).

## v0.39.2 — 2026-10-21

- Agents canvas shows the hierarchy only: You → the project lead → its workers (plus a sub-lead only while one is live). Epic boxes, the not-running sub-lead placeholders and the Group umbrellas toggle are gone; epics live on the issues canvas, and their data stays in `agents.json`. Radial is one ring around the lead.
- Agent names on the canvas drop the `agent-` prefix and are cut to fit, with the full name as a tooltip.
- An epic's detail page names the project lead (not a stopped sub-lead) in its Lead section.
- At phone width the canvas no longer scrolls the page sideways: issue cards wrap under their agent.

## v0.39.1 — 2026-10-21

- The **Spawn sub-lead** buttons are gone from the agents canvas (epic box and selected-agent panel) and from an epic's detail page: one lead per project for now. `issue-cli lead spawn --epic` and the endpoint still work.

## v0.39.0 — 2026-10-21

- Fix: agents from other projects no longer show up in this project's views. The agents canvas, the board's and list's active-agent markers, approval notifications and `issue-cli lead list` / `whoami` listed every `agent-*` / `lead-*` session on the tmux server, so another project's workers appeared as this project's (and an equal slug in two projects looked like the same agent). A project with `tmux_session` now only sees that session's windows; without one, only standalone sessions started under its root.
- Parked status: issue-viewer's `workflow.yaml` gains a global `parked` status reachable from any status, with a required reason under `## Parked Record`, and a board column. Agents dispatched onto a parked issue stop.
- Types of work in issue-viewer's own workflow: `feature` (default, full path), `tweak` (no design or docs) and `bugfix` (a `## Repro` section and a failing test first), via `issue-cli workflow upgrade` against the bundled `development@v0.36.0` template. Shell gates and scoring were declined.
- `make playtest`, `ux-test` and `playtest-report` infer `ISSUE` from a dispatched worktree's `work/<slug>` branch; `ISSUE=` still overrides.

## v0.38.0 — 2026-10-21

- New Decisions tab, `/p/<project>/decisions`: a generated index of every `- **D-n** · date · how: …` line under the issues' `## Decisions` sections, with how it was made, the quote, the recording issue, the issues that cite it (`<slug>#D-n`) and its state: active, deferred, replaced, or replaced · stale when open issues still cite a replaced decision. System and state chips filter it, and "copy ref" copies the ref. Nobody edits the list; it reloads when an issue changes.
- `issue-cli decide <slug> --text "…" [--how ..] [--quote ..] [--deferred] [--replaces <slug>#D-n]` records a decision. With `--replaces` it lists the open issues that still cite the old one. `issue-cli decisions [--system] [--state] [--issue] [--json]` prints the same index, and `show` prints a `== Cites replaced decisions ==` block on a citing issue.
- Structured questions: `issue-cli ask <slug> --question .. --option "A=Label|Tradeoff" … --recommend B` writes a `### Q-n · open` entry under `## Questions`; the worker says so in chat and stops, and the lead relays it. `issue-cli answer <slug> Q-n --option B [--note ..]` collapses the question to an answered pointer and appends an "answered in lead chat (Q-n)" decision. `show` and `checklist` print open questions. `ask --wait` / `--wait-for Q-n` block until answered (`--timeout` exits 3), for scripts.
- New Balance tab, `/p/<project>/balance`, opt-in with a `workbench.balance` block in `workflow.yaml`: typed `balance:` notes grouped by metric (state, hypothesis, change, evidence, verdict, pass, earlier notes), joint passes from `balance_pass:` issues with a Start pass button, and free-text `balance:` comments under Unsorted. `issue-cli balance <slug> --metric .. --state ..` writes a typed note (a later note with the same metric and change updates it), and `balance --list` prints the current notes. Without the block the tab is hidden.
- Queued approvals: the detail page's "Approved for backlog and in progress" sets `human_approval: backlog` and `queued_approval: in progress`, so an agent goes design → backlog → in progress with one click. A second click clears both.
- `workflow.yaml` accepts an optional, project-wide `workbench:` block (systems, metrics, tuning, balance). Paths must be relative and stay inside the project root; `process schema` lists it.
- Playtest: a `workbench` scenario and sessions for the Decisions tab, the Balance tab and queued approvals / ask / answer, with `data-testid` labels on both pages.

## v0.37.0 — 2026-10-21

- Lead agents are a feature, not a convention. A project lead (`lead-<project>`, in the project checkout) supervises the project; a sub-lead (`lead-<epic-slug>`) stabilises one epic on its base branch. The depth is fixed at project lead → sub-lead → worker.
- `issue-cli lead spawn [--epic <slug>]` starts or re-attaches to a lead without the viewer running. `--epic` creates the base branch (the epic's `base:`, else `base/<slug>`, from main) and its worktree, and records `base:` and `lead:` on the epic. Workers and sub-leads are refused with a hint.
- `issue-cli lead whoami` shows your role, scope, lead and what you may tick. `issue-cli lead list` shows every lead, its base, main commits not yet in the base, children running, and epics with no sub-lead. `issue-cli lead message <target> "text"` pastes a signed message into a session (leads only).
- `issue-cli approve <slug> --to <status> --quote "…"` records a gate the human approved in chat: an `approval:` comment with their words, and `human_approval` set without toggling. Workers, sub-leads outside their epic and Human Testing are refused.
- `parent:` is the child → epic link; `umbrella:` is read as an alias, and `show`/`start` print a rename hint. Dispatching a child of an epic with `base:` branches its worktree from the base, and the epic itself reuses the base worktree.
- Every dispatch prompt gains `## Your lead` (who the worker reports to: the epic's live sub-lead, else the project lead) and, for children of an epic, `## Base branch`. `show` and `start` print a `Lead:` line.
- `workflow.yaml` gains an optional `leads:` block (`project:` and `epic:` briefs, with `{{project}}`, `{{lead}}`, `{{project_lead}}`, `{{epic}}`, `{{title}}`, `{{base}}`). Without it, built-in briefs apply. Each brief ends with the lead's role, authority, messaging and handoff note (`sessions/YYYY-MM-DD-lead-<scope>.md`).
- Claude agents and leads start as `claude -n <tmux name>`, so their tmux name is also their address for Claude Code's built-in messaging (ListAgents / SendMessage). Briefs say agents may message their lead or a peer that way.
- New agents canvas, `/p/<project>/agents`: you → project lead → one box per epic (base branch, children running, main ahead, sub-lead) → workers as working, at gate, question or stopped, plus an Unfinished jobs lane. Tree and Radial layouts, and Show issues / Group umbrellas / Show stopped toggles. Clicking a live agent opens its tmux session in a terminal, or focuses the one already open (i3, by window title). The panel has Approve gate, Spawn, Copy attach, recent comments and the branch's changed files.
- Spawn lead in the Actions bar and on the canvas, Spawn sub-lead on epic boxes and in a new Lead section on the epic's detail page.
- The default i3 + alacritty terminal titles its windows with the session name. An attach command names a private tmux server when the viewer runs with `TMUX_TMPDIR` set.
- Playtest runner: scenario projects are now headless (`terminal: "none"`), so a dispatch or spawn in a scenario no longer opens a window attached to the real tmux server. `REPEAT=2` ignores timestamps and the scenario's temp dir in check output.

## v0.36.0 — 2026-10-21

- Bundled workflow templates are versioned. `development`, `review` and `writing` start with `template: <name>@v0.36.0` and a `features:` record, so `workflow init` writes a stamped, recorded file. `template: custom` opts a hand-built workflow out.
- The refreshed templates add: approval gates that hand off to the lead (the agent says what it needs and ends its turn; no waiting or polling), design overlap and discoverability checks, throwaway-sandbox manual tests, a rule for documenting unrelated test failures, types of work (`feature`, `tweak`, `bugfix`) in `development`, and a sample project action.
- New feature catalog (bundled `templates/workflow-features.yaml`): 35 entries, one per `workflow.yaml` capability (statuses, gates, automation, actions, board, worktrees, template conventions such as the v0.34.0 playtest-session wording). Each has what/why/where, a YAML snippet, the question to ask the human, and structural detection probes over the parsed YAML.
- `issue-cli workflow scan` detects which catalog features a `workflow.yaml` has and writes its `features:` record (installed `<id>@<version>`, declined `<id>`). It rewrites only that block. `--dry-run` previews; `--installed`, `--declined` and `--forget` record what an upgrade picked, declined or re-opened.
- `issue-cli workflow upgrade` (alias `issue-cli upgrade`) is read-only. It proposes the features a project lacks, item by item, using the `features:` record first and the probes as a fallback, lists declined features, proposes updates for older installed versions, and prints the upgrade steps. `--all` lists the whole catalog, and `--json` gives agents a plan.
- `start` and `process workflow` print a one-line hint when upgrade has proposals: tell the operator, and upgrade only if they ask.
- `process schema` lists `template` and `features`, and the web designer's save keeps both.
- `issue-cli workflow init` works in an empty directory without a `projects.yaml`.

## v0.35.0 — 2026-10-21

- **Issues canvas.** A new **Canvas** tab (`/p/<project>/canvas`) shows what each epic holds and what each version plans, on one pannable, zoomable map.
  - Columns are versions, sorted as dotted numbers (0.2 before 0.10). Each header shows done/total, side-status counts and a status bar.
  - Rows are epics by default. An epic is an issue with `type: epic` (EPIC), or one that another issue's `parent:` or legacy `umbrella:` points at (UMBRELLA). Each row shows the epic's base branch and a roll-up of its children; sub-epics are indented, and "No epic" comes last. `?rows=system` switches to one row per system, with an epic badge on each card.
  - Issues without a version are hidden unless `?nover=1` (always shown when no issue has a version). Done and obsolete cards are dimmed, and `?done=collapse` folds them into a count per cell.
  - Highlight one epic (`?hl=<slug>`) from its row, a card's badge, or **View on canvas** on the epic's detail page.
  - Pan by dragging or with the wheel, zoom with Ctrl+wheel, or use −/+/Fit. Nothing is saved; the layout is always computed.
- Web UI: "in progress" and "in design" now get their status colours (yellow, blue) everywhere. They rendered gray because the colour map's keys didn't match `canonicalStatusKey`.
- Playtest: `canvas-basics` and `canvas-phone` sessions on a new `canvas` scenario; the canvas carries `canvas.*` test ids.

## v0.34.0 — 2026-10-21

- **Playtest sessions.** An issue's manual checks can now be a YAML session in `playtest/sessions/`, each step's id one of the issue's checkbox ids.
  - `make ux-test ISSUE=<slug> [REPEAT=2]` runs the machine checks in headless Chrome (chromedp), at desktop and phone width, against a throwaway scenario board. Each step gets its own git project, config, HOME, private tmux server and branch-built binaries. The run checks layout rules and console errors and takes screenshots.
  - `make playtest ISSUE=<slug>` shows the owner only the judgement steps, in an overlay the runner injects into a Chrome window it drives: highlighted targets, DO / EXPECT / WHY, live checks, Pass / Fail / Skip / Replay, and Alt+N opinions.
  - `make playtest-report ISSUE=<slug> APPLY=1` ticks the boxes that passed and posts failures and opinions as `playtest:` comments.
  - `make playtest-check` validates every session and is part of `make validate`.
- Web UI: the list, board and detail pages carry `data-testid` labels. They are all listed in `internal/testids`, and a test keeps the two in step.
- CLI: `issue-cli process playtest` prints the shared playtest contract. `process playtest web` and `process playtest game` print worked examples that bots in other projects follow to build their own playtest system.
- Workflow: in this repo's dev workflow, the Test Plan asks for a playtest session, Testing asks for a `ux-test REPEAT=2` run before handoff, and the human-testing prompt sends the owner to `make playtest` and the report. The bundled `development` template gets opt-in wording.
- Build: Go 1.25 is now required (for chromedp).

## v0.33.2 — 2026-10-08

- Dispatch: Claude now starts with its briefing as the launch argument instead of a paste typed in after a fixed 3 s wait. When Claude took longer to boot, the paste was dropped and the agent sat at an empty prompt while every step reported ok. Dispatch now polls the pane and reports **Prompt delivered**, or a failed step after 20 s.
- Web UI: a **Re-send prompt** button in the issue sidebar and the reattach banner (`POST /p/<project>/issue/<slug>/dispatch/reprompt`) pastes the current briefing into the issue's live agent session, after a confirmation.
- API and CLI: `GET /p/<project>/issue/<slug>/dispatch-prompt` (`?format=json`) and `issue-cli dispatch-prompt <slug> [--json]` print the prompt a dispatch would send, read-only, from one shared builder.

## v0.33.1 — 2026-10-06

- `make demo` works on a fresh clone. Its config, `demo/projects.yaml`, was hidden by the `projects.yaml` rule in `.gitignore` and never committed, so the board exited with `Failed to load config`. The file is now in the repo, points at the demo's own `workflow.yaml`, and uses `terminal: "none"` so ▶ prints the `tmux attach` command instead of opening i3 + alacritty. A test keeps the demo loadable.

## v0.33.0 — 2026-10-06

- Workflow: types of work. `workflow.yaml` can define `types:` (for example feature, tweak, bugfix, epic) and a `default_type`, and an issue's `type:` frontmatter picks its path. A type is an overlay like `systems:`, plus:
  - `path:`, the ordered subset of base statuses the type walks;
  - `replace: true` on a transition, which overwrites the base edge's actions (for example to drop a gate).
  - Resolution is base → type → system. Validity, the `== Next ==` hint, approvals, prompts and appended sections all follow the type's path.
  - Base edges whose two ends are on the path are inherited. A status the type skips can't be re-added by a system overlay.
  - Projects without `types:` behave exactly as before, and `type:` stays a plain custom field there. Design and a worked example: `docs/Workflow/types.md`.
- CLI: `create --type <t>` starts the issue at the type's first status and names the other types. New `set-type <slug> <type>` changes the type while the issue is at its first status; later, it points at the viewer, because past triage the type is the human's call. `set-meta --key type` is refused in typed projects.
- CLI: `show`, `start` and `transition` print `Type:` and the type's path, and warn on an undefined type or a status off the path. A bad-order error names the type's path. `process workflow` lists the types with their paths and the `workflow.yaml` lint warnings: section gaps, bad `path` entries, and a type and a system overriding the same prompt. `process workflow --type`, `process transitions --type|<slug>` and `list --type` scope to one type. `--json` outputs carry `type` and `type_path`.
- Web UI: when a project defines types, the board and list get a type badge and filter, `graph?type=` draws one type's path, and the create modal has a type select. The issue sidebar has a Type select for the human, which asks for a status on the new path when needed. `issues.json` and the dispatch briefing include the type.
- Workflow: overlay-added transitions now keep their `fields:`; they were silently dropped before.

## v0.32.0 — 2026-10-02

- CLI: `issue-cli` now records local usage telemetry: one names-only JSON line per invocation in `<workdir>/.agent-logs/telemetry.jsonl`, or `~/.local/state/issue-cli/telemetry.jsonl` when no project resolves.
  - It records the command, subcommand or topic, flag names, project, issue, exit code, error class, caller type (`dispatched`, `agent`, `tty` or `script`) and duration.
  - The only argument values kept are `--to` and `--section`. Titles, bodies and other free text are never stored. Nothing is sent off-machine.
  - The file rotates to `.1` at 10 MB.
  - Opt out with `ISSUE_CLI_TELEMETRY=off`, or `telemetry: false` on a project in `projects.yaml`.
  - The agent timeline and retry hints are unchanged; they keep their separate log.
- CLI: new `issue-cli telemetry report [--since 30d] [--global] [--top N]` (and `--json`) shows calls per command, the never-used commands, aliases, subcommands, help topics and per-command flags, the top errors, unknown commands and flags that bots tried, and what callers ran next after a failure. `issue-cli telemetry path` shows the active file and whether recording is on.
- Web UI: the `/stats` tab has a new "CLI usage (last 30 days)" section built from the telemetry report. If the CLI is missing or telemetry is disabled, it shows an inline notice.
- Workflow: the bundled `workflow.yaml` Test Plan `### Manual` section now asks for a throwaway, worktree-only sandbox scenario with isolated config, issues and state, plus exact human steps, expected results and cleanup. The `testing` and `human-testing` prompts point at it.

## v0.31.0 — 2026-10-02

- CLI: `issue-cli transition <slug> --to <status> --dry-run` lists every unmet requirement at once (unticked gate boxes, missing comments, required `--field` answers, human approval), each with the command that fixes it, and changes nothing. Exit 0 when the transition would succeed, 1 otherwise; `--json` returns `{dry_run, ready, from, to, problems[], side_effects}`.
- CLI: `issue-cli transition … --wait` and `issue-cli start <slug> --wait` block until the required human approval exists, then proceed — instead of agents retrying until the approval lands. Unmet machine checks fail fast before any waiting. `--timeout <dur>` gives up with **exit code 3** (nothing changed; re-run to keep waiting, and the original wait start is kept); without it the wait is unlimited. Agents in a Bash tool should use `--timeout 9m` and not pipe the command (a pipe hides exit 3).
- CLI: agents discover `--wait` from the output they already read. The `== Next ==` block prints the `--wait` command (with `start` from handoff statuses) when the next step needs approval, but never for `done`. The missing-approval error ends with the exact `--wait` command, and the repeated-failure hint points at it. `transition --json` gains `next_command`, `next_command_note` and `wait`.
- Stats: transitions in `<issue>.stats.json` record `wait_started_at` (when an agent began waiting) and `approved_at` (when the viewer recorded the approval). `start` now records its handoff advances, which were previously missing from stats.
- Web UI: approving an issue timestamps the approval for stats, and failed tmux nudges to the agent session are logged on the server.
- Workflow: the bundled `workflow.yaml` design phase now asks for an overlap review (similar existing features, and whether this one is still needed) and a discoverability plan. Approval-gate prompts tell agents to ask in chat and then block on `--wait`.

## v0.30.0 — 2026-10-02

- CLI: checkboxes now have short, stable ids, and `issue-cli check` ticks several in one call: `check <slug> D3 D4 AC1`. An id is the section's initials plus the box's index in that section (`D3` = 3rd box under `## Design`). When initials collide, the section earlier in the document keeps the short form and later ones extend their first word (`Documentation` → `Do`, `Deployment` → `De`). Ids are case-insensitive, and the long form `Design#3` also works. Several ids are all-or-nothing: an unknown id ticks nothing and exits non-zero. Text matching, `--index`, and `--section --index` work as before; a single word that looks like an id but names no section is still treated as text.
- CLI: `check <slug> --section "Design" --all` ticks every open box in a section. On a complete section it is a reported no-op.
- CLI: `checklist`, `show`, `start`, and the checklist printed after `transition` show each box's id (`D3 [ ] text`, replacing `3. [ ] text`), grouped by section. When boxes are open they end with a `Tick done boxes by id, several at once: issue-cli check <slug> <id> [<id>...]` hint. `check` confirmations read `✓ Checked: D3 [Design #3] text` and add per-section progress (`Progress: 9/14 (Design 4/4)`). Ambiguous text matches suggest the id to use. `checklist --json` items gain `id`; `transition --json` checklist items gain `id`, `section`, and `index`.
- Workflow: a transition blocked by `section_checkboxes_checked` / `all_checkboxes_checked` now says `N of M boxes still open in section "X"`, lists each open box with its id, and gives `issue-cli check <slug> <ids>`. The old `N/M checkboxes incomplete` printed the *checked* count, which agents misread as the number open. Gate and progress counts now skip `- [ ]` lines inside fenced code blocks, matching `checklist`.
- Agent dispatch: the dispatch prompt tells agents to tick finished boxes by id, several at once, with text matching as a fallback.

## v0.29.0 — 2026-06-14

- Web UI: custom actions can now run at the project level, not just per issue. A new `project_actions:` list in `workflow.yaml` renders one-shot agent buttons in an "Actions" bar on the list, board, and graph views. Unlike issue actions they are not bound to a single issue, so their prompts template with `{{project}}` only. Clicking a button POSTs to `/p/<project>/action/<id>` and dispatches a fresh agent in the project checkout.
- Workflow config: the per-issue `actions:` key has been renamed to `issue_actions:` to parallel `project_actions:`. The old `actions:` key still works as a deprecated alias, so existing `workflow.yaml` files need no change; if both are present, `issue_actions` entries win on an id clash and legacy-only ids are merged in.

## v0.28.0 — 2026-06-08

- CLI: `issue-cli check` can now address a checkbox by stable index instead of verbatim text: `check <slug> --section "Design" --index 2` ticks the 2nd box in that section, and `check <slug> --index 5` ticks the 5th box in the whole body. Indexes are 1-based and stable — they count checked and unchecked boxes in document order, so an index never shifts as boxes get ticked. Re-checking an already-checked box is a reported no-op, not an error.
- CLI: a text query to `check` that matches more than one *unchecked* box now errors and lists every candidate with its `[Section #index]` label (instead of silently ticking the first), so you can re-run with `--section/--index`. A single match still checks as before.
- CLI: `checklist`, `show`, `start`, and `check`'s listings now group checkboxes by `## ` section and print each box's stable index, so the value to pass to `--index` is visible. `checklist --json` gains an `items` array with `section`, `index`, `text`, and `checked` per box.
- Workflow: the `section_checkboxes_checked: <Section>` validation no longer passes vacuously when the gated section is missing or has no checkboxes — it now fails with a clear message. This closes a hole where an issue imported without a templated section (e.g. `Design`) could transition with none of that section's checklist content. In the normal flow each gated section is appended by the prior transition, so this only affects issues that skipped it.
- Workflow: `process transitions` output now annotates checkbox gates — `section_checkboxes_checked` reads "(gates this transition)" and `section_has_checkboxes` reads "(existence only — contents verified later)", so it's clear which sections must be complete now versus merely present.

## v0.27.1 — 2026-06-06

- CLI: `issue-cli start` no longer advances a handoff status silently. When `start` auto-advances an approved handoff status (`backlog → in progress`, `human-testing → documentation`) it now prints a prominent `⚠ AUTO-ADVANCED  <from> → <to>` banner that names the move and notes the consumed approval, instead of a single quiet `✓ Status →` line. Plain claims, the `Status unchanged` message, and the missing-approval failure (no mutation) are unchanged. `start --help` and the workflow/CLI docs now describe the behavior.

## v0.27.0 — 2026-06-06

- CLI: `append` and `comment` now accept `--body-file <path>` to read the body from a file, or `--body-file -` to read it from stdin. The content is used verbatim — no shell quoting and no `\n`-escape normalization — so inline code spans (`` `like this` ``) and parentheses no longer get mangled by the calling shell, which was the most common friction point in agent workflows. `--body`/`--text` still work unchanged; combining them with `--body-file` is an error.

## v0.26.4 — 2026-06-05

- Web UI: fixed the issue detail view dropping approve buttons when a status had more than one non-optional `require_human_approval` transition. Only the first such transition rendered an approve button, so the others could never be approved from the UI. Every non-optional approval transition now renders its own approve button (with its `#approve-<status>` deep-link anchor), matching how optional-path approvals already render one CTA each.

## v0.26.3 — 2026-06-05

- Web UI: fixed long fenced code blocks (` ``` `) in an issue body stretching the detail page several times its width. The content column of the detail layout now shrinks correctly so wide code lines scroll horizontally within their box instead of overflowing the page.

## v0.26.2 — 2026-06-04

- Agent dispatch: fixed per-issue worktrees being created empty with every tracked file shown as staged-for-deletion. When `worktree_sparse_exclude` was set (default `["issues/"]`), the dispatch configured sparse-checkout but never materialized the tree, so agents landed in a broken working directory containing only `.git`. The worktree now always runs `git checkout HEAD` after writing the sparse patterns, so source files are present while excluded paths (e.g. `issues/`) stay out. Recover an already-broken worktree with `git -C .worktrees/<slug> checkout HEAD`.

## v0.26.1 — 2026-06-01

- GitHub sync: imported issues now land in the workflow's first status (e.g. `idea`) instead of `backlog`, so they go through the full process from the start. An explicit `import_status` on the project still overrides where imports land.

## v0.26.0 — 2026-05-29

- Workflow: new top-level `actions:` list in `workflow.yaml` defines custom one-shot agent buttons. Each action renders on the issue detail view below the built-in Claude/Codex dispatch buttons, and clicking it opens a tmux agent session briefed with a configured prompt — no validation, no status change. Useful for lightweight coordination tasks like "defer to team". Each action takes `id`, `label`, `prompt`, and an optional `agent` (`claude` default, or `codex`).
- Action prompts are templated with `{{slug}}`, `{{title}}`, `{{status}}`, `{{system}}`, `{{priority}}`, and `{{number}}`; unrecognized tokens are left verbatim. Actions run in the project checkout (not a per-issue worktree) and use a dedicated `agent-<slug>-<id>` tmux session so they run alongside, rather than clobber, a working agent.

## v0.25.1 — 2026-05-28

- Web UI: workflow designer now has Show/Hide buttons for the builder and inspector panels, so the canvas can take the full width when needed. Both panels default to hidden on load.
- CLI: agent timeline now picks up `issue-cli` invocations from shells that don't inherit `ISSUE_CLI_LOG` (Claude Code's Bash tool, IDE terminals, manual `cd` into a worktree). The per-agent clilog is resolved from `--project` + slug so every invocation writes to it, and the global `$TMPDIR/issue-cli-logs/actions.jsonl` is merged into the timeline filtered by project + issue slug.
- `make install` now installs both `issue-viewer` and `issue-cli` (previously only `issue-cli`).
- Docs: README now clarifies that `tmux` is the only hard requirement for agent dispatch; i3 + alacritty are the default fallback when `terminal:` is unset. Set `terminal: "none"` for headless (viewer creates the detached tmux session and returns an `AttachCmd`), or any shell command with `{{session}}` substituted for custom emulators.
- Internal: marked `WorkflowConfig` runtime-only fields with `json:"-"` so they don't leak into JSON output.

## v0.25.0 — 2026-05-28

- Web UI: new **+ New issue** button in the header on both the list and board views opens a modal that creates an issue without leaving the browser. Title and body are required; the body textarea is prefilled from the per-status `template:` block in `workflow.yaml`, and changing the Status dropdown refills it only if the body hasn't been edited.
- The form prefills the system from the currently-active `?system=` filter, defaults status to the first creatable status (any status before `backlog`), and on success navigates to the new issue's detail page.
- The 3-second auto-refresh polling no longer wipes an open create modal — `static/auto-refresh.js` skips its full-page reload while `window.__createModalOpen` is true.
- The board's per-column `+` buttons keep working as before; they now share the same modal partial (`templates/_create_modal.html`) as the new header button.
- No new endpoint, no schema change — the modal posts to the existing `POST /p/<project>/issues/create` handler (`handlers_issue_mutate.go`).
- See `docs/UI/overview.md` ("Create issue modal" section) for the field/wiring contract.
- Resolves ui/add-issue-from-ui-via-button.

## v0.21.0 — 2026-05-10

- Agent dispatch worktrees now use `git sparse-checkout` to keep the `issues/` tree out of the agent's working copy. Previously every worktree was a full checkout, so agents saw their own task file alongside dozens of unrelated issue markdowns and got confused about which one was theirs.
- New optional `worktree_sparse_exclude` field in `workflow.yaml`. Default is `["issues/"]` when worktree is enabled. Set to `[]` to disable sparse-checkout and get a full checkout (previous behavior).
- The dispatch step list now includes a `Sparse-checkout excludes: ...` (or `git checkout HEAD` when disabled) step after worktree creation; failure aborts dispatch the same way `worktree_setup` failures already do.
- `issue-cli` running inside the worktree continues to read/write issues through the primary checkout via the injected `--project` flag, so excluding `issues/` from the worktree doesn't affect agent access to the tracker.

## v0.20.0 — 2026-05-08

- *Edit in nvim* on the issue detail page now opens the **whole markdown file** (frontmatter + body) instead of only the body. Keyboard-driven users can edit metadata — labels, priority, version, status, custom fields, anything — without leaving the editor; on save the file is parse-validated and written back atomically.
- New `tracker.RewriteIssueFile(filePath, content)` next to `UpdateIssueFrontmatter` validates parseability via `ParseIssue`, holds `withIssueLock`, and writes via `writeFileAtomically`. The user's exact byte sequence is preserved — comment placement, key order, and blank lines all survive.
- Failed nvim edits surface as comments on the issue with source `edit-in-nvim` (e.g. `edit-in-nvim aborted: invalid issue file: ...`) instead of failing silently. The frontend is unchanged; users discover failures on next page reload via the existing comments timeline.
- Trust model: nvim is treated as a power-user free-form editor — every frontmatter field is editable, including workflow-managed ones. Users who want guarded edits should keep using the sidebar UI or `issue-cli set-meta` (which still enforces `ProtectedFrontmatterFields`).
- The existing `baseHash` race check is unchanged — concurrent sidebar saves still cause the nvim write to lose. No new locking mechanism.
- See `docs/API/overview.md` (new "Edit in nvim" section) for the save-path contract.
- Resolves api/edit-in-nvim-should-include-frontmatter.

## v0.19.0 — 2026-05-08

- Agent dispatch and "Edit in nvim" no longer fail with a `duplicate session` error when a tmux session of the target name is still alive. Both flows now probe `tmux has-session` first; on a hit they skip `new-session` plus all setup and just open a terminal attached to the existing session. The dispatch response carries `status: "reattached"` (with a single matching step); the edit response adds `reattached: true`. The web UI renders both as a yellow warning banner/toast, distinct from the regular success and error styling. The agent is not re-prompted and the body editor does not register a duplicate save-on-exit goroutine. Kill the session manually if you want a fresh dispatch.

## v0.18.0 — 2026-05-08

- Per-issue git worktrees on dispatch. New optional top-level `worktree` field in `workflow.yaml` controls whether the dispatch handler creates an isolated working tree before launching the agent's tmux session.
  - Default is `false` (opt-in): existing projects keep their current dispatch behaviour unless they explicitly set `worktree: true`.
  - When enabled, dispatch creates branch `work/<issue-slug>` and worktree `<workdir>/.worktrees/<issue-slug>` (gitignored), opens the tmux session with `cwd` inside the worktree, and appends a `## Worktree` block to the agent prompt naming the path/branch and noting that cleanup is the human's job.
  - Re-dispatch on the same issue reuses the existing worktree directory instead of erroring on duplicate-branch.
  - Failure (dirty tree, branch already exists, etc.) surfaces an error step in the dispatch response and aborts — no silent fallback to the primary checkout.
  - Dispatches without an issue slug (retros review) never create a worktree regardless of the toggle.
- Issue-viewer's own `workflow.yaml` opts in (`worktree: true`) so concurrent agent sessions on this repo land in separate working trees. The `shipping` checklist gains a worktree-cleanup reminder.
- `docs/Workflow/overview.md` documents the toggle, conventions, and cleanup contract; `workflow.yaml.example` documents the field in its header comment.
- Resolves workflow/workflow-promote-git-worktree-by-default-before-work-starts.

## v0.17.0 — 2026-05-08

- Issue detail page sidebar gains a small toolbar at the top with two toggles:
  - **Lock** — pins the frontmatter sidebar with `position: sticky` so it stays visible while the body scrolls. Default is on, so long issues no longer scroll the metadata out of view.
  - **Hide** — collapses the sidebar entirely; the body expands to full width and a "Show sidebar" button appears in the body toolbar to bring it back.
- Both states persist independently in `localStorage` (`sidebar-locked`, `sidebar-hidden`). An early-load script in `<head>` reads them and sets `data-sidebar-locked` / `data-sidebar-hidden` on `<html>` before paint, so reloads do not flash the wrong layout.
- A locked sidebar that is taller than the viewport scrolls internally (`max-height: calc(100vh - 48px); overflow-y: auto`) instead of clipping or growing the page.

## v0.16.0 — 2026-05-07

- Data entries gain a structured **second-axis field** (`tier`) orthogonal to `status`, configurable per-workflow via a parallel `<!-- data tiers=… -->` attribute on the data marker. Examples: ADR review (`🔴 critical` / `🟢 nice`), bug triage (`S1` / `S2` / `S3`), operational (high / medium / low blast-radius). Replaces the `[CRITICAL]` / `[NICE]` description-prefix workaround with a sortable, filterable, validated enum.
- New CLI surface: `issue-cli data add --tier <value>`, `issue-cli data set-tier <slug> <id> <value>` (pass `""` to clear). `data list` grows a `<tier>` column when relevant. `--tier` and `set-tier` reject values not in the body's `tiers=` enum (when one is declared) so typos like `🔴 critcal` fail loudly; workflows without a `tiers=` marker accept any value (opt-out).
- New API endpoint: `POST /issue/<slug>/data/<id>/tier` (body `{tier}`). The `POST /issue/<slug>/data` add endpoint now accepts an optional `tier` field.
- Viewer: Status and Tier render as **stacked selects in a single "Status / Tier" column** to preserve horizontal space; the toolbar gains a second filter dropdown (`dataTable.tierFilter`) that composes with the status filter via AND. The data table now uses an explicit `<colgroup>` so column widths (especially `#` and the action button) stop reflowing under wide layouts.
- Backward compatible: existing `*.data.json` sidecars without a `tier` field load and save as-is (`omitempty`); bodies without a `tiers=` marker render exactly as before (no Tier column, no tier filter, no `has-tier` class).
- Resolves issue #23.

## v0.15.0 — 2026-05-07

- Fix data-table layout brittleness on issue detail pages: the status column no longer clips long labels (e.g. `✨ positive (no action)`) and a long unbroken token in any cell no longer steals width from sibling columns. The table now uses `table-layout: fixed`, the status column is sized for the widest configured label, descriptions wrap with `word-break`, and the comment cell truncates with ellipsis + a hover `title` when not focused (full content shown on edit).
- New view-preferences toolbar above the data-table, persisted per-user in localStorage:
  - **Status filter** (`dataTable.statusFilter`) — dropdown listing each status currently in use; selecting one hides non-matching rows.
  - **↔ Expand / ↔ Shrink** (`dataTable.wide`) — toggles a layout breakout that lets the table spill rightward under the metadata sidebar on wide viewports. Default is shrunk; auto-disabled below the 768px breakpoint.
- Resolves issue #24.

## v0.14.2 — 2026-05-02

- 13 new structured workflow validators land in `internal/tracker/validations/` (one file per validator). Frontmatter: `field_present`, `field_not_empty`, `field_in`, `field_matches`, `has_label`, `has_any_label`. Linkage: `has_pr_url`, `linked_issue_in_status`. Body-structure: `has_section`, `section_min_length`, `section_max_length`, `no_todo_markers`. Shell: `command_succeeds` — opt-in via top-level `allow_shell: true` on `workflow.yaml`; templated with `{{slug}}/{{number}}/{{repo}}/{{system}}`; per-action `timeout_seconds` (default 10s); env scrubbed to `PATH`/`HOME`/`GH_TOKEN`; captured stdout/stderr surfaced in the failure message.
- Validators land in the new structured action-param form (companion fields on `transitions[].actions[]`: `field`, `values`, `pattern`, `section`, `min`, `max`, `command`, `ref_key`, `linked_status`, `hint`); legacy colon-string rules continue to parse unchanged through the existing `checkRule` shim. Every validator returns a failure message of the form `<problem> — <hint>` with a concrete `issue-cli` remediation command; per-action `hint:` overrides the default and is templated.
- See `docs/workflow.md` (new "Validation Rules" section) for the catalog with workflow.yaml usage examples, and `docs/Workflow/overview.md` for the architecture (leaf `validations` package, `Registry`-based dispatch, how to add a new validator).

## v0.14.1 — 2026-05-01

- Filled the test-coverage gap in `cmd/issue-cli/`. Every previously-untested `runX` subcommand now has at least one happy-path and one error-path test in the new `cmd_subcommands_test.go`: `claim`, `unclaim`, `done`, `comment`, `check`, `update`, `replace`, `append`, `retrospective`, `report-bug`, `search`, `next`, `checklist`, `show`/`context`, `create`, `help`, `stats`, the six `data` subcommands (with on-disk sidecar JSON shape assertions against `tracker.DataStore`), the `process` and `workflow` dispatchers, plus `runProcessWorkflow` / `runProcessSystems`. Package coverage moved from 47.0% to 77.3%.
- New `make validate` target runs `go vet ./...`, the full test suite, and a coverage gate on `cmd/issue-cli/` that fails when coverage drops below `CLI_COVERAGE_FLOOR` (default 70). `make vet`, `make test`, and `make cover-cli` are exposed as standalone steps for incremental use. `CLAUDE.md` gains a `## Validation` section describing the chain.

## v0.14.0 — 2026-04-30

- New per-project Stats tab at `/p/<project>/stats` surfacing the approximate token cost of the workflow itself. Four blocks: a constant Dispatch base prompt cost (the scaffolding text the issue viewer writes into every fresh agent dispatch), a Static reference table covering every (from→to) declared in `workflow.yaml`, an averaged Recorded transitions table sorted by avg dynamic descending, and Per-issue totals sorted by total dynamic cost. Token counts use a `len(s)/4` approximation — useful for relative comparison, not exact for any specific model.
- A per-issue stats sidecar `<issue>.stats.json` is now written next to the markdown file every time `tracker.ApplyTransitionToFile` runs a successful transition. Each record carries `from`, `to`, `ts`, `static_tokens` (workflow scaffolding only — validate rules, append_section bodies, inject_prompt prompts, target-status template and entry guidance), `dynamic_tokens` (scaffolding plus the issue body and joined comments captured at the transition moment), and a nullable `actual_tokens` slot reserved for a future hybrid pass that records measured agent-run tokens. Atomic writes via the same helper as the data-table sidecar; a missing sidecar is treated as "no records", never an error. The `tracker.MarkIssueDoneOnce` multi-step fast-path bypasses this and is intentionally not recorded — it is uncommon and skips multiple statuses.
- Refactored: the `buildAgentPrompt` format string in `handlers_dispatch.go` is now a single `tracker.AgentDispatchPromptTemplate` constant defined in `internal/tracker/dispatch.go`. The Stats tab uses the same constant via `tracker.AgentDispatchPromptStaticCost()` so the two stay in sync. No behavior change for dispatched bots.
- See `docs/workflow-stats.md` for the full feature description, sidecar schema, and out-of-scope items.

## v0.13.0 — 2026-04-30

- The `== Next ==` hint printed by `issue-cli start` and after `issue-cli transition` now shows the next transition's full contract beneath the transition command. A `Requires:` sub-block lists every validation rule and human-approval gate that blocks the transition; a `Will:` sub-block lists every side-effect (appended sections, frontmatter changes, prompt injection). Wording is sourced from `tracker.DescribeAction`, so it matches `issue-cli process transitions` exactly. Either bucket is omitted when empty so transitions with no rules render unchanged. Agents previously discovered these requirements by attempting the transition and reading the validation error; the new sub-blocks let them prepare correctly on the first try. The `transition --json` output gains additive `next_requires` and `next_side_effects` arrays (omitempty) carrying the same strings, so JSON consumers see the same contract.
- Fixed `issue-cli process workflow|systems|transitions` crashing with a nil-pointer panic when `--project <slug>` failed to resolve (missing config or unknown slug). The project-resolution gate previously let `process` through unconditionally, which only mattered when bootstrap mode could fall back; an explicit but invalid `--project` produced a confusing crash deep inside the subtopic. The resolver now surfaces the original resolution error when `--project` was passed explicitly, and the three subtopics that need a project carry defensive nil-guards.

## v0.12.0 — 2026-04-30

- Approval-gate errors now point at the right approve button. `issue-cli start` and `issue-cli transition` failures with a missing `human_approval` print a deep link to `<viewer>/p/<project>/issue/<slug>#approve-<status>`; the detail page adds matching `id="approve-<status>"` anchors and an on-load fragment handler that scrolls to the target, visually flashes it, and auto-reveals optional-approval CTAs that would otherwise be hidden behind a "Divert to..." button. The base URL comes from the new `ISSUE_VIEWER_URL` env var, falling back to `http://localhost:8080`. Dispatched bot sessions inherit the URL automatically: the dispatcher reconstructs `<scheme>://<host>` from the inbound request (honouring `X-Forwarded-Proto`/`X-Forwarded-Host`) and exports it into the agent's tmux session next to the existing `ISSUE_VIEWER_SERVER_PWD`/`ISSUE_VIEWER_ISSUE_SLUG` envs. The `tracker.ApprovalMissingError` type now carries a `Verb` discriminator so both the `start`-form and the `transition`-form messages flow through a single `errors.As`-compatible value, and the two plain `fmt.Errorf` callers in `workflow_validate.go` were converted to return `*ApprovalMissingError` so `errors.Is(err, ErrApprovalMissing)` works uniformly across both code paths. See `docs/agent-dispatch.md` (Approval-gate Deep Links section) for the URL contract.

## v0.11.0 — 2026-04-30

- Multi-project ergonomics for dispatched bots. Resolution order in `loadProjectOrErr` is now explicit: passing `--project <slug>` always wins, even when cwd has its own `./issues/`, so a bot inside one project workdir can still query a sibling project. With no `--project`, a local `./issues/` triggers bootstrap mode (single-project) and otherwise `projects.yaml` is consulted; a multi-project setup with no `--project` exits non-zero with a fail-loud error enumerating configured slugs instead of silently using `projects[0]`. Single-project setups produce byte-identical output and exit codes — pinned by new regression-guard tests.
- New `issue-cli projects` command lists configured projects (`slug`, `name`, `issue_dir`) with `(active)` / `(historical default)` markers; `--json` for scripting. The command tolerates missing or unreadable config so it stays useful as the discovery surface a confused bot reaches for first — same allow-list as `help` and `process`.
- `issue not found` errors now enumerate configured projects and point at `--project` in multi-project setups; single-project format is unchanged. `issue-cli --help` (and bare `issue-cli`) prints a `Configured projects:` block when more than one project is configured. Web-app dispatch prompts (`buildAgentPrompt`) rewrite every `issue-cli ` invocation to `issue-cli --project <slug>` so the bot does not have to discover the project; bootstrap mode (no slug) leaves prompts unchanged.

## v0.10.4 — 2026-04-30

- Internal refactor: `cmd/issue-cli/main.go` (2380 lines, ~250-line dispatch switch) split into one `cmd_<name>.go` per subcommand plus a `Command`/`Context` registry (`commands.go`, `context.go`, `helpers.go`). `main()` is now 12 lines and only logs the invocation, parses globals, and delegates to `run()`. Every subcommand owns its own `flag.FlagSet` and returns errors instead of calling `os.Exit`; the package-global `jsonOutput` is gone (`Context.JSONOutput` carries the flag), so two CLI invocations with different `--json` modes can run concurrently without racing — verified by a new `TestConcurrentJSONOutputDoesNotRace` under `go test -race`. Top-level help is generated from the registry rather than a hand-rolled blob, so adding a subcommand no longer requires editing `printHelp`. No public CLI behavior change: text and `--json` shapes are byte-for-byte identical to v0.10.3.
- Side-effect fixes embedded in the refactor: `findIssue` now returns `(*Issue, error)` (the old `fatal()`-on-miss path swallowed every "not-found" branch); `runStart` matches missing approvals via `errors.Is(err, tracker.ErrApprovalMissing)` instead of `strings.Contains` against the message; `claim --force` is parsed by `flag.FlagSet` rather than walked out of `os.Args` with a `goto`; `report-bug` opens its log file with `O_CREATE|O_EXCL` plus a counter suffix so two reports in the same second no longer truncate each other; `update --title ""` is now distinguishable from "title flag absent" (`flag.Visit`).

## v0.10.3 — 2026-04-29

- Internal refactor: `internal/tracker/workflow.go` (1812 lines) split into six cohesive files (`workflow_config.go`, `workflow_transition.go`, `workflow_merge.go`, `workflow_validate.go`, `workflow_preview.go`, `workflow_schema.go`), plus a new `heading.go` for the section/heading helpers shared with `issue.go`. No public API change, no behavior change — pure source reorganization to make subsequent workflow-area changes produce smaller, single-area diffs. `docs/API/overview.md`, `docs/CLI/overview.md`, and `docs/Workflow/overview.md` updated to reference the new layout.

## v0.10.2 — 2026-04-29

- Fixed three latent correctness bugs in `internal/tracker`. (1) Frontmatter parsing: `updateIssueFrontmatterLocked` and `SetFrontmatterField` were splitting on bare `"---"` instead of `"\n---"`, so an issue file whose YAML carried a `---` substring (a hyphen-separator inside a value) got corrupted on the next write. All four split sites now use the same `"\n---"` form as `ParseFrontmatter`. (2) `WorkflowConfig.Clone` shallow-copied `Scoring` and `Board`, leaking map and slice mutations from per-system overlays back to the base config. Both are now deep-copied. (3) `ApplyTransitionWithFields` ran the post-action `human_approval` clear after the action loop, silently overwriting any `set_fields` action that targeted `human_approval`; the clear now runs first so explicit `set_fields` values win. The `set_fields` row in `docs/Workflow/overview.md` now enumerates the supported `field` values.

## v0.10.1 — 2026-04-29

- Internal refactor: `handlers.go` (3512 lines) split into 13 cohesive files (`routes.go`, `template_funcs.go`, `helpers.go`, `tmux.go`, and `handlers_<area>.go` per responsibility). `handlers_test.go` (60K) split to mirror the new layout. No public API change, no behavior change — pure source reorganization to make subsequent handler-area changes produce smaller, single-area diffs.

## v0.10.0 — 2026-04-29

- New `issue-cli workflow init` command bootstraps a fresh project in one shot: writes `workflow.yaml` from a bundled template and scaffolds `issues/` and `docs/` if they don't exist. Three templates ship: `development` (the canonical software-delivery flow this repo uses), `review` (`inbox → … → archived` triage flow), and `writing` (`idea → … → published` long-form content flow). `--template <name>` picks one; without it, an interactive prompt appears when stdin is a terminal, otherwise the command exits non-zero with the list of valid names. `--force` overwrites an existing `workflow.yaml`. Templates live as editable YAML under `cmd/issue-cli/templates/workflow/*.yaml`, are embedded via `//go:embed`, and the list of valid `--template` names is derived from the embedded directory so adding a new template is just dropping a file and rebuilding.

## v0.9.0 — 2026-04-29

- New per-issue **structured data store**. Every issue can carry a sidecar `<slug>.data.json` of `{id, description, status, comment}` rows. Manage rows with `issue-cli data add | list | set-status | set-comment | remove` (`add` prints the assigned id on stdout for shell composition; `list --json` emits the entries array). The detail view renders the rows as an inline table at a `<!-- data statuses=... -->` marker in the body — per-issue dropdown statuses (spaces and emojis allowed inside a token), inline status select, contenteditable comment, row remove button. Designed for agent code-review findings the human triages inline. Atomic temp+rename writes; ids are monotonic and not reused after delete; missing sidecar means empty store, no error. New endpoints: `POST /issue/<slug>/data`, `POST /issue/<slug>/data/<id>/status`, `POST /issue/<slug>/data/<id>/comment`, `DELETE /issue/<slug>/data/<id>`. See `docs/data-store.md`.
- Markdown rendering now passes raw HTML through (`goldmark` `html.WithUnsafe()`), so HTML comments — including the new `<!-- data -->` marker — survive into the rendered body instead of being replaced by `<!-- raw HTML omitted -->`. Issue bodies are already trusted content, so this aligns with the existing trust model.

## v0.8.0 — 2026-04-29

- `issue-cli list --json` now emits `Score` (float) and `ScoreBreakdown` (`{Total, Components[]}`) on every entry when `workflow.yaml` has `scoring.enabled: true`. Both fields are `null` when scoring is disabled or the issue has no scoring inputs. The breakdown is the same `tracker.ComputeScore` output that drives the viewer's `⚡N` badge — CLI consumers no longer need to re-implement the scoring formula in bash against raw frontmatter.
- New `--sort score` flag on `issue-cli list` orders output by `Score` descending. When scoring is enabled and `default_sort: score_desc` is set in `workflow.yaml`, the sort is applied automatically with no flag.
- Documented that `scoring.formula.priority` keys should be lowercase: frontmatter values are normalized to lowercase before lookup. Uppercase keys still match via a case-insensitive fallback but lowercase is the canonical form.

## v0.7.3 — 2026-04-28

- Fixed `issue-cli transition` silently dropping the `append_section` payload when the issue body quoted the section heading inside a fenced code block (` ``` ` or `~~~`). Heading detection in `findHeadingMatches` / `findAllHeadings` now skips lines inside fences, so transitions like `human-testing → documentation` correctly add the `## Documentation` section even when the body documents a `## Documentation` example.
- `issue-cli check <slug> "<query>"` now skips checkboxes inside fenced code blocks. Quoting `- [ ] …` examples in the issue body no longer absorbs the next `check` and leaves the real workflow checkbox unchecked.

## v0.7.2 — 2026-04-28

- `issue-cli append <slug> --body "..."` now auto-routes into an existing section when `--body` starts with a heading already present in the issue and contains only deeper subheadings — no more retrying with `--section` after a duplicate-heading failure for the common `## Implementation\n…` pattern. The duplicate-heading error still fires when a *peer* heading collides (e.g. `## New\n…\n## Existing`); pass `--section` in that case.
- `--section` is now documented in `issue-cli help` and surfaced in the dispatch prompt's "commands you can use freely" block, so agents see it before hitting the duplicate-heading error.

## v0.7.1 — 2026-04-28

- `issue-cli transition` no longer rejects valid `target: frontmatter` answers that are already on the issue. The validator now consults the issue's existing frontmatter for required `target: frontmatter` fields, so `set-meta <key> <value>` followed by `transition` succeeds without re-supplying the value. `target: section:<Title>` fields still require an explicit answer (they record a fresh body line each time).
- New repeatable `--field key=value` flag on `issue-cli transition` for inline answers: `issue-cli transition <slug> --to "waiting-for-team-input" --field waiting="design review"`. Explicit `--field` values win over frontmatter fallback when both are set.
- `issue-cli process transitions` now renders required fields with target context: `Required frontmatter field "waiting" (set via \`--field waiting=…\` or \`set-meta\`)` instead of the prior generic `Prompts for field "waiting" (required) before commit`.

## v0.7.0 — 2026-04-28

- `issue-cli process transitions` now renders rules from the loaded workflow rather than a hardcoded ruleset. Each row lists validation rules, human-approval gates (previously only discovered by failing a transition), and side effects (`set_fields`, `append_section`, `inject_prompt`); optional and global statuses are surfaced separately. Three scopes are supported: no-arg prints the project default and lists configured per-system overlays at the bottom; `--system <name>` (alias `--workflow <name>`) prints the rules merged with that overlay; passing an issue slug resolves the issue's `system` field and labels the header accordingly (issues with no system explicitly say `(no system overlay; project default)`). Backed by new `tracker.DescribeAction` and `tracker.ValidationSummary` exports so descriptions stay in sync with transition previews.

## v0.6.0 — 2026-04-27

- New `issue-cli replace <slug> --section "<name>" --body "<content>"` command. Replaces the content of an existing section in place — finds the heading at any depth, swaps everything between it and the next heading of equal or shallower depth, and preserves the heading line. Errors if the section doesn't exist (use `append --section` to create), and requires `--force` when multiple normalized matches exist. Pairs naturally with `append --section` so evolving sections (status tables, checklist progress, summary paragraphs) can be rewritten rather than accreted.

## v0.5.0 — 2026-04-24

- Board drag-and-drop now runs the same workflow engine as `issue-cli transition`. Moving a card runs validations, executes `actions[]` (`append_section`, `inject_prompt`, `set_fields`, `require_human_approval`), and blocks the move with HTTP 409 + a toast when any rule fails. Cancelling the prompt reverts the card to its source column and leaves the file untouched.
- New `transitions[].fields[]` declarative block in `workflow.yaml`. Each field has `name`, `prompt`, `target` (`frontmatter` or `section:<Title>`), `required`, and `type` (`text` or `multiline`). Answers are captured through a modal before the transition commits. Frontmatter targets write an arbitrary scalar key; section targets append `- **<prompt>:** <answer>` under the named section.
- New wildcard source: `from: "*"` matches any source status when no exact `from:/to:` edge is defined, so a single transition can cover every column (e.g. "defer from anywhere"). Exact edges still win over wildcards.
- New `statuses[].global: true` flag: marks a status as an escape hatch where transitions out to any other status are allowed, with no linear-lifecycle constraint. The board column renders a `global` badge next to the existing `optional` badge.
- New `GET /p/<proj>/issue/<slug>/transition?to=<status>` preview endpoint returns the same `PreviewTransition` struct used by the CLI, plus the declarative `fields[]` — the board uses it to decide whether to open the prompt modal and what to render in it.
- `IssueUpdate` gains an `extra_fields` map for writing arbitrary scalar frontmatter keys; protected keys (`title`, `status`, `human_approval`, `started_at`, `done_at`, `number`, `repo`, `created`, `labels`) are always refused.

## v0.4.2 — 2026-04-24

- Agent Timeline now strips issue-cli global flags (`--json`, `--config <val>`, `--project <val>`) from clilog entries before interpreting the subcommand, so calls like `issue-cli --project demo show 42` render as `show` instead of mis-classifying `--project` as the command. Reader-side fix: existing historical logs now render correctly.

## v0.4.1 — 2026-04-24

- Agent dispatch now persists the exact briefing prompt to `.agent-logs/<session>/dispatch-prompt.txt` at dispatch time. The Agent Timeline's dispatch row replays the real prompt when the file exists and falls back to the reconstructed version (labeled `(reconstructed)`) for older logs.

## v0.4.0 — 2026-04-24

- Agent Timeline on the issue detail view. Parses `.agent-logs/<assignee>/<assignee>.clilog` into a structured list of events (start, show, process, append, check, transition, comment, retrospective) with click-to-expand detail for bodies.
- Transition events are enriched from `workflow.yaml`: validations (with their rule descriptions), `inject_prompt` text, appended section bodies, required human-approval gates, and `set_fields` actions all render inside the expanded row.
- `start` events show the canonical claim-to-work transition (typically `backlog → in progress`) with the same workflow actions the bot received.
- Every transition surfaces the target status's `prompt` field as a `status_prompt` action, so the view shows the guidance the agent reads on entering each new status.
- A synthetic `dispatch` row at the top reconstructs the base agent prompt (via `buildAgentPrompt`) using the pre-start status, approximating what the bot was briefed with at dispatch time.
- Purely server-side: works on every existing `.clilog` file without any CLI changes.

## v0.3.1 — 2026-04-24

- `issue-cli process changes` now fetches release history live from the GitHub releases API for `michal-franc/ai-native-project-viewer`. The embedded CHANGELOG.md remains as an offline fallback when the API is unreachable or rate-limited.
- Shipping workflow gains a `CHANGELOG.md updated` checkbox so the offline fallback stays in sync with the GitHub release notes.

## v0.3.0 — 2026-04-23

- Opt-in ticket scoring system driven by a `scoring` block in `workflow.yaml`: priority weights, due-date urgency (capped), staleness per day, per-label weights, and a `default_sort` option.
- New `score_boost` frontmatter field for manual bumps on individual issues.
- Board cards show a color-graded `⚡ N` badge; list view adds a score chip and a default/score toggle; detail sidebar renders the full per-component breakdown.
- `?sort=score` works on list and board; `default_sort: score_desc` controls initial order.
- No migration required — existing projects see no change unless `scoring.enabled: true` is set.

## v0.2.0 — 2026-04-23

- New `issue-cli process schema` command — prints every `workflow.yaml` field (from `yaml:"..."` + `desc:"..."` struct tags), every action type, and every validation rule. Drift guarded by tracker tests.
- New `issue-cli process changes` command (aliases: `changelog`, `versions`) — prints this changelog, newest-first, capped to the last 20 version entries.
- Both commands listed under `issue-cli process` (no topic), `issue-cli --help`, and the unknown-topic error.
- docs/CLI/overview.md and README.md updated with the new commands.

## v0.1.1 — 2026-04-23

- Shipping status now includes release-description polish and explicit semver bump guidance.
- Release-marker workflow publishes a GitHub Release when a `vX.Y.Z` tag is pushed.

## v0.1.0 — 2026-04-23

- First tagged release. Establishes the release-marker workflow and the shipping-to-release process going forward.
- Workflow capabilities available at this release:
  - `optional: true` on statuses (skippable on forward transitions, CTA in the viewer).
  - Per-transition actions: `validate`, `require_human_approval`, `append_section`, `inject_prompt`, `set_fields`.
  - Validation rules: `body_not_empty`, `has_checkboxes`, `section_has_checkboxes`, `has_assignee`, `all_checkboxes_checked`, `section_checkboxes_checked`, `has_test_plan`, `has_comment_prefix`, `approved_for` / `human_approval`.
  - Per-system workflow overlays merged over the base workflow (`systems:` block).
  - Board configuration: `board.columns` and `board.card_fields`.
