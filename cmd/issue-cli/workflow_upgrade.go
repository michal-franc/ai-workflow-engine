package main

import (
	"os"
	"path/filepath"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

// projectWorkflowFile mirrors Project.LoadWorkflow's file choice: the
// project's configured file, then ./workflow.yaml. "" means the project runs
// on the built-in default workflow.
func projectWorkflowFile(p *tracker.Project) string {
	if p != nil && p.WorkflowFile != "" && isRegularFile(p.WorkflowFile) {
		return p.WorkflowFile
	}
	if isRegularFile("workflow.yaml") {
		return "workflow.yaml"
	}
	return ""
}

// staleRecordWarning is the warning for a features: record that misses
// catalog features the file has; "" when the record is fine or there is no
// file to read.
func staleRecordWarning(p *tracker.Project) string {
	path := projectWorkflowFile(p)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	plan, err := workflowupgrade.BuildPlan(path, raw, "", false)
	if err != nil {
		return ""
	}
	return plan.StaleRecord()
}

// workflowFileLabel names the workflow file p runs on as an absolute path,
// or "built-in default" when it has none, for the "Workflow file:" line in
// process and show output.
func workflowFileLabel(p *tracker.Project) string {
	path := projectWorkflowFile(p)
	if path == "" {
		return "built-in default"
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
