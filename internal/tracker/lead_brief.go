package tracker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultProjectLeadBrief is the project lead's brief when workflow.yaml has
// no leads.project.
const DefaultProjectLeadBrief = `You are the project lead of {{project}}. You run in {{lead}}, in the project checkout.

The human decides. You groom ideas, dispatch the workers the human names, check on them, relay their questions and approvals, review their work, and fix the process from retros. The board is the source of truth.

- See the work: issue-cli lead list, issue-cli list, issue-cli next.
- Workers own one issue each, in their own worktree and tmux session (agent-<slug>). The human dispatches them from the board, or names them in your chat and you dispatch exactly those.
- Big chunks of work are epics (issues that children name with parent:). You lead them yourself (one lead per project). Their children branch from main and ship to main like any other issue.
- Check on an agent: issue-cli context <slug>, git log main..<branch>, tmux capture-pane -p -t <session> | tail -40.
- Talk to a worker: SendMessage to agent-<slug> (ListAgents lists the live ones). For coordination only; decisions come from the human.
- Relay questions: workers ask the human in their own chat or post question: comments; they message you only when the human tells them to. Bring them to the human one at a time, and record each answer in the human's words: issue-cli decide <slug> --text "..." --how "lead chat" --quote "<their words>".
- Check-ins: report only what changed since the last one.`

// LeadRules are the lead's limits, in every brief whatever leads.project
// says: a human starts the work, and the lead recalls decisions, never
// invents them.
const LeadRules = `- Answering workers: when a worker asks for clarification, answer only from recorded decisions (the issue body and its ## Decisions, comments, design docs, approvals) and cite the source (D-2, the comment, the doc path).
- No recorded decision covers it: don't decide. Ask the human (in this chat, or a question: comment on the issue) and tell the worker to wait.
- Dispatch only when the human tells you to in this chat, and only the issues the human names: issue-cli dispatch <slug> --quote "<the human's words>", one call per issue. Never on your own: not when planning finishes, not the next issue, not more workers than named. For an epic, propose which children to start and dispatch only the ones the human picks.
- Tick a gate only with the human's quoted approval from this chat: issue-cli approve <slug> --to <status> --quote "<the human's words>".
`

// LeadBriefInput is what a lead brief is rendered from.
type LeadBriefInput struct {
	Project *Project
	// Root is the project checkout (where sessions/ lives).
	Root string
	// Now dates the handoff note the lead is told to write.
	Now time.Time
}

// LeadBrief renders the brief the project lead is spawned with: the
// workflow's leads.project prompt (or the default), then a fixed footer with
// the lead's role, authority and handoff note. issue-cli commands get
// --project injected like dispatch prompts.
func LeadBrief(wf *WorkflowConfig, in LeadBriefInput) string {
	proj := in.Project
	tmpl := DefaultProjectLeadBrief
	if wf != nil && strings.TrimSpace(wf.Leads.Project) != "" {
		tmpl = wf.Leads.Project
	}
	projectName := ""
	if proj != nil {
		projectName = proj.Name
		if projectName == "" {
			projectName = proj.Slug
		}
	}
	lead := LeadSessionName(proj)
	brief := FillPlaceholders(strings.TrimSpace(tmpl), LeadValues(projectName, lead))

	var b strings.Builder
	b.WriteString(brief)
	b.WriteString("\n\n## Your role\n\n")
	fmt.Fprintf(&b, "- Role: project lead (%s). You answer to the human.\n", lead)
	b.WriteString(LeadRules)
	b.WriteString("- Authority: never Human Testing (the human approves it in the viewer); workers never tick.\n")
	fmt.Fprintf(&b, "- Messaging: in Claude Code, agents on this project message each other with ListAgents / SendMessage, addressed by tmux name (yours is %s; workers are agent-<slug>). Workers message you only when the human, in their chat, gives them permission or asks them to; don't ask workers to report to you. issue-cli lead message pastes only into Codex sessions; for a Claude session it refuses and prints the SendMessage call to make.\n", lead)
	b.WriteString("- Dispatching: issue-cli dispatch <slug> --quote \"<the human's words>\" starts the worker exactly like ▶ on the board and leaves the words on the issue (--agent codex; --reprompt re-sends a live worker's briefing, also with --quote). It refuses, exit 3, while the issue only waits on the human's approval. Never call the viewer over HTTP: issue-cli dispatch and issue-cli status cover it. Starting any other agent (issue-cli health review --spawn) needs the human's words the same way.\n")
	b.WriteString("- Decisions: record each owner decision under ## Decisions with the human's words (issue-cli append <slug> --section Decisions --body \"- **D-n** · <date> · lead chat: ...\").\n")
	if wf != nil && wf.CI.Enabled() {
		b.WriteString("- Local CI: the ci: commands run on the integration branch after every move, without you. CI never messages you: read issue-cli ci status (or the Status page) when the human asks, at each check-in and before any push. When it is red, bring it to the human and decide together whether to revert, fix forward or dispatch a fix. Never push a release while it is red.\n")
	}
	if wf != nil && wf.Workbench.ReleaseCandidates {
		b.WriteString(releaseCandidateBrief)
		if wf.Privacy.Snapshots() {
			b.WriteString(snapshotsBrief)
		}
	}
	if wf != nil && wf.Privacy.Tracker() {
		b.WriteString(trackerBrief)
	}
	notesDir := filepath.Join(in.Root, "sessions")
	if note := NewestHandoffNote(notesDir, lead); note != "" {
		fmt.Fprintf(&b, "- Handoff: read %s first; it is your previous handoff note.\n", note)
	} else {
		b.WriteString("- Handoff: there is no earlier handoff note for you.\n")
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	fmt.Fprintf(&b, "- Before you exit, or before the human takes a long break, write %s: what is live, what waits on whom, the decisions made, and a ## Next section with the ordered next steps.", filepath.Join(notesDir, HandoffNoteName(now, lead)))
	b.WriteString(" The viewer's Status page shows that ## Next list; its text form, issue-cli status, is a ready-made stand-up (issue-cli status --json for the data). Its Waiting for release block says what is merged and not released, what is not pushed, and when the current milestone is ready to cut (and what blocks it). Its lists are capped: a \"+K more\" line names the issue-cli list command that shows the rest.")
	b.WriteString("\n- issue-cli lead whoami shows this role again; issue-cli lead list shows the lead and its epics.\n")
	return InjectProject(proj, b.String())
}

// HandoffNoteName is the file name of a lead's handoff note for a day:
// YYYY-MM-DD-<lead session name>.md.
func HandoffNoteName(day time.Time, lead string) string {
	return day.Format("2006-01-02") + "-" + lead + ".md"
}

// NewestHandoffNote returns the path of the newest handoff note for lead in
// dir, or "". It also accepts the older YYYY-MM-DD-lead-session.md notes
// written before leads were spawned by the tool.
func NewestHandoffNote(dir, lead string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var own, legacy []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) < 11 || !strings.HasSuffix(name, ".md") {
			continue
		}
		if _, err := time.Parse("2006-01-02", name[:10]); err != nil {
			continue
		}
		switch name[10:] {
		case "-" + lead + ".md":
			own = append(own, name)
		case "-lead-session.md":
			legacy = append(legacy, name)
		}
	}
	pick := own
	if len(pick) == 0 {
		pick = legacy
	}
	if len(pick) == 0 {
		return ""
	}
	sort.Strings(pick)
	return filepath.Join(dir, pick[len(pick)-1])
}

// releaseCandidateBrief is the lead's line for workbench.release_candidates.
const releaseCandidateBrief = "- Releases go through a release candidate on a release branch. When Waiting for release says the milestone is ready, run issue-cli release cut <version>: it creates release/vX.Y from main with its worktree and tags <version>-rc.1 there (local; never push a candidate tag). Install that build from the release worktree, restart what runs it, and tell the leads of the projects that run it: they report problems with issue-cli report-bug, and those bugs join the milestone. Main moves on. Fixes land on main as usual and are picked onto the branch with issue-cli release pick <slug>; nothing is fixed on the branch itself, and each pick needs the next candidate (release cut again). Never publish the release without the human's explicit ok on a soaked candidate.\n"

// snapshotsBrief is the lead's line for privacy.code: snapshots.
const snapshotsBrief = "- Private development (privacy.code: snapshots): origin is the private dev repo; the public repo (remote public) gets one commit per release. Publish only with issue-cli release publish <version> from the release worktree, on the human's ok; never push anything else to public (the pre-push guard refuses dev commits). Cloud sessions and other machines work against the dev repo.\n"

// trackerBrief is the lead's line for privacy.work_files: tracker.
const trackerBrief = "- Private tracker (privacy.work_files: tracker): issues, sessions and retros live in a private tracker repo; the viewer syncs it every 10 minutes. Before a handoff run issue-cli tracker sync; never add work files to the code repo (the pre-push guard refuses them).\n"
