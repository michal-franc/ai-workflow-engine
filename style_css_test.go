package main

import (
	"os"
	"regexp"
	"testing"
)

// A lost "}" (a merge that drops a block's closing line) nests every later
// rule inside the block above it, so whole pages silently lose their styles:
// in v0.48.0 the Metrics and Status CSS ended up inside a max-width media
// query and only applied on narrow screens. Keep the braces balanced.
func TestStyleCSSBracesBalanced(t *testing.T) {
	data, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(data), "")
	depth, line, openedAt := 0, 1, []int{}
	for _, ch := range css {
		switch ch {
		case '\n':
			line++
		case '{':
			depth++
			openedAt = append(openedAt, line)
		case '}':
			depth--
			if depth < 0 {
				t.Fatalf("static/style.css: unmatched } near line %d (comments stripped)", line)
			}
			openedAt = openedAt[:len(openedAt)-1]
		}
	}
	if depth != 0 {
		t.Fatalf("static/style.css: %d block(s) never closed; the first opens near line %d (comments stripped)", depth, openedAt[0])
	}
}
