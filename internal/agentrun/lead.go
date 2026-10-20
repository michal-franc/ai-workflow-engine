package agentrun

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// LeadSpec builds the launch spec for the project lead (epic nil) or for an
// epic's sub-lead, rooted at the project checkout root. For a sub-lead it
// refuses a non-epic and records the epic's base branch (`base:`) when it has
// none yet, so the brief and the worktree agree. The caller fills in
// AgentType, ViewerURL and Headless, starts it, then calls RecordLead.
func LeadSpec(proj *tracker.Project, wf *tracker.WorkflowConfig, issues []*tracker.Issue, epic *tracker.Issue, root string, now time.Time) (Spec, error) {
	spec := Spec{WorkDir: root, Workflow: wf}
	if epic == nil {
		spec.Session = tracker.LeadSessionName(proj, tracker.LeadScopeProject)
		spec.LeadScope = tracker.LeadScopeProject
	} else {
		if !tracker.IsEpic(epic, issues) {
			return Spec{}, fmt.Errorf("%s is not an epic: no issue names it as parent, and it has no type: epic or lead:.\n\nMake it one by linking a child:\n  issue-cli set-meta <child-slug> --key parent --value %s", epic.Slug, epic.Slug)
		}
		if epic.Field("base") == "" {
			base := tracker.EpicBase(epic)
			if err := tracker.SetFrontmatterField(epic.FilePath, "base", base, false); err != nil {
				return Spec{}, fmt.Errorf("recording base: on %s: %w", epic.Slug, err)
			}
			epic.ExtraFields = append(epic.ExtraFields, tracker.ExtraField{Key: "base", Value: base})
		}
		spec.Session = tracker.LeadSessionName(proj, epic.Slug)
		spec.LeadScope = epic.Slug
		spec.Worktree = tracker.EpicWorktree(root, epic)
	}
	spec.Prompt = tracker.LeadBrief(wf, tracker.LeadBriefInput{Project: proj, Epic: epic, Root: root, Now: now})
	return spec, nil
}

// RecordLead writes `lead: <session>` on the epic once its sub-lead started.
// A nil epic (the project lead) or a failed launch records nothing.
func RecordLead(epic *tracker.Issue, resp Response, session string) error {
	if epic == nil || resp.Status == "error" || epic.Field("lead") == session {
		return nil
	}
	return tracker.SetFrontmatterField(epic.FilePath, "lead", session, false)
}

// MainBranch names the repository's main branch: main, else master, else "".
func MainBranch(workdir string) string {
	for _, candidate := range []string{"main", "master"} {
		if exec.Command("git", "-C", workdir, "rev-parse", "--verify", "--quiet", "refs/heads/"+candidate).Run() == nil {
			return candidate
		}
	}
	return ""
}

// MainAhead counts the commits on the main branch that base lacks. ok is
// false when either branch can't be resolved.
func MainAhead(workdir, base string) (n int, mainBranch string, ok bool) {
	mainBranch = MainBranch(workdir)
	if mainBranch == "" {
		return 0, "", false
	}
	out, err := exec.Command("git", "-C", workdir, "rev-list", "--count", base+".."+mainBranch).Output()
	if err != nil {
		return 0, mainBranch, false
	}
	n, err = strconv.Atoi(strings.TrimSpace(string(out)))
	return n, mainBranch, err == nil
}
