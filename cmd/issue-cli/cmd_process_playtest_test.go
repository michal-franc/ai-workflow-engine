package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessPlaytestGuides(t *testing.T) {
	for args, want := range map[string]string{
		"":         "# Playtest sessions",
		"web":      "# Playtest example: a web app driven by headless Chrome",
		"chromedp": "# Playtest example: a web app driven by headless Chrome",
		"game":     "# Playtest example: a game",
	} {
		var out bytes.Buffer
		var a []string
		if args != "" {
			a = []string{args}
		}
		if err := runProcessPlaytest(&Context{Stdout: &out}, a); err != nil {
			t.Fatalf("process playtest %s: %v", args, err)
		}
		got := out.String()
		if !strings.HasPrefix(got, want) {
			t.Errorf("process playtest %s starts with %q, want %q", args, firstLine(got), want)
		}
		if strings.HasPrefix(got, "---") || strings.Contains(got, "\ntitle: \"") {
			t.Errorf("process playtest %s printed frontmatter", args)
		}
	}
	var out bytes.Buffer
	err := runProcessPlaytest(&Context{Stdout: &out}, []string{"cli"})
	if err == nil || !strings.Contains(err.Error(), "issue-cli process playtest web") {
		t.Errorf("unknown guide error should list the guides, got %v", err)
	}
	if normalizeTopic("ux-test") != "playtest" || normalizeTopic("Playtests") != "playtest" {
		t.Error("playtest aliases don't normalise")
	}
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// TestPlaytestGuidesInSync: the embedded guides and the docs/ copies the web
// docs view shows must be the same files.
func TestPlaytestGuidesInSync(t *testing.T) {
	entries, err := playtestGuides.ReadDir("guides/playtest")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		embedded, _ := playtestGuides.ReadFile("guides/playtest/" + e.Name())
		doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "patterns", "playtest", e.Name()))
		if err != nil {
			t.Errorf("docs/patterns/playtest/%s missing: copy cmd/issue-cli/guides/playtest/%s there", e.Name(), e.Name())
			continue
		}
		if !bytes.Equal(embedded, doc) {
			t.Errorf("docs/patterns/playtest/%s differs from cmd/issue-cli/guides/playtest/%s; edit the guide and copy it over", e.Name(), e.Name())
		}
	}
}
