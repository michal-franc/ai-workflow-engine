package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var createCommand = &Command{
	Name:      "create",
	ShortHelp: "Create a new issue",
	LongHelp: `Create a new issue file in the issues directory (or in issues/<system>/ when
--system is supplied). Only allows statuses earlier than backlog.

Example:
  issue-cli create --title "Fix heat overflow" --system Combat --status idea`,
	Run: runCreate,
}

func init() {
	registerCommand(createCommand)
}

func runCreate(ctx *Context, args []string) error {
	fs := newFlagSet("create", ctx)
	titleFlag := fs.String("title", "", "issue title (required)")
	systemFlag := fs.String("system", "", "system / category")
	statusFlag := fs.String("status", "", "initial status")
	priorityFlag := fs.String("priority", "", "priority")
	if err := fs.Parse(args); err != nil {
		return err
	}
	title := *titleFlag
	system := *systemFlag
	status := *statusFlag
	priority := *priorityFlag
	if title == "" {
		return fmt.Errorf("--title is required\n\nExample:\n  issue-cli create --title \"Fix heat overflow\" --system Combat --status idea")
	}

	proj := ctx.Project
	wf := proj.LoadWorkflow()
	statusOrder := wf.GetStatusOrder()

	if status == "" {
		status = "idea"
		for _, s := range statusOrder {
			if s != "none" {
				status = s
				break
			}
		}
	}

	idx := wf.GetStatusIndex(status)
	backlogIdx := wf.GetStatusIndex("backlog")
	if backlogIdx == -1 {
		backlogIdx = 3
	}
	if idx == -1 || idx >= backlogIdx {
		var allowed []string
		for _, s := range statusOrder {
			if s == "none" {
				continue
			}
			if wf.GetStatusIndex(s) < backlogIdx {
				allowed = append(allowed, "\""+s+"\"")
			}
		}
		return fmt.Errorf("cannot create issue with status %q — allowed: %s", status, strings.Join(allowed, ", "))
	}

	dir := proj.IssueDir
	if system != "" {
		dir = filepath.Join(dir, system)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create system directory: %w", err)
		}
	}

	slug := tracker.Slugify(title)
	filename := filepath.Join(dir, slug+".md")
	if _, err := os.Stat(filename); err == nil {
		return fmt.Errorf("issue already exists: %s\nUse 'update' to modify existing issues.", filename)
	}

	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString(fmt.Sprintf("title: \"%s\"\n", strings.ReplaceAll(title, "\"", "\\\"")))
	content.WriteString(fmt.Sprintf("status: \"%s\"\n", status))
	if system != "" {
		content.WriteString(fmt.Sprintf("system: \"%s\"\n", system))
	}
	if priority != "" {
		content.WriteString(fmt.Sprintf("priority: \"%s\"\n", priority))
	}
	content.WriteString("---\n")

	tmpl := wf.TemplateForStatus(status)
	if tmpl != "" {
		content.WriteString("\n")
		content.WriteString(tmpl)
		content.WriteString("\n")
	} else {
		content.WriteString("\n")
	}

	if err := os.WriteFile(filename, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	display := slug
	if system != "" {
		display = strings.ToLower(system) + "/" + slug
	}
	fmt.Fprintf(ctx.Stdout, "✓ Created: %s\n", filename)
	fmt.Fprintf(ctx.Stdout, "file: %s\n", filename)
	fmt.Fprintf(ctx.Stdout, "  Slug: %s\n", display)
	if tmpl != "" {
		fmt.Fprintln(ctx.Stdout, "✓ Template checkboxes added to issue body")
	}
	fmt.Fprintln(ctx.Stdout, "\nThank you!")
	return nil
}
