package main

import (
	"log"
	"time"

	"github.com/michal-franc/issue-viewer/internal/privacy"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The tracker watcher: every trackerSyncInterval the server syncs each
// project's private tracker repo (privacy.work_files: tracker): commit any
// change under the work paths, pull, push. It prompts no session; a failed
// sync is recorded in the tracker's sync file and shows on Status as
// "Backup failing". issue-cli tracker sync does the same on demand.

const trackerSyncInterval = 10 * time.Minute

// trackerSync is privacy.Tracker.Sync; a var so tests count the calls.
var trackerSync = func(t privacy.Tracker, now time.Time) (privacy.SyncResult, error) { return t.Sync(now) }

// watchTrackers syncs on a tick until stop closes.
func (s *Server) watchTrackers(stop <-chan struct{}) {
	lastErr := map[string]string{}
	ticker := time.NewTicker(trackerSyncInterval)
	defer ticker.Stop()
	for {
		syncTrackers(s.projects, time.Now(), lastErr)
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// syncTrackers syncs every tracker once (two projects on one tracker share
// a sync) and logs each distinct error once.
func syncTrackers(projects []tracker.Project, now time.Time, lastErr map[string]string) {
	seen := map[string]bool{}
	for i := range projects {
		proj := &projects[i]
		cfg := proj.LoadWorkflow().Privacy
		if !cfg.Tracker() {
			continue
		}
		t := privacy.ForProject(proj, cfg, now).TrackerFor()
		if seen[t.GitDir] || !t.Exists() {
			continue
		}
		seen[t.GitDir] = true
		res, err := trackerSync(t, now)
		if err != nil {
			if msg := err.Error(); lastErr[t.GitDir] != msg {
				lastErr[t.GitDir] = msg
				log.Printf("tracker: %s: %v", proj.Slug, err)
			}
			continue
		}
		delete(lastErr, t.GitDir)
		if res.Committed > 0 || res.Pulled {
			log.Printf("tracker: %s: %d files committed, pulled %v, pushed %v", proj.Slug, res.Committed, res.Pulled, res.Pushed)
		}
	}
}
