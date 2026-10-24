package milestone

import (
	"regexp"
	"strconv"
	"strings"
)

// A release candidate is a local tag <cut>-rc.N ("v0.54.0-rc.2") that runs
// on this machine's projects before <cut> is published
// (workbench.release_candidates). A build's version is `git describe`:
// the tag itself, or "<tag>-<commits>-g<sha>[-dirty]" past it.

// RC is a release candidate version, or a build past one.
type RC struct {
	// Tag is the candidate's tag: "v0.54.0-rc.2".
	Tag string
	// Cut is the release it is a candidate for: "v0.54.0".
	Cut string
	N   int
	// Exact is false for a build past the tag (commits after it, or a dirty
	// tree).
	Exact bool
}

var rcRe = regexp.MustCompile(`^(v?\d+\.\d+\.\d+)-rc\.(\d+)(-.*)?$`)

// ParseRC reads a candidate tag or a describe of one; ok is false for any
// other version ("v0.54.0", "dev", a bare sha).
func ParseRC(v string) (RC, bool) {
	m := rcRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return RC{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return RC{}, false
	}
	cut := m[1]
	if !strings.HasPrefix(cut, "v") {
		cut = "v" + cut
	}
	return RC{Tag: m[1] + "-rc." + m[2], Cut: cut, N: n, Exact: m[3] == ""}, true
}

// RCTag is cut's candidate tag number n: ("v0.54.0", 2) → "v0.54.0-rc.2".
func RCTag(cut string, n int) string {
	return cut + "-rc." + strconv.Itoa(n)
}

// IsPreRelease reports whether a tag carries a pre-release suffix
// ("v0.54.0-rc.1", "v1.0.0-beta"): not a release.
func IsPreRelease(tag string) bool {
	v := strings.TrimSpace(tag)
	return Norm(v) != "" && strings.Contains(v, "-")
}

// NewestRC picks the highest candidate for cut among tags; ok is false when
// there is none.
func NewestRC(tags []string, cut string) (RC, bool) {
	var best RC
	found := false
	for _, t := range tags {
		rc, ok := ParseRC(t)
		if !ok || !rc.Exact || Norm(rc.Cut) != Norm(cut) {
			continue
		}
		if !found || rc.N > best.N {
			best, found = rc, true
		}
	}
	return best, found
}

// IssueVersion is the version: label an issue in cut's milestone carries:
// the label already in use when one matches ("0.54" for v0.54.0), else
// major.minor for a .0 release and the full number otherwise.
func IssueVersion(cut string, inUse []string) string {
	want := Norm(cut)
	if want == "" {
		return ""
	}
	for _, v := range inUse {
		if Norm(v) == want {
			return v
		}
	}
	if strings.HasSuffix(want, ".0") && strings.Count(want, ".") == 2 {
		return strings.TrimSuffix(want, ".0")
	}
	return want
}

// ReleaseBranch is the dev branch a release and its patches are cut on:
// "v0.54.0" and "v0.54.1" → "release/v0.54".
func ReleaseBranch(cut string) string {
	n := Norm(cut)
	parts := strings.Split(n, ".")
	if len(parts) >= 2 {
		n = parts[0] + "." + parts[1]
	}
	return "release/v" + n
}

// ReleaseWorktreeName is the release branch's worktree under .worktrees/:
// "release-v0.54".
func ReleaseWorktreeName(cut string) string {
	return strings.ReplaceAll(ReleaseBranch(cut), "/", "-")
}
