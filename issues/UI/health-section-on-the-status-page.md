---
title: "Health section on the Status page"
status: "idea"
system: "UI"
priority: "medium"
type: "feature"
parent: "workflow/harness-checks-the-work-ideas-from-rigone"
created: "2026-10-22"
version: "0.57"

---

When something the engine needs is broken (claude not signed in, tmux missing, a bad `workflow.yaml`, a full disk),
the board gives no sign until a dispatch fails. RigOne's **Health check** lists everything it needs, each row
**OK**, **Attention**, **Problem** or **Optional**, a problem with a button to the place that fixes it, and
**Run again**.

## Idea

A Health section on `/status` (and in `/status.md`), built on `issue-cli doctor --json` from
`cli/easier-setup-install-config-and-agent-wiring` (#5):

- rows: tmux, git, `gh`, each agent CLI with version and sign-in, `workflow.yaml` errors and warnings, worktree dir
  writable, free disk, `projects.yaml` paths;
- each row OK / Attention / Problem / Optional, with the fix text or a link (docs page, the doctor command to run);
- **Run again** re-runs the checks; the result is cached for a minute so the page stays fast;
- a Problem row counts in the sidebar's attention count, so a broken setup is visible from every page.

Not in scope: writing the checks themselves; they belong to `doctor`.
