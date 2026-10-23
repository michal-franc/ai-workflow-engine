package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var setMetaCommand = &Command{
	Name:      "set-meta",
	ShortHelp: "Set/clear a frontmatter field",
	LongHelp: `Set or clear a frontmatter field on an issue.

Examples:
  issue-cli set-meta <slug> --key waiting --value "design review"
  issue-cli set-meta <slug> --key waiting --clear
  issue-cli set-meta <slug> --key depends_on --value "ui/other-issue, cli/another"

depends_on takes the slugs of the issues this one waits on (comma-separated);
each must exist. Two or more are written as a YAML list. The new value
replaces the old one: pass every slug the issue waits on. The detail page and show list them, and the Metrics view's
Branches tab shows them per branch.`,
	Run: runSetMeta,
}

func init() {
	registerCommand(setMetaCommand)
}

func runSetMeta(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "set-meta")
	if err != nil {
		return err
	}
	fs := newFlagSet("set-meta", ctx)
	keyFlag := fs.String("key", "", "frontmatter key")
	valueFlag := fs.String("value", "", "value to set")
	clearFlag := fs.Bool("clear", false, "clear the field")
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}
	key := *keyFlag
	value := normalizeEscapedText(*valueFlag)
	clear := *clearFlag
	if key == "" {
		return fmt.Errorf("set-meta requires --key\n\nExamples:\n  issue-cli set-meta %s --key waiting --value \"waiting on design review\"\n  issue-cli set-meta %s --key waiting --clear", slug, slug)
	}
	if clear && flagWasSet(fs, "value") {
		return fmt.Errorf("set-meta: --value and --clear are mutually exclusive")
	}
	if !clear && value == "" {
		return fmt.Errorf("set-meta requires --value or --clear\n\nExamples:\n  issue-cli set-meta %s --key waiting --value \"...\"\n  issue-cli set-meta %s --key waiting --clear", slug, slug)
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	if key == "type" && ctx.Project.LoadWorkflow().HasTypes() {
		return fmt.Errorf("set-meta can't change type: in a project with types (it picks the issue's path)\n\nUse:\n  issue-cli set-type %s <type>", issue.Slug)
	}
	var list []string
	if key == tracker.DependsOnField && !clear {
		slugs, err := checkDependsOn(ctx, issue, value)
		if err != nil {
			return err
		}
		value = strings.Join(slugs, ", ")
		if len(slugs) > 1 {
			list = slugs
		}
	}
	if list != nil {
		err = tracker.SetFrontmatterList(issue.FilePath, key, list)
	} else {
		err = tracker.SetFrontmatterField(issue.FilePath, key, value, clear)
	}
	if err != nil {
		return fmt.Errorf("failed to set frontmatter: %w", err)
	}
	if clear {
		fmt.Fprintf(ctx.Stdout, "✓ Cleared %s on %s\n", key, issue.Slug)
	} else {
		fmt.Fprintf(ctx.Stdout, "✓ Set %s = %q on %s\n", key, value, issue.Slug)
	}
	if key == tracker.DependsOnField && !clear {
		fmt.Fprintln(ctx.Stdout, "The detail page and show now list this dependency.")
	}
	fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
	return nil
}

// checkDependsOn parses a depends_on value and checks every slug names
// another existing issue, suggesting close matches for unknown ones.
func checkDependsOn(ctx *Context, issue *tracker.Issue, value string) ([]string, error) {
	issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
	if err != nil {
		return nil, err
	}
	slugs := tracker.ParseSlugList(value)
	if len(slugs) == 0 {
		return nil, fmt.Errorf("set-meta: depends_on needs at least one issue slug")
	}
	for _, s := range slugs {
		if strings.EqualFold(s, issue.Slug) {
			return nil, fmt.Errorf("set-meta: %s cannot depend on itself", issue.Slug)
		}
		if tracker.FindIssue(issues, s) != nil {
			continue
		}
		msg := fmt.Sprintf("set-meta: depends_on: no issue %q", s)
		if near := closeSlugs(issues, s, 3); len(near) > 0 {
			msg += "\n\nDid you mean:\n  " + strings.Join(near, "\n  ")
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return slugs, nil
}

// closeSlugs returns up to n slugs that share the most words with s.
func closeSlugs(issues []*tracker.Issue, s string, n int) []string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' })
	type scored struct {
		slug  string
		score int
	}
	var cands []scored
	for _, is := range issues {
		score := 0
		for _, w := range words {
			if len(w) > 2 && strings.Contains(is.Slug, w) {
				score++
			}
		}
		if score > 0 {
			cands = append(cands, scored{is.Slug, score})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	var out []string
	for i := 0; i < len(cands) && i < n; i++ {
		out = append(out, cands[i].slug)
	}
	return out
}
