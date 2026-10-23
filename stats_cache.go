package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// The stats page's two slow inputs, cached per project so the page renders
// in well under 300 ms on a big project (RogueTech: the 30-day aggregate
// alone is ~500 ms, the CLI usage report ~300 ms).
//
// The aggregate is kept for the widest window asked for and filtered down
// for narrower ones. It is rebuilt at once when the done issues change
// (their fingerprint), and in the background, serving the old one
// meanwhile, once it is older than statsAggregateTTL (clilogs move on
// without touching an issue file). The CLI usage report never blocks: it is
// served from the cache and refreshed in the background.

const (
	statsAggregateTTL = 15 * time.Minute
	statsUsageTTL     = 5 * time.Minute
	// statsWarmWindow is what the server computes at start: the 90-day
	// range plus the 30 days a trend point looks back.
	statsWarmWindow = 120 * 24 * time.Hour
)

type statsAggregate struct {
	fp      string
	window  time.Duration
	at      time.Time
	rep     timeline.Report
	ms      int64
	running bool
}

type statsUsage struct {
	at      time.Time
	rep     *telemetry.Report
	notice  string
	running bool
}

type statsCache struct {
	mu    sync.Mutex
	agg   map[string]*statsAggregate
	usage map[string]*statsUsage
	// buildMu serializes aggregate builds per project, so two requests on
	// a cold cache build it once.
	buildMu map[string]*sync.Mutex
}

func newStatsCache() *statsCache {
	return &statsCache{agg: map[string]*statsAggregate{}, usage: map[string]*statsUsage{}, buildMu: map[string]*sync.Mutex{}}
}

// doneFingerprint changes when an issue is done or leaves done, or a done
// issue's file changes: what the aggregate reads besides the clilogs.
func doneFingerprint(issues []*tracker.Issue) string {
	h := sha256.New()
	for _, is := range issues {
		if is.Status == "done" {
			fmt.Fprintf(h, "%s:%d\n", is.Slug, is.ModTime.UnixNano())
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (c *statsCache) lockFor(slug string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.buildMu[slug]
	if !ok {
		m = &sync.Mutex{}
		c.buildMu[slug] = m
	}
	return m
}

// aggregate is the project report over at least window before now, and how
// long it took to build. bg runs the stale refresh; nil refreshes inline.
func (c *statsCache) aggregate(proj *tracker.Project, wf *tracker.WorkflowConfig, issues []*tracker.Issue, window time.Duration, now time.Time, bg func(func())) (timeline.Report, int64) {
	fp := doneFingerprint(issues)
	c.mu.Lock()
	a := c.agg[proj.Slug]
	if a != nil && a.fp == fp && a.window >= window {
		rep, ms := a.rep, a.ms
		if now.Sub(a.at) > statsAggregateTTL && !a.running && bg != nil {
			a.running = true
			bg(func() { c.build(proj, wf, issues, fp, a.window, time.Now()) })
		}
		c.mu.Unlock()
		return rep, ms
	}
	c.mu.Unlock()
	if a != nil && a.window > window {
		window = a.window
	}
	return c.build(proj, wf, issues, fp, window, now)
}

func (c *statsCache) build(proj *tracker.Project, wf *tracker.WorkflowConfig, issues []*tracker.Issue, fp string, window time.Duration, now time.Time) (timeline.Report, int64) {
	lock := c.lockFor(proj.Slug)
	lock.Lock()
	defer lock.Unlock()
	c.mu.Lock()
	if a := c.agg[proj.Slug]; a != nil && a.fp == fp && a.window >= window && now.Sub(a.at) <= statsAggregateTTL {
		c.mu.Unlock()
		return a.rep, a.ms
	}
	c.mu.Unlock()
	start := time.Now()
	rep := timeline.Aggregate(context.Background(), proj, wf, issues, now, window)
	ms := time.Since(start).Milliseconds()
	c.mu.Lock()
	c.agg[proj.Slug] = &statsAggregate{fp: fp, window: window, at: now, rep: rep, ms: ms}
	c.mu.Unlock()
	return rep, ms
}

// cliUsage is the cached CLI usage report and its notice. It never waits
// for the report: a missing or stale one is refreshed through bg, and
// loading is true until the first one lands.
func (c *statsCache) cliUsage(proj *tracker.Project, now time.Time, bg func(func())) (rep *telemetry.Report, notice string, loading bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := c.usage[proj.Slug]
	if u == nil {
		u = &statsUsage{}
		c.usage[proj.Slug] = u
	}
	if (u.at.IsZero() || now.Sub(u.at) > statsUsageTTL) && !u.running {
		u.running = true
		refresh := func() {
			r, n := loadCLIUsage(proj)
			c.mu.Lock()
			u.rep, u.notice, u.at, u.running = r, n, time.Now(), false
			c.mu.Unlock()
		}
		if bg == nil {
			c.mu.Unlock()
			refresh()
			c.mu.Lock()
		} else {
			bg(refresh)
		}
	}
	return u.rep, u.notice, u.at.IsZero()
}

// warmStats builds each project's aggregate and CLI usage in the
// background after start, so the first stats page is fast too.
// One project at a time, so start-up doesn't load every core.
func (s *Server) warmStats(stop <-chan struct{}) {
	for i := range s.projects {
		select {
		case <-stop:
			return
		default:
		}
		proj := &s.projects[i]
		issues, err := tracker.LoadIssues(proj.IssueDir)
		if err != nil {
			log.Printf("stats: %s: %v", proj.Slug, err)
			continue
		}
		s.stats.aggregate(proj, proj.LoadWorkflow(), issues, statsWarmWindow, time.Now(), nil)
		s.stats.cliUsage(proj, time.Now(), nil)
	}
}

// statsBG runs fn on the server's background runner.
func (s *Server) statsBG(name string) func(func()) {
	return func(fn func()) { s.bg.Go(name, fn) }
}
