package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The daily health snapshot: the server looks every healthCheckInterval and
// takes one for each project (once per repository) that has none from
// today. issue-cli ci run takes the others, after each run.

const healthCheckInterval = 10 * time.Minute

// watchHealth checks every project on a tick until stop closes.
func (s *Server) watchHealth(stop <-chan struct{}) {
	lastErr := map[string]string{}
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	for {
		healthDaily(s.projects, time.Now(), lastErr)
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// healthDaily takes today's snapshot where it is missing. Projects outside
// git or with health.off are skipped; other errors are logged once each.
func healthDaily(projects []tracker.Project, now time.Time, lastErr map[string]string) {
	seen := map[string]bool{}
	for i := range projects {
		proj := &projects[i]
		if !proj.LoadWorkflow().Health.Enabled() {
			continue
		}
		root := ciRootFor(proj)
		if seen[root] {
			continue
		}
		seen[root] = true
		if health.TakenOn(root, now) {
			continue
		}
		env, err := health.NewEnv(proj, projectRoot(proj), now)
		if errors.Is(err, health.ErrNotGit) {
			continue
		}
		var path string
		if err == nil {
			_, path, err = health.Take(context.Background(), env, health.TriggerDaily)
		}
		if err != nil {
			if msg := err.Error(); lastErr[root] != msg {
				lastErr[root] = msg
				log.Printf("health: %s: %v", proj.Slug, err)
			}
			continue
		}
		delete(lastErr, root)
		log.Printf("health: %s: daily snapshot %s", proj.Slug, path)
	}
}
