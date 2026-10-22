---
title: "issue-cli prove: link each criterion to its proof"
status: "idea"
system: "Workflow"
priority: "medium"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
---

Nothing records which test, playtest step or commit proves which acceptance criterion. Agents write `tests:`
comments as free text, criteria are often plain bullets (the Eldritch boss ticked only the template placeholder),
and proof screenshots sit unlinked in the worktree's `screenshots/`. The Evidence tab's centre, criterion → proof,
has nothing to read.

Owner, 2026-10-22: a command, yes; but "we don't want to have 20 screenshots in there for each change".

## Rules

1. **Proof is a link to something that exists, never a copy.** Nothing is copied into the issue or the repo.
2. **The engine checks every link.** The agent says where the proof is; the viewer verifies it. This keeps 0.54's
   "agents don't self-report" rule.
3. **Screenshots come from playtest runs, at most one per step.** The Evidence gallery shows the latest run's shot of
   each step a criterion points to (`.playtest/runs/…/results.yaml`, gitignored). Three visual criteria show about
   three shots.
4. **A hand-picked shot only where no playtest runner exists** (RogueTech has one, so it gets none): one per criterion,
   only for a criterion marked visual, stored as a path.
5. **No proof stays "unproven".** A screenshot can't stand in for a missing test or step.

## Command

```
issue-cli prove <slug> <criterion-id> --test <TestName>       # repeatable
issue-cli prove <slug> <criterion-id> --step <TP3>            # a playtest session step id
issue-cli prove <slug> <criterion-id> --commit <sha>
issue-cli prove <slug> <criterion-id> --value "690 vs 380 (1.82×)"
issue-cli prove <slug> <criterion-id> --shot <path>           # only when the project has no playtest runner
issue-cli prove <slug> --list                                 # each criterion with its proof and its check result
```

Links are stored in the data sidecar. Running `prove` again on a criterion adds to its links; `--clear` removes them.

## What the engine checks

| Link | Check | Shown as |
|:--|:--|:--|
| `--test` | the test function exists on the branch, and a `tests:` comment was posted after the branch's last commit | reported after the last change · stale (code changed since) · not found |
| `--step` | the step exists in a session for this issue and passed in its latest `results.yaml` (`ux-test` or judge) | passed · failed · not run |
| `--commit` | reachable from the work branch or the merge | found · not found |
| `--value` | none; shown as the agent wrote it, marked "reported" | reported |
| `--shot` | the file exists; refused when the project has a playtest runner or the criterion isn't marked visual | shown · missing |

Owner, 2026-10-22: the test result comes from `tests:` comments. Agents already post one before
`testing → shipping` (`has_comment_prefix: tests:`). The check compares its time with the branch's last commit, so it
needs the full comment timestamps from `cli/clilog-and-stats-record-what-the-timeline-needs`. The comment is free
text, so "passed" is the agent's report; what the engine adds is that the test exists and the report is newer than
the code.

## Agent asks (workflow.yaml)

- Design gate: acceptance criteria are checkboxes with ids; a validation checks it. Visual criteria carry a marker
  (for example `(visual)`).
- Testing prompt: one `prove` per criterion before `testing → human-testing`; a validation requires every criterion
  to have at least one link that passes its check.
- Shipping prompt: `risk:` comments for anything to watch after the ship.

## Open question

- Is a criterion with only `--value` proven, or does it need a test or step as well?
