package main

import (
	"embed"
	"fmt"
	"strings"
)

// The playtest guides ship inside issue-cli so bots in any project can read
// them; docs/patterns/playtest/ holds the same files for the docs view
// (TestPlaytestGuidesInSync keeps them equal).
//
//go:embed guides/playtest/*.md
var playtestGuides embed.FS

// playtestGuideFiles maps a `process playtest <name>` argument to its guide.
var playtestGuideFiles = map[string]string{
	"":         "README.md",
	"contract": "README.md",
	"web":      "web-app.md",
	"web-app":  "web-app.md",
	"webapp":   "web-app.md",
	"chromedp": "web-app.md",
	"game":     "game.md",
	"godot":    "game.md",
}

func runProcessPlaytest(ctx *Context, args []string) error {
	args, err := parsePositionals(ctx, "process playtest", args, 1)
	if err != nil {
		return err
	}
	name := ""
	if len(args) > 0 {
		name = strings.ToLower(strings.TrimSpace(args[0]))
	}
	file, ok := playtestGuideFiles[name]
	if !ok {
		return fmt.Errorf("unknown playtest guide %q\n\nGuides:\n  issue-cli process playtest        the shared contract: session shape, results, commands, workflow wiring\n  issue-cli process playtest web    example: a web app driven by headless Chrome (issue-viewer)\n  issue-cli process playtest game   example: a game engine (RogueTech.X)", args[0])
	}
	data, err := playtestGuides.ReadFile("guides/playtest/" + file)
	if err != nil {
		return err
	}
	fmt.Fprint(ctx.Stdout, stripFrontmatter(string(data)))
	if file == "README.md" {
		fmt.Fprint(ctx.Stdout, "\n== Next ==\n  issue-cli process playtest web    worked example: a web app (chromedp)\n  issue-cli process playtest game   worked example: a game engine\n")
	}
	return nil
}

// stripFrontmatter drops a leading `---` YAML block (the docs view uses it;
// the terminal doesn't need it).
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if end := strings.Index(s[4:], "\n---\n"); end >= 0 {
		return strings.TrimLeft(s[4+end+5:], "\n")
	}
	return s
}
