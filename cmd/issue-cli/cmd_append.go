package main

import (
	"fmt"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var appendCommand = &Command{
	Name:      "append",
	ShortHelp: "Append content to issue body",
	LongHelp: `Append content to the issue body. With --section, append into an existing
section; if --body starts with an existing heading, it auto-routes into that section.

Use --body-file <path> (or --body-file - for stdin) to supply the body as raw
bytes, which avoids shell mangling of backticks and parentheses in inline code.

Examples:
  issue-cli append <slug> --section "Design" --body "- [ ] edge case covered"
  issue-cli append <slug> --body "## Test Plan\n\n### Automated\n- test 1"
  issue-cli append <slug> --section "Design" --body-file design.md
  printf '%s' "- uses ` + "`Asteroid.NetRate`" + ` (workers × yield)" | issue-cli append <slug> --body-file -`,
	Run: runAppend,
}

func init() {
	registerCommand(appendCommand)
}

func runAppend(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "append")
	if err != nil {
		return err
	}
	fs := newFlagSet("append", ctx)
	bodyFlag := fs.String("body", "", "content to append")
	textFlag := fs.String("text", "", "alias for --body")
	bodyFileFlag := fs.String("body-file", "", "read body from file (or - for stdin); avoids shell mangling")
	sectionFlag := fs.String("section", "", "section name to append into")
	forceFlag := fs.Bool("force", false, "force append even when target section is missing")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	inline := normalizeEscapedText(*bodyFlag)
	if inline == "" {
		inline = normalizeEscapedText(*textFlag)
	}
	inlineSet := flagWasSet(fs, "body") || flagWasSet(fs, "text")
	text, err := resolveBodyInput(ctx, inline, inlineSet, *bodyFileFlag, flagWasSet(fs, "body-file"))
	if err != nil {
		return err
	}
	section := *sectionFlag
	force := *forceFlag
	if text == "" {
		return fmt.Errorf("append requires --body or --body-file\n\nExamples:\n  issue-cli append <slug> --section \"Design\" --body \"- [ ] edge case covered\"\n  issue-cli append <slug> --section \"Design\" --body-file design.md\n  cat design.md | issue-cli append <slug> --section \"Design\" --body-file -")
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}

	_, _, err = tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		if strings.TrimSpace(section) != "" {
			return tracker.AppendIssueBodyToSection(body, section, text, force)
		}
		return tracker.AppendIssueBody(body, text)
	})
	if err != nil {
		return fmt.Errorf("failed to append: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Appended to %s\n", issue.Slug)
	return nil
}
