package privacy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// releaseRepo is a dev checkout whose origin is a bare "public" repo that
// already has the history (as a public dev repo does before the switch),
// plus an empty bare "dev" repo.
type releaseRepo struct {
	root, public, dev string
	cfg               *tracker.PrivacyConfig
}

func newReleaseRepo(t *testing.T) releaseRepo {
	t.Helper()
	gitEnv(t)
	base := t.TempDir()
	r := releaseRepo{root: filepath.Join(base, "proj"), public: filepath.Join(base, "public.git"), dev: filepath.Join(base, "dev.git"),
		cfg: &tracker.PrivacyConfig{Code: "snapshots", Decided: "x"}}
	run(t, base, "git", "init", "-q", "--bare", "-b", "main", r.public)
	run(t, base, "git", "init", "-q", "--bare", "-b", "main", r.dev)
	write(t, filepath.Join(r.root, "main.go"), "package main\n")
	write(t, filepath.Join(r.root, "CHANGELOG.md"), "# Changelog\n\n## Unreleased\n\n- feature A\n\n## v0.53.0 — 2026-10-23\n\n- old\n")
	run(t, r.root, "git", "init", "-q", "-b", "main")
	run(t, r.root, "git", "add", ".")
	run(t, r.root, "git", "commit", "-q", "-m", "feat: A, as the owner said \"make it so\"")
	run(t, r.root, "git", "tag", "-a", "v0.53.0", "-m", "v0.53.0")
	run(t, r.root, "git", "tag", "-a", "v0.53.1-rc.1", "-m", "local candidate")
	run(t, r.root, "git", "remote", "add", "origin", r.public)
	run(t, r.root, "git", "push", "-q", "origin", "main")
	return r
}

// land commits change on work/<slug> and fast-forwards main to it.
func (r releaseRepo) land(t *testing.T, slug, file, text, changelogLine string) {
	t.Helper()
	run(t, r.root, "git", "checkout", "-q", "-b", "work/"+slug)
	write(t, filepath.Join(r.root, file), text)
	if changelogLine != "" {
		data, _ := os.ReadFile(filepath.Join(r.root, "CHANGELOG.md"))
		s := strings.Replace(string(data), "## Unreleased\n\n", "## Unreleased\n\n"+changelogLine+"\n", 1)
		write(t, filepath.Join(r.root, "CHANGELOG.md"), s)
	}
	run(t, r.root, "git", "add", ".")
	run(t, r.root, "git", "commit", "-q", "-m", "fix: "+slug+" per D-3")
	run(t, r.root, "git", "checkout", "-q", "main")
	run(t, r.root, "git", "merge", "-q", "--ff-only", "work/"+slug)
}

func TestReleaseFlowCutSoakPickPublish(t *testing.T) {
	r := newReleaseRepo(t)
	setup, err := Setup(r.root, r.dev, r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.RenamedOrigin || setup.Base == "" || setup.SkippedRC != 1 {
		t.Errorf("setup = %+v", setup)
	}
	if got := run(t, r.root, "git", "remote", "get-url", "public"); got != r.public {
		t.Errorf("public = %s", got)
	}
	if got := run(t, r.root, "git", "config", "remote.public.tagOpt"); got != "--no-tags" {
		t.Errorf("tagOpt = %s", got)
	}
	if tags := run(t, r.root, "git", "--git-dir="+r.dev, "tag", "-l"); tags != "v0.53.0" {
		t.Errorf("dev tags = %q, want only the release (no candidate)", tags)
	}

	// Cut: the branch, its worktree, rc.1.
	cut, err := Cut(r.root, "v0.54.0", "main", true)
	if err != nil {
		t.Fatal(err)
	}
	if !cut.Created || cut.Branch != "release/v0.54" || cut.Tag != "v0.54.0-rc.1" || !cut.Pushed {
		t.Errorf("cut = %+v", cut)
	}
	if _, err := Cut(r.root, "v0.54.0", "main", true); err == nil || !strings.Contains(err.Error(), "already tags") {
		t.Errorf("a second cut on the same tip = %v", err)
	}
	if out := run(t, r.root, "git", "--git-dir="+r.dev, "branch", "--list", "release/*"); !strings.Contains(out, "release/v0.54") {
		t.Errorf("dev branches = %q", out)
	}

	// Main moves on: a fix for the release and a later feature.
	r.land(t, "ui/fix", "fix.go", "package main // fix\n", "- fix B")
	r.land(t, "ui/later", "later.go", "package main // later\n", "- later C")

	pick, err := Pick(r.root, "v0.54.0", []string{"ui/fix"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pick.Picked) != 1 {
		t.Errorf("pick = %+v", pick)
	}
	if again, _ := Pick(r.root, "v0.54.0", []string{"ui/fix"}); len(again.Already) != 1 || len(again.Picked) != 0 {
		t.Errorf("second pick = %+v", again)
	}
	if _, err := os.Stat(filepath.Join(cut.Worktree, "later.go")); err == nil {
		t.Error("the later feature reached the release branch")
	}
	cut2, err := Cut(r.root, "v0.54.0", "main", true)
	if err != nil || cut2.Tag != "v0.54.0-rc.2" || cut2.Created {
		t.Fatalf("cut rc.2 = %+v, %v", cut2, err)
	}

	in := PublishInput{Root: r.root, Worktree: cut.Worktree, Cut: "v0.54.0", Cfg: r.cfg, Changelog: "CHANGELOG.md", Now: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)}

	// Refused outside the release worktree.
	wrong := in
	wrong.Worktree = r.root
	if _, err := Publish(wrong); err == nil || !strings.Contains(err.Error(), "publish runs on release/v0.54, in its worktree: cd "+cut.Worktree) {
		t.Errorf("publish from main = %v", err)
	}

	res, err := Publish(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Parent != setup.Base || !res.MainMoved {
		t.Errorf("publish = %+v", res)
	}
	// The public repo: one new commit, message exactly v0.54.0, the branch's tree.
	if msg := run(t, r.root, "git", "--git-dir="+r.public, "log", "-1", "--format=%B", "main"); msg != "v0.54.0" {
		t.Errorf("public head message = %q", msg)
	}
	if n := run(t, r.root, "git", "--git-dir="+r.public, "rev-list", "--count", setup.Base+"..main"); n != "1" {
		t.Errorf("public got %s commits, want 1", n)
	}
	pubTree := run(t, r.root, "git", "--git-dir="+r.public, "rev-parse", "main^{tree}")
	if branchTree := run(t, r.root, "git", "rev-parse", "release/v0.54^{tree}"); pubTree != branchTree {
		t.Errorf("public tree %s != release branch tree %s", pubTree, branchTree)
	}
	if out := run(t, r.root, "git", "--git-dir="+r.public, "branch", "--list"); strings.Contains(out, "release/") {
		t.Errorf("public got a release branch: %q", out)
	}
	if out := run(t, r.root, "git", "--git-dir="+r.public, "log", "--format=%s", "main"); strings.Contains(out, "per D-3") {
		t.Errorf("a dev commit message reached public:\n%s", out)
	}
	// Dev: the tag on the branch, both pushed.
	if out := run(t, r.root, "git", "--git-dir="+r.dev, "rev-parse", "v0.54.0^{commit}"); out != run(t, r.root, "git", "rev-parse", "release/v0.54") {
		t.Errorf("dev tag v0.54.0 is not on the release branch tip")
	}
	// The branch's changelog has the heading; main's Unreleased keeps only what came after the cut.
	branchLog, _ := os.ReadFile(filepath.Join(cut.Worktree, "CHANGELOG.md"))
	if !strings.Contains(string(branchLog), "## v0.54.0 — 2026-10-30\n\n- fix B\n- feature A\n") {
		t.Errorf("branch changelog:\n%s", branchLog)
	}
	mainLog, _ := os.ReadFile(filepath.Join(r.root, "CHANGELOG.md"))
	want := "## Unreleased\n\n- later C\n\n## v0.54.0 — 2026-10-30\n\n- fix B\n- feature A\n\n## v0.53.0"
	if !strings.Contains(string(mainLog), want) {
		t.Errorf("main changelog:\n%s\nwant it to contain:\n%s", mainLog, want)
	}

	// Published twice: refused.
	if _, err := Publish(in); err == nil || !strings.Contains(err.Error(), "already tagged") {
		t.Errorf("second publish = %v", err)
	}

	// The code badge: green, last published v0.54.0 (the guard is not installed here: amber first).
	badgeIn := Input{Root: r.root, Slug: "proj", Cfg: r.cfg, Now: time.Now()}
	if b := CodeBadge(badgeIn); b.Level != Amber || b.Label != "Guard off" {
		t.Errorf("badge without the hook = %+v", b)
	}
	if _, _, err := InstallHook(r.root, "--project proj"); err != nil {
		t.Fatal(err)
	}
	// No user.signingkey here: the snapshot is unsigned, so the badge says so.
	if b := CodeBadge(badgeIn); b.Level != Amber || b.Label != "Release unsigned" || !strings.Contains(b.Fix, "git config user.signingkey") {
		t.Errorf("unsigned badge = %+v", b)
	}

	// A dev commit pushed to public by hand (no guard): red, and publish refuses.
	run(t, r.root, "git", "push", "-q", "--no-verify", "public", "main:main", "--force")
	run(t, r.root, "git", "fetch", "-q", "public")
	if b := CodeBadge(badgeIn); b.Level != Red || b.Label != "Dev history public" {
		t.Errorf("badge after a dev push = %+v", b)
	}
}

func TestPublishRefusals(t *testing.T) {
	r := newReleaseRepo(t)
	if _, err := Setup(r.root, r.dev, r.cfg); err != nil {
		t.Fatal(err)
	}
	cut, err := Cut(r.root, "v0.54.0", "main", false)
	if err != nil {
		t.Fatal(err)
	}
	in := PublishInput{Root: r.root, Worktree: cut.Worktree, Cut: "v0.54.0", Cfg: r.cfg, Changelog: "CHANGELOG.md", Now: time.Now()}

	notSnap := in
	notSnap.Cfg = &tracker.PrivacyConfig{Code: "public"}
	if _, err := Publish(notSnap); err == nil || !strings.Contains(err.Error(), "not snapshots") {
		t.Errorf("code: public = %v", err)
	}

	write(t, filepath.Join(cut.Worktree, "main.go"), "package main // dirty\n")
	if _, err := Publish(in); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("dirty = %v", err)
	}
	run(t, cut.Worktree, "git", "checkout", "--", "main.go")

	// A work path in the tree.
	write(t, filepath.Join(cut.Worktree, "issues", "a.md"), "x\n")
	run(t, cut.Worktree, "git", "add", "issues")
	run(t, cut.Worktree, "git", "commit", "-q", "-m", "oops")
	if _, err := Publish(in); err == nil || !strings.Contains(err.Error(), "work files are in release/v0.54's tree (issues/a.md)") {
		t.Errorf("work path = %v", err)
	}
	run(t, cut.Worktree, "git", "reset", "-q", "--hard", "HEAD~1")

	// The public tip moved past the base to a dev commit.
	run(t, r.root, "git", "commit", "-q", "--allow-empty", "-m", "dev work")
	run(t, r.root, "git", "push", "-q", "--no-verify", "public", "main:main")
	if _, err := Publish(in); err == nil || !strings.Contains(err.Error(), "is not a release snapshot: dev history reached the public repo") {
		t.Errorf("non-snapshot tip = %v", err)
	}
	first := in
	first.First = true
	if _, err := Publish(first); err != nil {
		t.Errorf("--first = %v", err)
	}
}

func TestPrePushGuardsThePublicRemoteAndQuotes(t *testing.T) {
	r := newReleaseRepo(t)
	if _, err := Setup(r.root, r.dev, r.cfg); err != nil {
		t.Fatal(err)
	}
	head := run(t, r.root, "git", "rev-parse", "HEAD")
	ref := []PushRef{{"refs/heads/main", head, "refs/heads/main", strings.Repeat("0", 40)}}
	// A dev commit: fine to origin, refused to public.
	run(t, r.root, "git", "commit", "-q", "--allow-empty", "-m", "dev work")
	head = run(t, r.root, "git", "rev-parse", "HEAD")
	ref[0].LocalSHA = head
	if err := PrePush(r.root, r.cfg, "origin", r.dev, ref, nil); err != nil {
		t.Errorf("push to the dev repo refused: %v", err)
	}
	err := PrePush(r.root, r.cfg, "public", r.public, ref, nil)
	if err == nil || !strings.Contains(err.Error(), "public gets release snapshots only") {
		t.Errorf("dev commit to public = %v", err)
	}
	// By URL too (git push <url>).
	if err := PrePush(r.root, r.cfg, r.public, r.public, ref, nil); err == nil {
		t.Error("dev commit to the public URL went out")
	}
	// A snapshot goes out.
	tree := run(t, r.root, "git", "rev-parse", "HEAD^{tree}")
	snap := run(t, r.root, "git", "commit-tree", tree, "-m", "v0.54.0")
	ref[0].LocalSHA = snap
	if err := PrePush(r.root, r.cfg, "public", r.public, ref, nil); err != nil {
		t.Errorf("snapshot refused: %v", err)
	}
	// A tag on a dev commit is refused.
	run(t, r.root, "git", "tag", "-a", "v9.9.9", "-m", "v9.9.9", head)
	tag := []PushRef{{"refs/tags/v9.9.9", run(t, r.root, "git", "rev-parse", "v9.9.9"), "refs/tags/v9.9.9", strings.Repeat("0", 40)}}
	if err := PrePush(r.root, r.cfg, "public", r.public, tag, nil); err == nil || !strings.Contains(err.Error(), "tag v9.9.9 is on a dev commit") {
		t.Errorf("dev tag to public = %v", err)
	}

	// code: public: a decision's quote in a message is refused.
	pub := &tracker.PrivacyConfig{Code: "public", Decided: "x"}
	run(t, r.root, "git", "commit", "-q", "--allow-empty", "-m", "feat: as the owner said, keep the current repo public still")
	ref[0].LocalSHA = run(t, r.root, "git", "rev-parse", "HEAD")
	quotes := []string{"keep the current repo public still", "yes"}
	if err := PrePush(r.root, pub, "origin", r.public, ref, quotes); err == nil || !strings.Contains(err.Error(), "quotes a decision") {
		t.Errorf("quote = %v", err)
	}
	if err := PrePush(r.root, pub, "origin", r.public, ref, []string{"yes"}); err != nil {
		t.Errorf("a short quote should not count: %v", err)
	}
}

func TestDecisionQuotesSkipsShortOnes(t *testing.T) {
	is := &tracker.Issue{Slug: "a/b", BodyRaw: "## Decisions\n\n- **D-1** · 2026-10-24 · lead chat: Keep it, \"no no we would want to keep current repo public\"\n- **D-2** · 2026-10-24 · lead chat: Go, \"yes\"\n"}
	got := DecisionQuotes([]*tracker.Issue{is})
	if len(got) != 1 || got[0] != "no no we would want to keep current repo public" {
		t.Errorf("quotes = %q", got)
	}
}

func TestParseVersionAndBranch(t *testing.T) {
	if v, err := ParseVersion("0.54.0"); err != nil || v != "v0.54.0" {
		t.Errorf("ParseVersion = %q, %v", v, err)
	}
	if _, err := ParseVersion("v0.54"); err == nil {
		t.Error("v0.54 accepted")
	}
}

// signingKey makes an SSH signing key and sets it as root's user.signingkey
// (gpg.format ssh), so the test needs no GPG agent.
func signingKey(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	key := filepath.Join(t.TempDir(), "id_ed25519")
	run(t, root, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
	run(t, root, "git", "config", "gpg.format", "ssh")
	run(t, root, "git", "config", "user.signingkey", key)
}

func TestPublishSignsTheSnapshot(t *testing.T) {
	r := newReleaseRepo(t)
	if _, err := Setup(r.root, r.dev, r.cfg); err != nil {
		t.Fatal(err)
	}
	cut, err := Cut(r.root, "v0.54.0", "main", false)
	if err != nil {
		t.Fatal(err)
	}
	in := PublishInput{Root: r.root, Worktree: cut.Worktree, Cut: "v0.54.0", Cfg: r.cfg, Changelog: "CHANGELOG.md", Now: time.Now()}

	// A key that can't sign stops the publish: nothing goes public.
	before := run(t, r.root, "git", "ls-remote", r.public, "refs/heads/main")
	run(t, r.root, "git", "config", "gpg.format", "ssh")
	run(t, r.root, "git", "config", "user.signingkey", filepath.Join(t.TempDir(), "missing"))
	if _, err := Publish(in); err == nil || !strings.Contains(err.Error(), "signing the v0.54.0 snapshot with user.signingkey failed, so nothing was published") {
		t.Fatalf("bad key = %v", err)
	}
	if after := run(t, r.root, "git", "ls-remote", r.public, "refs/heads/main"); after != before {
		t.Fatalf("public moved after a failed signature: %q -> %q", before, after)
	}

	signingKey(t, r.root)
	res, err := Publish(in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Signed || !commitSigned(r.root, res.Snapshot) {
		t.Fatalf("snapshot %s not signed (Signed=%v)", res.Snapshot, res.Signed)
	}
	if _, _, err := InstallHook(r.root, "--project proj"); err != nil {
		t.Fatal(err)
	}
	if b := CodeBadge(Input{Root: r.root, Slug: "proj", Cfg: r.cfg, Now: time.Now()}); b.Level != Green || !strings.Contains(b.Sentence, "last published v0.54.0") {
		t.Errorf("badge after a signed publish = %+v", b)
	}
}
