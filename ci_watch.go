package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Local CI in the viewer: the server only notices new commits on a
// project's CI branch and starts a detached `issue-cli ci run`, which
// outlives a server restart. Pages read .ci/state.json. Projects without a
// ci: block are skipped.

// ciPollInterval is how often the watcher looks at each branch head.
const ciPollInterval = 5 * time.Second

// ciSpawn starts a detached run for proj. A var so tests never start one.
var ciSpawn = func(proj *tracker.Project) error {
	exe, err := exec.LookPath("issue-cli")
	if err != nil {
		return fmt.Errorf("issue-cli is not on PATH (make install): %w", err)
	}
	cmd := exec.Command(exe, "--project", proj.Slug, "ci", "run", "--detach", "--if-untested")
	cmd.Dir = projectRoot(proj)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ciRoots caches each project root's main checkout (one git call each).
var ciRoots sync.Map

// ciRootFor is the main checkout proj's CI state lives in.
func ciRootFor(proj *tracker.Project) string {
	dir := projectRoot(proj)
	if root, ok := ciRoots.Load(dir); ok {
		return root.(string)
	}
	root := ci.RepoRoot(dir)
	ciRoots.Store(dir, root)
	return root
}

// ciWatcher holds one ci.Watcher per repository root, so two projects on
// one repo start one run, and logs each distinct error once.
type ciWatcher struct {
	watchers map[string]*ci.Watcher
	lastErr  map[string]string
}

func newCIWatcher() *ciWatcher {
	return &ciWatcher{watchers: map[string]*ci.Watcher{}, lastErr: map[string]string{}}
}

// watchCI checks every project on a tick until stop closes.
func (s *Server) watchCI(stop <-chan struct{}) {
	w := newCIWatcher()
	ticker := time.NewTicker(ciPollInterval)
	defer ticker.Stop()
	for {
		w.check(s.projects)
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// check looks at each configured project once. The workflow is re-read
// every time, so adding or removing ci: needs no restart.
func (w *ciWatcher) check(projects []tracker.Project) {
	seen := map[string]bool{}
	for i := range projects {
		proj := &projects[i]
		wf := proj.LoadWorkflow()
		if !wf.CI.Enabled() {
			continue
		}
		root := ciRootFor(proj)
		if seen[root] {
			continue
		}
		seen[root] = true
		watcher := w.watchers[root]
		if watcher == nil {
			watcher = &ci.Watcher{Spawn: func(string) error { return ciSpawn(proj) }}
			w.watchers[root] = watcher
		}
		head, spawned, err := watcher.Check(root, wf.CI)
		switch {
		case err != nil:
			if msg := err.Error(); w.lastErr[root] != msg {
				w.lastErr[root] = msg
				log.Printf("ci: %s: %v", proj.Slug, err)
			}
		case spawned:
			delete(w.lastErr, root)
			log.Printf("ci: %s: new head %s, run started", proj.Slug, ci.Short(head))
		default:
			delete(w.lastErr, root)
		}
	}
}
