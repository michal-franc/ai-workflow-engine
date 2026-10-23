package tracker

import "github.com/michal-franc/issue-viewer/internal/tokens"

// AgentDispatchPromptTemplate is the constant scaffolding text the issue
// viewer writes into every dispatched agent session's prompt. BuildAgentPrompt
// fills its named {{placeholders}} (slug, system, title, status, body, ...)
// from agentPromptFields; this string is the constant skeleton. Named, not
// positional: a ~20-slot Sprintf once put the system where the retrospective
// command's slug belongs.
//
// Kept in the tracker package so the workflow stats view can attribute its
// token weight without depending on the main package, and so the dispatch
// handler and the stats view share a single source of truth for the prompt
// text.
const AgentDispatchPromptTemplate = `You have been assigned this issue: {{slug}}

## Before you start

Learn the workflow process first:
  issue-cli process workflow      # understand the status lifecycle
  issue-cli process transitions   # understand what each transition requires

## Your goal

Move this issue forward correctly using the configured workflow.
Use issue-cli to inspect the current status requirements, complete the required work, and only transition when the workflow says the issue is ready.
Stop and ask the user whenever clarification or manual verification is required.
At a human-approval gate, tell the user in chat exactly what needs approving and where, then stop and end your turn.
Do not wait, poll or re-run commands to check for the approval: the human messages you once it is approved.

## Current status guidance

{{status_guidance}}

{{status_reminder}}

## How to work on this issue

1. Run: issue-cli start {{slug}}
   If the issue is already approved for the next status, this claims the issue and shows your checklist and next steps.
   If approval is missing, tell the human what needs approving in the issue viewer, then stop and end your turn.
   Continue when the human messages you that it is approved.

2. Run: issue-cli show {{slug}}
   Read the full context — body, comments, checklist status.

3. Work through each checkbox in the issue. 'issue-cli checklist' shows each box's id (D3, AC2).
   Mark finished boxes by id — several in one call:
   issue-cli check {{slug}} <id> [<id>...]
   (Matching by text, issue-cli check <slug> "<checkbox text>", still works as a fallback.)

4. If you are unsure about something or need clarification, ask the user (or message your lead) in chat before proceeding.
   When the human decides something, record it in their words:
   issue-cli decide <slug> --text "<what was decided>" --quote "<their words>"

5. When the current status checkboxes are done, transition to the next status:
   issue-cli transition {{slug}} --to "<next-status>"
   The CLI will tell you what the valid next status is and what it requires.
   Add --dry-run to list every unmet requirement (with fix commands) without changing anything.
   If the transition needs human approval, tell the human what to approve, then stop and end your turn.

6. Repeat steps 3-5 for each status. Each transition may add new checkboxes — work through them all.

## issue-cli commands you can use freely

These are safe to run without asking the user:
  issue-cli process workflow          # learn status lifecycle
  issue-cli process transitions       # learn transition requirements
  issue-cli show {{slug}}                   # full context dump
  issue-cli checklist {{slug}}              # checkbox status
  issue-cli next                      # see available work
  issue-cli start {{slug}}                  # claim and begin work
  issue-cli check {{slug}} <id> [<id>...]   # mark checkboxes done by id (from checklist)
  issue-cli transition {{slug}} --to "<next-status>"  # move forward
      (add --dry-run to list unmet requirements)
  issue-cli append {{slug}} --body "content"          # append section to issue body
  issue-cli append {{slug}} --section "Name" --body "..."  # append into an existing section (also auto-routes if --body starts with that heading)
  issue-cli retrospective {{slug}} --body "content"   # save workflow feedback under retros/ in the project

## CRITICAL: NEVER modify issue .md files manually. Always use issue-cli commands.

## If you hit a bug in issue-cli or the workflow engine itself, report it:
  issue-cli report-bug "description of what went wrong"
It becomes a cli-bug issue on the issue-viewer board, or a comment on the
existing one; the output names the issue. Your project and issue slug are
attached automatically. Bugs in this project itself go on this board with
issue-cli create, not report-bug.

## Workflow review

If you stop because human approval, manual verification, clarification, or tooling/workflow blockage is required, run:
  issue-cli retrospective {{slug}} --body "Base workflow: ...\nSubsystem workflow for {{system}}: ...\nMissing system-specific instructions: ...\nTooling/workflow friction: ..."
This saves a retrospective under retros/ in the project.

Then briefly tell the user how the workflow could be improved.
Cover both:
  - the base workflow
  - the relevant subsystem workflow guidance for this issue
  - any missing system-specific instructions that should be added for {{system}}

If you hit friction, ambiguity, or missing guardrails while using issue-cli or the workflow, call that out explicitly.

## Commands that require user approval — DO NOT run without asking

  Any transition backwards                       # ask first
  Creating or deleting issues                    # ask first

## Issue metadata
  Title: {{title}}
  Status: {{status}}
  Priority: {{priority}}

{{body}}`

// AgentDispatchPromptStaticCost returns the approximate token count of the
// dispatch prompt scaffolding the bot reads on every fresh agent dispatch.
// Issue-specific substitutions (slug, title, body) are NOT included — the
// template's {{placeholders}} contribute negligible tokens. Use this as a
// per-dispatch baseline alongside per-transition costs.
func AgentDispatchPromptStaticCost() int {
	return tokens.Estimate(AgentDispatchPromptTemplate)
}
