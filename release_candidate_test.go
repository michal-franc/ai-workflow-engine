package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

const rcBlock = "workbench:\n  release_candidates: true\n"

// stubCandidates makes the repo hold tags (each made at its time) with
// since commits after any of them.
func stubCandidates(t *testing.T, tags map[string]time.Time, since int) {
	t.Helper()
	origTags, origSince := gitRCTags, gitCommitsSince
	gitRCTags = func(_, cut string) []rcTag {
		var out []rcTag
		for name, at := range tags {
			if strings.HasPrefix(name, cut+"-rc.") {
				out = append(out, rcTag{Name: name, At: at})
			}
		}
		return out
	}
	gitCommitsSince = func(_, _, _, changelog string) (int, bool) {
		if changelog != "CHANGELOG.md" {
			t.Errorf("commits since should leave out the changelog, got %q", changelog)
		}
		return since, true
	}
	t.Cleanup(func() { gitRCTags, gitCommitsSince = origTags, origSince })
}

// readyRCProject is a project whose 0.53 milestone is ready (done, CI green
// on the head, entries under ## Unreleased), with release candidates on.
func readyRCProject(t *testing.T, block string) tracker.Project {
	t.Helper()
	head := "7c41e0a0000"
	proj := releaseProject(t, releaseFixture{
		changelog: releaseChangelog, ciBlock: ciBlockOn + block, versions: true,
		ciState:  &ci.State{Branch: "main", State: ci.Green, GreenSha: head, GreenAt: time.Now()},
		statuses: map[string]string{"shipped": "done"},
	})
	stubBranchState(t, &BranchState{Branch: "main", Upstream: "origin/main", Ahead: 3, Head: head, HeadAt: time.Now()})
	return proj
}

func TestReleaseCandidateCutFirst(t *testing.T) {
	proj := readyRCProject(t, rcBlock)
	stubCandidates(t, nil, 0)
	page, md := getStatusBoth(t, proj)

	wantAll(t, "page", page,
		`data-testid="status.release_wait.state">ready to cut v0.53.0-rc.1</span>`,
		`v0.53.0 is published when`,
		`data-testid="status.release_wait.cut_rc"`,
		`Ready to cut release candidate v0.53.0-rc.1`,
		`Cut it: <code>issue-cli release cut v0.53.0</code>. It creates <code>release/v0.53</code> from main and its worktree, and tags v0.53.0-rc.1 there; the candidate tag is never pushed.`,
	)
	// Never the direct publish steps while no candidate has soaked.
	wantNone(t, "page", page, `status.release_wait.ready"`, `status.release_wait.publish`, `status.release_wait.candidate"`, `rename <code>## Unreleased</code>`, `rc_badge`)
	wantAll(t, "status.md", md,
		"- Milestone 0.53: 1/1 done; ready to cut release candidate v0.53.0-rc.1: issue-cli release cut v0.53.0 (tags it on release/v0.53, never pushed), install that build from the release worktree and restart what runs it, then tell the leads of the projects that run it\n",
	)
	wantNone(t, "status.md", md, "rename ## Unreleased", "- Candidate")
}

func TestReleaseCandidateSoakingAsksTheOwner(t *testing.T) {
	proj := readyRCProject(t, rcBlock)
	stubCandidates(t, map[string]time.Time{
		"v0.53.0-rc.1": time.Now().Add(-72 * time.Hour),
		"v0.53.0-rc.2": time.Now().Add(-49 * time.Hour),
		"v0.54.0-rc.1": time.Now(), // another milestone's
	}, 0)
	page, md := getStatusBoth(t, proj)

	today := time.Now().Format("2006-01-02")
	wantAll(t, "page", page,
		`data-testid="status.release_wait.state">v0.53.0-rc.2 soaking</span>`,
		// Silver on the card, under its title.
		`data-testid="status.rc_badge" title="A release candidate runs on this machine's projects; it is released only on the owner's ok">v0.53.0-rc.2 · release candidate · soaking 2d</a>`,
		`data-testid="status.release_wait.check.soaked" data-ok="true"`,
		`Nothing merged after v0.53.0-rc.2 but the changelog`,
		`data-testid="status.release_wait.candidate"`,
		// Folded behind a one-line summary; the rows open on click.
		`<details class="status-more status-fold" data-fold="release_wait.candidate" data-testid="status.release_wait.candidate.fold">`,
		`<summary data-testid="status.release_wait.candidate.summary"><span class="mono">v0.53.0-rc.2</span> · cut 2d ago · nothing merged since</summary>`,
		`<span class="status-strong mono">v0.53.0-rc.2</span><span class="mono status-meta">nothing merged since</span>`,
		`cut 2d ago`,
		`data-testid="status.release_wait.publish"`,
		`v0.53.0-rc.2 soaking: publish v0.53.0 only on the owner's ok`,
		`Never publish without their ok.`,
		`rename <code>## Unreleased</code> to <code>## v0.53.0 — `+today+`</code>`,
	)
	// Not the engine project: no soak rows, no reports check.
	wantNone(t, "page", page, `status.release_wait.ready"`, `status.release_wait.cut_rc`, `candidate.running`, `check.reports`)
	wantAll(t, "status.md", md,
		"- Milestone 0.53: 1/1 done; v0.53.0-rc.2 soaking since 2d; publish v0.53.0 only on the owner's ok on the soak: rename ## Unreleased in CHANGELOG.md to ## v0.53.0 — "+today+", then push\n",
		"- Candidate v0.53.0-rc.2: cut 2d ago; nothing merged since but the changelog\n",
	)
}

func TestReleaseCandidateMergesAfterItNeedTheNext(t *testing.T) {
	proj := readyRCProject(t, rcBlock)
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.2": time.Now().Add(-5 * time.Hour)}, 3)
	page, md := getStatusBoth(t, proj)

	wantAll(t, "page", page,
		`data-testid="status.release_wait.state">ready to cut v0.53.0-rc.3</span>`,
		`data-testid="status.release_wait.check.soaked" data-ok="false"`,
		`3 commits since v0.53.0-rc.2: cut v0.53.0-rc.3`,
		`3 commits since: they soak in v0.53.0-rc.3`,
		`Ready to cut release candidate v0.53.0-rc.3`,
	)
	wantNone(t, "page", page, `status.release_wait.publish`)
	wantAll(t, "status.md", md,
		"ready to cut release candidate v0.53.0-rc.3: issue-cli release cut v0.53.0",
		"- Candidate v0.53.0-rc.2: cut 5h ago; 3 commits merged since: they soak in v0.53.0-rc.3\n",
	)
}

func TestReleaseCandidateNotReadyStaysNotReady(t *testing.T) {
	proj := releaseProject(t, releaseFixture{
		changelog: releaseChangelog, ciBlock: ciBlockOn + rcBlock, versions: true,
		ciState:  &ci.State{Branch: "main", State: ci.Green, GreenSha: "7c41e0a0000", GreenAt: time.Now()},
		statuses: map[string]string{"shipped": "done", "busy": "in progress"},
	})
	stubBranchState(t, &BranchState{Branch: "main", Head: "7c41e0a0000"})
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": time.Now().Add(-time.Hour)}, 2)
	page, md := getStatusBoth(t, proj)
	wantAll(t, "page", page, `data-testid="status.release_wait.state">v0.53.0 not ready</span>`)
	wantNone(t, "page", page, `status.release_wait.cut_rc`, `status.release_wait.publish`)
	wantAll(t, "status.md", md, "v0.53.0 not ready: 1 issue open; 2 commits since v0.53.0-rc.1: cut v0.53.0-rc.2\n")
}

func TestReleaseCandidateTagsIgnoredWithoutTheKey(t *testing.T) {
	proj := readyRCProject(t, "")
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": time.Now()}, 0)
	page, md := getStatusBoth(t, proj)
	wantAll(t, "page", page, `data-testid="status.release_wait.ready"`, `ready to cut</span>`)
	wantNone(t, "page", page, `candidate`, `check.soaked`, `cut_rc`, `rc_badge`)
	wantAll(t, "status.md", md, "ready to cut v0.53.0: rename ## Unreleased in CHANGELOG.md")
	wantNone(t, "status.md", md, "Candidate", "release candidate")
}

func TestReleaseCandidateSoakOnTheEngine(t *testing.T) {
	cut := time.Now().Add(-26 * time.Hour)
	proj := readyRCProject(t, rcBlock)
	proj.Repo = "michal-franc/ai-workflow-engine"
	root := proj.WorkDir
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	event := func(at time.Time, ver string) string {
		return `{"v":1,"ts":"` + at.UTC().Format(time.RFC3339Nano) + `","cmd":"show","exit":0,"caller":"human","pid":1,"ppid":1,"args_hash":"x","ver":"` + ver + `"}` + "\n"
	}
	// The engine's own log: calls on rc.1, the newest from a build past it.
	write(".agent-logs/telemetry.jsonl", event(cut.Add(time.Hour), "v0.53.0-rc.1")+event(cut.Add(2*time.Hour), "v0.53.0-rc.1")+event(time.Now().Add(-time.Hour), "v0.53.0-rc.1-1-gabc"))
	// An open report from rc.1, an older one fixed after the cut, one from a
	// release build, and an open bug that is not a report.
	write("issues/CLI/rc-crash.md", "---\ntitle: \"Rc crash\"\nstatus: \"idea\"\nversion: \"0.53\"\nlabels:\n  - cli-bug\n---\n\n## Report\n\nBoom.\n\n- reported from: other\n- issue-cli: v0.53.0-rc.1 linux\n")
	write("issues/CLI/fixed-one.md", "---\ntitle: \"Fixed one\"\nstatus: \"done\"\nversion: \"0.53\"\ndone_at: \""+time.Now().Add(-time.Hour).Format(time.RFC3339)+"\"\nlabels:\n  - cli-bug\n---\n\n- issue-cli: v0.53.0-rc.1-2-gdef linux\n")
	write("issues/CLI/old-bug.md", "---\ntitle: \"Old bug\"\nstatus: \"idea\"\nlabels:\n  - cli-bug\n---\n\n- issue-cli: v0.52.0 linux\n")

	other := t.TempDir()
	os.MkdirAll(filepath.Join(other, "issues"), 0o755)
	os.MkdirAll(filepath.Join(other, ".agent-logs"), 0o755)
	os.WriteFile(filepath.Join(other, ".agent-logs", "telemetry.jsonl"), []byte(event(cut.Add(-time.Hour), "v0.52.0")), 0o644)
	quiet := t.TempDir()
	os.MkdirAll(filepath.Join(quiet, "issues"), 0o755)
	projects := []tracker.Project{
		proj,
		{Name: "Rogue", Slug: "rogue", IssueDir: filepath.Join(other, "issues"), WorkDir: other},
		{Name: "Quiet", Slug: "quiet", IssueDir: filepath.Join(quiet, "issues"), WorkDir: quiet},
	}
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": cut}, 0)

	withMockTmuxSessions(t, nil)
	stubAgentsGit(t)
	ts := newTestServer(t, projects)
	defer ts.Close()
	_, page := getResp(t, ts.URL+"/p/rel/status")
	_, md := getResp(t, ts.URL+"/p/rel/status.md")

	wantAll(t, "page", page,
		// The open report is in 0.53, so the issues check fails too.
		`data-testid="status.release_wait.state">v0.53.0 not ready</span>`,
		`data-testid="status.release_wait.check.reports" data-ok="false"`,
		`1 report open: cli/rc-crash`,
		`data-testid="status.release_wait.candidate.running" data-stale="true"`,
		`Running: viewer <code>dev</code>, issue-cli <code>v0.53.0-rc.1-1-gabc</code>`,
		`not the candidate: install v0.53.0-rc.1 and restart the viewer`,
		`<summary data-testid="status.release_wait.candidate.summary"><span class="mono">v0.53.0-rc.1</span> · cut 1d ago · nothing merged since · ran by 1 of 3 · 1 open report · <span class="status-check-detail">not the candidate</span></summary>`,
		`Ran v0.53.0-rc.1 · 1 of 3`,
		`data-testid="status.release_wait.candidate.project.rel"`,
		`2 calls, last 1d ago`,
		`Not yet: Rogue`,
		`No usage log: Quiet`,
		`data-testid="status.release_wait.candidate.report.cli/rc-crash"`,
		`rc.1 · idea`,
		`Fixed since v0.53.0-rc.1: soak in v0.53.0-rc.2`,
	)
	wantNone(t, "page", page, `candidate.report.cli/old-bug`, `candidate.report.cli/fixed-one`)
	wantAll(t, "status.md", md,
		"v0.53.0 not ready: 1 issue open; 1 report open: cli/rc-crash\n",
		"- Running: viewer dev, issue-cli v0.53.0-rc.1-1-gabc (not the candidate: install v0.53.0-rc.1 and restart the viewer)\n",
		"- Ran v0.53.0-rc.1: rel (2 calls, last 1d ago); not yet: rogue; no usage log: quiet\n",
		"- Open reports from candidates (1):\n  - cli/rc-crash (found in rc.1, idea)\n",
		"- Fixed since v0.53.0-rc.1, soak in v0.53.0-rc.2: cli/fixed-one\n",
	)
}

func TestCandidateGitCalls(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		return runGitT(t, root, append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "tag.gpgSign=false", "-c", "commit.gpgSign=false"}, args...)...)
	}
	commit := func(file, msg string) {
		os.WriteFile(filepath.Join(root, file), []byte(msg), 0o644)
		git("add", file)
		git("commit", "-q", "-m", msg)
	}
	git("init", "-q")
	commit("a.go", "a")
	git("tag", "-a", "v0.53.0-rc.1", "-m", "rc1")
	git("tag", "-a", "v0.54.0-rc.1", "-m", "other")
	commit("CHANGELOG.md", "heading only")
	if n, ok := gitCommitsSince(root, "v0.53.0-rc.1", "", "CHANGELOG.md"); !ok || n != 0 {
		t.Fatalf("a changelog-only commit counted: %d, %v", n, ok)
	}
	commit("a.go", "fix")
	if n, ok := gitCommitsSince(root, "v0.53.0-rc.1", "", "CHANGELOG.md"); !ok || n != 1 {
		t.Fatalf("commits since = %d, %v; want 1", n, ok)
	}
	tags := gitRCTags(root, "v0.53.0")
	if len(tags) != 1 || tags[0].Name != "v0.53.0-rc.1" || tags[0].At.IsZero() {
		t.Fatalf("tags = %+v", tags)
	}
	c := findCandidate(root, "v0.53.0", "", "CHANGELOG.md")
	if c == nil || c.Tag != "v0.53.0-rc.1" || c.Since != 1 || c.Next != "v0.53.0-rc.2" {
		t.Fatalf("candidate = %+v", c)
	}
}

// stubReleaseBranch makes release/<cut> exist, with unpicked slugs.
func stubReleaseBranch(t *testing.T, unpicked map[string]int) {
	t.Helper()
	origHas, origUn := gitHasBranch, gitUnpicked
	gitHasBranch = func(_, branch string) bool { return branch == "release/v0.53" }
	gitUnpicked = func(_, branch string, slugs []string) map[string]int {
		if branch != "release/v0.53" {
			t.Errorf("unpicked asked for %s", branch)
		}
		return unpicked
	}
	t.Cleanup(func() { gitHasBranch, gitUnpicked = origHas, origUn })
}

func TestReleaseCandidateOnTheBranchWantsFixesPicked(t *testing.T) {
	proj := readyRCProject(t, rcBlock+"privacy:\n  code: snapshots\n  decided: x\n")
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": time.Now().Add(-5 * time.Hour)}, 0)
	stubReleaseBranch(t, map[string]int{"ui/shipped": 2})
	page, md := getStatusBoth(t, proj)
	wantAll(t, "page", page,
		`data-testid="status.release_wait.check.picked" data-ok="false"`,
		`Every done 0.53 fix is on release/v0.53`,
		`issue-cli release pick ui/shipped`,
		`Nothing picked onto release/v0.53 after v0.53.0-rc.1 but the changelog`,
		`data-testid="status.release_wait.state">v0.53.0 not ready</span>`,
	)
	wantNone(t, "page", page, `status.release_wait.publish`, `status.release_wait.cut_rc`)
	wantAll(t, "status.md", md, "issue-cli release pick ui/shipped")
}

func TestReleaseCandidateOnTheBranchPublishesFromTheWorktree(t *testing.T) {
	proj := readyRCProject(t, rcBlock+"privacy:\n  code: snapshots\n  decided: x\n")
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": time.Now().Add(-50 * time.Hour)}, 0)
	stubReleaseBranch(t, nil)
	page, md := getStatusBoth(t, proj)
	wantAll(t, "page", page,
		`data-testid="status.release_wait.check.picked" data-ok="true"`,
		`data-testid="status.release_wait.publish"`,
		`issue-cli release publish v0.53.0</code>`,
		`release-v0.53 &amp;&amp; issue-cli release publish v0.53.0`,
	)
	wantNone(t, "page", page, `rename <code>## Unreleased</code>`)
	wantAll(t, "status.md", md, "publish v0.53.0 only on the owner's ok on the soak: cd ", "/.worktrees/release-v0.53 && issue-cli release publish v0.53.0\n")
}

func TestReleaseCandidateBranchSinceCountsTheBranch(t *testing.T) {
	proj := readyRCProject(t, rcBlock)
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": time.Now().Add(-5 * time.Hour)}, 1)
	var heads []string
	orig := gitCommitsSince
	gitCommitsSince = func(_, _, head, _ string) (int, bool) { heads = append(heads, head); return 1, true }
	t.Cleanup(func() { gitCommitsSince = orig })
	stubReleaseBranch(t, nil)
	_, md := getStatusBoth(t, proj)
	if len(heads) == 0 || heads[0] != "release/v0.53" {
		t.Errorf("commits since counted on %v, want release/v0.53", heads)
	}
	wantAll(t, "status.md", md, "ready to cut release candidate v0.53.0-rc.2")
}

func TestReleaseJSONIsTheCardsMilestone(t *testing.T) {
	proj := readyRCProject(t, rcBlock+"privacy:\n  code: snapshots\n  decided: x\n")
	stubCandidates(t, map[string]time.Time{"v0.53.0-rc.1": time.Now().Add(-50 * time.Hour)}, 0)
	stubReleaseBranch(t, nil)
	withMockTmuxSessions(t, nil)
	stubAgentsGit(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, body := getResp(t, ts.URL+"/p/"+proj.Slug+"/release.json")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	wantAll(t, "release.json", body, `"cut":"v0.53.0"`, `"soaking":true`, `"candidate":"v0.53.0-rc.1"`, `"branch":"release/v0.53"`, `"key":"picked"`, `"ok":true`)
}
