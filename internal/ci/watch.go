package ci

import (
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// respawnAfter is how long a watcher waits before starting another run for a
// head it already started one for (the run may have failed before writing
// any state).
const respawnAfter = 5 * time.Minute

// Watcher starts a run when a project's branch head is untested and no run
// is live. The viewer and `issue-cli ci watch` both call Check on a tick;
// the lock makes sure only one run starts.
type Watcher struct {
	// Spawn starts a detached `issue-cli ci run` for the project.
	Spawn func(root string) error
	Now   func() time.Time

	mu      sync.Mutex
	spawned map[string]spawnRecord
}

type spawnRecord struct {
	head string
	at   time.Time
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Check looks at root's branch once and spawns a run when it needs one. It
// returns the head and whether it spawned.
func (w *Watcher) Check(root string, cfg *tracker.CIConfig) (string, bool, error) {
	if !cfg.Enabled() {
		return "", false, nil
	}
	branch, err := ResolveBranch(root, cfg)
	if err != nil {
		return "", false, err
	}
	head, err := Head(root, branch)
	if err != nil {
		return "", false, err
	}
	if _, alive := ReadLock(root); alive {
		return head, false, nil
	}
	if st, _ := ReadState(root); st != nil && st.LastSha == head {
		return head, false, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.spawned == nil {
		w.spawned = map[string]spawnRecord{}
	}
	if rec, ok := w.spawned[root]; ok && rec.head == head && w.now().Sub(rec.at) < respawnAfter {
		return head, false, nil
	}
	if err := w.Spawn(root); err != nil {
		return head, false, err
	}
	w.spawned[root] = spawnRecord{head: head, at: w.now()}
	return head, true, nil
}
