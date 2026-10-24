package milestone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseRC(t *testing.T) {
	for in, want := range map[string]RC{
		"v0.54.0-rc.1":                  {Tag: "v0.54.0-rc.1", Cut: "v0.54.0", N: 1, Exact: true},
		"v0.54.0-rc.12":                 {Tag: "v0.54.0-rc.12", Cut: "v0.54.0", N: 12, Exact: true},
		"v0.54.0-rc.2-3-gabc1234":       {Tag: "v0.54.0-rc.2", Cut: "v0.54.0", N: 2},
		"v0.54.0-rc.2-dirty":            {Tag: "v0.54.0-rc.2", Cut: "v0.54.0", N: 2},
		"v0.54.0-rc.2-3-gabc1234-dirty": {Tag: "v0.54.0-rc.2", Cut: "v0.54.0", N: 2},
		"0.54.0-rc.1":                   {Tag: "0.54.0-rc.1", Cut: "v0.54.0", N: 1, Exact: true},
	} {
		got, ok := ParseRC(in)
		if !ok || got != want {
			t.Errorf("ParseRC(%q) = %+v, %v; want %+v", in, got, ok, want)
		}
	}
	for _, in := range []string{"v0.54.0", "dev", "abc1234", "v0.54.0-3-gabc", "v0.54-rc.1", "v0.54.0-rc.x"} {
		if got, ok := ParseRC(in); ok {
			t.Errorf("ParseRC(%q) = %+v, want not a candidate", in, got)
		}
	}
}

func TestNewestRC(t *testing.T) {
	tags := []string{"v0.53.0", "v0.54.0-rc.1", "v0.54.0-rc.10", "v0.54.0-rc.2", "v0.55.0-rc.3"}
	got, ok := NewestRC(tags, "v0.54.0")
	if !ok || got.Tag != "v0.54.0-rc.10" || got.N != 10 {
		t.Fatalf("NewestRC = %+v, %v; want rc.10 (numeric, not text, order)", got, ok)
	}
	if _, ok := NewestRC(tags, "v0.56.0"); ok {
		t.Fatal("NewestRC found a candidate for a cut with none")
	}
	if RCTag("v0.54.0", 11) != "v0.54.0-rc.11" {
		t.Fatal(RCTag("v0.54.0", 11))
	}
}

func TestIssueVersion(t *testing.T) {
	for _, c := range []struct {
		cut   string
		inUse []string
		want  string
	}{
		{"v0.54.0", []string{"0.53", "0.54", "0.55"}, "0.54"},
		{"v0.54.0", []string{"0.54.0"}, "0.54.0"},
		{"v0.54.0", nil, "0.54"},
		{"v0.54.1", nil, "0.54.1"},
		{"dev", nil, ""},
	} {
		if got := IssueVersion(c.cut, c.inUse); got != c.want {
			t.Errorf("IssueVersion(%q, %v) = %q, want %q", c.cut, c.inUse, got, c.want)
		}
	}
}

func TestReleasedSkipsCandidateTags(t *testing.T) {
	if got := newestRelease("v0.54.0-rc.2\nv0.54.0-rc.1\nv0.53.0\n"); got != "v0.53.0" {
		t.Fatalf("newestRelease = %q, want v0.53.0", got)
	}
	if got := newestRelease("v0.54.0-rc.1\n"); got != "" {
		t.Fatalf("newestRelease with only a candidate = %q, want none", got)
	}

	// A real repo without a changelog: the rc tag is newer, v0.53.0 is the release.
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "tag.gpgSign=false", "-c", "commit.gpgSign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a")
	git("commit", "-q", "-m", "a")
	git("tag", "-a", "v0.53.0", "-m", "v0.53.0")
	git("tag", "-a", "v0.54.0-rc.1", "-m", "rc")
	if got := Released(context.Background(), root, ""); got != "v0.53.0" {
		t.Fatalf("Released = %q, want v0.53.0", got)
	}
}
