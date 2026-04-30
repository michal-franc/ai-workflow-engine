package main

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed templates/workflow/*.yaml
var workflowTemplatesFS embed.FS

const workflowTemplatesDir = "templates/workflow"

func availableWorkflowTemplates() []string {
	entries, err := fs.ReadDir(workflowTemplatesFS, workflowTemplatesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		if name == e.Name() {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func readWorkflowTemplate(name string) ([]byte, error) {
	return fs.ReadFile(workflowTemplatesFS, filepath.ToSlash(filepath.Join(workflowTemplatesDir, name+".yaml")))
}

// stdinIsTTY reports whether the supplied reader is a terminal. Used by the
// interactive workflow-init prompt to decide whether to read a selection.
func stdinIsTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// doWorkflowInit is the testable core. It takes explicit IO and a TTY hint so
// tests can drive interactive selection without a real terminal, and returns
// errors instead of calling os.Exit.
func doWorkflowInit(template string, force bool, in io.Reader, out io.Writer, isTTY bool) error {
	templates := availableWorkflowTemplates()
	if len(templates) == 0 {
		return fmt.Errorf("no workflow templates are bundled in this binary — this is a build issue, not a usage issue")
	}

	chosen, err := resolveTemplate(template, templates, in, out, isTTY)
	if err != nil {
		return err
	}

	target := "workflow.yaml"
	if _, statErr := os.Stat(target); statErr == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", target)
	}

	data, err := readWorkflowTemplate(chosen)
	if err != nil {
		return fmt.Errorf("reading bundled template %q: %w", chosen, err)
	}

	if err := os.WriteFile(target, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}

	scaffolded, err := scaffoldProjectDirs()
	if err != nil {
		return err
	}

	if len(scaffolded) > 0 {
		fmt.Fprintf(out, "✓ Wrote %s (template: %s) and scaffolded %s\n", target, chosen, strings.Join(scaffolded, ", "))
	} else {
		fmt.Fprintf(out, "✓ Wrote %s (template: %s)\n", target, chosen)
	}
	return nil
}

func resolveTemplate(flagValue string, templates []string, in io.Reader, out io.Writer, isTTY bool) (string, error) {
	if flagValue != "" {
		for _, t := range templates {
			if t == flagValue {
				return t, nil
			}
		}
		return "", fmt.Errorf("unknown template %q\n\nAvailable templates: %s", flagValue, strings.Join(templates, ", "))
	}

	if !isTTY {
		return "", fmt.Errorf("--template is required when stdin is not a terminal\n\nAvailable templates: %s\n\nExample:\n  issue-cli workflow init --template %s", strings.Join(templates, ", "), templates[0])
	}

	fmt.Fprintln(out, "Pick a workflow template:")
	for i, t := range templates {
		fmt.Fprintf(out, "  %d) %s\n", i+1, t)
	}
	fmt.Fprint(out, "Selection (number or name): ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading selection: %v", err)
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return "", fmt.Errorf("no selection provided")
	}

	if n, err := strconv.Atoi(choice); err == nil {
		if n < 1 || n > len(templates) {
			return "", fmt.Errorf("selection %d out of range (1..%d)", n, len(templates))
		}
		return templates[n-1], nil
	}

	for _, t := range templates {
		if t == choice {
			return t, nil
		}
	}
	return "", fmt.Errorf("unknown template %q\n\nAvailable templates: %s", choice, strings.Join(templates, ", "))
}

// scaffoldProjectDirs creates ./issues and ./docs if they do not already exist.
// Returns the list of directories that were freshly created, in display order.
func scaffoldProjectDirs() ([]string, error) {
	var created []string
	for _, dir := range []string{"issues", "docs"} {
		if _, err := os.Stat(dir); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("checking %s: %w", dir, err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
		created = append(created, dir+"/")
	}
	return created, nil
}
