package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// TestConcurrentJSONOutputDoesNotRace exercises the acceptance criterion that
// removing the package-global `jsonOutput` lets two subcommands run in
// parallel with different JSONOutput values without racing. Run with
// `go test -race -run TestConcurrentJSONOutputDoesNotRace -count=10` to catch
// any reintroduction of shared mutable state.
func TestConcurrentJSONOutputDoesNotRace(t *testing.T) {
	dir := t.TempDir()
	issuesDir := filepath.Join(dir, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}
	for i := 0; i < 5; i++ {
		body := fmt.Sprintf("---\ntitle: \"sample %d\"\nstatus: \"backlog\"\n---\nbody\n", i)
		if err := os.WriteFile(filepath.Join(issuesDir, fmt.Sprintf("s%d.md", i)), []byte(body), 0644); err != nil {
			t.Fatalf("write fixture %d: %v", i, err)
		}
	}
	proj := &tracker.Project{Name: "race", Slug: "race", IssueDir: issuesDir}

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		jsonOut := i%2 == 0
		go func(jsonOut bool) {
			defer wg.Done()
			ctx, stdout, _ := newTestContext(proj, jsonOut)

			if err := runList(ctx, nil); err != nil {
				t.Errorf("runList: %v", err)
				return
			}
			out := stdout.String()
			if jsonOut {
				var entries []listJSONIssue
				if err := json.Unmarshal([]byte(out), &entries); err != nil {
					t.Errorf("expected JSON output, got: %q (err: %v)", out, err)
				}
			} else {
				if strings.TrimSpace(out) == "" {
					t.Errorf("expected text output, got empty")
				}
				// Text output ends with "N issues\n"; JSON would not.
				if !strings.Contains(out, " issues\n") {
					t.Errorf("expected text output with N-issues footer, got: %q", out)
				}
			}
		}(jsonOut)
	}
	wg.Wait()
}

// TestConcurrentRunDifferentCommandsDoNotRace runs two different subcommands
// with different JSONOutput configurations against isolated temp projects
// concurrently to confirm no package-global state leaks between them.
func TestConcurrentRunDifferentCommandsDoNotRace(t *testing.T) {
	mkProject := func(numIssues int) *tracker.Project {
		dir := t.TempDir()
		issuesDir := filepath.Join(dir, "issues")
		if err := os.MkdirAll(issuesDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for i := 0; i < numIssues; i++ {
			body := fmt.Sprintf("---\ntitle: \"sample %d\"\nstatus: \"backlog\"\n---\nbody\n", i)
			if err := os.WriteFile(filepath.Join(issuesDir, fmt.Sprintf("s%d.md", i)), []byte(body), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		return &tracker.Project{Name: "p", Slug: "p", IssueDir: issuesDir}
	}

	projA := mkProject(3)
	projB := mkProject(7)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, _, _ := newTestContext(projA, true)
		if err := runList(ctx, nil); err != nil {
			t.Errorf("list A: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ctx, _, _ := newTestContext(projB, false)
		if err := runStats(ctx, nil); err != nil {
			t.Errorf("stats B: %v", err)
		}
	}()
	wg.Wait()
}
