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
	if !strings.Contains(got, "a ## Next section with the ordered next steps. The viewer's Status page shows that ## Next list; its text form, /p/<project>/status.md,") {
		t.Fatalf("project brief should ask for ## Next and name status.md:\n%s", got)
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
