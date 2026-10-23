package clilog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScrubMergesWorktreeLogsAndRemovesFreeText(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "issue-cli-logs", "actions.jsonl")
	writeLines(t, legacy, `{"ts":"2026-10-01T00:00:00Z","args":["comment","x","--text","old secret"]}`)

	agent := "agent-x"
	shared := `{"ts":"2026-10-01T10:00:00Z","pid":5,"ppid":1,"args":["comment","cli/x","--text","shared secret"]}`
	projLog := AgentPath(root, agent)
	writeLines(t, projLog, shared, `{"ts":"2026-10-01T12:00:00Z","pid":7,"ppid":1,"args":["show","cli/x"]}`)
	wtLogs := filepath.Join(root, ".worktrees", "cli", "x", ".agent-logs")
	writeLines(t, filepath.Join(wtLogs, agent, agent+".clilog"),
		`{"ts":"2026-10-01T09:00:00Z","pid":4,"ppid":1,"args":["process","workflow"]}`,
		shared,
		`{"ts":"2026-10-01T11:00:00Z","pid":6,"ppid":1,"args":["append","cli/x","--body","## Design\nprivate"]}`,
		`not json with a secret`)
	writeLines(t, filepath.Join(wtLogs, agent, "dispatch-prompt.txt"), "the prompt")
	writeLines(t, filepath.Join(wtLogs, agent, "messages", "m.txt"), "a message")
	writeLines(t, filepath.Join(root, ".agent-logs", FileName), `{"ts":"2026-10-02T00:00:00Z","args":["show","y"]}`)

	backupDir := filepath.Join(root, ".agent-logs", ".scrub-backup")
	opt := ScrubOptions{Root: root, BackupDir: backupDir, Legacy: legacy}

	dry := opt
	dry.DryRun = true
	rep, err := Scrub(dry)
	if err != nil || rep.Merged != 1 || rep.Copied != 2 || !rep.LegacyRemoved {
		t.Fatalf("dry run: %+v %v", rep, err)
	}
	if data, _ := os.ReadFile(projLog); strings.Contains(string(data), "process") || !fileExists(legacy) {
		t.Fatal("dry run changed files")
	}

	rep, err = Scrub(opt)
	if err != nil {
		t.Fatalf("Scrub: %v", err)
	}
	if rep.Merged != 1 || rep.Copied != 2 || !rep.LegacyRemoved || rep.Rewritten != 2 {
		t.Fatalf("report %+v", rep)
	}

	var got []Record
	Read(projLog, func(r Record) { got = append(got, r) })
	if len(got) != 4 {
		t.Fatalf("merged %d records, want 4 (deduplicated): %+v", len(got), got)
	}
	wantCmds := []string{"process", "comment", "append", "show"}
	for i, r := range got {
		if r.Args[0] != wantCmds[i] {
			t.Errorf("record %d: %q, want %s (sorted by start)", i, r.Args, wantCmds[i])
		}
	}
	for _, p := range []string{projLog, filepath.Join(wtLogs, agent, agent+".clilog")} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
			t.Fatalf("%s still holds free text:\n%s", p, data)
		}
	}
	if !strings.Contains(string(mustRead(t, projLog)), Placeholder("shared secret")) {
		t.Fatal("comment text not replaced by its placeholder")
	}
	if string(mustRead(t, filepath.Join(root, ".agent-logs", agent, "dispatch-prompt.txt"))) != "the prompt\n" ||
		string(mustRead(t, filepath.Join(root, ".agent-logs", agent, "messages", "m.txt"))) != "a message\n" {
		t.Fatal("prompt or message not copied")
	}
	if fileExists(legacy) {
		t.Fatal("legacy log not removed")
	}
	if !strings.Contains(string(mustRead(t, filepath.Join(backupDir, ".agent-logs", agent, agent+".clilog"))), "shared secret") ||
		!fileExists(filepath.Join(backupDir, "tmp", "actions.jsonl")) {
		t.Fatal("originals not backed up")
	}

	// A second run finds nothing left to do.
	rep, err = Scrub(opt)
	if err != nil || rep.Rewritten != 0 || rep.Copied != 0 || rep.LegacyRemoved {
		t.Fatalf("second run: %+v %v", rep, err)
	}
	var again []Record
	Read(projLog, func(r Record) { again = append(again, r) })
	if len(again) != 4 {
		t.Fatalf("second run changed the merged log: %d records", len(again))
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
