package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLeadBrief_CustomPromptsAndPlaceholders(t *testing.T) {
	var wf WorkflowConfig
	if err := yaml.Unmarshal([]byte("leads:\n  project: \"Lead {{project}} as {{lead}}.\"\n  epic: \"Stabilise {{epic}}.\"\n"), &wf); err != nil {
		t.Fatal(err)
	}
	proj := &Project{Slug: "p", Name: "Proj"}
	day := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	got := LeadBrief(&wf, LeadBriefInput{Project: proj, Root: "/r", Now: day})
	if !strings.HasPrefix(got, "Lead Proj as lead-p.\n\n## Your role") {
		t.Fatalf("project brief:\n%s", got)
	}
	if !strings.Contains(got, "/r/sessions/2026-10-18-lead-p.md") || !strings.Contains(got, "no earlier handoff note") {
		t.Fatalf("project brief handoff lines:\n%s", got)
	}
	if !strings.Contains(got, "message each other with ListAgents / SendMessage, addressed by tmux name (yours is lead-p;") {
		t.Fatalf("project brief messaging line:\n%s", got)
	}
	if !strings.Contains(got, "a ## Next section with the ordered next steps. The viewer's Status page shows that ## Next list; its text form, issue-cli --project p status,") {
		t.Fatalf("project brief should ask for ## Next and name issue-cli status:\n%s", got)
	}
	if !strings.Contains(got, "issue-cli --project p dispatch <slug> --quote \"<the human's words>\" starts the worker exactly like ▶") {
		t.Fatalf("project brief should name issue-cli dispatch:\n%s", got)
	}
	// The brief never sends the lead to the viewer's HTTP endpoints.
	for _, raw := range []string{"curl", "/status.md", "/status.json", "/dispatch"} {
		if strings.Contains(got, raw) {
			t.Errorf("project brief mentions %q:\n%s", raw, got)
		}
	}

	// leads.epic is obsolete: it loads, but nothing renders it.
	if strings.Contains(got, "Stabilise") || strings.Contains(got, "sub-lead") {
		t.Fatalf("project brief uses the obsolete epic brief or names sub-leads:\n%s", got)
	}
}

func TestLeadBrief_DefaultsWithoutLeadsBlock(t *testing.T) {
	got := LeadBrief(nil, LeadBriefInput{Project: &Project{Slug: "p"}, Root: "/r"})
	if !strings.HasPrefix(got, "You are the project lead of p.") {
		t.Fatalf("default brief:\n%s", got)
	}
	// The default brief agrees with the rules: dispatch is conditional, and
	// workers are reached with SendMessage, not a tmux paste.
	for _, want := range []string{"dispatch the workers the human names", "names them in your chat and you dispatch exactly those", "Talk to a worker: SendMessage to agent-<slug>"} {
		if !strings.Contains(got, want) {
			t.Errorf("default brief missing %q", want)
		}
	}
	for _, stale := range []string{"You groom ideas, dispatch workers,", "Talk to a worker: issue-cli", "tmux-paste fallback", "or you ask the human to"} {
		if strings.Contains(got, stale) {
			t.Errorf("default brief still says %q", stale)
		}
	}
}

// Every lead brief carries the four rules, whatever leads.project says: recall
// decisions with a source, escalate what none covers, dispatch only on the
// human's word, tick only with the human's quote.
func TestLeadBrief_CarriesTheFourRules(t *testing.T) {
	custom := &WorkflowConfig{Leads: LeadsConfig{Project: "Lead {{project}}."}}
	for name, wf := range map[string]*WorkflowConfig{"default": nil, "custom": custom} {
		got := LeadBrief(wf, LeadBriefInput{Project: &Project{Slug: "p"}, Root: "/r"})
		for _, want := range []string{
			"answer only from recorded decisions (the issue body and its ## Decisions, comments, design docs, approvals) and cite the source",
			"No recorded decision covers it: don't decide. Ask the human",
			"tell the worker to wait",
			"Dispatch only when the human tells you to in this chat, and only the issues the human names: issue-cli --project p dispatch <slug> --quote",
			"Never on your own: not when planning finishes, not the next issue, not more workers than named",
			"Tick a gate only with the human's quoted approval from this chat: issue-cli --project p approve <slug> --to <status> --quote",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s brief missing %q:\n%s", name, want, got)
			}
		}
	}
}

func TestNewestHandoffNote(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"2026-10-01-lead-session.md", "2026-10-13-lead-session.md",
		"2026-10-02-lead-p.md", "2026-10-09-lead-p.md",
		"2026-10-20-lead-other.md", "notes.md",
	} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644)
	}
	if got := NewestHandoffNote(dir, "lead-p"); got != filepath.Join(dir, "2026-10-09-lead-p.md") {
		t.Errorf("own note should win over legacy: %q", got)
	}
	os.Remove(filepath.Join(dir, "2026-10-02-lead-p.md"))
	os.Remove(filepath.Join(dir, "2026-10-09-lead-p.md"))
	if got := NewestHandoffNote(dir, "lead-p"); got != filepath.Join(dir, "2026-10-13-lead-session.md") {
		t.Errorf("falls back to the newest legacy note: %q", got)
	}
	if got := NewestHandoffNote(filepath.Join(dir, "missing"), "lead-p"); got != "" {
		t.Errorf("missing dir: %q", got)
	}
}

func TestLeadsSurviveCloneAndSchema(t *testing.T) {
	wf := &WorkflowConfig{Leads: LeadsConfig{Project: "P", Epic: "E"}}
	if got := wf.Clone().Leads; got != wf.Leads {
		t.Fatalf("Clone dropped leads: %+v", got)
	}
	var found bool
	for _, sec := range WorkflowSchemaSections() {
		if sec.Path == "leads" && len(sec.Fields) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("help schema has no leads section with project/epic")
	}
}
