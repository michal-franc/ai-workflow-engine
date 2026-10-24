---
title: "Proof Links"
order: 3
---

# Proof Links

Every acceptance criterion (`AC1`, `AC2`, … under `## Acceptance Criteria`) is linked to the proof that it works:
a test, a playtest step, a commit, or a measured value. A link points at something that exists. Nothing is copied
into the issue or the repo, and the engine checks every link each time it reads it. A ticked criterion means
"proven", not "the agent said so".

The `proof-links` workflow feature turns this on. It is in this repo's `workflow.yaml`, in the bundled
`development` template, and `issue-cli workflow upgrade` offers it to other projects.

## Where links come from

Most links need no command: the agent declares them where it writes the proof.

| Declared in | Example | Link |
|:--|:--|:--|
| a playtest step | `proves: [AC2]` on the step in the session YAML | step |
| a commit trailer | `Proves: AC2 TestBadgeCount` on the commit that adds or changes the test | test |
| a commit trailer | `Proves: AC2` on the commit that is the change | commit |
| `issue-cli prove` | `issue-cli prove <slug> AC2 --value "690 vs 380 (1.82×)"` | any kind |

- **Trailers** go in the commit message's last paragraph, one or more lines:
  `Proves: AC1, AC3 TestA TestB` links AC1 and AC3 to both tests. Words shaped like a criterion id (the section's
  prefix plus a number) are ids, and the rest are test names.
  - Only the work branch's own first-parent commits count. While the branch is live they are read as
    `main..work/<slug>`, so commits merged in from main never count.
  - After the merge they are read from the recorded branch head, back to the issue's first dispatch.
- **`proves:`** is an optional field on a playtest step (see the [playtest contract](../patterns/playtest/README.md)).
  The runner copies it into each run's `results.yaml`, which is what the engine reads. `make playtest-check`
  rejects an id that isn't one of the issue's criteria.
- **`issue-cli prove`** is the fallback, and the only way to record a value. Its links are stored in the issue's
  data sidecar under `proofs` (see [Per-issue Data Store](../data-store.md)).

## What the engine checks

| Link | Passes when | Otherwise |
|:--|:--|:--|
| test | the name is a whole word in a test file on the branch (`git grep -w` at the branch head; paths such as `_test.`, `test_`, `.test.`, `.spec.`, `tests/`, `*Tests.cs`), **and** a `tests:` comment is newer than the branch's last code commit (see below) | not found · stale (code changed since the last tests: comment) · no tests: report |
| step | the step's latest verdict (pass or fail, any mode) is pass | failed · not run |
| commit | a trailer's commit; or, for `prove --commit`, the sha is reachable from the work branch or its merge | not found |
| value | always; shown as "reported" | — |
| shot | the file exists in the worktree or the project checkout | missing |

A criterion is **proven** when one of its links passes. Any kind counts on its own, a value included (D-1 of
`workflow/issue-cli-prove-link-each-criterion-to-its-proof`).

**The last code commit** is the newest of the branch's own first-parent commits that changed code. Two kinds don't count, so they never make a `tests:` report stale:
- merges of main into the branch;
- docs-only commits, where every file is under `docs/`, a Markdown file (`*.md`), or a `CHANGELOG` in any folder (`ui/evidence-tab-proof-shots-and-criteria` D-6). The docs commit an agent makes in the documentation status comes after its tests, and would otherwise make every test link read stale on a done issue.

**Limits of the test check** (D-6): the engine checks that the test exists and that the report is newer than the
code. It does not check:
- that the commit carrying the trailer touched the test;
- that the match is a test definition rather than a mention;
- that the test passed. The `tests:` comment is free text, so "passed" is the agent's report.

`prove --shot` is for projects with no playtest runner. It is refused when the workflow records the
`playtest-sessions` feature, or when the criterion's text doesn't end in `(visual)`. Each criterion has at most one
shot, and a new one replaces the old.

## The command

```bash
issue-cli prove <slug> <AC-id> --test <TestName>       # every link flag is repeatable
issue-cli prove <slug> <AC-id> --step <TP3>            # a playtest session step id
issue-cli prove <slug> <AC-id> --commit <sha>          # stored as the full sha; refused when unknown
issue-cli prove <slug> <AC-id> --value "690 vs 380"
issue-cli prove <slug> <AC-id> --shot <path>           # see above
issue-cli prove <slug> <AC-id> --clear                 # removes this criterion's prove links
issue-cli prove <slug> [--list] [--json]               # every criterion, its links and checks
```

The list shows each criterion as `proven` or `UNPROVEN`, and under it each link with where it came from and its
check result (with a `→` fix line when it fails).
- After the criteria it lists the ways to prove the unproven ones once, then any link naming a criterion the issue
  doesn't have ("Unknown criterion AC9").
- A prove link whose criterion was rewritten or renumbered since is marked `[criterion text changed since linked]`.
- `--json` returns the report: `criteria[]` with `links[]` and each link's `check`, plus `strays`, `missing`,
  `proven`, `total` and `ticked`.

## Ticking

`prove`, `prove --list` and the transition that runs `criteria_proven` tick every proven criterion that is still
unticked. A ticked box whose proof stops passing is reported, not unticked.

While the gate covers an issue, `issue-cli check <slug> AC2` (by id, text, index or `--section … --all`) is
refused, with a pointer to `prove --list`. Ticking a box in the web UI stays allowed, because the owner decides.
`issue-cli checklist` adds a line with the count ("2/3 proven").

## The gates

Two validation rules:

- `criteria_checkboxes: Acceptance Criteria` runs at `in design → backlog` and `in design → in progress`. The
  section must have checkboxes, every list item in it must be one, and no box may still hold the text the
  workflow's `append_section` put there ("First observable requirement").
- `criteria_proven: since YYYY-MM-DD` runs at `testing → human-testing`. Every criterion needs a passing link.
  - The error names each unproven criterion, why each of its links fails, and the ways to prove it.
    `transition --dry-run` shows it next to the other unmet requirements.
  - Issues `created` before the date are exempt, so issues in flight when a project adopts the feature keep working.
    So are issues without criteria checkboxes (bugfix, tweak). Both pass with no message; `prove --list` says
    when the gate doesn't cover an issue. An issue without `created:` counts as new (covered). Every way of
    creating an issue writes the date, `issue-cli health lint` flags an undated one, and the Evidence tab's label
    follows the same rule (`ProofRequired`).
  - With no argument (`rule: "criteria_proven"`, as in the bundled template), it covers every issue.

`criteria_proven` needs git and the playtest runs, which the `tracker` package can't reach. `internal/proof`
registers the check with `tracker.SetProofChecker` in its `init`. issue-cli imports it, and the viewer imports it
in `main.go` for transition previews; a program that doesn't import it fails the rule with a pointer to issue-cli.

## Prompts

The feature adds two status prompt lines:

- **`in progress`:** declare each criterion's proof where you write it (`proves:`, `Proves:` trailers).
- **`shipping`:** post a `risk:` comment for anything to watch after the ship, or "risk: none".

## Code

- `internal/proof/`: `Collect(root, issue, comments)` builds the report (criteria, links, checks); `GateError`
  and `TickProven` work from it. The Evidence tab reads the same report.
- `internal/tracker/proof.go`: the `ProofLink` sidecar type, `Criteria`, both rules, `ProofGateSince`,
  `ProofRequired`, `TransitionChecksProof`.
- `cmd/issue-cli/cmd_prove.go`; the refusal is in `cmd_check.go`, and ticking before the gate in `cmd_transition.go`.
- `cmd/playtest`: `proves:` on `Step` and `StepResult`; `Validator.Checklist` returns each id's section.
