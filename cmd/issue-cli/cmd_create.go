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

When workflow.yaml defines types of work, --type picks the issue's path
(default: default_type); the issue starts at that type's first status.

Example:
  issue-cli create --title "Fix heat overflow" --system Combat --status idea
  issue-cli create --title "Bigger play cards" --system UI --type tweak`,
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
	typeFlag := fs.String("type", "", "type of work (when workflow.yaml defines types:)")
	if err := parseFlags(ctx, fs, args); err != nil {
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
	base := proj.LoadWorkflow()
	typ := strings.TrimSpace(*typeFlag)
	if typ != "" {
		if !base.HasTypes() {
			return fmt.Errorf("--type %q: this project defines no types (add a types: block to workflow.yaml; see docs/Workflow/types.md)", typ)
		}
		if _, ok := base.Types[typ]; !ok {
			return fmt.Errorf("unknown type %q (types: %s)", typ, strings.Join(base.TypeNames(), ", "))
		}
	}
	typ, _ = base.ResolveType(typ)
	// The type's path decides which statuses exist; the backlog cut-off is
	// the base position so a type without backlog still can't start late.
	wf := base.ForType(typ)
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

	idx := base.GetStatusIndex(status)
	backlogIdx := base.GetStatusIndex("backlog")
	if backlogIdx == -1 {
		backlogIdx = 3
	}
	if idx == -1 || idx >= backlogIdx || wf.GetStatusIndex(status) == -1 {
		var allowed []string
		for _, s := range statusOrder {
			if s == "none" {
				continue
			}
			if base.GetStatusIndex(s) < backlogIdx {
				allowed = append(allowed, "\""+s+"\"")
			}
		}
		if typ != "" {
			return fmt.Errorf("cannot create a %s issue with status %q — allowed: %s", typ, status, strings.Join(allowed, ", "))
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
	if typ != "" {
		content.WriteString(fmt.Sprintf("type: \"%s\"\n", typ))
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
	if typ != "" {
		fmt.Fprintf(ctx.Stdout, "  Type: %s%s\n", typ, otherTypesNote(base, typ, *typeFlag == "", display, status))
		fmt.Fprintf(ctx.Stdout, "  Path: %s\n", wf.PathLine())
	}
	if tmpl != "" {
		fmt.Fprintln(ctx.Stdout, "✓ Template checkboxes added to issue body")
	}
	fmt.Fprintln(ctx.Stdout, "\nThank you!")
	return nil
}

// otherTypesNote tells the agent which other types exist and how to switch,
// so types are discoverable from create output alone.
func otherTypesNote(wf *tracker.WorkflowConfig, typ string, defaulted bool, slug, status string) string {
	var others []string
	for _, n := range wf.TypeNames() {
		if n != typ {
			others = append(others, n)
		}
	}
	if len(others) == 0 {
		return ""
	}
	prefix := ""
	if defaulted {
		prefix = "default; "
	}
	if status != wf.FirstStatus(typ) {
		return fmt.Sprintf(" (%salso: %s; only a human can change it past %q)", prefix, strings.Join(others, ", "), wf.FirstStatus(typ))
	}
	return fmt.Sprintf(" (%salso: %s — issue-cli set-type %s <type> while at %s)", prefix, strings.Join(others, ", "), slug, status)
}
