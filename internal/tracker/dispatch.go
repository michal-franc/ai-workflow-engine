package tracker

import "github.com/michal-franc/issue-viewer/internal/tokens"

// AgentDispatchPromptTemplate is the constant scaffolding text the issue
// viewer writes into every dispatched agent session's prompt. The handler
// formats it with issue-specific substitutions (slug, title, status, body);
// this string is the constant skeleton.
//
// Kept in the tracker package so the workflow stats view can attribute its
// token weight without depending on the main package, and so the dispatch
// handler and the stats view share a single source of truth for the prompt
// text.
const AgentDispatchPromptTemplate = `You have been assigned this issue: %s

## Before you start

Learn the workflow process first:
  issue-cli process workflow      # understand the status lifecycle
  issue-cli process transitions   # understand what each transition requires

## Your goal

Move this issue forward correctly using the configured workflow.
Use issue-cli to inspect the current status requirements, complete the required work, and only transition when the workflow says the issue is ready.
Stop and ask the user whenever clarification, approval, or manual verification is required.

## Current status guidance

%s

%s

## How to work on this issue

1. Run: issue-cli start %s
   If the issue is already approved for the next status, this claims the issue and shows your checklist and next steps.
   If approval is missing, stop and ask the human to approve it in the issue viewer.

2. Run: issue-cli show %s
   Read the full context — body, comments, checklist status.

3. Work through each checkbox in the issue one at a time. After completing each one, mark it:
   issue-cli check %s "<checkbox text>"

4. If you are unsure about something or need clarification, ask the user before proceeding.

5. When the current status checkboxes are done, transition to the next status:
   issue-cli transition %s --to "<next-status>"
   The CLI will tell you what the valid next status is and what it requires.

6. Repeat steps 3-5 for each status. Each transition may add new checkboxes — work through them all.

## issue-cli commands you can use freely

These are safe to run without asking the user:
  issue-cli process workflow          # learn status lifecycle
  issue-cli process transitions       # learn transition requirements
  issue-cli show %s                   # full context dump
  issue-cli checklist %s              # checkbox status
  issue-cli next                      # see available work
  issue-cli start %s                  # claim and begin work
  issue-cli check %s "<text>"         # mark a checkbox done
  issue-cli transition %s --to "<next-status>"  # move forward
  issue-cli append %s --body "content"          # append section to issue body
  issue-cli append %s --section "Name" --body "..."  # append into an existing section (also auto-routes if --body starts with that heading)
  issue-cli retrospective %s --body "content"   # save workflow feedback under retros/ in the project

## CRITICAL: NEVER modify issue .md files manually. Always use issue-cli commands.

## If you encounter a bug in issue-cli itself, report it:
  issue-cli report-bug "description of what went wrong"
This saves a bug report under bugs/ in the server root.
The current issue slug is attached automatically for dispatched sessions.

## Workflow review

If you stop because human approval, manual verification, clarification, or tooling/workflow blockage is required, run:
  issue-cli retrospective %s --body "Base workflow: ...\nSubsystem workflow for %s: ...\nMissing system-specific instructions: ...\nTooling/workflow friction: ..."
This saves a retrospective under retros/ in the project.

Then briefly tell the user how the workflow could be improved.
Cover both:
  - the base workflow
  - the relevant subsystem workflow guidance for this issue
  - any missing system-specific instructions that should be added for %s

If you hit friction, ambiguity, or missing guardrails while using issue-cli or the workflow, call that out explicitly.

## Commands that require user approval — DO NOT run without asking

  issue-cli transition <slug> --to "done"       # only humans close issues
  Any transition backwards                       # ask first
  Creating or deleting issues                    # ask first

## Issue metadata
  Title: %s
  Status: %s
  Priority: %s

%s`

// AgentDispatchPromptStaticCost returns the approximate token count of the
// dispatch prompt scaffolding the bot reads on every fresh agent dispatch.
// Issue-specific substitutions (slug, title, body) are NOT included — the
// template's %s placeholders contribute negligible tokens. Use this as a
// per-dispatch baseline alongside per-transition costs.
func AgentDispatchPromptStaticCost() int {
	return tokens.Estimate(AgentDispatchPromptTemplate)
}
