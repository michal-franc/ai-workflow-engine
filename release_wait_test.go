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

const releaseChangelog = `# Changelog

## Unreleased

- **Local CI.** Runs on every merge,
  wrapped onto a second line.
  - nested detail, left out
- Canvas lanes fold.

## v0.52.0 — 2026-10-22

- One lead.
`

// releaseProject is a project with issues on versions 0.52 (done, released
// in the changelog), 0.53 (the given statuses) and 0.54 (idea). changelog ""
// writes none; ciBlock "" leaves CI off.
type releaseFixture struct {
	changelog string
	ciBlock   string
	ciState   *ci.State
	statuses  map[string]string // 0.53 issue slug → status
	versions  bool              // false: no version: on any issue
}

func releaseProject(t *testing.T, f releaseFixture) tracker.Project {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(slug, title, status, version, extra string) {
		v := ""
		if f.versions {
			v = "version: \"" + version + "\"\n"
		}
		write("issues/UI/"+slug+".md", "---\ntitle: \""+title+"\"\nstatus: \""+status+"\"\n"+v+extra+"---\n\nBody.\n")
	}
	issue("old", "Old", "done", "0.52", "")
	for slug, status := range f.statuses {
		extra := ""
		if slug == "busy" {
			extra = "assignee: \"agent-busy\"\n"
		}
		issue(slug, strings.ToUpper(slug[:1])+slug[1:], status, "0.53", extra)
	}
	issue("later", "Later", "idea", "0.54", "")
	if f.changelog != "" {
		write("CHANGELOG.md", f.changelog)
	}
	if f.ciState != nil {
		if err := ci.WriteState(root, f.ciState); err != nil {
			t.Fatal(err)
		}
	}
	write("workflow.yaml", f.ciBlock+`statuses:
  - name: "idea"
  - name: "in design"
  - name: "in progress"
  - name: "done"
  - name: "parked"
    optional: true
`)
	return tracker.Project{Name: "Rel", Slug: "rel", IssueDir: filepath.Join(root, "issues"), WorkDir: root, WorkflowFile: filepath.Join(root, "workflow.yaml")}
}

// stubBranchState makes gitBranchState return st (ok false when st is nil).
func stubBranchState(t *testing.T, st *BranchState) {
	t.Helper()
	orig := gitBranchState
	gitBranchState = func(string, string) (BranchState, bool) {
		if st == nil {
			return BranchState{}, false
		}
		return *st, true
	}
	t.Cleanup(func() { gitBranchState = orig })
}

func getStatusBoth(t *testing.T, proj tracker.Project) (page, md string) {
	t.Helper()
	withMockTmuxSessions(t, nil)
	stubAgentsGit(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()
	resp, page := getResp(t, ts.URL+"/p/"+proj.Slug+"/status")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, page)
	}
	_, md = getResp(t, ts.URL+"/p/"+proj.Slug+"/status.md")
	return page, md
}

func wantAll(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s missing %q:\n%s", what, w, body)
		}
	}
}

func wantNone(t *testing.T, what, body string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(body, w) {
			t.Errorf("%s should not contain %q", what, w)
		}
	}
}

func TestParseUnreleased(t *testing.T) {
	entries, found := parseUnreleased(releaseChangelog)
	if !found {
		t.Fatal("## Unreleased not found")
	}
	want := []string{"**Local CI.** Runs on every merge, wrapped onto a second line.", "Canvas lanes fold."}
	if strings.Join(entries, "|") != strings.Join(want, "|") {
		t.Errorf("entries = %q, want %q", entries, want)
	}

	if e, found := parseUnreleased("## [unreleased]\n\n### Added\n* One\n\n## v1.0.0\n- Old\n"); !found || len(e) != 1 || e[0] != "One" {
		t.Errorf("keep-a-changelog style: %q, %v", e, found)
	}
	if e, found := parseUnreleased("## Unreleased\n\n## v1.0.0\n- Old\n"); !found || len(e) != 0 {
		t.Errorf("empty Unreleased: %q, %v", e, found)
	}
	if e, found := parseUnreleased("## v1.0.0\n- Old\n"); found || len(e) != 0 {
		t.Errorf("no heading: %q, %v", e, found)
	}
	if e, _ := parseUnreleased("## Unreleased\n- " + strings.Repeat("x", 200) + "\n"); len([]rune(e[0])) != 140 {
		t.Errorf("entry not clipped to 140: %d", len([]rune(e[0])))
	}
	// Recently shipped still skips the heading.
	if rels := parseChangelog(releaseChangelog, 3); len(rels) != 1 || rels[0].Version != "v0.52.0" {
		t.Errorf("parseChangelog = %+v", rels)
	}
}

func TestNormVersionAndCutName(t *testing.T) {
	for in, want := range map[string]string{"0.53": "0.53.0", "v0.52.0": "0.52.0", "1": "1.0.0", "0.53.1": "0.53.1", "later": "", "": ""} {
		if got := normVersion(in); got != want {
			t.Errorf("normVersion(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cutName("0.53"); got != "v0.53.0" {
		t.Errorf("cutName = %q", got)
	}
	if compareVersions(normVersion("0.52"), normVersion("v0.52.0")) != 0 {
		t.Error("0.52 and v0.52.0 should compare equal")
	}
}

func TestParseBranchState(t *testing.T) {
	out := " \x00work/x\x00\x00\x00aaa\x001700000000\n" +
		"*\x00main\x00origin/main\x00[ahead 14, behind 2]\x00bbb\x001792695065\n" +
		" \x00old\x00origin/old\x00[gone]\x00ccc\x001700000000\n"
	st, ok := parseBranchState(out, "")
	if !ok || st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 14 || st.Head != "bbb" || st.HeadAt.Unix() != 1792695065 {
		t.Errorf("checked-out row = %+v, %v", st, ok)
	}
	if st, ok := parseBranchState(out, "old"); !ok || st.Upstream != "" {
		t.Errorf("gone upstream should read as none: %+v", st)
	}
	if st, ok := parseBranchState(out, "work/x"); !ok || st.Upstream != "" || st.Ahead != 0 {
		t.Errorf("no upstream: %+v", st)
	}
	if _, ok := parseBranchState(out, "missing"); ok {
		t.Error("missing branch found")
	}
}

func TestPickMilestone(t *testing.T) {
	cs := newCanvasStatuses(&tracker.WorkflowConfig{Statuses: []tracker.WorkflowStatus{{Name: "idea"}, {Name: "done"}, {Name: "parked", Optional: true}}})
	is := func(status string) *tracker.Issue { return &tracker.Issue{Status: status} }
	byVersion := map[string][]*tracker.Issue{
		"0.51":  {is("idea")}, // leftover below the release: ignored
		"0.52":  {is("done")},
		"0.53":  {is("parked")}, // parked only: skipped
		"0.54":  {is("done"), is("idea"), is("parked")},
		"later": {is("idea")},
	}
	if v, p, _ := pickMilestone(byVersion, cs, "v0.52.0"); v != "0.54" || p.Done != 1 || p.Total != 2 {
		t.Errorf("after v0.52.0: %q %+v", v, p)
	}
	if v, _, _ := pickMilestone(byVersion, cs, "v0.51.0"); v != "0.52" {
		t.Errorf("after v0.51.0: %q (0.52 is done but not cut, so it is the milestone)", v)
	}
	if v, _, _ := pickMilestone(byVersion, cs, "v0.54.0"); v != "" {
		t.Errorf("nothing after v0.54.0: %q", v)
	}
	if v, _, _ := pickMilestone(byVersion, cs, ""); v != "0.51" {
		t.Errorf("no release: lowest with open issues, got %q", v)
	}
	allDone := map[string][]*tracker.Issue{"0.1": {is("done")}, "0.2": {is("done")}}
	if v, _, _ := pickMilestone(allDone, cs, ""); v != "0.2" {
		t.Errorf("no release, all done: highest, got %q", v)
	}
}

func TestCICheck(t *testing.T) {
	head := "7c41e0a0000"
	for name, tc := range map[string]struct {
		st     *ci.State
		ok     bool
		detail string
	}{
		"green on head":   {&ci.State{Branch: "main", State: ci.Green, GreenSha: head}, true, ""},
		"head untested":   {&ci.State{Branch: "main", State: ci.Green, GreenSha: "d89c1b3000"}, false, "head 7c41e0a not tested yet (green at d89c1b3)"},
		"red":             {&ci.State{Branch: "main", State: ci.Red, RedSince: "25526c7000", Failed: "make validate"}, false, "main red since 25526c7: make validate"},
		"error":           {&ci.State{Branch: "main", State: ci.Error, Error: "no worktree\nmore"}, false, "CI can't run: no worktree"},
		"no run":          {nil, false, "no CI run yet"},
		"head unreadable": {&ci.State{State: ci.Green, GreenSha: "abc"}, true, ""},
	} {
		h := head
		if name == "head unreadable" {
			h = ""
		}
		c := ciCheck(tc.st, "main", h)
		if c.OK != tc.ok || c.Detail != tc.detail {
			t.Errorf("%s: ok=%v detail=%q, want ok=%v detail=%q", name, c.OK, c.Detail, tc.ok, tc.detail)
		}
	}
}

func TestReleaseWaitNotReady(t *testing.T) {
	head := "7c41e0a0000"
	proj := releaseProject(t, releaseFixture{
		changelog: releaseChangelog, ciBlock: ciBlockOn, versions: true,
		ciState:  &ci.State{Branch: "main", State: ci.Green, GreenSha: "d89c1b3000", GreenAt: time.Now()},
		statuses: map[string]string{"shipped": "done", "busy": "in progress", "fresh": "idea", "shelved": "parked"},
	})
	stubBranchState(t, &BranchState{Branch: "main", Upstream: "origin/main", Ahead: 14, Head: head, HeadAt: time.Now().Add(-17 * time.Minute)})
	page, md := getStatusBoth(t, proj)

	wantAll(t, "page", page,
		`data-testid="status.release_wait"`,
		`data-testid="status.release_wait.state">v0.53.0 not ready</span>`,
		`Milestone 0.53</span><span class="mono status-meta">1/3 done</span>`,
		`data-testid="status.release_wait.check.issues" data-ok="false"`,
		`<span class="mono status-check-meta">2 open</span>`,
		`data-testid="status.release_wait.check.ci" data-ok="false"`,
		`head 7c41e0a not tested yet (green at d89c1b3)`,
		`<a href="#status-ci-h">Local CI</a>`,
		`data-testid="status.release_wait.check.changelog" data-ok="true"`,
		`data-testid="status.release_wait.open.row.ui/busy"`,
		`agent-busy`,
		`data-testid="status.release_wait.open.row.ui/fresh"`,
		`no assignee`,
		`Unreleased · 2`,
		`<strong>Local CI.</strong> Runs on every merge, wrapped onto a second line.`,
		`14 commits on main not on origin/main`,
		`newest 17m ago`,
	)
	wantNone(t, "page", page, `status.release_wait.ready`, `open.row.ui/shelved`, `open.row.ui/shipped`, `nested detail`)
	// Furthest along first.
	if strings.Index(page, "open.row.ui/busy") > strings.Index(page, "open.row.ui/fresh") {
		t.Error("open issues should list in progress before idea")
	}

	wantAll(t, "status.md", md,
		"## Waiting for release\n- Milestone 0.53: 1/3 done; v0.53.0 not ready: 2 issues open; head 7c41e0a not tested yet (green at d89c1b3)\n",
		"  - ui/busy (in progress, agent-busy)\n  - ui/fresh (idea, no assignee)\n",
		"- Unreleased in CHANGELOG.md (2):\n  - **Local CI.** Runs on every merge, wrapped onto a second line.\n  - Canvas lanes fold.\n",
		"- Not pushed: 14 commits on main not on origin/main, newest 17m ago\n",
	)
	// The milestone and its release wait come before history.
	if strings.Index(md, "## Waiting for release") > strings.Index(md, "## Recently shipped") {
		t.Error("Waiting for release should come before Recently shipped in status.md")
	}
}

func TestReleaseWaitReady(t *testing.T) {
	head := "7c41e0a0000"
	proj := releaseProject(t, releaseFixture{
		changelog: releaseChangelog, ciBlock: ciBlockOn, versions: true,
		ciState:  &ci.State{Branch: "main", State: ci.Green, GreenSha: head, GreenAt: time.Now()},
		statuses: map[string]string{"shipped": "done", "busy": "done", "shelved": "parked"},
	})
	stubBranchState(t, &BranchState{Branch: "main", Upstream: "origin/main", Ahead: 0, Head: head, HeadAt: time.Now()})
	page, md := getStatusBoth(t, proj)

	today := time.Now().Format("2006-01-02")
	wantAll(t, "page", page,
		`status-ready-card`,
		`data-testid="status.release_wait.state">ready to cut</span>`,
		`data-testid="status.release_wait.ready"`,
		`Ready to cut v0.53.0</span>`,
		`rename <code>## Unreleased</code> to <code>## v0.53.0 — `+today+`</code>`,
		`push main to origin/main`,
		`Nothing: main matches origin/main`,
	)
	wantNone(t, "page", page, `data-ok="false"`, `status.release_wait.open"`)
	wantAll(t, "status.md", md,
		"- Milestone 0.53: 2/2 done; ready to cut v0.53.0: rename ## Unreleased in CHANGELOG.md to ## v0.53.0 — "+today+", then push (with the owner's go-ahead)\n",
		"- Not pushed: nothing, main matches origin/main\n",
	)
}

func TestReleaseWaitBlockedByRedCIAndEmptyChangelog(t *testing.T) {
	proj := releaseProject(t, releaseFixture{
		changelog: "## Unreleased\n\n## v0.52.0 — 2026-10-22\n\n- One lead.\n", ciBlock: ciBlockOn, versions: true,
		ciState:  &ci.State{Branch: "main", State: ci.Red, RedSince: "25526c7000", Failed: "make validate", Updated: time.Now()},
		statuses: map[string]string{"shipped": "done"},
	})
	stubBranchState(t, nil)
	page, md := getStatusBoth(t, proj)

	wantAll(t, "page", page,
		`v0.53.0 not ready`,
		`data-testid="status.release_wait.check.issues" data-ok="true"`,
		`main red since 25526c7: make validate`,
		`data-testid="status.release_wait.check.changelog" data-ok="false"`,
		`Unreleased · 0`,
		`Nothing under <code>## Unreleased</code>`,
	)
	wantNone(t, "page", page, `status.release_wait.ready`, `status.release_wait.unpushed`)
	wantAll(t, "status.md", md,
		"- Milestone 0.53: 1/1 done; v0.53.0 not ready: main red since 25526c7: make validate; nothing under ## Unreleased\n",
		"- Unreleased in CHANGELOG.md: nothing under ## Unreleased\n",
	)
	wantNone(t, "status.md", md, "Not pushed")
}

func TestReleaseWaitHidesMissingRows(t *testing.T) {
	t.Run("no versions, no upstream: only Unreleased", func(t *testing.T) {
		proj := releaseProject(t, releaseFixture{changelog: releaseChangelog, statuses: map[string]string{"busy": "in progress"}})
		stubBranchState(t, &BranchState{Branch: "main", Head: "abc"})
		page, md := getStatusBoth(t, proj)
		wantAll(t, "page", page, `data-testid="status.release_wait"`, `data-testid="status.release_wait.unreleased"`)
		wantNone(t, "page", page, `status.release_wait.milestone`, `status.release_wait.state`, `status.release_wait.unpushed`)
		wantAll(t, "status.md", md, "## Waiting for release\n- Unreleased in CHANGELOG.md (2):")
		wantNone(t, "status.md", md, "- Milestone", "Not pushed")
	})
	t.Run("no changelog, no ci: issues check only, no changelog row", func(t *testing.T) {
		proj := releaseProject(t, releaseFixture{versions: true, statuses: map[string]string{"busy": "in progress"}})
		stubBranchState(t, &BranchState{Branch: "main", Upstream: "origin/main", Ahead: 1, Head: "abc", HeadAt: time.Now()})
		page, md := getStatusBoth(t, proj)
		// No release anywhere: the lowest version with open work.
		wantAll(t, "page", page, `Milestone 0.53`, `status.release_wait.check.issues`, `1 commit on main not on origin/main`)
		wantNone(t, "page", page, `status.release_wait.check.ci`, `status.release_wait.check.changelog`, `status.release_wait.unreleased`)
		wantNone(t, "status.md", md, "Unreleased in")
	})
	t.Run("changelog without ## Unreleased and no versions: no card", func(t *testing.T) {
		proj := releaseProject(t, releaseFixture{changelog: "## v0.52.0 — 2026-10-22\n\n- One lead.\n", statuses: map[string]string{"busy": "in progress"}})
		stubBranchState(t, nil)
		page, md := getStatusBoth(t, proj)
		wantNone(t, "page", page, `status.release_wait`)
		wantNone(t, "status.md", md, "## Waiting for release")
	})
	t.Run("nothing at all: no card", func(t *testing.T) {
		proj := releaseProject(t, releaseFixture{statuses: map[string]string{"busy": "in progress"}})
		stubBranchState(t, nil)
		page, md := getStatusBoth(t, proj)
		wantNone(t, "page", page, `status.release_wait`)
		wantNone(t, "status.md", md, "## Waiting for release")
	})
}
