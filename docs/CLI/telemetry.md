---
title: "CLI Usage Telemetry"
order: 2
---

`issue-cli` records one small JSON line per invocation to a **local** file. The log shows how bots actually use the CLI, and which commands, flags and help topics nobody uses, so they can be removed or merged. Nothing is ever sent off-machine.

Telemetry is separate from the existing action log (`logAction` → `$TMPDIR/issue-cli-logs/actions.jsonl`, `$ISSUE_CLI_LOG`, the per-agent `.clilog`). That log keeps raw arguments and feeds the agent timeline and the repeated-failure hint, and it is unchanged. Telemetry stores **names only**.

## Where events go

| Situation | File |
|:--|:--|
| A project resolves (`--project`, single-project config, or `./issues` bootstrap) | `<workdir>/.agent-logs/telemetry.jsonl`. Without a `workdir`, the parent of the project's issues directory is used. |
| No project resolves (bad `--project`, ambiguous multi-project config, missing config) | `${XDG_STATE_HOME:-~/.local/state}/issue-cli/telemetry.jsonl` |

`issue-cli telemetry path [--global]` prints the active file and whether recording is enabled.

**Rotation.** Before a write, a file of 10 MB or more is renamed to `telemetry.jsonl.1`, replacing any older `.1`, and a fresh file is started. Readers read `.1` and then the current file, so at most about 20 MB is kept per location.

**Concurrency.** Stat, rotate and append run under an exclusive `flock` on `telemetry.jsonl.lock`, so concurrent agents can't double-rotate. Each event is a single `O_APPEND` write capped at 4 KB, so lines never interleave. If the lock isn't acquired within 500 ms, the event is dropped.

**Failure policy.** Telemetry never changes CLI behaviour. A failed write, an unwritable directory, a lock timeout or a panic in event building is swallowed. Stdout, stderr and the exit code are identical with telemetry on or off.

## Opting out

- `ISSUE_CLI_TELEMETRY=off` (also `0`, `false`, `no`) disables recording for that shell.
- `telemetry: false` on a project in `projects.yaml` disables recording for that project. Leaving it out, or setting `telemetry: true`, means enabled.

```yaml
projects:
  - name: "Issue Viewer"
    slug: "issue-viewer"
    workdir: "/home/me/Work/issue-viewer"
    telemetry: true   # default; false opts this project out
```

Opting out doesn't affect the agent timeline or the retry hint, which use the separate action log.

`ISSUE_CLI_TELEMETRY_SKIP=1` is an internal variable. It skips recording only the current call and doesn't count as an opt-out. The viewer sets it on its own `telemetry report` call so `/stats` page loads aren't counted as CLI usage.

Telemetry files live under `.agent-logs/`. Make sure each project repo gitignores that directory, or the file shows up as untracked.

## Event schema (v1)

```json
{"v":1,"ts":"2026-10-02T18:51:06.423Z","dur_ms":4,
 "cmd":"transition","sub":"","alias":"",
 "flags":["config","project","to"],
 "project":"issue-viewer","issue":"cli/foo","assignee":"agent-foo",
 "safe":{"to":"testing"},
 "exit":1,"err_class":"invalid_transition","unknown":"",
 "caller":"agent","agent":"claude-code","tmux":true,
 "pid":3639775,"ppid":3639693,"args_hash":"6d0807cacc5a8c5c"}
```

| Field | Meaning |
|:--|:--|
| `v` | Schema version. Readers skip lines with a higher `v` than they understand. |
| `ts`, `dur_ms` | Start time (RFC 3339, UTC) and wall-clock duration. |
| `cmd` | Canonical command name. Empty for an unknown command. |
| `sub` | Known subcommand (`data add`, `telemetry report`, `workflow init`) or help/process topic (`help transitions` → `transitions`; `help transition` → `transition`, the command). Unknown values are never stored here. |
| `alias` | The alias typed, when it differs from `cmd` (e.g. `show` → `context`). |
| `flags` | Flag **names** used, global flags included, sorted. Never values. |
| `project`, `issue`, `assignee` | Resolved project slug, and the issue slug and assignee when the command resolved a real issue. For an unresolvable project, `project` is the raw `--project` token (≤ 64 chars). |
| `safe` | The only stored values: `to` (`--to`) and `section` (`--section`), ≤ 64 chars. |
| `exit` | Process exit code: 0, 1, 3 (`--wait` timeout) or 2 (panic). |
| `err_class` | See below. Empty on success. |
| `unknown` | The offending token for unknown command, subcommand, topic or flag errors, ≤ 64 chars. Only that one token is kept, never the rest of the args. |
| `caller` | `dispatched` (`ISSUE_VIEWER_ISSUE_SLUG` set by viewer dispatch), `agent` (`CLAUDECODE`, `AI_AGENT` or `CODEX_*` env), `tty` (interactive stdin) or `script`. |
| `agent`, `tmux` | Agent name from env (`claude-code`, `codex`, …), and whether `$TMUX` is set. |
| `pid`, `ppid`, `args_hash` | Process ids and a 16-hex-char SHA-256 prefix of the full raw args. `ppid` groups calls from one agent shell; `args_hash` spots identical repeats without storing values. |

Titles, comment and append bodies, retrospective text, `set-meta` values, `--field` answers, descriptions and positional values never reach telemetry. `cmd/issue-cli/telemetry_test.go` asserts this with a sentinel string.

### Error classes

`unknown_command`, `unknown_subcommand`, `unknown_topic`, `unknown_flag`, `flag_parse`, `usage` (missing positional or required flag), `project_resolution`, `approval_missing`, `issue_not_found`, `invalid_transition` (target status not reachable), `validation` (a workflow rule, a missing field answer, or `--dry-run` / `--wait` reporting unmet requirements), `wait_timeout` (exit 3), `panic`, `other`.

## `issue-cli telemetry report`

```
issue-cli --project <slug> telemetry report [--since 30d|12h|2026-09-01|all] [--global] [--top N]
issue-cli --project <slug> --json telemetry report ...
```

- `--since` sets the window (default `30d`). It accepts a duration with a `d`, `h` or `m` suffix, a date, or `all`.
- `--global` reads the no-project fallback file instead.
- `--top` caps the Errors, Unknown and Retry sequences sections (default 10).

The text output is agent-facing, with stable section headings:

| Section | Contents |
|:--|:--|
| `== Summary ==` | Source file, window, event and failure counts, callers and distinct assignees |
| `== Commands ==` | Calls, error % and median duration per `cmd` / `cmd sub` |
| `== Never used ==` | Commands, aliases, subcommands, topics, global flags and per-command flags with no calls in the window |
| `== Errors ==` | Top `err_class` × command |
| `== Unknown ==` | Top unknown command, subcommand, topic and flag tokens: what bots *expect* to exist |
| `== Retry sequences ==` | A failed call paired with the next call from the same `ppid` within 120 s, e.g. `transition (validation) ⇒ same args again` or `?add-comment (unknown_command) ⇒ comment --text → ok` |

### How "never used" knows every flag

Commands define their flags inside `Run`, so there's no static flag list. Every command parses through **`parseFlags(ctx, fs, args)`** (in `context.go`). In introspection mode it panics with the fully defined FlagSet before any command logic runs. `buildManifest` uses this for every command and subcommand, with an empty temp issues dir, discarded output and empty stdin. Hand-parsed flags are declared on the command as `ExtraFlags`: transition's `--field`, and process's `--system` and `--workflow`. `TestIntrospectFlagsEveryCommandWithoutSideEffects` fails if a command can't be introspected or creates files.

### `--json` contract (report v1)

```json
{
  "v": 1, "source": "/…/.agent-logs/telemetry.jsonl", "enabled": true, "disabled": "",
  "since": "2026-09-02T18:51:06Z",
  "summary": {"events": 18, "failures": 6, "error_pct": 33.3, "first": "…", "last": "…",
              "callers": {"agent": 17, "dispatched": 1}, "assignees": 0},
  "commands": [{"command": "transition", "calls": 5, "failures": 4, "error_pct": 80, "p50_ms": 2}],
  "never_used": {"commands": [], "aliases": [], "subcommands": [], "topics": [],
                 "global_flags": [], "flags": {"list": ["sort"]}},
  "errors": [{"class": "validation", "command": "transition", "count": 2}],
  "unknown": [{"class": "unknown_command", "token": "add-comment", "count": 1}],
  "retry_sequences": [{"failed": "transition (validation)", "next": "same args again", "count": 2}]
}
```

Lists are always arrays, never `null`. `enabled: false` with a `disabled` reason means recording is opted out; previously recorded events are still reported. The viewer's `/stats` page renders this JSON (see [Workflow Stats](../workflow-stats.md)).

## Adding a command

New commands must call `parseFlags(ctx, fs, args)` instead of `fs.Parse`. Group commands declare `Subcommands` (and `SubAliases`), and anything parsed by hand goes in `ExtraFlags`. Otherwise the flags and subcommands don't show up in events or in the never-used report.

## Key files

- `internal/telemetry/event.go`: `Event`, `Safe`, error and caller constants, `HashArgs`, `Truncate`
- `internal/telemetry/store.go`: `FileSink` (lock, rotate, append), `ReadFiles`, path and opt-out helpers. `Sink` is the extension point for a future event-hub sink.
- `internal/telemetry/report.go`: `Manifest`, `Aggregate`, `Report`, `RenderText`
- `cmd/issue-cli/telemetry.go`: event building, `classifyError`, `detectCaller`, flag introspection, `buildManifest`
- `cmd/issue-cli/cmd_telemetry.go`: `telemetry report` and `telemetry path`
- `cmd/issue-cli/main.go`: `main` times `runTraced` and calls `recordTelemetry` after every invocation, including panics
