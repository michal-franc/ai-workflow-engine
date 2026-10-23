package health

import (
	"path"
	"strings"
)

// matchGlob reports whether the slash path p matches pattern. A pattern
// segment of ** matches any number of segments, including none; other
// segments use path.Match. A pattern without a slash matches the file name
// in any directory, as in .gitignore ("*.min.js").
func matchGlob(pattern, p string) bool {
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "/")
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(p))
		return ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			for i := 0; i <= len(segs); i++ {
				if matchSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// matchAny reports whether p matches one of the patterns.
func matchAny(patterns []string, p string) bool {
	for _, pat := range patterns {
		if matchGlob(pat, p) {
			return true
		}
	}
	return false
}
