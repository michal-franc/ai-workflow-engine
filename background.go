package main

import (
	"context"
	"sort"
	"sync"
)

// background tracks the server's goroutines that outlive a request (CI
// watcher, GitHub close, nvim edit watcher) so shutdown can wait for them
// and name the ones it gives up on.
type background struct {
	// ctx is cancelled when shutdown starts: stop waiting and polling.
	ctx context.Context
	// hard is cancelled when the grace period runs out: kill subprocesses.
	hard       context.Context
	cancelHard context.CancelFunc

	mu      sync.Mutex
	running map[string]int
	n       int
	// idle is closed when n drops to zero; nil while nothing runs. Not a
	// WaitGroup: a handler that outlives Shutdown may still call Go while
	// Wait waits.
	idle chan struct{}
}

func newBackground(ctx context.Context) *background {
	hard, cancel := context.WithCancel(context.Background())
	return &background{ctx: ctx, hard: hard, cancelHard: cancel, running: map[string]int{}}
}

// Go runs fn in a goroutine that Wait waits for; name is what the shutdown
// log prints if fn is still running at the deadline.
func (b *background) Go(name string, fn func()) {
	b.mu.Lock()
	if b.n == 0 {
		b.idle = make(chan struct{})
	}
	b.n++
	b.running[name]++
	b.mu.Unlock()
	go func() {
		defer func() {
			b.mu.Lock()
			if b.running[name]--; b.running[name] == 0 {
				delete(b.running, name)
			}
			if b.n--; b.n == 0 {
				close(b.idle)
				b.idle = nil
			}
			b.mu.Unlock()
		}()
		fn()
	}()
}

// Wait blocks until every goroutine has returned or deadline is done. At the
// deadline it cancels hard, so subprocesses started with it die, and returns
// the names still running.
func (b *background) Wait(deadline context.Context) []string {
	defer b.cancelHard()
	b.mu.Lock()
	idle := b.idle
	b.mu.Unlock()
	if idle == nil {
		return nil
	}
	select {
	case <-idle:
		return nil
	case <-deadline.Done():
	}
	b.cancelHard()
	b.mu.Lock()
	defer b.mu.Unlock()
	names := make([]string, 0, len(b.running))
	for name := range b.running {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
