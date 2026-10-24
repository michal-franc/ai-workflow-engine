package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/privacy"
	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

// privacyProject is a git project at <base>/<slug> with one issue. tracked
// commits issues/ to the code repo; workflowTail goes after the statuses.
func privacyProject(t *testing.T, base, slug, workflowTail string, tracked bool) tracker.Project {
	t.Helper()
	root := filepath.Join(base, slug)
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write("workflow.yaml", "statuses:\n  - name: idea\n  - name: done\ntransitions:\n  - from: idea\n    to: done\n"+workflowTail)
	write("issues/UI/a.md", "---\ntitle: a\nstatus: idea\nsystem: UI\n---\n\nBody.\n")
	git("init", "-q", "-b", "main")
	git("config", "commit.gpgsign", "false")
	git("add", "workflow.yaml")
	if tracked {
		git("add", "issues")
	}
	git("commit", "-q", "-m", "init")
	return tracker.Project{Name: slug, Slug: slug, IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root}
}

// okBody is getBody that fails the test unless the status is 200.
func okBody(t *testing.T, url string) string {
	t.Helper()
	code, body := getBody(t, url)
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d\n%s", url, code, body)
	}
	return body
}

func TestPrivacyBadgesOnStatusProjectsAndStats(t *testing.T) {
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@test", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@test"} {
		t.Setenv(k, v)
	}
	base := t.TempDir()
	red := privacyProject(t, base, "redproj", "", true)
	green := privacyProject(t, base, "greenproj", "privacy:\n  work_files: tracker\n  decided: \"2026-10-24, owner: \\\"private\\\"\"\n", false)
	remote := filepath.Join(base, "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	tr := privacy.ForProject(&green, green.LoadWorkflow().Privacy, time.Now()).TrackerFor()
	if _, err := tr.Init(remote); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Sync(time.Now()); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, []tracker.Project{red, green})
	defer ts.Close()

	body := okBody(t, ts.URL+"/p/redproj/status")
	assertContains(t, body, `data-testid="status.privacy.badge.work" data-level="red"`)
	assertContains(t, body, `title="Work files are in the code repo: issues/ (1) is tracked by redproj"`)
	assertContains(t, body, `data-testid="status.privacy.line.work" data-level="red"`)
	assertContains(t, body, `href="/p/redproj/updates" data-testid="status.privacy.setup.work">Set up a private tracker on the Updates tab →`)

	body = okBody(t, ts.URL+"/p/greenproj/status")
	assertContains(t, body, `data-testid="status.privacy.badge.work" data-level="green"`)
	assertContains(t, body, `Work files private: remote, last backup just now`)

	md := okBody(t, ts.URL+"/p/greenproj/status.md")
	assertContains(t, md, "Work private: Work files private: remote, last backup just now\n")

	body = okBody(t, ts.URL+"/")
	assertContains(t, body, `data-testid="projects.privacy.work" data-level="red"`)
	assertContains(t, body, `Work: In code repo`)
	assertContains(t, body, `data-testid="projects.privacy.work" data-level="green"`)

	ov := buildOverview([]tracker.Project{red, green}, nil, time.Now())
	if ov.Privacy != "1 in code repo · 1 private" {
		t.Errorf("overview counts = %q", ov.Privacy)
	}
}

func TestInstallPromptWalksTheSetupStepsAndLeavesOwnerStepsToTheOwner(t *testing.T) {
	entries, err := workflowupgrade.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	var p workflowupgrade.Proposal
	for _, e := range entries {
		if e.ID == "privacy" {
			p = workflowupgrade.Proposal{ID: e.ID, Version: e.Version, Question: e.Question, Setup: e.Setup}
		}
	}
	if len(p.Setup) == 0 {
		t.Fatal("the privacy entry has no setup steps")
	}
	got := installPrompt(&tracker.Project{Name: "P", Slug: "p"}, p, "")
	assertContains(t, got, "for an owner step, tell the operator exactly what to do and wait until they say it is done, never do it for them")
	assertContains(t, got, "[owner] For a private tracker: create a private GitHub repo")
	assertContains(t, got, "[agent] issue-cli tracker init --remote <tracker url>")
}
