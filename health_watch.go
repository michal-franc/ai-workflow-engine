package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/playtestruns"
	"github.com/michal-franc/issue-viewer/internal/privacy"
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
		// The privacy badges read the repo's GitHub visibility from a cache; ask
		// GitHub at most once an hour per repo, here, never on a page view.
		if vis, asked, err := privacy.RefreshVisibility(context.Background(), root, now, privacy.VisibilityMaxAge); err != nil {
			if msg := "visibility: " + err.Error(); lastErr[root+"#vis"] != msg {
				lastErr[root+"#vis"] = msg
				log.Printf("privacy: %s: %v", proj.Slug, err)
			}
		} else if asked {
			delete(lastErr, root+"#vis")
			if vis == "" {
				vis = "not on GitHub"
			}
			log.Printf("privacy: %s: repo is %s on GitHub", proj.Slug, vis)
		}
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
		if line := shrinkOldShots(proj, now); line != "" {
			log.Print(line)
		}
	}
}

// shrinkOldShots shrinks the playtest shots of issues done longer ago than
// health.shrink_shots_after_days, once a day after the snapshot. It returns
// the line to log, "" when nothing changed.
func shrinkOldShots(proj *tracker.Project, now time.Time) string {
	after, ok := proj.LoadWorkflow().Health.ShrinkShotsAfter()
	if !ok {
		return ""
	}
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return ""
	}
	bySlug := map[string]*tracker.Issue{}
	for _, is := range issues {
		bySlug[is.Slug] = is
	}
	doneSince := func(slug string) (time.Time, bool) {
		is, ok := bySlug[slug]
		if !ok || is.Status != "done" {
			return time.Time{}, false
		}
		at, _ := doneAt(is) // the status page's: sidecar, done_at, else mtime
		return at, true
	}
	rep, err := playtestruns.Shrink(projectRoot(proj), doneSince, after, now)
	if err != nil {
		return fmt.Sprintf("health: %s: shrinking shots: %v", proj.Slug, err)
	}
	if rep.Issues == 0 {
		return ""
	}
	return fmt.Sprintf("health: %s: shrank shots of %d issues done before %s: %s → %s", proj.Slug, rep.Issues,
		now.Add(-after).Format("2006-01-02"), playtestruns.MB(rep.BytesBefore), playtestruns.MB(rep.BytesAfter))
}
