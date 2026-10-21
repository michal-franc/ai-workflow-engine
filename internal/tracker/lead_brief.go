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

The human decides. You groom ideas, dispatch workers, check on them, relay their questions and approvals, review their work, and fix the process from retros. The board is the source of truth.

- See the work: issue-cli lead list, issue-cli list, issue-cli next.
- Workers own one issue each, in their own worktree and tmux session (agent-<slug>). The human dispatches them from the board, or you ask the human to.
- Big chunks of work are epics (issues that children name with parent:). You lead them yourself: one lead per project, no sub-leads. Their children branch from main and ship to main like any other issue.
- Check on an agent: issue-cli context <slug>, git log main..<branch>, tmux capture-pane -p -t <session> | tail -40.
- Talk to a lead or worker: issue-cli lead message <lead-name|slug> "...". For coordination only; decisions come from the human.
- Relay questions: workers ask in chat or message you (SendMessage). Bring them to the human one at a time, and record each answer in the human's words: issue-cli decide <slug> --text "..." --how "lead chat" --quote "<their words>".
- Check-ins: report only what changed since the last one.`

// DefaultEpicLeadBrief is a sub-lead's brief when workflow.yaml has no
// leads.epic.
const DefaultEpicLeadBrief = `You are the sub-lead of epic {{epic}} ("{{title}}") in {{project}}. You run in {{lead}}, in the worktree on the epic's base branch {{base}}.

You stabilise this epic. The human decides; you report to the project lead {{project_lead}}.

- Read the epic first: issue-cli context {{epic}}. Its children are the issues with parent: {{epic}} (issue-cli lead list shows how many are running).
- Get workers onto the children: ask {{project_lead}} to have the human dispatch them. Each child's worktree is created from {{base}}, and its PR targets {{base}}, not main.
- Review each child's work and merge it into {{base}}. Keep {{base}} green: run the tests here after every merge.
- Keep {{base}} up to date: when issue-cli lead list shows main commits not in {{base}}, merge main into {{base}}.
- Run the joint checks the epic needs (integration, balance) on {{base}}, once, here.
- Take {{base}} to main only when the human decides; the epic issue's own workflow holds those steps.
- You cannot spawn sub-leads. Workers report to you while you are live.
- Reach the human through {{project_lead}}: issue-cli lead message {{project_lead}} "...". Relay workers' questions that way, and wait for {{project_lead}} to relay the answers and approvals back.`

// LeadBriefInput is what a lead brief is rendered from.
type LeadBriefInput struct {
	Project *Project
	// Epic is nil for the project lead.
	Epic *Issue
	// Root is the project checkout (where sessions/ lives).
	Root string
	// Now dates the handoff note the lead is told to write.
	Now time.Time
}

// LeadBrief renders the brief a lead is spawned with: the workflow's
// leads.project / leads.epic prompt (or the default), then a fixed footer
// with the lead's role, authority and handoff note. issue-cli commands get
// --project injected like dispatch prompts.
func LeadBrief(wf *WorkflowConfig, in LeadBriefInput) string {
	proj := in.Project
	scope, tmpl := LeadScopeProject, DefaultProjectLeadBrief
	if wf != nil && strings.TrimSpace(wf.Leads.Project) != "" {
		tmpl = wf.Leads.Project
	}
	epic, title, base := "", "", ""
	if in.Epic != nil {
		scope, epic, title, base = in.Epic.Slug, in.Epic.Slug, in.Epic.Title, EpicBase(in.Epic)
		tmpl = DefaultEpicLeadBrief
		if wf != nil && strings.TrimSpace(wf.Leads.Epic) != "" {
			tmpl = wf.Leads.Epic
		}
	}
	projectName := ""
	if proj != nil {
		projectName = proj.Name
		if projectName == "" {
			projectName = proj.Slug
		}
	}
	lead := LeadSessionName(proj, scope)
	projectLead := LeadSessionName(proj, LeadScopeProject)
	brief := strings.NewReplacer(
		"{{project}}", projectName,
		"{{lead}}", lead,
		"{{project_lead}}", projectLead,
		"{{epic}}", epic,
		"{{title}}", title,
		"{{base}}", base,
	).Replace(strings.TrimSpace(tmpl))

	var b strings.Builder
	b.WriteString(brief)
	b.WriteString("\n\n## Your role\n\n")
	if in.Epic == nil {
		fmt.Fprintf(&b, "- Role: project lead (%s). You answer to the human.\n", lead)
		b.WriteString("- Authority: you may tick a gate only when the human approved it in your chat: issue-cli approve <slug> --to <status> --quote \"<the human's words>\". Never Human Testing; workers never tick.\n")
	} else {
		fmt.Fprintf(&b, "- Role: sub-lead (%s) of epic %s, guarding base branch %s. You report to %s.\n", lead, epic, base, projectLead)
		fmt.Fprintf(&b, "- Authority: you may tick gates only on %s and its children, and only after %s relays the human's words: issue-cli approve <slug> --to <status> --quote \"<the human's words>\" --how \"relayed by %s\". Never Human Testing; workers never tick.\n", epic, projectLead, projectLead)
	}
	fmt.Fprintf(&b, "- Messaging: in Claude Code, agents on this project message each other with ListAgents / SendMessage, addressed by tmux name (yours is %s; workers are agent-<slug>). Workers and sub-leads may message you directly. issue-cli lead message is the tmux-paste fallback, e.g. for Codex.\n", lead)
	b.WriteString("- Decisions: record each owner decision under ## Decisions with the human's words (issue-cli append <slug> --section Decisions --body \"- **D-n** · <date> · lead chat: ...\").\n")
	notesDir := filepath.Join(in.Root, "sessions")
	if note := NewestHandoffNote(notesDir, lead, in.Epic == nil); note != "" {
		fmt.Fprintf(&b, "- Handoff: read %s first; it is your previous handoff note.\n", note)
	} else {
		b.WriteString("- Handoff: there is no earlier handoff note for you.\n")
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	fmt.Fprintf(&b, "- Before you exit, or before the human takes a long break, write %s: what is live, what waits on whom, the decisions made, and a ## Next section with the ordered next steps.", filepath.Join(notesDir, HandoffNoteName(now, lead)))
	if in.Epic == nil {
		b.WriteString(" The viewer's Status page shows that ## Next list; its text form, /p/<project>/status.md, is a ready-made stand-up.")
		b.WriteString(" List your live sub-leads, so a restart can re-spawn them with issue-cli lead spawn --epic <slug>.")
	} else {
		b.WriteString(" When the epic is done and its base branch is in main, write your retro (issue-cli retrospective), then this note, then exit.")
	}
	b.WriteString("\n- issue-cli lead whoami shows this role again; issue-cli lead list shows every lead.\n")
	return InjectProject(proj, b.String())
}

// HandoffNoteName is the file name of a lead's handoff note for a day:
// YYYY-MM-DD-<lead session name>.md.
func HandoffNoteName(day time.Time, lead string) string {
	return day.Format("2006-01-02") + "-" + lead + ".md"
}

// NewestHandoffNote returns the path of the newest handoff note for lead in
// dir, or "". The project lead also accepts the older YYYY-MM-DD-lead-session.md
// notes written before leads were spawned by the tool.
func NewestHandoffNote(dir, lead string, projectLead bool) string {
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
			if projectLead {
				legacy = append(legacy, name)
			}
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
