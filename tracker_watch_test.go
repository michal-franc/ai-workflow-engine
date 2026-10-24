package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/privacy"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestSyncTrackersSyncsEachTrackerOnceAndSkipsTheOthers(t *testing.T) {
	base := t.TempDir()
	mk := func(name, workflow string) tracker.Project {
		root := filepath.Join(base, name)
		os.MkdirAll(filepath.Join(root, "issues"), 0o755)
		wf := filepath.Join(root, "workflow.yaml")
		os.WriteFile(wf, []byte("statuses:\n  - name: idea\n"+workflow), 0o644)
		return tracker.Project{Name: name, Slug: name, IssueDir: filepath.Join(root, "issues"), WorkflowFile: wf, WorkDir: root}
	}
	block := "privacy:\n  work_files: tracker\n  decided: x\n"
	a := mk("a", block)
	b := mk("b", block)
	b.TrackerGitDir = filepath.Join(base, ".trackers", "a.git") // shares a's tracker
	off := mk("off", "")
	missing := mk("missing", block) // chose a tracker but has no git folder yet
	gitDir := filepath.Join(base, ".trackers", "a.git")
	os.MkdirAll(gitDir, 0o755)
	os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)

	var synced []string
	orig := trackerSync
	defer func() { trackerSync = orig }()
	trackerSync = func(tr privacy.Tracker, now time.Time) (privacy.SyncResult, error) {
		synced = append(synced, tr.GitDir)
		return privacy.SyncResult{}, nil
	}
	syncTrackers([]tracker.Project{a, b, off, missing}, time.Now(), map[string]string{})
	if len(synced) != 1 || synced[0] != gitDir {
		t.Errorf("synced %v, want only %s", synced, gitDir)
	}
}
