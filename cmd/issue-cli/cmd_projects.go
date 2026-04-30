package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var projectsCommand = &Command{
	Name:      "projects",
	ShortHelp: "List configured projects (slug, name, issue dir)",
	LongHelp: `List every project configured in projects.yaml.

The active project (resolved via --project or the single-project default) is
marked "(active)". When --project is omitted in a multi-project setup the
historical default (projects[0]) is marked instead.

Examples:
  issue-cli projects
  issue-cli projects --json
  issue-cli --project cli projects`,
	Run: runProjects,
}

func init() {
	registerCommand(projectsCommand)
}

type projectListEntry struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	IssueDir string `json:"issue_dir"`
	Active   bool   `json:"active,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

func runProjects(ctx *Context, args []string) error {
	fs := newFlagSet("projects", ctx)
	if err := fs.Parse(args); err != nil {
		return err
	}

	projects := ctx.AllProjects
	if len(projects) == 0 {
		// Bootstrap mode populates AllProjects with the synthesized project,
		// but defensively fall back to ctx.Project for older callers.
		if ctx.Project != nil {
			projects = []tracker.Project{*ctx.Project}
		}
	}

	activeSlug := ""
	if ctx.Project != nil {
		activeSlug = ctx.Project.Slug
	}
	defaultSlug := ""
	if ctx.ProjectSlug == "" && len(projects) > 0 {
		defaultSlug = projects[0].Slug
	}

	if ctx.JSONOutput {
		entries := make([]projectListEntry, 0, len(projects))
		for _, p := range projects {
			entries = append(entries, projectListEntry{
				Slug:     p.Slug,
				Name:     p.Name,
				IssueDir: p.IssueDir,
				Active:   p.Slug == activeSlug,
				Default:  p.Slug == defaultSlug,
			})
		}
		return writeJSON(ctx.Stdout, entries)
	}

	fmt.Fprintln(ctx.Stdout, "== Configured Projects ==")
	if len(projects) == 0 {
		fmt.Fprintln(ctx.Stdout, "  (none)")
		return nil
	}
	for _, p := range projects {
		marker := ""
		switch {
		case p.Slug == activeSlug:
			marker = " (active)"
		case p.Slug == defaultSlug:
			marker = " (historical default)"
		}
		fmt.Fprintf(ctx.Stdout, "  %s%s\n      name: %s\n      issues: %s\n", p.Slug, marker, p.Name, p.IssueDir)
	}
	return nil
}
