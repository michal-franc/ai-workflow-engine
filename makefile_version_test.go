package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Makefile stamps both binaries with `git describe`, so a build on a
// release candidate tag reports it (issue-cli version, issue-viewer
// -version, the usage log, the Status card).
func TestMakefileStampsGitDescribe(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), makefile, 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		return runGitT(t, root, append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "tag.gpgSign=false", "-c", "commit.gpgSign=false"}, args...)...)
	}
	dryRun := func(target string) string {
		t.Helper()
		cmd := exec.Command("make", "-n", target)
		cmd.Dir = root
		// VERSION must come from git, not the caller's env.
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "VERSION=") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", target, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("add", "Makefile")
	git("commit", "-q", "-m", "a")
	git("tag", "-a", "v9.9.0-rc.1", "-m", "rc")

	out := dryRun("install")
	for _, want := range []string{
		`go install -ldflags "-X main.Version=v9.9.0-rc.1" ./cmd/issue-cli/`,
		`go install -ldflags "-X main.Version=v9.9.0-rc.1" .`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("make install on the tag: missing %q:\n%s", want, out)
		}
	}
	if out := dryRun("build-cli"); !strings.Contains(out, "main.Version=v9.9.0-rc.1\"") {
		t.Errorf("make build-cli on the tag:\n%s", out)
	}

	os.WriteFile(filepath.Join(root, "b"), []byte("b"), 0o644)
	git("add", "b")
	git("commit", "-q", "-m", "b")
	sha := git("rev-parse", "--short", "HEAD")
	if out := dryRun("build"); !strings.Contains(out, "main.Version=v9.9.0-rc.1-1-g"+sha+"\"") {
		t.Errorf("make build past the tag should say so:\n%s", out)
	}
	os.WriteFile(filepath.Join(root, "b"), []byte("changed"), 0o644)
	if out := dryRun("build"); !strings.Contains(out, "-g"+sha+"-dirty\"") {
		t.Errorf("make build from a dirty tree should say so:\n%s", out)
	}
}
