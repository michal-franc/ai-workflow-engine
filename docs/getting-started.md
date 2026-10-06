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

Needs Go 1.23 or newer.

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

`projects.yaml.example` in this repo lists every key. The dispatch keys (`terminal`, `tmux_session`, `agent_models`) are explained in [Agent Dispatch](agent-dispatch.md).

For a quick look at a single folder without a config file:

```bash
issue-viewer -dir ./issues -docs ./docs
```

This mode can't set dispatch options such as `terminal`, so use a `projects.yaml` before dispatching agents.

### Server flags

| Flag       | Default    | Description                               |
|:-----------|:-----------|:------------------------------------------|
| `-config`  | none       | Path to `projects.yaml` (multi-project)   |
| `-dir`     | `./issues` | Issues directory (single-project mode)    |
| `-docs`    | `./docs`   | Docs directory (single-project mode)      |
| `-port`    | `8080`     | HTTP port                                 |
| `-version` | `false`    | Print the version and exit                |

Open `http://localhost:8080`. The tabs at the top lead to the list, the board, the workflow graph, the docs, retros, the workflow designer and stats.

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
