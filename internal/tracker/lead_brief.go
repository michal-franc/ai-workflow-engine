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
- Big chunks of work are epics (issues that children name with parent:). You lead them yourself (one lead per project). Their children branch from main and ship to main like any other issue.
- Check on an agent: issue-cli context <slug>, git log main..<branch>, tmux capture-pane -p -t <session> | tail -40.
- Talk to a worker: issue-cli lead message <slug> "...". For coordination only; decisions come from the human.
- Relay questions: workers ask in chat or message you (SendMessage). Bring them to the human one at a time, and record each answer in the human's words: issue-cli decide <slug> --text "..." --how "lead chat" --quote "<their words>".
- Check-ins: report only what changed since the last one.`

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
	brief := strings.NewReplacer(
		"{{project}}", projectName,
		"{{lead}}", lead,
		"{{project_lead}}", lead,
	).Replace(strings.TrimSpace(tmpl))

	var b strings.Builder
	b.WriteString(brief)
	b.WriteString("\n\n## Your role\n\n")
	fmt.Fprintf(&b, "- Role: project lead (%s). You answer to the human.\n", lead)
	b.WriteString("- Authority: you may tick a gate only when the human approved it in your chat: issue-cli approve <slug> --to <status> --quote \"<the human's words>\". Never Human Testing; workers never tick.\n")
	fmt.Fprintf(&b, "- Messaging: in Claude Code, agents on this project message each other with ListAgents / SendMessage, addressed by tmux name (yours is %s; workers are agent-<slug>). Workers may message you directly. issue-cli lead message is the tmux-paste fallback, e.g. for Codex.\n", lead)
	b.WriteString("- Decisions: record each owner decision under ## Decisions with the human's words (issue-cli append <slug> --section Decisions --body \"- **D-n** · <date> · lead chat: ...\").\n")
	if wf != nil && wf.CI.Enabled() {
		b.WriteString("- Local CI: the ci: commands run on the integration branch after every move, without you (issue-cli ci status; the Status page). When it goes red you get a [from ci] message: bring it to the human and decide whether to revert, fix forward or dispatch a fix. Run issue-cli ci status before any push, and never push a release while it is red.\n")
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
	b.WriteString(" The viewer's Status page shows that ## Next list; its text form, /p/<project>/status.md, is a ready-made stand-up. Its Waiting for release block says what is merged and not released, what is not pushed, and when the current milestone is ready to cut (and what blocks it). Its lists are capped: a \"+K more\" line names the issue-cli list command that shows the rest.")
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
