package privacy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestGitHubRepo(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:michal-franc/gpu-runner.git":      "michal-franc/gpu-runner",
		"https://github.com/michal-franc/gpu-runner":      "michal-franc/gpu-runner",
		"https://github.com/michal-franc/gpu-runner.git/": "michal-franc/gpu-runner",
		"ssh://git@github.com/michal-franc/ctx.git":       "michal-franc/ctx",
		"git@gitlab.com:someone/thing.git":                "",
		"":                                                "",
	} {
		if got := GitHubRepo(url); got != want {
			t.Errorf("GitHubRepo(%q) = %q, want %q", url, got, want)
		}
	}
}

// fakeGitHub answers /repos/o/<name>: "pub" public, "priv" private (as a
// signed-in owner sees it), anything else 404 (what anyone else sees of a
// private repo).
func fakeGitHub(t *testing.T) *int {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/repos/o/pub":
			w.Write([]byte(`{"private": false}`))
		case "/repos/o/priv":
			w.Write([]byte(`{"private": true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = old })
	return &calls
}

func visRepo(t *testing.T, origin string) string {
	t.Helper()
	gitEnv(t)
	root := filepath.Join(t.TempDir(), "proj")
	write(t, filepath.Join(root, "main.go"), "package main\n")
	run(t, root, "git", "init", "-q", "-b", "main")
	if origin != "" {
		run(t, root, "git", "remote", "add", "origin", origin)
	}
	return root
}

func TestRefreshVisibility(t *testing.T) {
	calls := fakeGitHub(t)
	now := time.Now()
	for origin, want := range map[string]string{
		"git@github.com:o/pub.git":    VisibilityPublic,
		"git@github.com:o/priv.git":   VisibilityPrivate,
		"git@github.com:o/hidden.git": VisibilityPrivate, // 404: not publicly visible
	} {
		root := visRepo(t, origin)
		if got, asked, err := RefreshVisibility(context.Background(), root, now, time.Hour); err != nil || !asked || got != want {
			t.Errorf("%s: %q asked=%v err=%v, want %q", origin, got, asked, err, want)
		}
		if got := Visibility(root); got != want {
			t.Errorf("%s: cached %q, want %q", origin, got, want)
		}
	}
	// A fresh answer is not asked again; an old one is.
	root := visRepo(t, "git@github.com:o/pub.git")
	RefreshVisibility(context.Background(), root, now, time.Hour)
	before := *calls
	if _, asked, _ := RefreshVisibility(context.Background(), root, now.Add(30*time.Minute), time.Hour); asked || *calls != before {
		t.Error("asked GitHub again within maxAge")
	}
	if _, asked, _ := RefreshVisibility(context.Background(), root, now.Add(2*time.Hour), time.Hour); !asked {
		t.Error("an hour-old answer should be asked again")
	}
	// The remote changed: the cached answer no longer counts.
	run(t, root, "git", "remote", "set-url", "origin", "git@github.com:o/priv.git")
	if got := Visibility(root); got != "" {
		t.Errorf("cache for another URL = %q, want unknown", got)
	}
	// Not GitHub: unknown, nobody asked.
	other := visRepo(t, "git@gitlab.com:o/pub.git")
	if got, asked, err := RefreshVisibility(context.Background(), other, now, time.Hour); got != "" || asked || err != nil {
		t.Errorf("gitlab: %q asked=%v err=%v", got, asked, err)
	}
}

// TestBadgesForAPrivateRepo: work files tracked in a private repo, and no
// privacy set up, read as info with the going-public note, not red and grey.
func TestBadgesForAPrivateRepo(t *testing.T) {
	fakeGitHub(t)
	root := visRepo(t, "git@github.com:o/hidden.git")
	write(t, filepath.Join(root, "issues", "a.md"), "x\n")
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-q", "-m", "init")
	in := Input{Name: "proj", Root: root, Slug: "proj", Now: time.Now()}
	if b := WorkBadge(in); b.Level != Red {
		t.Fatalf("visibility unknown: work = %+v, want red", b)
	}
	if b := CodeBadge(in); b.Label != "Not set up" {
		t.Fatalf("visibility unknown: code = %+v", b)
	}
	if _, _, err := RefreshVisibility(context.Background(), root, time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	in.Visibility = Visibility(root)
	w := WorkBadge(in)
	if w.Level != Green || w.Label != "Private repo" || !strings.Contains(w.Sentence, "which is private on GitHub") || !strings.Contains(w.Note, "issue-cli tracker init") {
		t.Errorf("private work = %+v", w)
	}
	c := CodeBadge(in)
	if c.Level != Green || c.Label != "Private repo" || !strings.Contains(c.Note, "issue-cli release setup --dev") {
		t.Errorf("private code = %+v", c)
	}
	// A project that chose the tracker but still tracks work files is red even in a private repo.
	in.Cfg = &tracker.PrivacyConfig{WorkFiles: "tracker", Decided: "x"}
	if b := WorkBadge(in); b.Level != Red {
		t.Errorf("tracker chosen, files tracked = %+v, want red", b)
	}
}
