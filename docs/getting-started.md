---
title: "Getting Started"
order: 1
---

## Install

One line for Linux and macOS. It detects your OS and architecture, downloads the latest release and puts `issue-viewer` (the board) and `issue-cli` (the agent CLI) into `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/michal-franc/ai-workflow-engine/main/install.sh | bash
```

Override the target with `INSTALL_DIR=...`, or pin a release with `VERSION=v0.32.0`.

Or download an archive from the [latest release](https://github.com/michal-franc/ai-workflow-engine/releases/latest). Each one holds both binaries; extract them onto your `$PATH`.

| Platform            | Archive                                       |
|:--------------------|:----------------------------------------------|
| Linux x86_64        | `issue-viewer_<version>_linux_amd64.tar.gz`   |
| Linux arm64         | `issue-viewer_<version>_linux_arm64.tar.gz`   |
| macOS Intel         | `issue-viewer_<version>_darwin_amd64.tar.gz`  |
| macOS Apple Silicon | `issue-viewer_<version>_darwin_arm64.tar.gz`  |

Releases ship Linux and macOS only. Agent dispatch also needs `tmux`, `git` and the `claude` or `codex` CLI.

### Build from source

Needs Go 1.25 or newer.

```bash
make build        # issue-viewer
make build-cli    # issue-cli
make install      # both, via go install
make validate     # go vet, tests and the issue-cli coverage gate
```

## Try the demo

From a checkout of this repo:

```bash
make demo
# http://localhost:8080
```

## Bootstrap a project

In your project's directory, write a `workflow.yaml` and the standard `issues/` and `docs/` layout in one go:

```bash
issue-cli init --template development
```

`init` is an alias for `issue-cli workflow init`. Pick `development` for software delivery, `review` for triage queues, or `writing` for long-form content. Pass `--force` to overwrite an existing `workflow.yaml`. Run it without `--template` in a terminal for an interactive picker. See [CLI Overview → workflow init](CLI/overview.md#workflow-init) for the full reference.

## Create your first issue

```bash
issue-cli create --title "My first issue" --system UI
```

This writes `issues/UI/my-first-issue.md` in the workflow's first status. You can also write the file by hand:

```markdown
---
title: "My first issue"
status: "idea"
system: "UI"
---

Description of the issue.
```

`status` must be one of the statuses in your `workflow.yaml`. Every field is described in [Issue File Format](issue-format.md).

## Run the board

### One project, many projects

With a `projects.yaml` (recommended), one server hosts any number of projects, each with its own issues, docs and workflow, and each can set how agents are dispatched:

```yaml
projects:
  - name: "My Project"
    slug: "my-project"
    issues: "./issues"
    docs: "./docs"
    workdir: "."
    terminal: "none"
  - name: "Renderer"
    slug: "renderer"
    issues: "../renderer/issues"
    docs: "../renderer/docs"
```

```bash
issue-viewer -config projects.yaml
```

`projects.yaml.example` in this repo lists every key. The dispatch keys (`terminal`, `tmux_session`, `agent_models`) are explained in [Agent Dispatch](agent-dispatch.md). Two top-level keys set machine-wide defaults for projects that leave them unset:

- `terminal:`, e.g. `none`
- `tmux_session_per_project: true`, which gives each project its own tmux session for its lead and workers

`engine_project: <slug>` names the project that `issue-cli report-bug` files issue-cli bugs into. Without it, report-bug uses the project whose `repo:` is `michal-franc/ai-workflow-engine`; see [`report-bug`](CLI/overview.md#report-bug).

With one terminal and tmux, as on a Mac, see [One terminal, a session per project](agent-dispatch.md#one-terminal-a-session-per-project).

#### How issue-cli finds the config

issue-cli needs the same `projects.yaml` as the viewer, from whatever folder it runs in. Without `--config` it looks, in order:

1. `--config <path>`
2. `$ISSUE_VIEWER_CONFIG` (the viewer sets it for the agents it dispatches)
3. `./projects.yaml`, only if it exists
4. the running viewer's config: at start, `issue-viewer -config …` writes `~/.local/state/ai-workflow-engine/viewer.json` (`$XDG_STATE_HOME`) with the absolute config path, URL, pid and start time. The entry is used while that config file exists, even after the viewer stops
5. `~/.config/ai-workflow-engine/projects.yaml` (`$XDG_CONFIG_HOME`)

So once the viewer has started with `-config` on this machine, `issue-cli --project <slug> …` and `issue-cli report-bug` work from any folder with no env var. `issue-cli projects` prints the chosen config and why on its first line, and `issue-cli --debug <command>` prints it on stderr. When nothing is found, the error lists every place checked and the fix: `export ISSUE_VIEWER_CONFIG=/path/to/projects.yaml`, or start the viewer.

For a quick look at a single folder without a config file:

```bash
issue-viewer -dir ./issues -docs ./docs
```

This mode can't set dispatch options such as `terminal`, so use a `projects.yaml` before dispatching agents.

### Server flags

| Flag       | Default     | Description                               |
|:-----------|:------------|:------------------------------------------|
| `-config`  | none        | Path to `projects.yaml` (multi-project); recorded in `viewer.json` for issue-cli |
| `-dir`     | `./issues`  | Issues directory (single-project mode)    |
| `-docs`    | `./docs`    | Docs directory (single-project mode)      |
| `-host`    | `127.0.0.1` | Interface to listen on                    |
| `-port`    | `8080`      | HTTP port                                 |
| `-version` | `false`     | Print the version and exit                |

The server listens on loopback only. Its POST endpoints start agents, tmux sessions and editors on your machine, so binding to another interface (`-host 0.0.0.0`) lets anyone who can reach the port drive them; it is an explicit opt-in and the server prints a warning at startup. Writes from another site in your browser are refused either way (see [API: Design Considerations](API/overview.md#design-considerations)).

### Stopping the server

Ctrl-C (or SIGTERM) stops the server gracefully. It stops accepting connections, waits up to 10 seconds for requests and background work to finish (a GitHub close, an open edit-in-nvim, the CI watcher), and exits with status 0. If anything is still running after 10 seconds, it prints `shutdown: still running after 10s: …` and exits anyway. Press Ctrl-C a second time to exit immediately.

An edit-in-nvim session that is still open when the server stops is closed. Its changes are discarded, and the issue gets a comment saying so. Agent tmux sessions and local CI runs keep running; they don't belong to the server process.

Open `http://localhost:8080` and pick a project. The sidebar on the left leads to the board, the list, decisions and the docs; **More** holds the workflow graph, the canvas, agents, retros and stats. The swatches at the bottom switch between the dark, Dracula and light themes.

## Docs pages

The Docs tab renders the markdown files in your docs directory. Frontmatter is optional:

```markdown
---
title: "Page Title"
order: 1
---

Content here.
```

`title` defaults to the file name in title case. Pages sort by `order` (lower first), then alphabetically. Subdirectories become sections in the sidebar.
