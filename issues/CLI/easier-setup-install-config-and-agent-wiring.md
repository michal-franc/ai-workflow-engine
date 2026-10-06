---
title: "Easier setup: install, config and agent wiring"
status: "idea"
system: "CLI"
priority: "high"
type: "feature"
umbrella: "cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use"
created: "2026-10-06"
ideas: "https://claude.ai/artifact/EQ1ttCchKDkjnC8M4Mi3o1"
---

Getting the binaries, config and agent tooling in place without reading three docs pages. Ideas #1–#7 from the
adoption artifact. Each one can ship on its own. #1 is a bug and should go first.

## Ideas

### 1. Make `make demo` work on a fresh clone (bug, S)

The Makefile runs `issue-viewer -config demo/projects.yaml`, but the bare `projects.yaml` rule in `.gitignore`
also matches `demo/projects.yaml`, so that file was never committed. A fresh clone fails with:

```
Failed to load config: reading config demo/projects.yaml: open demo/projects.yaml: no such file or directory
```

`git check-ignore -v demo/projects.yaml` → `.gitignore:5:projects.yaml`. Fix: anchor the rule (`/projects.yaml`,
`/projects-*.yaml`) or add `!demo/projects.yaml`, commit the file, and add a CI step that starts the demo and
fetches `/`.

### 2. `issue-cli init` writes `projects.yaml` too (S)

`init` writes `workflow.yaml` and creates `issues/` and `docs/`
(`cmd/issue-cli/workflow_init.go:93`). The README then asks for a `projects.yaml` written by hand. Write it from
`init`: name and slug from the folder, paths filled in, terminal from #4. Don't overwrite an existing one. Then
print the next command:

```
✓ Wrote workflow.yaml (template: development)
✓ Wrote projects.yaml (project: my-project, terminal: none)
✓ Created issues/, docs/

Next: issue-viewer        # opens http://localhost:8080
```

### 3. `issue-viewer` with no flags (M)

Today `-dir` mode is easy but can't dispatch, and `-config` mode needs the file. With no flags, look for
`projects.yaml` in the current folder and its parents, then for `workflow.yaml`, and build a one-project config.
Open the browser on start (`-no-open` to skip). Share the project-root lookup with issue-cli.

### 4. A terminal default that works everywhere (S)

Unset `terminal` means `i3-msg exec alacritty -e tmux attach …` (`handlers_dispatch.go:302`,
`handlers_issue_mutate.go:289`). On macOS, GNOME, KDE or a remote box, the first ▶ fails. Make unset mean
"detect": i3 only when `i3-msg` is on `PATH`, then known terminals (Terminal.app, iTerm, gnome-terminal, kitty,
wezterm), else `none`, with the `tmux attach` command shown on the card and a copy button. Keep explicit `terminal:`
values as they are.

### 5. `issue-cli doctor` (M)

The `workflow.yaml` checks and improvement suggestions have their own issue:
`workflow/issue-cli-doctor-check-workflow-yaml-and-suggest-improvements`. This one covers the environment checks
and the shared command.

One command that checks tmux, git, claude/codex on `PATH`, `workflow.yaml` (with line numbers and "did you mean"
for unknown statuses and rules, and `WorkflowConfig.Lint()` output), `projects.yaml` paths, and a free port. It
prints the fix next to each failure. It absorbs the roadmap's "a command that validates `workflow.yaml`". Add
`--json` for agents (the agent-led onboarding child depends on it). The board can show the same results on a setup
page.

```
✓ tmux 3.4      ✓ git 2.46      ✗ claude not on PATH → see https://docs.claude.com/claude-code
✗ workflow.yaml:41 transition to "in-progress": no such status (did you mean "in progress"?)
✓ projects.yaml  ✓ port 8080 free
```

### 6. More install channels (M)

Only `curl | bash`, a tarball, or building from source exist today. Add a Homebrew tap, document
`go install …@latest` for both binaries, add a WSL section for Windows, and add a devcontainer with tmux and the
binaries so people can try dispatch in Codespaces.

### 7. Wire up agents during `init` (S)

An agent the user starts by hand doesn't know the workflow exists. Offer to append a short block to `CLAUDE.md` and
`AGENTS.md`: "This repo tracks work with issue-cli. Run `issue-cli process` before changing code." The agent-led
onboarding child turns this into a plugin. Here it's only the `init` step.

### Found while filing these

- `issue-cli create --help` outside a project fails with `cannot load config projects.yaml` instead of printing
  help. Help should never need a project.
- `issue-cli set-meta --help` treats `--help` as the slug (`Examples: issue-cli set-meta --help --key …`).
- `issue-cli create` ends with `Thank you!` (`cmd/issue-cli/cmd_create.go:153`), which reads oddly in agent logs.

## Out of scope

- The guided `quickstart` and the tour issue (first-run child).
- Renaming the binaries (one-binary child).
