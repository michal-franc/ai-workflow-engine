// Package workflowupgrade compares a project's workflow.yaml with the
// templates and feature catalog bundled in cmd/issue-cli/templates: it
// detects which catalog features a file has (Scan), writes its features:
// record (WriteRecord) and lists what to propose (BuildPlan). issue-cli's
// `workflow scan` / `workflow upgrade` and the viewer's Updates tab use it.
package workflowupgrade

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/cmd/issue-cli/templates"
)

const templatesDir = "workflow"

// Templates lists the bundled template names (development, review, …).
func Templates() []string {
	entries, err := fs.ReadDir(templates.FS, templatesDir)
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

// ReadTemplate returns a bundled template's workflow.yaml.
func ReadTemplate(name string) ([]byte, error) {
	return fs.ReadFile(templates.FS, path.Join(templatesDir, name+".yaml"))
}

// catalogYAML is the bundled feature catalog.
func catalogYAML() []byte {
	data, _ := fs.ReadFile(templates.FS, "workflow-features.yaml")
	return data
}
