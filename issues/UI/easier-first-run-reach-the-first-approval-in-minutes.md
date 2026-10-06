---
title: "Easier first run: reach the first approval in minutes"
status: "idea"
system: "UI"
priority: "high"
type: "feature"
umbrella: "cli/adoption-make-ai-workflow-engine-easy-to-set-up-start-and-use"
created: "2026-10-06"
ideas: "https://claude.ai/artifact/EQ1ttCchKDkjnC8M4Mi3o1"
---

The first fifteen minutes. A new user gets the tool when an agent stops at a gate and carries on after they tick
it. Today that moment is a full design pass away. Ideas #8–#14 from the adoption artifact. #9 (empty states) and #12
(hosted demo) are UI work and need mockups per the UI overlay. The rest are CLI, docs or workflow templates.

## Ideas

### 8. A guided first run: `issue-cli quickstart` (M)

One interactive command: pick a template (default `lite`, see #13), run the setup child's `init` (with
`projects.yaml`), create a first issue from a one-line prompt, run `doctor`, start the board and open the browser
on that issue. Each step prints the plain command it ran, so users learn the real CLI as they go. Depends on the
setup child's #2, #3 and #5.

### 9. Design the empty board (S)

A new project opens to nine empty columns. When there are no issues, show a panel with a "New issue" button, the
matching `issue-cli create` command, and three lines on how a card moves (you create it, the agent works it, you
tick the gates). Do the same for empty Retros and Stats tabs.

### 10. Record the hero GIF and a two-minute video (S)

The README has a slot and a script for it ("press ▶ Claude; the agent blocks on a human approval; tick it; the
agent carries on"), but nothing is recorded yet. A still screenshot can't show the gate, and the gate is the whole
pitch. Record it on the demo project. Add a longer walkthrough that replays Raid League's "Sapper traps" issue from
its logs.

### 11. A tour issue that reaches a gate fast (S)

`init` (or `quickstart`) seeds one issue, "Tour: add a line to README", with a tiny scope. The agent writes a
two-line design and asks for approval, and the user ticks a first gate within five minutes. The body explains each
step as it happens and links the docs page for it. With types (docs/Workflow/types.md) it can be a `tour` type
with a short path and one gate.

### 12. A hosted, read-only demo board (M)

Seeing the board today means installing it or building with Go. Add a static export (`issue-viewer export ./site`)
and publish the demo project on GitHub Pages: board, issue pages with agent timelines, the graph and Retros, all
clickable, with ▶ and approvals disabled and a pointer to the install.

### 13. A `lite` template with one gate (M)

The `development` template has nine statuses and four gates, and the README cites a 710-line workflow. For a
first try that reads as heavy process. Add `--template lite` (`todo → doing → review → done`, one approval before
merge, one `tests:` comment check) and a "grow your workflow" docs page that shows how to add a design gate, an
overlay or a type when the team is ready.

### 14. Import the work people already have (M)

GitHub import exists in the web UI and `sync-issues.sh`, but not as a first-run step. Add
`issue-cli import github --label agent-ready`, `import todo TODO.md` (one issue per checkbox) and a CSV import for
Linear and Jira exports. Offer them at the end of `init`.

## Out of scope

- Setup mechanics (`init` writing `projects.yaml`, `doctor`, terminal detection): the setup child.
- The agent-driven version of this flow: the agent-led onboarding child. It should reuse `quickstart`'s steps
  instead of duplicating them.
