package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var retrospectiveCommand = &Command{
	Name:      "retrospective",
	ShortHelp: "Save workflow feedback under retros/ in the project",
	LongHelp: `Save a workflow retrospective to retros/ for the current issue.

Examples:
  issue-cli retrospective <slug> --body "Base workflow: ...\nSubsystem workflow: ..."`,
	Run: runRetrospective,
}

func init() {
	registerCommand(retrospectiveCommand)
}

func runRetrospective(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "retrospective")
	if err != nil {
		return err
	}
	fs := newFlagSet("retrospective", ctx)
	bodyFlag := fs.String("body", "", "retrospective text")
	textFlag := fs.String("text", "", "alias for --body")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	text := normalizeEscapedText(*bodyFlag)
	if text == "" {
		text = normalizeEscapedText(*textFlag)
	}
	if text == "" {
		text = strings.Join(fs.Args(), " ")
	}
	if text == "" {
		return fmt.Errorf("retrospective requires --body\n\nExample:\n  issue-cli retrospective %s --body \"Base workflow: ...\\nSubsystem workflow: ...\\nTooling friction: ...\"", slug)
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}

	retroDir := filepath.Join(projectRoot(ctx.Project), "retros")
	if err := os.MkdirAll(retroDir, 0755); err != nil {
		return fmt.Errorf("failed to create retrospective directory: %w", err)
	}

	now := ctx.Now()
	body := strings.TrimSpace(text)
	report := fmt.Sprintf(`# Workflow Retrospective

Issue: %s
Title: %s
Status: %s
System: %s
Date: %s

%s
`, issue.Slug, issue.Title, issue.Status, issue.System, now.Format(time.RFC3339), body)

	name := fmt.Sprintf("%s-%s.md", now.UTC().Format("20060102-150405"), sanitizePathPart(issue.Slug))
	path := filepath.Join(retroDir, name)
	if err := os.WriteFile(path, []byte(report), 0644); err != nil {
		return fmt.Errorf("failed to save retrospective: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Retrospective saved for %s\n", issue.Slug)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", path)
	return nil
}
