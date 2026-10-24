package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const snapshotsBlock = "privacy:\n  code: snapshots\n  decided: \"2026-10-24, owner: \\\"private dev\\\"\"\n"

// releaseViewer serves release.json with body.
func releaseViewer(t *testing.T, body string) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/release.json") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestReleaseCutRefusesUnlessTheCardSaysReady(t *testing.T) {
	proj, _, _ := makePrivacyFixture(t, snapshotsBlock)
	proj.ViewerURL = releaseViewer(t, `{"milestone":{"version":"0.54","cut":"v0.54.0","rc":true,"checks":[{"key":"issues","text":"Every 0.54 issue done","detail":"","ok":false},{"key":"ci","text":"Local CI green","detail":"main red since abc","ok":false}]}}`)
	ctx, _, _ := newTestContext(proj, false)
	err := runRelease(ctx, []string{"cut", "v0.54.0"})
	if err == nil || !strings.Contains(err.Error(), "does not say ready to cut v0.54.0") || !strings.Contains(err.Error(), "✗ Local CI green: main red since abc") {
		t.Errorf("err = %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	if err := runRelease(ctx, []string{"cut", "v0.55.0"}); err == nil || !strings.Contains(err.Error(), "the milestone to cut is v0.54.0") {
		t.Errorf("wrong version = %v", err)
	}
}

func TestReleaseCutTagsTheCandidateOnTheBranch(t *testing.T) {
	proj, root, _ := makePrivacyFixture(t, "")
	proj.ViewerURL = releaseViewer(t, `{"milestone":{"version":"0.54","cut":"v0.54.0","rc":true,"ready":false,"cut_next":"v0.54.0-rc.1","checks":[]}}`)
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runRelease(ctx, []string{"cut", "0.54.0"}); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, ".worktrees", "release-v0.54")
	assertContains(t, stdout.String(), "✓ Created release/v0.54 from main, worktree "+wt)
	assertContains(t, stdout.String(), "✓ Tagged v0.54.0-rc.1 on release/v0.54 (local; never push a candidate tag)")
	if strings.Contains(stdout.String(), "Pushed") {
		t.Error("without code: snapshots the branch must not be pushed")
	}
	if got := gitOut(t, root, "rev-parse", "v0.54.0-rc.1^{commit}"); got != gitOut(t, root, "rev-parse", "release/v0.54") {
		t.Error("the candidate is not on the branch tip")
	}

	// pick finds the only release worktree by itself.
	gitIn(t, root, "checkout", "-q", "-b", "work/ui/a")
	os.WriteFile(filepath.Join(root, "fix.go"), []byte("package main\n"), 0o644)
	gitIn(t, root, "add", "fix.go")
	gitIn(t, root, "commit", "-q", "-m", "fix")
	gitIn(t, root, "checkout", "-q", "main")
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runRelease(ctx, []string{"pick", "ui/a"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ Picked 1 commit onto release/v0.54")
}

func TestReleasePublishRefusals(t *testing.T) {
	proj, _, _ := makePrivacyFixture(t, "")
	ctx, _, _ := newTestContext(proj, false)
	if err := runRelease(ctx, []string{"publish", "v0.54.0"}); err == nil || !strings.Contains(err.Error(), "publishes by pushing main") {
		t.Errorf("without snapshots = %v", err)
	}
	if err := runRelease(ctx, []string{"setup", "--dev", "/x"}); err == nil || !strings.Contains(err.Error(), "the owner has not chosen private development") {
		t.Errorf("setup without snapshots = %v", err)
	}

	proj, _, _ = makePrivacyFixture(t, snapshotsBlock)
	proj.ViewerURL = releaseViewer(t, `{"milestone":{"version":"0.54","cut":"v0.54.0","rc":true,"soaking":false,"candidate":"v0.54.0-rc.1","checks":[{"key":"soaked","text":"Nothing picked onto release/v0.54 after v0.54.0-rc.1","detail":"1 commit since","ok":false}]}}`)
	ctx, _, _ = newTestContext(proj, false)
	err := runRelease(ctx, []string{"publish", "v0.54.0"})
	if err == nil || !strings.Contains(err.Error(), "does not say a v0.54.0 candidate is soaking") || !strings.Contains(err.Error(), "1 commit since") {
		t.Errorf("not soaking = %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	if err := runRelease(ctx, []string{"setup"}); err == nil || !strings.Contains(err.Error(), "needs --dev <url>") {
		t.Errorf("setup without --dev = %v", err)
	}
}
