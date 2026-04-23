---
title: "Agent Workflow Flow"
order: 4
---

## Purpose

This page explains the full workflow path an agent follows in the app, from dispatch to stop conditions.

## 1. Dispatch

When you dispatch an issue from the app, the server builds a prompt with:

- generic operational instructions
- `issue-cli process workflow`
- `issue-cli process transitions`
- a generic goal: move the issue forward using the configured workflow
- current status guidance from `statuses[].prompt`
- the allowed `issue-cli` commands
- bug-report guidance via `issue-cli report-bug "..."`
- retrospective guidance via `issue-cli retrospective <slug> --body "..."`

The dispatch flow also exports these environment variables into the agent session:

- `ISSUE_CLI_LOG`
- `ISSUE_VIEWER_SERVER_PWD`

The issue detail page can also notify the active tmux-backed agent session when a human grants approval. Approval metadata on the issue remains the source of truth, and the UI reports whether the follow-up notification reached a matching session.

## 2. First Commands

The expected first commands are:

```bash
issue-cli process workflow
issue-cli process transitions
issue-cli start <slug>
issue-cli show <slug>
```

`start` works from any status: it claims the issue, auto-advances through handoff states (`backlog`, `human-testing`) honoring their approval gates, and leaves the issue at a work status with its checklist and guidance ready. If an approval gate is crossed without the matching approval, `start` fails before mutating assignee or status and tells the user that no changes were made. Re-running `start` on an already-started issue is idempotent. `show` prints the full issue context.

## 3. Status Guidance

Workflow guidance comes from two layers:

`statuses[].prompt` — baseline guidance for how to work while in the current status.

`transitions[].actions` — ordered transition behavior such as validations, append actions, prompt injection, field updates, and approvals.

In practice:

- current status prompt tells the agent how to behave now
- transition actions tell the agent what must happen before moving forward

## 4. Statuses

The normal lifecycle is:

1. `idea` — clarify the idea with the human, narrow scope, and ask questions.
2. `in design` — review docs and code, structure the solution, capture assumptions and human input. When the design is complete, stop and request backlog approval before attempting the transition.
3. `backlog` — ready state after design and approval. `issue-cli start` from here requires `in progress` to be approved in the issue viewer; if approval is missing the command fails without mutating state — stop and ask for approval instead of retrying blindly.
4. `in progress` — implement the approved design and keep the issue updated.
5. `testing` — add relevant automated coverage and log test evidence.
6. `human-testing` — prepare explicit manual verification for a human. `issue-cli start` from here advances to `documentation` once that status is approved — otherwise it errors with a clear approval-missing message.
7. `documentation` — update the relevant docs for the affected system.
8. `done` — no active prompt. Work is complete.

## 5. Transitions

For each `from -> to` move, the workflow engine processes the configured actions in order.

The high-level behavior is:

1. validations run first
2. if blocked, the transition fails and explains why
3. if allowed, transition effects run: append sections, inject prompts, set fields, approval checks

Examples:

- `idea -> in design` — validate body is not empty; append `Idea`, `Design`, and `Human Input`
- `in design -> backlog` — validate those sections are complete; require human approval; append `Readiness`; clear assignee
- `backlog -> in progress` — require human approval; require assignee; validate `Readiness`; append `Implementation` and `Test Plan`
- `testing -> human-testing` — validate `Testing`; require a `tests:` comment; append `Human Testing`
- `human-testing -> documentation` — validate `Human Testing`; require a `tests:` comment; require human approval; append `Documentation`

## 6. System Overlays

The effective workflow is:

- base workflow
- plus the system-specific overlay for the issue's `system`

This means the agent gets extra prompts or requirements for systems like `Combat`, `UI`, `Data`, and others without changing the base lifecycle.

System overlays are especially useful in:

- testing
- documentation

For example, documentation overlays can point the agent to concrete doc areas such as:

- `docs/combat/`
- `docs/ui/`
- `docs/data/`

## 7. Stop Conditions

If the agent cannot proceed because it needs:

- human approval
- manual verification
- clarification from the user
- or it hits workflow or tooling blockage

then it should stop and save a retrospective:

```bash
issue-cli retrospective <slug> --body "Base workflow: ...
Subsystem workflow for <System>: ...
Missing system-specific instructions: ...
Tooling/workflow friction: ..."
```

This writes a markdown file under `retros/` in the project root.

## 8. Bug Reporting

If the agent finds a bug in `issue-cli`, it should run:

```bash
issue-cli report-bug "description"
```

Bug reports are written to:

- `{server_pwd}/bugs/` for sessions launched by the app
- `./bugs/` under the current shell working directory for manual CLI use

## 9. Persistence

Different parts of the workflow are stored in different places:

- workflow semantics: `workflow.yaml`
- workflow designer draft and layout: browser `localStorage`
- retrospectives: project `retros/`
- `issue-cli` bug reports: `bugs/`
- issue state and body updates: issue markdown files managed through `issue-cli`

## 10. Summary

The intended agent flow is:

1. get dispatched with current status guidance
2. inspect the workflow through `issue-cli`
3. complete the work required by the current status
4. attempt transitions in order
5. apply system-specific overlay guidance when relevant
6. stop cleanly for approvals, manual verification, or clarification
7. save a retrospective when stopping for those reasons
8. report `issue-cli` bugs into `bugs/`
