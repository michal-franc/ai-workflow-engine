package health

import "testing"

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*_test.go", "a_test.go", true},
		{"**/*_test.go", "cmd/x/a_test.go", true},
		{"**/*_test.go", "cmd/x/a.go", false},
		{"vendor/**", "vendor/a/b.go", true},
		{"vendor/**", "src/vendor/b.go", false},
		{"*.min.js", "static/js/app.min.js", true}, // no slash: any directory
		{"Tests/**/*.cs", "Tests/Unit/A.cs", true},
		{"Tests/**/*.cs", "Tests/A.cs", true},
		{"Tests/**/*.cs", "Src/A.cs", false},
		{"/docs/*.md", "docs/a.md", true},
		{"", "a.go", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	for p, want := range map[string]language{
		"a/b.GO":      {"go", KindCode},
		"Makefile":    {"make", KindCode},
		"x/README.md": {"markdown", KindDocs},
		"go.sum":      {"go-mod", KindData},
		"Map/a.board": {"other", KindOther},
		"LICENSE":     {"other", KindOther},
	} {
		if got := classify(p); got != want {
			t.Errorf("classify(%q) = %v, want %v", p, got, want)
		}
	}
}
